package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCaptureRequiresAllTabsAndMedia(t *testing.T) {
	t.Parallel()
	secret := "burner-session-secret"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.Header.Get("Cookie"), "sessionid="+secret) {
			t.Fatalf("request did not authenticate")
		}
		var body string
		switch req.URL.Path {
		case "/api/v1/accounts/current_user/":
			body = `{"user":{"pk":"1"},"status":"ok"}`
		case "/api/v1/fbsearch/top_serp/":
			body = `{"media_grid":{"sections":[{"layout_content":{"fill_items":[{"media":{"pk":"123","id":"123_9","code":"ABC123","media_type":1,"product_type":"feed","taken_at":1700000000,"caption":{"text":"public caption"},"user":{"pk":"9","username":"creator"}}}]}}],"has_more":true,"next_max_id":"cursor-secret","rank_token":"rank-secret"},"status":"ok"}`
		case "/api/v1/fbsearch/reels_serp/":
			body = `{"media_grid":{"sections":[{"layout_content":{"medias":[{"media":{"pk":"124","id":"124_9","code":"REEL124","media_type":2,"product_type":"clips","taken_at":1700000001,"user":{"pk":"9","username":"creator"}}}]}}],"has_more":false,"reels_max_id":"reel-cursor"},"status":"ok"}`
		case "/api/v1/fbsearch/account_serp/":
			body = `{"users":[{"pk":"9","username":"creator"}],"has_more":false,"next_page_token":null,"status":"ok"}`
		case "/api/v1/fbsearch/typeahead_stream/":
			body = `{"stream_rows":[{"users":[{"pk":"9","username":"creator"}]}],"status":"ok"}`
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, body), nil
	})}

	p := probe{
		baseURL:    "https://i.instagram.test",
		query:      "coffee roaster",
		cookies:    cookieSet{"sessionid": secret, "csrftoken": "csrf-secret"},
		httpClient: client,
		userAgent:  "test",
		appID:      "app",
		now:        func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) },
	}
	report, err := p.capture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.REST) != 4 || report.MediaCount != 2 {
		t.Fatalf("got %d surfaces and %d media", len(report.REST), report.MediaCount)
	}
	rendered, err := renderReport(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, "csrf-secret", "cursor-secret", "rank-secret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("report leaked %q", forbidden)
		}
	}
	for _, expected := range []string{"Top", "Reels", "Accounts", "Keyword typeahead", "`Post.PK`", "$.media_grid.next_max_id"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("report missing %q", expected)
		}
	}
}

func TestCaptureFailsClosedWhenEitherMediaTabHasNoMedia(t *testing.T) {
	t.Parallel()
	for _, emptyTab := range []string{"Top", "Reels"} {
		emptyTab := emptyTab
		t.Run(emptyTab, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/api/v1/accounts/current_user/":
					return jsonResponse(http.StatusOK, `{"user":{"pk":"1"},"status":"ok"}`), nil
				case "/api/v1/fbsearch/top_serp/":
					if emptyTab == "Top" {
						return jsonResponse(http.StatusOK, `{"media_grid":{"sections":[]},"status":"ok"}`), nil
					}
					return jsonResponse(http.StatusOK, `{"media":{"pk":"2","code":"TOP","media_type":1},"status":"ok"}`), nil
				case "/api/v1/fbsearch/reels_serp/":
					if emptyTab == "Reels" {
						return jsonResponse(http.StatusOK, `{"media_grid":{"sections":[]},"status":"ok"}`), nil
					}
					return jsonResponse(http.StatusOK, `{"media":{"pk":"3","code":"REEL","media_type":2},"status":"ok"}`), nil
				default:
					return jsonResponse(http.StatusOK, `{"status":"ok","users":[]}`), nil
				}
			})}
			p := probe{baseURL: "https://i.instagram.test", query: "nothing", cookies: cookieSet{"sessionid": "secret"}, httpClient: client}
			_, err := p.capture(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), emptyTab+" returned no media/post nodes") {
				t.Fatalf("expected %s no-media failure, got %v", emptyTab, err)
			}
		})
	}
}

func TestCaptureReturnsClearAuthError(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnauthorized, `{"message":"login_required","status":"fail"}`), nil
	})}
	p := probe{baseURL: "https://i.instagram.test", query: "coffee", cookies: cookieSet{"sessionid": "invalid"}, httpClient: client}
	_, err := p.capture(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "authentication rejected or challenged") {
		t.Fatalf("expected auth failure, got %v", err)
	}
}

