package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	instagram "github.com/teslashibe/instagram-go"
	igmcp "github.com/teslashibe/instagram-go/mcp"
	"github.com/teslashibe/mcptool"
)

// TestEveryClientMethodIsWrappedOrExcluded fails when a new exported method
// is added to *instagram.Client without either being wrapped by an MCP tool
// or being added to igmcp.Excluded with a reason. This is the drift-
// prevention mechanism: keeping the MCP surface in lockstep with the package
// API is enforced by CI rather than convention.
func TestEveryClientMethodIsWrappedOrExcluded(t *testing.T) {
	rep := mcptool.Coverage(
		reflect.TypeOf(&instagram.Client{}),
		igmcp.Provider{}.Tools(),
		igmcp.Excluded,
	)
	if len(rep.Missing) > 0 {
		t.Fatalf("methods missing MCP exposure (add a tool or list in excluded.go): %v", rep.Missing)
	}
	if len(rep.UnknownExclusions) > 0 {
		t.Fatalf("excluded.go references methods that don't exist on *Client (rename?): %v", rep.UnknownExclusions)
	}
	if len(rep.Wrapped)+len(rep.Excluded) == 0 {
		t.Fatal("no wrapped or excluded methods detected — coverage helper is mis-configured")
	}
}

// TestToolsValidate verifies every tool has a non-empty name in canonical
// snake_case form, a description within length limits, and a non-nil Invoke
// + InputSchema.
func TestToolsValidate(t *testing.T) {
	if err := mcptool.ValidateTools(igmcp.Provider{}.Tools()); err != nil {
		t.Fatal(err)
	}
}

// TestPlatformName guards against accidental rebrands.
func TestPlatformName(t *testing.T) {
	if got := (igmcp.Provider{}).Platform(); got != "instagram" {
		t.Errorf("Platform() = %q, want instagram", got)
	}
}

// TestToolsHaveInstagramPrefix encodes the per-platform naming convention.
func TestToolsHaveInstagramPrefix(t *testing.T) {
	for _, tool := range (igmcp.Provider{}).Tools() {
		if !strings.HasPrefix(tool.Name, "instagram_") {
			t.Errorf("tool %q lacks instagram_ prefix", tool.Name)
		}
	}
}

func TestAccountAdministrationToolsAreNarrowAndConfirmed(t *testing.T) {
	readTools := []string{
		"instagram_get_current_account", "instagram_get_account_settings", "instagram_get_professional_account_state",
	}
	for _, name := range readTools {
		if tool := findTool(t, name); tool.WrapsMethod == "" {
			t.Fatalf("%s is not registered", name)
		}
	}
	for _, name := range []string{
		"instagram_update_profile_fields", "instagram_set_privacy", "instagram_update_professional_settings",
	} {
		tool := findTool(t, name)
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties = %#v", name, tool.InputSchema["properties"])
		}
		for _, field := range []string{"expected_account_id", "before", "after", "confirm"} {
			if _, ok := properties[field]; !ok {
				t.Errorf("%s schema lacks %q", name, field)
			}
		}
		raw, _ := json.Marshal(tool.InputSchema)
		for _, forbidden := range []string{"password", "email", "phone", "two_factor", "deactivation", "deletion", "ownership"} {
			if strings.Contains(strings.ToLower(string(raw)), forbidden) {
				t.Errorf("%s schema contains excluded capability %q: %s", name, forbidden, raw)
			}
		}
	}
}

func TestAccountAdministrationToolsReturnStructuredSafetyErrors(t *testing.T) {
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		return mcpJSONResponse(req, http.StatusOK, `{}`), nil
	})
	_, err := findTool(t, "instagram_set_privacy").Invoke(context.Background(), client, json.RawMessage(`{
		"expected_account_id":"viewer","before":true,"after":false,"confirm":false
	}`))
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "precondition_failed" || toolErr.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

