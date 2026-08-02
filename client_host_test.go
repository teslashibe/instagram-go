package instagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
		"www.instagram.test": `{"data":{"xdt_fbsearch__top_serp_graphql":{"edges":[]}},"errors":[]}`,
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
	c.validatedMu.Lock()
	validated := c.validated
	c.validatedMu.Unlock()
	if !validated {
		t.Fatal("GraphQL envelope with an empty errors array must mark the client validated")
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

func TestMobileRequestMapsExpiredSession2xxWithoutStatus(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"i.instagram.test": `{"message":"login_required"}`,
	}}
	c := newHostTestClient(t, transport)
	err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		&requestOptions{Host: requestHostAPI}, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
	c.validatedMu.Lock()
	validated := c.validated
	c.validatedMu.Unlock()
	if validated {
		t.Fatal("login-required envelope must not mark the client validated")
	}
}

func TestGraphQLAuthErrorDoesNotValidateClient(t *testing.T) {
	transport := &recordingTransport{responses: map[string]string{
		"www.instagram.test": `{"data":null,"errors":[{"message":"login_required"}]}`,
	}}
	c := newHostTestClient(t, transport)
	err := c.doJSON(context.Background(), http.MethodPost, "/graphql/query", nil, nil, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
	c.validatedMu.Lock()
	validated := c.validated
	c.validatedMu.Unlock()
	if validated {
		t.Fatal("GraphQL login-required envelope must not mark the client validated")
	}
}

func TestGraphQLErrorsAreClassifiedBeforeClientValidation(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		want         error
		wantDetail   string
		wantCooldown bool
	}{
		{name: "challenge required", message: "challenge_required", want: ErrChallengeRequired},
		{name: "checkpoint", message: "checkpoint_required", want: ErrChallengeRequired},
		{name: "rate limit", message: "Rate limit exceeded", want: ErrRateLimited, wantCooldown: true},
		{name: "feedback", message: "feedback_required", want: ErrRateLimited, wantCooldown: true},
		{name: "persisted query", message: "PersistedQueryNotFound", want: ErrUnexpectedResponse, wantDetail: "PersistedQueryNotFound"},
		{name: "schema", message: "Cannot query field x on type Query", want: ErrUnexpectedResponse, wantDetail: "Cannot query field x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"data":{"partial":true},"errors":[{"message":"` + tt.message + `"}]}`
			transport := &recordingTransport{responses: map[string]string{
				"www.instagram.test": body,
			}}
			c := newHostTestClient(t, transport)
			err := c.doJSON(context.Background(), http.MethodPost, "/graphql/query", nil, nil, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.want)
			}
			if tt.wantDetail != "" && !strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("error = %v, want detail %q", err, tt.wantDetail)
			}
			if got := c.RateLimit().CooldownReadUntil; tt.wantCooldown && !got.After(time.Now()) {
				t.Fatalf("read cooldown = %v, want future deadline", got)
			} else if !tt.wantCooldown && !got.IsZero() {
				t.Fatalf("read cooldown = %v, want zero", got)
			}
			c.validatedMu.Lock()
			validated := c.validated
			c.validatedMu.Unlock()
			if validated {
				t.Fatal("failed GraphQL envelope must not mark the client validated")
			}
		})
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
	// Never-validated 401 login cues map to ErrInvalidAuth; 2xx login-required
	// envelopes still use ErrSessionExpired (covered above).
	if !errors.Is(err, ErrInvalidAuth) {
		t.Fatalf("error = %v, want ErrInvalidAuth", err)
	}
}

func TestHTTPAuthStatusesPreserveKnownMessageClassification(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		want    error
	}{
		{name: "403 csrf", status: http.StatusForbidden, message: "CSRF token missing or incorrect", want: ErrCSRF},
		{name: "403 challenge", status: http.StatusForbidden, message: "challenge_required", want: ErrChallengeRequired},
		{name: "401 challenge", status: http.StatusUnauthorized, message: "checkpoint_required", want: ErrChallengeRequired},
		{name: "generic 403 remains auth", status: http.StatusForbidden, message: "request rejected", want: ErrInvalidAuth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.status,
					Status:     http.StatusText(tt.status),
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(
						`{"message":` + strconv.Quote(tt.message) + `,"status":"fail"}`,
					)),
					Request: req,
				}, nil
			})
			c := newHostTestClient(t, transport)
			err := c.doJSON(context.Background(), http.MethodPost, "/api/v1/direct_v2/create_group_thread/", nil,
				&requestOptions{Host: requestHostAPI, IsWrite: true}, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.want)
			}
		})
	}
}

func TestRequireLoginFlagPreservesValidatedSessionRateLimitHandling(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "false", body: `{"require_login":false}`},
		{name: "true", body: `{"require_login":true}`},
		{name: "true with generic failure status", body: `{"status":"fail","require_login":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requestCount++
				status := http.StatusOK
				body := `{"status":"ok"}`
				if requestCount == 2 {
					status = http.StatusUnauthorized
					body = tt.body
				}
				return &http.Response{
					StatusCode: status,
					Status:     http.StatusText(status),
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    req,
				}, nil
			})
			c := newHostTestClient(t, transport)
			opts := &requestOptions{Host: requestHostAPI}
			if err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
				opts, nil); err != nil {
				t.Fatalf("healthy request: %v", err)
			}
			err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
				opts, nil)
			if !errors.Is(err, ErrRateLimited) {
				t.Fatalf("error = %v, want ErrRateLimited", err)
			}
			if errors.Is(err, ErrSessionExpired) {
				t.Fatalf("error = %v, must not be ErrSessionExpired", err)
			}
		})
	}
}

func TestRequireLoginFlagOnSuccessfulResponseRateLimitsValidatedSession(t *testing.T) {
	requestCount := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		body := `{"status":"ok"}`
		if requestCount == 2 {
			body = `{"require_login":true}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	c := newHostTestClient(t, transport)
	opts := &requestOptions{Host: requestHostAPI}
	if err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		opts, nil); err != nil {
		t.Fatalf("healthy request: %v", err)
	}

	err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
		opts, nil)
	if !errors.Is(err, ErrRateLimited) || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want ErrRateLimited and not ErrSessionExpired", err)
	}
	if c.RateLimit().CooldownReadUntil.IsZero() {
		t.Fatal("validated require_login response did not trip read cooldown")
	}
}

func TestExpiredSessionCueUsesStructuredValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "message", body: `{"message":"login_required"}`, want: true},
		{name: "error message", body: `{"error_message":"session expired"}`, want: true},
		{name: "require login true", body: `{"require_login":true}`, want: true},
		{name: "require login false", body: `{"require_login":false}`, want: false},
		{name: "GraphQL login required", body: `{"errors":[{"message":"login_required"}]}`, want: true},
		{name: "GraphQL login required after unrelated error", body: `{"errors":[{"message":"PersistedQueryNotFound"},{"message":"session expired"}]}`, want: true},
		{name: "GraphQL persisted query error", body: `{"errors":[{"message":"PersistedQueryNotFound"}]}`, want: false},
		{name: "GraphQL schema error", body: `{"errors":[{"message":"Cannot query field x on type Query"}]}`, want: false},
		{name: "unrelated nested text", body: `{"detail":{"require_login":true}}`, want: false},
		{name: "malformed", body: `{"require_login":`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasExpiredSessionCue([]byte(tt.body)); got != tt.want {
				t.Errorf("hasExpiredSessionCue(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
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