func TestLoadCookiesAndMissingCredentials(t *testing.T) {
	t.Parallel()
	env := map[string]string{"INSTAGRAM_COOKIES_JSON": `[{"name":"sessionid","value":"s"},{"name":"csrftoken","value":"c"}]`}
	cookies, err := loadCookies(context.Background(), func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cookies["sessionid"] != "s" || cookies["csrftoken"] != "c" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	_, err = loadCookies(context.Background(), func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "provide INSTAGRAM_SESSIONID") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}

func TestInspectHARCapturesSearchDocIDWithoutHeadersOrValues(t *testing.T) {
	t.Parallel()
	har := map[string]any{"log": map[string]any{"entries": []any{map[string]any{
		"request": map[string]any{
			"method": "POST", "url": "https://www.instagram.com/graphql/query",
			"headers": []any{
				map[string]any{"name": "x-fb-friendly-name", "value": "PolarisKeywordSearchQuery"},
				map[string]any{"name": "Cookie", "value": "sessionid=never-retain-this"},
			},
			"postData": map[string]any{"params": []any{
				map[string]any{"name": "doc_id", "value": "987654321"},
				map[string]any{"name": "variables", "value": `{"query":"coffee","after":"cursor-secret"}`},
			}},
		},
		"response": map[string]any{"status": 200, "content": map[string]any{"text": `{"data":{"search":{"edges":[{"node":{"pk":"222","code":"XYZ","media_type":2,"product_type":"clips"}}],"page_info":{"has_next_page":true,"end_cursor":"response-cursor"}}}}`}},
	}}}}
	raw, err := json.Marshal(har)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "search.har")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	surfaces, err := inspectHAR(path, "coffee")
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 1 {
		t.Fatalf("got %d GraphQL surfaces", len(surfaces))
	}
	s := surfaces[0]
	if s.DocID != "987654321" || s.FriendlyName != "PolarisKeywordSearchQuery" {
		t.Fatalf("unexpected GraphQL identity: %#v", s)
	}
	joined := strings.Join(s.RequiredParams, " ")
	if !strings.Contains(joined, "variables.after") || strings.Contains(joined, "cursor-secret") {
		t.Fatalf("variables were not name-only: %s", joined)
	}
	reportText, err := renderReport(report{CapturedAt: time.Now(), Host: "https://i.instagram.com", Query: "coffee", REST: []surface{}, GraphQL: surfaces, MediaCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"never-retain-this", "cursor-secret", "response-cursor"} {
		if strings.Contains(reportText, forbidden) {
			t.Fatalf("report leaked %q", forbidden)
		}
	}
}

func TestInspectHARRejectsUnusableGraphQLCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   int
		response string
		want     string
	}{
		{
			name:     "non-success status",
			status:   http.StatusInternalServerError,
			response: `{"errors":[{"message":"upstream failure"}]}`,
			want:     "HTTP 500",
		},
		{
			name:     "error-only response",
			status:   http.StatusOK,
			response: `{"errors":[{"message":"persisted query not found"}]}`,
			want:     "no usable data",
		},
		{
			name:     "data without media",
			status:   http.StatusOK,
			response: `{"data":{"search":{"users":[{"id":"1","username":"account"}]}}}`,
			want:     "no media/post node",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			har := map[string]any{"log": map[string]any{"entries": []any{map[string]any{
				"request": map[string]any{
					"method": "POST",
					"url":    "https://www.instagram.com/graphql/query?doc_id=987654321&fb_api_req_friendly_name=PolarisKeywordSearchQuery",
				},
				"response": map[string]any{"status": tt.status, "content": map[string]any{"text": tt.response}},
			}}}}
			raw, err := json.Marshal(har)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "search.har")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			surfaces, err := inspectHAR(path, "coffee")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q rejection, got surfaces=%#v err=%v", tt.want, surfaces, err)
			}
		})
	}
}

func TestInspectHARDoesNotLetFailedDuplicateHideSuccessfulCapture(t *testing.T) {
	t.Parallel()
	request := map[string]any{
		"method": "POST",
		"url":    "https://www.instagram.com/graphql/query?doc_id=987654321&fb_api_req_friendly_name=PolarisKeywordSearchQuery",
	}
	har := map[string]any{"log": map[string]any{"entries": []any{
		map[string]any{
			"request":  request,
			"response": map[string]any{"status": 500, "content": map[string]any{"text": `{"errors":[{"message":"temporary"}]}`}},
		},
		map[string]any{
			"request":  request,
			"response": map[string]any{"status": 200, "content": map[string]any{"text": `{"data":{"search":{"edges":[{"node":{"pk":"222","code":"XYZ","media_type":2}}]}}}`}},
		},
	}}}
	raw, err := json.Marshal(har)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "search.har")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	surfaces, err := inspectHAR(path, "coffee")
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 1 || surfaces[0].StatusCode != http.StatusOK {
		t.Fatalf("expected one successful surface, got %#v", surfaces)
	}
}

func TestCaptureRejectsHARWithoutSearchDocID(t *testing.T) {
	t.Parallel()
	harPath := filepath.Join(t.TempDir(), "empty.har")
	if err := os.WriteFile(harPath, []byte(`{"log":{"entries":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/accounts/current_user/" {
			return jsonResponse(http.StatusOK, `{"user":{"pk":"1"},"status":"ok"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"media":{"pk":"2","code":"ABC","media_type":1},"status":"ok"}`), nil
	})}
	p := probe{baseURL: "https://i.instagram.test", query: "coffee", cookies: cookieSet{"sessionid": "s"}, httpClient: client}
	_, err := p.capture(context.Background(), harPath)
	if err == nil || !strings.Contains(err.Error(), "no search-related call with a doc_id") {
		t.Fatalf("expected partial HAR rejection, got %v", err)
	}
}

func TestWriteAtomicRefusesOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "inventory.md")
	r := report{CapturedAt: time.Now(), Host: "https://i.instagram.test", Query: "q", MediaCount: 1}
	if err := writeAtomic(path, r); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, r); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