func TestAccountAdministrationToolsRejectOmittedTransitionsWithoutHTTP(t *testing.T) {
	tests := []struct {
		name string
		tool string
		body string
		want string
	}{
		{
			name: "profile before", tool: "instagram_update_profile_fields", want: "before",
			body: `{"expected_account_id":"viewer","after":{"full_name":"Name","biography":"Bio","external_url":""},"confirm":true}`,
		},
		{
			name: "profile after", tool: "instagram_update_profile_fields", want: "after",
			body: `{"expected_account_id":"viewer","before":{"full_name":"Name","biography":"Bio","external_url":""},"confirm":true}`,
		},
		{
			name: "privacy before", tool: "instagram_set_privacy", want: "before",
			body: `{"expected_account_id":"viewer","after":false,"confirm":true}`,
		},
		{
			name: "privacy after", tool: "instagram_set_privacy", want: "after",
			body: `{"expected_account_id":"viewer","before":false,"confirm":true}`,
		},
		{
			name: "professional before", tool: "instagram_update_professional_settings", want: "before",
			body: `{"expected_account_id":"viewer","after":{"category_id":"1001","display_category":false},"confirm":true}`,
		},
		{
			name: "professional after", tool: "instagram_update_professional_settings", want: "after",
			body: `{"expected_account_id":"viewer","before":{"category_id":"1001","display_category":false},"confirm":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return mcpJSONResponse(req, http.StatusOK, `{}`), nil
			})
			_, err := findTool(t, tt.tool).Invoke(context.Background(), client, json.RawMessage(tt.body))
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != "precondition_failed" || toolErr.Retryable {
				t.Fatalf("error = %#v", err)
			}
			if got := toolErr.Data["field"]; got != tt.want {
				t.Fatalf("precondition field = %#v, want %q", got, tt.want)
			}
			if requests != 0 {
				t.Fatalf("omitted transition made %d HTTP requests", requests)
			}
		})
	}
}

func TestAccountAdministrationReadToolsReturnStructuredIdentityErrors(t *testing.T) {
	tests := []struct {
		name, body, code string
		status           int
	}{
		{name: "expired credentials", status: http.StatusUnauthorized, body: `{"message":"login_required","status":"fail"}`, code: "credential_expired"},
		{name: "challenge", status: http.StatusOK, body: `{"message":"challenge_required","status":"fail"}`, code: "challenge_required"},
		{name: "account mismatch", status: http.StatusOK, body: `{"user":{"pk":"other","username":"burner","full_name":"","biography":"","external_url":"","is_private":true,"is_professional_account":false,"is_business":false,"account_type":1,"category_id":"0","category_name":"","should_show_category":false},"status":"ok"}`, code: "account_mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				return mcpJSONResponse(req, tt.status, tt.body), nil
			})
			_, err := findTool(t, "instagram_get_current_account").Invoke(context.Background(), client, json.RawMessage(`{}`))
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != tt.code || toolErr.Retryable {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

type mcpRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn mcpRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newMCPClient(t *testing.T, fn mcpRoundTripFunc) *instagram.Client {
	t.Helper()
	client, err := instagram.New(
		instagram.Cookies{SessionID: "session", CSRFToken: "csrf", DSUserID: "viewer"},
		instagram.WithHTTPClient(&http.Client{Transport: fn}),
		instagram.WithSkipSessionValidation(),
		instagram.WithMinRequestGap(0),
		instagram.WithRetry(1, 0),
	)
	if err != nil {
		t.Fatalf("instagram.New: %v", err)
	}
	return client
}

func mcpJSONResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func findTool(t *testing.T, name string) mcptool.Tool {
	t.Helper()
	for _, tool := range (igmcp.Provider{}).Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q is not registered", name)
	return mcptool.Tool{}
}

func TestKeywordSearchToolsRegisteredWithTypedInputs(t *testing.T) {
	for _, name := range []string{"instagram_search_posts", "instagram_search_reels"} {
		tool := findTool(t, name)
		if len(tool.Description) > 120 {
			t.Errorf("%s description has %d chars", name, len(tool.Description))
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema properties = %#v", name, tool.InputSchema["properties"])
		}
		for _, field := range []string{"query", "limit", "cursor"} {
			if _, ok := properties[field]; !ok {
				t.Errorf("%s schema lacks %q: %#v", name, field, properties)
			}
		}
		required, _ := tool.InputSchema["required"].([]any)
		if !reflect.DeepEqual(required, []any{"query"}) {
			t.Errorf("%s required fields = %#v, want [query]", name, required)
		}
	}

	// Guard the pre-existing blended search registration while extending its group.
	blended := findTool(t, "instagram_search")
	if blended.WrapsMethod != "Search" ||
		blended.Description != "Run an Instagram top-search for users, hashtags, and places matching a query" {
		t.Errorf("existing instagram_search changed: %#v", blended)
	}
}

func TestSearchPostsToolReturnsMediaIDsAndResumesCursor(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/fbsearch/top_serp/" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		}
		query := req.URL.Query()
		if query.Get("query") != "coffee" {
			t.Errorf("query = %q, want coffee", query.Get("query"))
		}
		switch requests {
		case 1:
			if query.Get("next_max_id") != "" || query.Get("rank_token") == "" {
				t.Errorf("first-page cursor fields: %s", req.URL.RawQuery)
			}
			return mcpJSONResponse(req, http.StatusOK, `{
				"media_grid": {
					"sections": [{"layout_content":{"medias":[{"media":{
						"pk":3925989427651196285,"id":"3925989427651196285_9","code":"FIRST101","media_type":1,
						"user":{"pk":"9","username":"creator"}
					}}]}}],
					"has_more":true,
					"next_max_id":"NEXT_1",
					"rank_token":"RANK_1"
				},
				"status":"ok"
			}`), nil
		case 2:
			if query.Get("next_max_id") != "NEXT_1" || query.Get("rank_token") != "RANK_1" {
				t.Errorf("second-page cursor fields: %s", req.URL.RawQuery)
			}
			return mcpJSONResponse(req, http.StatusOK, `{
				"media_grid": {
					"sections": [{"layout_content":{"medias":[{"media":{
						"pk":"102","id":"102_9","code":"SECOND102","media_type":1,
						"user":{"pk":"9","username":"creator"}
					}}]}}],
					"has_more":false
				},
				"status":"ok"
			}`), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})
	tool := findTool(t, "instagram_search_posts")

	firstRaw, err := tool.Invoke(context.Background(), client, json.RawMessage(`{"query":" coffee ","limit":12}`))
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	first, ok := firstRaw.(mcptool.Page[*instagram.Post])
	if !ok {
		t.Fatalf("first page type = %T", firstRaw)
	}
	if len(first.Items) != 1 || first.Items[0].PK != "3925989427651196285" || first.Items[0].ID != "3925989427651196285_9" || first.Items[0].Code != "FIRST101" {
		t.Fatalf("first page media identifiers = %#v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("first page has no next_cursor")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first page: %v", err)
	}
	if !strings.Contains(string(encoded), `"pk":"3925989427651196285"`) || !strings.Contains(string(encoded), `"next_cursor":`) {
		t.Fatalf("first page JSON lacks media ID or cursor: %s", encoded)
	}

	secondInput, err := json.Marshal(map[string]any{
		"query": "coffee", "limit": 12, "cursor": first.NextCursor,
	})
	if err != nil {
		t.Fatalf("marshal second input: %v", err)
	}
	secondRaw, err := tool.Invoke(context.Background(), client, secondInput)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	second, ok := secondRaw.(mcptool.Page[*instagram.Post])
	if !ok {
		t.Fatalf("second page type = %T", secondRaw)
	}
	if len(second.Items) != 1 || second.Items[0].PK != "102" || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestSearchPostsToolRejectsCrossQueryCursorsWithoutHTTP(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		body  string
	}{
		{
			name:  "SDK continuation cursor",
			limit: 12,
			body: `{"media_grid":{"sections":[{"layout_content":{"medias":[{"media":{
				"pk":"1","id":"1_9","code":"POST1","media_type":1,"user":{"pk":"9","username":"creator"}
			}}]}}],"has_more":true,"next_max_id":"NEXT_1","rank_token":"RANK_1"},"status":"ok"}`,
		},
		{
			name:  "MCP partial-page cursor",
			limit: 1,
			body: `{"media_grid":{"sections":[{"layout_content":{"medias":[
				{"media":{"pk":"1","id":"1_9","code":"POST1","media_type":1,"user":{"pk":"9","username":"creator"}}},
				{"media":{"pk":"2","id":"2_9","code":"POST2","media_type":1,"user":{"pk":"9","username":"creator"}}}
			]}}],"has_more":true,"next_max_id":"NEXT_1","rank_token":"RANK_1"},"status":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return mcpJSONResponse(req, http.StatusOK, tt.body), nil
			})
			tool := findTool(t, "instagram_search_posts")

			firstInput, err := json.Marshal(map[string]any{"query": "coffee", "limit": tt.limit})
			if err != nil {
				t.Fatalf("marshal first input: %v", err)
			}
			firstRaw, err := tool.Invoke(context.Background(), client, firstInput)
			if err != nil {
				t.Fatalf("first page: %v", err)
			}
			first := firstRaw.(mcptool.Page[*instagram.Post])
			if first.NextCursor == "" {
				t.Fatal("first page has no next_cursor")
			}

			mismatchedInput, err := json.Marshal(map[string]any{
				"query": "tea", "limit": tt.limit, "cursor": first.NextCursor,
			})
			if err != nil {
				t.Fatalf("marshal mismatched input: %v", err)
			}
			_, err = tool.Invoke(context.Background(), client, mismatchedInput)
			if err == nil || !strings.Contains(err.Error(), "cursor query mismatch") {
				t.Fatalf("error = %v, want cursor query mismatch", err)
			}
			if requests != 1 {
				t.Fatalf("cross-query resume made %d requests, want 1 initial request only", requests)
			}
		})
	}
}

