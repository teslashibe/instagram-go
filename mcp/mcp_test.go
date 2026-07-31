package mcp_test

import (
	"context"
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
						"pk":"101","id":"101_9","code":"FIRST101","media_type":1,
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

	firstRaw, err := tool.Invoke(context.Background(), client, json.RawMessage(`{"query":"coffee","limit":12}`))
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	first, ok := firstRaw.(mcptool.Page[*instagram.Post])
	if !ok {
		t.Fatalf("first page type = %T", firstRaw)
	}
	if len(first.Items) != 1 || first.Items[0].PK != "101" || first.Items[0].ID != "101_9" || first.Items[0].Code != "FIRST101" {
		t.Fatalf("first page media identifiers = %#v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("first page has no next_cursor")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first page: %v", err)
	}
	if !strings.Contains(string(encoded), `"pk":"101"`) || !strings.Contains(string(encoded), `"next_cursor":`) {
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
