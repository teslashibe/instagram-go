package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scope is an official Meta OAuth permission.
type Scope string

const (
	ScopePublicProfile           Scope = "public_profile"
	ScopePagesShowList           Scope = "pages_show_list"
	ScopePagesReadEngagement     Scope = "pages_read_engagement"
	ScopeInstagramBasic          Scope = "instagram_basic"
	ScopeInstagramManageInsights Scope = "instagram_manage_insights"
	ScopeAdsRead                 Scope = "ads_read"
	ScopeBusinessManagement      Scope = "business_management"
	// ScopeAdsManagement is intentionally excluded from DefaultReadScopes and
	// must be separately granted and enabled for mutations.
	ScopeAdsManagement Scope = "ads_management"
)

// DefaultReadScopes are sufficient for Page linkage, professional Instagram
// insights, and read-only Marketing API access in the common configuration.
var DefaultReadScopes = []Scope{
	ScopePublicProfile,
	ScopePagesShowList,
	ScopePagesReadEngagement,
	ScopeInstagramBasic,
	ScopeInstagramManageInsights,
	ScopeAdsRead,
	ScopeBusinessManagement,
}

// AuthorizationURL creates the Facebook Login URL for this client. State must
// be generated and verified by the caller.
func (c *Client) AuthorizationURL(state string, scopes ...Scope) (string, error) {
	if strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("%w: OAuth state is required", ErrInvalidInput)
	}
	if len(scopes) == 0 {
		scopes = DefaultReadScopes
	}
	if err := validateScopes(scopes, c.allowMutations); err != nil {
		return "", err
	}
	q := url.Values{
		"client_id":     {c.appID},
		"redirect_uri":  {c.redirectURI},
		"response_type": {"code"},
		"state":         {state},
		"scope":         {joinScopes(scopes)},
	}
	return c.oauthBaseURL + "/dialog/oauth?" + q.Encode(), nil
}

// ExchangeCode exchanges an authorization code, then stores the token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (Token, error) {
	if strings.TrimSpace(code) == "" {
		return Token{}, fmt.Errorf("%w: authorization code is required", ErrInvalidInput)
	}
	q := url.Values{
		"client_id":     {c.appID},
		"client_secret": {c.appSecret},
		"redirect_uri":  {c.redirectURI},
		"code":          {code},
	}
	token, err := c.exchangeToken(ctx, q)
	if err != nil {
		return Token{}, err
	}
	if len(token.Scopes) == 0 {
		token.Scopes = append([]Scope(nil), c.grantedScopes...)
	}
	if err := c.tokenStore.Save(ctx, token); err != nil {
		return Token{}, fmt.Errorf("meta: store OAuth token: %w", err)
	}
	return token, nil
}

// RefreshToken exchanges the stored token for a new long-lived token. Meta
// may require the user to authorize again; those failures wrap
// ErrReauthorizationRequired.
func (c *Client) RefreshToken(ctx context.Context) (Token, error) {
	current, err := c.tokenStore.Load(ctx)
	if err != nil || current.AccessToken == "" {
		return Token{}, &TokenError{Kind: "missing", Message: "connect a Meta account"}
	}
	q := url.Values{
		"grant_type":        {"fb_exchange_token"},
		"client_id":         {c.appID},
		"client_secret":     {c.appSecret},
		"fb_exchange_token": {current.AccessToken},
	}
	token, err := c.exchangeToken(ctx, q)
	if err != nil {
		return Token{}, fmt.Errorf("%w: %v", ErrReauthorizationRequired, err)
	}
	if len(token.Scopes) == 0 {
		token.Scopes = append([]Scope(nil), current.Scopes...)
	}
	if err := c.tokenStore.Save(ctx, token); err != nil {
		return Token{}, fmt.Errorf("meta: store refreshed token: %w", err)
	}
	return token, nil
}

func (c *Client) exchangeToken(ctx context.Context, q url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.graphBaseURL+"/oauth/access_token?"+q.Encode(), nil)
	if err != nil {
		return Token{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// net/http errors can include the URL, whose query contains the app
		// secret or previous token. Never propagate that URL.
		return Token{}, fmt.Errorf("meta: OAuth exchange request failed")
	}
	defer resp.Body.Close()
	var raw struct {
		AccessToken string          `json:"access_token"`
		TokenType   string          `json:"token_type"`
		ExpiresIn   int64           `json:"expires_in"`
		Error       json.RawMessage `json:"error"`
	}
	if err := decodeLimited(resp.Body, &raw); err != nil {
		return Token{}, err
	}
	if len(raw.Error) > 0 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		graphErr := graphErrorFromRaw(resp.StatusCode, raw.Error)
		redactGraphError(graphErr, c.appSecret, q.Get("code"), q.Get("fb_exchange_token"))
		return Token{}, graphErr
	}
	if raw.AccessToken == "" {
		return Token{}, fmt.Errorf("%w: token exchange returned no access token", ErrUnexpectedResponse)
	}
	token := Token{AccessToken: raw.AccessToken, TokenType: raw.TokenType}
	if raw.ExpiresIn > 0 {
		token.ExpiresAt = c.now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return token, nil
}

func validateScopes(scopes []Scope, allowManagement bool) error {
	seen := make(map[Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(string(scope)) == "" {
			return fmt.Errorf("%w: empty OAuth scope", ErrInvalidInput)
		}
		if scope == ScopeAdsManagement && !allowManagement {
			return fmt.Errorf("%w: ads_management requires explicit mutation enablement", ErrMutationNotAllowed)
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func joinScopes(scopes []Scope) string {
	parts := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		parts = append(parts, string(scope))
	}
	return strings.Join(parts, ",")
}