func TestSearchPostsToolLimitCursorDoesNotSkipPageRemainder(t *testing.T) {
	medias := make([]string, 24)
	for i := range medias {
		id := fmt.Sprintf("%d", i+1)
		medias[i] = fmt.Sprintf(`{"media":{"pk":"%s","id":"%s_9","code":"POST%s","media_type":1,"user":{"pk":"9","username":"creator"}}}`, id, id, id)
	}
	firstPageBody := fmt.Sprintf(`{
		"media_grid": {
			"sections": [{"layout_content":{"medias":[%s]}}],
			"has_more": true,
			"next_max_id": "NEXT_1",
			"rank_token": "RANK_1"
		},
		"status":"ok"
	}`, strings.Join(medias, ","))
	lastPageBody := `{
		"media_grid": {
			"sections": [{"layout_content":{"medias":[{"media":{
				"pk":"25","id":"25_9","code":"POST25","media_type":1,
				"user":{"pk":"9","username":"creator"}
			}}]}}],
			"has_more": false
		},
		"status":"ok"
	}`

	var requests int
	var firstRankToken string
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		query := req.URL.Query()
		rankToken := query.Get("rank_token")
		if rankToken == "" {
			t.Error("request has no rank token")
		}
		switch query.Get("next_max_id") {
		case "":
			if firstRankToken == "" {
				firstRankToken = rankToken
			} else if rankToken != firstRankToken {
				t.Errorf("replayed page rank token = %q, want %q", rankToken, firstRankToken)
			}
			return mcpJSONResponse(req, http.StatusOK, firstPageBody), nil
		case "NEXT_1":
			if rankToken != "RANK_1" {
				t.Errorf("next-page rank token = %q, want RANK_1", rankToken)
			}
			return mcpJSONResponse(req, http.StatusOK, lastPageBody), nil
		default:
			t.Fatalf("unexpected cursor query: %s", req.URL.RawQuery)
			return nil, nil
		}
	})
	tool := findTool(t, "instagram_search_posts")

	firstRaw, err := tool.Invoke(context.Background(), client, json.RawMessage(`{"query":"coffee","limit":12}`))
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	first := firstRaw.(mcptool.Page[*instagram.Post])
	if len(first.Items) != 12 || first.Items[0].PK != "1" || first.Items[11].PK != "12" {
		t.Fatalf("first page identifiers = %#v", first.Items)
	}
	if first.NextCursor == "" || !first.Truncated {
		t.Fatalf("first page continuation = %#v", first)
	}

	secondInput, err := json.Marshal(map[string]any{
		"query": "coffee", "limit": 12, "cursor": first.NextCursor,
	})
	if err != nil {
		t.Fatalf("marshal second input: %v", err)
	}
	secondRaw, err := tool.Invoke(context.Background(), client, secondInput)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	second := secondRaw.(mcptool.Page[*instagram.Post])
	if len(second.Items) != 12 || second.Items[0].PK != "13" || second.Items[11].PK != "24" || second.NextCursor == "" || second.Truncated {
		t.Fatalf("second page = %#v", second)
	}

	thirdInput, err := json.Marshal(map[string]any{
		"query": "coffee", "limit": 12, "cursor": second.NextCursor,
	})
	if err != nil {
		t.Fatalf("marshal third input: %v", err)
	}
	thirdRaw, err := tool.Invoke(context.Background(), client, thirdInput)
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	third := thirdRaw.(mcptool.Page[*instagram.Post])
	if len(third.Items) != 1 || third.Items[0].PK != "25" || third.NextCursor != "" || third.Truncated {
		t.Fatalf("third page = %#v", third)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestSearchReelsToolReturnsEmptyList(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != "/api/v1/fbsearch/reels_serp/" {
			t.Errorf("unexpected path: %s", req.URL.Path)
		}
		return mcpJSONResponse(req, http.StatusOK, `{"reels_serp_modules":[],"status":"ok"}`), nil
	})

	raw, err := findTool(t, "instagram_search_reels").Invoke(
		context.Background(), client, json.RawMessage(`{"query":"coffee","limit":5}`),
	)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	page, ok := raw.(mcptool.Page[*instagram.Post])
	if !ok {
		t.Fatalf("page type = %T", raw)
	}
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("empty page = %#v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	if !strings.Contains(string(encoded), `"items":[]`) {
		t.Fatalf("empty page JSON = %s, want items array", encoded)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestSearchReelsToolTruncatesFirstPageWithoutContinuation(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != "/api/v1/fbsearch/reels_serp/" {
			t.Errorf("unexpected path: %s", req.URL.Path)
		}
		if got := req.URL.Query().Get("query"); got != "coffee" {
			t.Errorf("query = %q, want normalized query coffee", got)
		}
		return mcpJSONResponse(req, http.StatusOK, `{
			"reels_serp_modules":[{"clips":[
				{"media":{"pk":"1","id":"1_9","code":"REEL1","media_type":2,"product_type":"clips","user":{"pk":"9","username":"creator"}}},
				{"media":{"pk":"2","id":"2_9","code":"REEL2","media_type":2,"product_type":"clips","user":{"pk":"9","username":"creator"}}},
				{"media":{"pk":"3","id":"3_9","code":"REEL3","media_type":2,"product_type":"clips","user":{"pk":"9","username":"creator"}}}
			]}],
			"has_more":true,
			"reels_max_id":"UNPROVEN_CONTINUATION",
			"rank_token":"UNBOUND_RANKING",
			"status":"ok"
		}`), nil
	})

	raw, err := findTool(t, "instagram_search_reels").Invoke(
		context.Background(), client, json.RawMessage(`{"query":" coffee ","limit":2}`),
	)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	page := raw.(mcptool.Page[*instagram.Post])
	if len(page.Items) != 2 || page.Items[0].PK != "1" || page.Items[1].PK != "2" {
		t.Fatalf("page items = %#v, want first two ranked reels", page.Items)
	}
	if !page.Truncated || page.NextCursor != "" {
		t.Fatalf("terminal truncated page = %#v, want truncated with no continuation", page)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one inventory-proven request", requests)
	}
}

