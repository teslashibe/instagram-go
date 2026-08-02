package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teslashibe/instagram-go/meta"
	metamcp "github.com/teslashibe/instagram-go/meta/mcp"
	"github.com/teslashibe/mcptool"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func newClient(t *testing.T, scopes []meta.Scope, expiresAt time.Time, fn roundTripFunc) *meta.Client {
	t.Helper()
	client, err := meta.New(meta.Config{
		AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback", GrantedScopes: scopes,
		TokenStore: meta.NewMemoryTokenStore(meta.Token{AccessToken: "token", ExpiresAt: expiresAt, Scopes: scopes}),
	}, meta.WithGraphBaseURL("https://graph.test"), meta.WithClock(func() time.Time { return time.Unix(1000, 0) }),
		meta.WithHTTPClient(&http.Client{Transport: fn}))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func findTool(t *testing.T, name string) mcptool.Tool {
	t.Helper()
	for _, tool := range (metamcp.Provider{}).Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return mcptool.Tool{}
}

func TestProviderValidationIsolationAndCoverage(t *testing.T) {
	provider := metamcp.Provider{}
	if provider.Platform() != "instagram_meta" {
		t.Fatalf("platform=%s", provider.Platform())
	}
	if err := mcptool.ValidateTools(provider.Tools()); err != nil {
		t.Fatal(err)
	}
	for _, tool := range provider.Tools() {
		if !strings.HasPrefix(tool.Name, "instagram_meta_") {
			t.Errorf("tool prefix=%s", tool.Name)
		}
	}
	report := mcptool.Coverage(reflect.TypeOf(&meta.Client{}), provider.Tools(), metamcp.Excluded)
	if len(report.Missing) > 0 || len(report.UnknownExclusions) > 0 {
		t.Fatalf("coverage missing=%v unknown=%v", report.Missing, report.UnknownExclusions)
	}
}

func TestToolsExposeTypedInputsAndStructuredErrors(t *testing.T) {
	tool := findTool(t, "instagram_meta_get_account_insights")
	properties, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties=%#v", tool.InputSchema["properties"])
	}
	for _, field := range []string{"account_id", "metrics", "period", "since", "until", "limit", "cursor"} {
		if _, exists := properties[field]; !exists {
			t.Errorf("schema lacks %s", field)
		}
	}

	expired := newClient(t, meta.DefaultReadScopes, time.Unix(999, 0), func(*http.Request) (*http.Response, error) {
		t.Fatal("expired token reached HTTP")
		return nil, nil
	})
	listPages := findTool(t, "instagram_meta_list_pages")
	_, err := listPages.Invoke(context.Background(), expired, json.RawMessage(`{}`))
	structured, ok := err.(*mcptool.Error)
	if !ok || structured.Code != "credential_expired" {
		t.Fatalf("error=%T %#v", err, err)
	}

	limitedScopes := []meta.Scope{meta.ScopePagesShowList}
	limited := newClient(t, limitedScopes, time.Unix(2000, 0), func(*http.Request) (*http.Response, error) {
		t.Fatal("missing scope reached HTTP")
		return nil, nil
	})
	listCampaigns := findTool(t, "instagram_meta_list_campaigns")
	_, err = listCampaigns.Invoke(context.Background(), limited, json.RawMessage(`{"ad_account_id":"123"}`))
	structured, ok = err.(*mcptool.Error)
	if !ok || structured.Code != "missing_scope" || structured.Data["required_scopes"] == nil {
		t.Fatalf("error=%T %#v", err, err)
	}
}

func TestListCampaignsToolReturnsOpaquePagination(t *testing.T) {
	client := newClient(t, meta.DefaultReadScopes, time.Unix(2000, 0), func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "OK", Header: http.Header{}, Request: req,
			Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"c1","name":"Launch"}],"paging":{"cursors":{"after":"raw-secret"},"next":"https://next"}}`))}, nil
	})
	raw, err := findTool(t, "instagram_meta_list_campaigns").Invoke(context.Background(), client, json.RawMessage(`{"ad_account_id":"123","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	page, ok := raw.(meta.Page[meta.Campaign])
	if !ok || len(page.Items) != 1 || page.NextCursor == "" || strings.Contains(page.NextCursor, "raw-secret") {
		t.Fatalf("page=%#v", raw)
	}
}
