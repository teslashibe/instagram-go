package meta

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func testClient(t *testing.T, scopes []Scope, allowMutations bool, fn roundTripFunc) *Client {
	t.Helper()
	client, err := New(Config{
		AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback",
		TokenStore:    NewMemoryTokenStore(Token{AccessToken: "token-secret", ExpiresAt: time.Now().Add(time.Hour), Scopes: scopes}),
		GrantedScopes: scopes, EnableAdMutations: allowMutations,
	}, WithGraphBaseURL("https://graph.test"), WithOAuthBaseURL("https://facebook.test"),
		WithHTTPClient(&http.Client{Transport: fn}), WithClock(func() time.Time { return time.Unix(1000, 0) }))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func jsonResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func allReadScopes() []Scope { return append([]Scope(nil), DefaultReadScopes...) }

type failingStore struct{}

func (failingStore) Load(context.Context) (Token, error) { return Token{}, ErrTokenMissing }
func (failingStore) Save(context.Context, Token) error   { return ErrTokenMissing }
