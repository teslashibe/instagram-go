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

	mutationScopes := append(allReadScopes(), ScopeAdsManagement)
	mutationClient := testClient(t, mutationScopes, true, func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected HTTP")
		return nil, nil
	})
	mutationURL, err := mutationClient.AuthorizationURL("mutation-state")
	if err != nil {
		t.Fatal(err)
	}
	parsedMutationURL, err := url.Parse(mutationURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsedMutationURL.Query().Get("scope"), string(ScopeAdsManagement)) {
		t.Fatal("configured ads_management scope was omitted from authorization URL")
	}
}

func TestExchangeAndRefreshPersistTokens(t *testing.T) {
	requests := 0
	store := NewMemoryTokenStore(Token{AccessToken: "old", Scopes: allReadScopes()})
	c, err := New(Config{AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback", TokenStore: store, GrantedScopes: allReadScopes()},
		WithGraphBaseURL("https://graph.test"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.URL.Path == "/oauth/access_token" {
				if req.Header.Get("Authorization") != "" {
					t.Error("OAuth exchange sent bearer header")
				}
				return jsonResponse(req, http.StatusOK, `{"access_token":"new-token","token_type":"bearer","expires_in":3600}`), nil
			}
			if req.URL.Path != "/v23.0/me/permissions" || req.Header.Get("Authorization") != "Bearer new-token" {
				t.Fatalf("grant verification request = %s authorization=%q", req.URL, req.Header.Get("Authorization"))
			}
			return jsonResponse(req, http.StatusOK, `{"data":[
				{"permission":"pages_show_list","status":"granted"},
				{"permission":"ads_read","status":"granted"},
				{"permission":"ads_management","status":"declined"}
			]}`), nil
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
	if stored.AccessToken != "new-token" || !stored.ExpiresAt.Equal(time.Unix(4600, 0)) || requests != 4 {
		t.Fatalf("stored token=%s expires=%v requests=%d", stored, stored.ExpiresAt, requests)
	}
	if len(stored.Scopes) != 2 || !containsScope(stored.Scopes, ScopePagesShowList) ||
		!containsScope(stored.Scopes, ScopeAdsRead) || containsScope(stored.Scopes, ScopeAdsManagement) {
		t.Fatalf("stored unverified scopes: %v", stored.Scopes)
	}
}

func TestExchangePersistsOnlyVerifiedMutationGrant(t *testing.T) {
	requested := append(allReadScopes(), ScopeAdsManagement)
	store := NewMemoryTokenStore(Token{})
	c, err := New(Config{
		AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback",
		TokenStore: store, GrantedScopes: requested, EnableAdMutations: true,
	}, WithGraphBaseURL("https://graph.test"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/oauth/access_token":
			return jsonResponse(req, http.StatusOK, `{"access_token":"actual-token","expires_in":3600}`), nil
		case "/v23.0/me/permissions":
			return jsonResponse(req, http.StatusOK, `{"data":[
				{"permission":"ads_read","status":"granted"},
				{"permission":"ads_management","status":"declined"}
			]}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL)
			return nil, nil
		}
	})}))
	if err != nil {
		t.Fatal(err)
	}
	token, err := c.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if containsScope(token.Scopes, ScopeAdsManagement) || !containsScope(token.Scopes, ScopeAdsRead) {
		t.Fatalf("verified scopes = %v", token.Scopes)
	}
	stored, err := store.Load(context.Background())
	if err != nil || containsScope(stored.Scopes, ScopeAdsManagement) {
		t.Fatalf("stored scopes = %v, err=%v", stored.Scopes, err)
	}
	if err := c.requireScopes(context.Background(), ScopeAdsManagement); err == nil {
		t.Fatal("declined ads_management was treated as granted")
	}
}

func TestExchangeDoesNotStoreTokenWhenGrantVerificationIsMalformed(t *testing.T) {
	store := NewMemoryTokenStore(Token{AccessToken: "old-token", Scopes: []Scope{ScopeAdsRead}})
	c, err := New(Config{
		AppID: "app", AppSecret: "secret", RedirectURI: "https://example.test/callback",
		TokenStore: store, GrantedScopes: allReadScopes(),
	}, WithGraphBaseURL("https://graph.test"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth/access_token" {
			return jsonResponse(req, http.StatusOK, `{"access_token":"unverified-token","expires_in":3600}`), nil
		}
		return jsonResponse(req, http.StatusOK, `{}`), nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExchangeCode(context.Background(), "code"); !errors.Is(err, ErrUnexpectedResponse) {
		t.Fatalf("grant verification error=%v", err)
	}
	stored, err := store.Load(context.Background())
	if err != nil || stored.AccessToken != "old-token" {
		t.Fatalf("store changed after failed verification: token=%s err=%v", stored, err)
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
