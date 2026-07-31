package instagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const (
	testWWWHost = "https://www.instagram.test"
	testAPIHost = "https://i.instagram.test"
)

type recordedRequest struct {
	method string
	host   string
	path   string
	header http.Header
	body   string
}

type recordingTransport struct {
	mu        sync.Mutex
	requests  []recordedRequest
	responses map[string]string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var payload []byte
	if req.Body != nil {
		payload, _ = io.ReadAll(req.Body)
	}
	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{
		method: req.Method,
		host:   req.URL.Host,
		path:   req.URL.RequestURI(),
		header: req.Header.Clone(),
		body:   string(payload),
	})
	body := r.responses[req.URL.Host]
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (r *recordingTransport) onlyForHost(t *testing.T, host string) recordedRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var matches []recordedRequest
	for _, request := range r.requests {
		if request.host == host {
			matches = append(matches, request)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("recorded %d requests for %s, want 1", len(matches), host)
	}
	return matches[0]
}

func newHostTestClient(t *testing.T, transport http.RoundTripper, opts ...Option) *Client {
	t.Helper()
	options := []Option{
		WithWWWHost(testWWWHost + "/"),
		WithAPIHost(testAPIHost + "/"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithSkipSessionValidation(),
		WithMinRequestGap(0),
		WithRetry(1, 0),
	}
	options = append(options, opts...)
	c, err := New(Cookies{
		SessionID: "session", CSRFToken: "csrf", DSUserID: "123", Mid: "mid",
	}, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestClientRoutesWebAndMobileRequestsWithSurfaceHeaders(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"www.instagram.test": `{"users":[],"hashtags":[],"places":[],"status":"ok"}`,
		"i.instagram.test":   `{"media_grid":{"sections":[{"layout_content":{"fill_items":[{"media":{"id":"1","code":"ABC","media_type":1}}]}}]},"status":"ok"}`,
	}}
	c := newHostTestClient(t, transport)
	if _, err := c.Search(context.Background(), "coffee"); err != nil {
		t.Fatalf("Search: %v", err)
	}

	query := url.Values{
		"query":           {"coffee"},
		"search_surface":  {"top_serp"},
		"timezone_offset": {"0"},
		"rank_token":      {"rank"},
	}
	var serp struct {
		MediaGrid map[string]any `json:"media_grid"`
		Status    string         `json:"status"`
	}
	if err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", query,
		&requestOptions{Host: requestHostAPI}, &serp); err != nil {
		t.Fatalf("mobile SERP: %v", err)
	}
	if serp.Status != "ok" || serp.MediaGrid == nil {
		t.Fatalf("unexpected SERP envelope: %#v", serp)
	}

	webReq := transport.onlyForHost(t, "www.instagram.test")
	if !strings.HasPrefix(webReq.path, "/api/v1/web/search/topsearch/") {
		t.Errorf("web request path = %q", webReq.path)
	}
	assertHeader(t, webReq.header, "X-IG-App-ID", defaultAppID)
	assertHeader(t, webReq.header, "X-IG-WWW-Claim", "0")
	assertHeader(t, webReq.header, "Origin", testWWWHost)
	assertHeader(t, webReq.header, "Referer", testWWWHost+"/")
	if got := webReq.header.Get("X-IG-Capabilities"); got != "" {
		t.Errorf("web X-IG-Capabilities = %q, want empty", got)
	}

	apiReq := transport.onlyForHost(t, "i.instagram.test")
	if !strings.HasPrefix(apiReq.path, "/api/v1/fbsearch/top_serp/") {
		t.Errorf("API request path = %q", apiReq.path)
	}
	assertHeader(t, apiReq.header, "User-Agent", defaultAPIUserAgent)
	assertHeader(t, apiReq.header, "Accept-Language", "en-US")
	assertHeader(t, apiReq.header, "X-IG-App-ID", defaultAPIAppID)
	assertHeader(t, apiReq.header, "X-IG-Capabilities", "3brTv10=")
	assertHeader(t, apiReq.header, "X-IG-Connection-Type", "WIFI")
	assertHeader(t, apiReq.header, "X-CSRFToken", "csrf")
	if got := apiReq.header.Get("Cookie"); got != "sessionid=session; csrftoken=csrf; ds_user_id=123; mid=mid" {
		t.Errorf("API Cookie = %q", got)
	}
	for _, name := range []string{"Authorization", "Origin", "Referer", "X-IG-WWW-Claim", "X-Requested-With"} {
		if got := apiReq.header.Get(name); got != "" {
			t.Errorf("API %s = %q, want empty", name, got)
		}
	}
}

func TestGraphQLDefaultsToWWWHostAndAllowsTransportHeaders(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"www.instagram.test": `{"data":{"xdt_fbsearch__top_serp_graphql":{"edges":[]}}}`,
	}}
	c := newHostTestClient(t, transport)
	form := url.Values{"doc_id": {"123"}, "variables": {`{"query":"coffee"}`}}
	if err := c.doJSON(context.Background(), http.MethodPost, "/graphql/query", nil, &requestOptions{
		FormBody: form,
		ExtraHeaders: map[string]string{
			"X-FB-Friendly-Name": "PolarisKeywordSearchExplorePageRelayQuery",
		},
	}, nil); err != nil {
		t.Fatalf("GraphQL request: %v", err)
	}

	req := transport.onlyForHost(t, "www.instagram.test")
	if req.method != http.MethodPost || req.path != "/graphql/query" {
		t.Errorf("GraphQL request = %s %s", req.method, req.path)
	}
	assertHeader(t, req.header, "Content-Type", "application/x-www-form-urlencoded")
	assertHeader(t, req.header, "X-FB-Friendly-Name", "PolarisKeywordSearchExplorePageRelayQuery")
	if got := req.body; got != form.Encode() {
		t.Errorf("GraphQL body = %q, want %q", got, form.Encode())
	}
}

func TestMobileRequestMapsExpiredSessionEnvelope(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"i.instagram.test": `{"message":"login_required","status":"fail"}`,
	}}
	c := newHostTestClient(t, transport)
	err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		&requestOptions{Host: requestHostAPI}, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
}

func TestMobileRequestMapsExpiredSessionHTTPError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"message":"login_required"}`)),
			Request:    req,
		}, nil
	})
	c := newHostTestClient(t, transport)
	err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		&requestOptions{Host: requestHostAPI}, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
}

func TestAPIHeaderOptionsDoNotChangeWebProfile(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"www.instagram.test": `{"users":[],"hashtags":[],"places":[],"status":"ok"}`,
		"i.instagram.test":   `{"status":"ok"}`,
	}}
	c := newHostTestClient(t, transport,
		WithUserAgent("web-ua"),
		WithAppID("web-app"),
		WithAPIUserAgent("api-ua"),
		WithAPIAppID("api-app"),
	)
	if _, err := c.Search(context.Background(), "coffee"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		&requestOptions{Host: requestHostAPI}, nil); err != nil {
		t.Fatalf("mobile request: %v", err)
	}

	webReq := transport.onlyForHost(t, "www.instagram.test")
	apiReq := transport.onlyForHost(t, "i.instagram.test")
	assertHeader(t, webReq.header, "User-Agent", "web-ua")
	assertHeader(t, webReq.header, "X-IG-App-ID", "web-app")
	assertHeader(t, apiReq.header, "User-Agent", "api-ua")
	assertHeader(t, apiReq.header, "X-IG-App-ID", "api-app")
}

func assertHeader(t *testing.T, header http.Header, name, want string) {
	t.Helper()
	if got := header.Get(name); got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