func TestSearchReelsToolRejectsCursorsWithoutHTTP(t *testing.T) {
	legacyPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"offset":1}`))
	tests := []struct {
		name   string
		query  string
		cursor string
	}{
		{name: "same-query legacy replay", query: "coffee", cursor: "mcp-search-v1." + legacyPayload},
		{name: "cross-query legacy replay", query: "tea", cursor: "mcp-search-v1." + legacyPayload},
		{name: "malformed prefixed cursor", query: "coffee", cursor: "mcp-search-v1.not-base64!"},
		{name: "arbitrary non-prefixed cursor", query: "coffee", cursor: "arbitrary-cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return mcpJSONResponse(req, http.StatusOK, `{}`), nil
			})
			input, err := json.Marshal(map[string]any{
				"query": tt.query, "limit": 1, "cursor": tt.cursor,
			})
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}

			_, err = findTool(t, "instagram_search_reels").Invoke(context.Background(), client, input)
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" || !strings.Contains(toolErr.Message, "cursor") {
				t.Fatalf("error = %v, want structured invalid cursor error", err)
			}
			if requests != 0 {
				t.Fatalf("cursor validation made %d HTTP requests, want zero", requests)
			}
		})
	}
}

func TestSearchPostsToolRejectsInvalidCursorsWithoutHTTP(t *testing.T) {
	legacyPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"offset":1}`))
	emptyPageCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"offset":1,"page_cursor":""}`))
	whitespacePageCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"offset":1,"page_cursor":" \t\n"}`))
	tests := []struct {
		name   string
		query  string
		cursor string
	}{
		{name: "offset without page_cursor", query: "coffee", cursor: "mcp-search-v1." + legacyPayload},
		{name: "empty page_cursor", query: "coffee", cursor: "mcp-search-v1." + emptyPageCursor},
		{name: "whitespace page_cursor", query: "coffee", cursor: "mcp-search-v1." + whitespacePageCursor},
		{name: "malformed prefixed cursor", query: "coffee", cursor: "mcp-search-v1.not-base64!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return mcpJSONResponse(req, http.StatusOK, `{}`), nil
			})
			input, err := json.Marshal(map[string]any{
				"query": tt.query, "limit": 1, "cursor": tt.cursor,
			})
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}

			_, err = findTool(t, "instagram_search_posts").Invoke(context.Background(), client, input)
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" || !strings.Contains(toolErr.Message, "cursor") {
				t.Fatalf("error = %v, want structured invalid cursor error", err)
			}
			if requests != 0 {
				t.Fatalf("cursor validation made %d HTTP requests, want zero", requests)
			}
		})
	}
}

func TestKeywordSearchToolsRejectMissingQueryWithoutHTTP(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{}`), nil
	})

	for _, name := range []string{"instagram_search_posts", "instagram_search_reels"} {
		t.Run(name, func(t *testing.T) {
			_, err := findTool(t, name).Invoke(context.Background(), client, json.RawMessage(`{}`))
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" {
				t.Fatalf("error = %v, want structured invalid_input", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("missing queries made %d HTTP requests", requests)
	}
}

func TestSearchPostsToolReturnsStructuredExpiredSessionError(t *testing.T) {
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		response := mcpJSONResponse(req, http.StatusFound, `{"message":"login_required","status":"fail"}`)
		response.Header.Set("Location", "https://www.instagram.com/accounts/login/")
		return response, nil
	})

	_, err := findTool(t, "instagram_search_posts").Invoke(
		context.Background(), client, json.RawMessage(`{"query":"coffee"}`),
	)
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %v, want structured MCP error", err)
	}
	if toolErr.Code != "credential_expired" || toolErr.Retryable {
		t.Fatalf("structured error = %#v", toolErr)
	}
}
