package meta

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationURLUsesReadScopesAndState(t *testing.T) {
	c := testClient(t, allReadScopes(), false, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected HTTP"); return nil, nil })
	raw, err := c.AuthorizationURL("csrf-state")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "facebook.test" || u.Query().Get("state") != "csrf-state" {
		t.Fatalf("authorization URL = %s", raw)
	}
	if strings.Contains(u.Query().Get("scope"), string(ScopeAdsManagement)) {
		t.Fatal("default scopes include ads_management")
	}
	if _, err := c.AuthorizationURL("csrf-state", ScopeAdsManagement); !errors.Is(err, ErrMutationNotAllowed) {
		t.Fatalf("management scope error = %v", err)
	}
}

func TestExchangeAndRefreshPersistTokens(t *testing.T) {
	requests := 0
	store := NewMemoryTokenStore(Token{AccessToken: "old", Scopes: allReadScopes()})
	c, err := New(Config{AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback", TokenStore: store, GrantedScopes: allReadScopes()},
		WithGraphBaseURL("https://graph.test"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Header.Get("Authorization") != "" {
				t.Error("OAuth exchange sent bearer header")
			}
			return jsonResponse(req, http.StatusOK, `{"access_token":"new-token","token_type":"bearer","expires_in":3600}`), nil
		})}), WithClock(func() time.Time { return time.Unix(1000, 0) }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExchangeCode(context.Background(), "code"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RefreshToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Load(context.Background())
	if stored.AccessToken != "new-token" || !stored.ExpiresAt.Equal(time.Unix(4600, 0)) || requests != 2 {
		t.Fatalf("stored token=%s expires=%v requests=%d", stored, stored.ExpiresAt, requests)
	}
}

func TestTokenErrorsAndRedaction(t *testing.T) {
	if strings.Contains(Token{AccessToken: "super-secret"}.String(), "super-secret") {
		t.Fatal("Token.String leaked access token")
	}
	c, err := New(Config{AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback", TokenStore: failingStore{}, GrantedScopes: allReadScopes()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ListPages(context.Background(), ListOptions{})
	if !errors.Is(err, ErrTokenMissing) {
		t.Fatalf("error=%v", err)
	}
}

func TestOAuthGraphErrorRedactsSecrets(t *testing.T) {
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusBadRequest, `{"error":{"message":"bad secret and auth-code","code":100,"error_user_msg":"auth-code failed"}}`), nil
	})
	_, err := c.ExchangeCode(context.Background(), "auth-code")
	if err == nil || strings.Contains(err.Error(), "auth-code") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("OAuth error leaked secret: %v", err)
	}
}
