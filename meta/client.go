package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultGraphBaseURL = "https://graph.facebook.com"
	DefaultOAuthBaseURL = "https://www.facebook.com"
	// DefaultAPIVersion is isolated here so callers can override it as Meta
	// advances versions without changing endpoint code.
	DefaultAPIVersion  = "v23.0"
	defaultTimeout     = 30 * time.Second
	defaultMaxAttempts = 3
	defaultRetryBase   = 250 * time.Millisecond
	maxResponseBytes   = 16 * 1024 * 1024
)

// Config contains only official Meta OAuth settings. It never accepts or
// stores private Instagram cookies.
type Config struct {
	AppID       string
	AppSecret   string
	RedirectURI string
	TokenStore  TokenStore
	// GrantedScopes is the scope set to request when AuthorizationURL is called
	// without an override. Actual grants are always read from /me/permissions
	// and stored on Token.Scopes.
	GrantedScopes      []Scope
	PageID             string
	InstagramAccountID string
	AdAccountID        string
	EnableAdMutations  bool
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithGraphBaseURL(base string) Option {
	return func(c *Client) {
		if normalized := normalizeOrigin(base); normalized != "" {
			c.graphBaseURL = normalized
		}
	}
}

func WithOAuthBaseURL(base string) Option {
	return func(c *Client) {
		if normalized := normalizeOrigin(base); normalized != "" {
			c.oauthBaseURL = normalized
		}
	}
}

func WithAPIVersion(version string) Option {
	return func(c *Client) {
		version = strings.Trim(strings.TrimSpace(version), "/")
		if strings.HasPrefix(version, "v") && !strings.ContainsAny(version, "?#") {
			c.apiVersion = version
		}
	}
}

// WithRetry configures retries for idempotent Graph reads. Mutations are
// never automatically retried. Values below one disable retries.
func WithRetry(maxAttempts int, base time.Duration) Option {
	return func(c *Client) {
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		c.maxAttempts = maxAttempts
		if base >= 0 {
			c.retryBase = base
		}
	}
}

// WithClock is intended for deterministic token-expiry tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// Client is an official Meta Graph/Marketing API client. It is safe for
// concurrent use and has no dependency on the root cookie-auth Client.
type Client struct {
	appID           string
	appSecret       string
	redirectURI     string
	tokenStore      TokenStore
	requestedScopes []Scope
	allowMutations  bool
	httpClient      *http.Client
	graphBaseURL    string
	oauthBaseURL    string
	apiVersion      string
	now             func() time.Time
	maxAttempts     int
	retryBase       time.Duration

	selectionMu sync.RWMutex
	selection   AccountSelection
}

func New(config Config, options ...Option) (*Client, error) {
	if strings.TrimSpace(config.AppID) == "" {
		return nil, fmt.Errorf("%w: AppID is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(config.AppSecret) == "" {
		return nil, fmt.Errorf("%w: AppSecret is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(config.RedirectURI) == "" {
		return nil, fmt.Errorf("%w: RedirectURI is required", ErrInvalidConfig)
	}
	if config.TokenStore == nil {
		return nil, fmt.Errorf("%w: TokenStore is required", ErrInvalidConfig)
	}
	configuredScopes := append([]Scope(nil), config.GrantedScopes...)
	if len(configuredScopes) == 0 {
		configuredScopes = append([]Scope(nil), DefaultReadScopes...)
	}
	if err := validateScopes(configuredScopes, config.EnableAdMutations); err != nil {
		return nil, err
	}
	if config.EnableAdMutations && !containsScope(configuredScopes, ScopeAdsManagement) {
		return nil, &ScopeError{Required: []Scope{ScopeAdsManagement}, Granted: configuredScopes}
	}
	c := &Client{
		appID:           config.AppID,
		appSecret:       config.AppSecret,
		redirectURI:     config.RedirectURI,
		tokenStore:      config.TokenStore,
		requestedScopes: configuredScopes,
		allowMutations:  config.EnableAdMutations,
		httpClient:      &http.Client{Timeout: defaultTimeout},
		graphBaseURL:    DefaultGraphBaseURL,
		oauthBaseURL:    DefaultOAuthBaseURL,
		apiVersion:      DefaultAPIVersion,
		now:             time.Now,
		maxAttempts:     defaultMaxAttempts,
		retryBase:       defaultRetryBase,
		selection: AccountSelection{
			PageID: config.PageID, InstagramAccountID: config.InstagramAccountID,
			AdAccountID: normalizeAdAccountID(config.AdAccountID),
		},
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body url.Values, required []Scope, out any) error {
	token, err := c.currentToken(ctx)
	if err != nil {
		return err
	}
	if err := requireGrantedScopes(token.Scopes, required...); err != nil {
		return err
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = c.maxAttempts
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		err = c.doJSONOnce(ctx, method, path, query, body, token, out)
		if err == nil || !retryableGraphError(err) || attempt == attempts {
			return err
		}
		delay := c.retryBase * time.Duration(1<<(attempt-1))
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		}
	}
	return err
}

func (c *Client) doJSONOnce(ctx context.Context, method, path string, query url.Values, body url.Values, token Token, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(body.Encode())
	}
	endpoint := c.graphBaseURL + "/" + c.apiVersion + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("meta: Graph request: %w", err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := decodeLimited(resp.Body, &raw); err != nil {
		return err
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if len(envelope.Error) > 0 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		graphErr := graphErrorFromRaw(resp.StatusCode, envelope.Error)
		redactGraphError(graphErr, token.AccessToken)
		if graphErr.Code == 190 {
			return fmt.Errorf("%w: %v", ErrReauthorizationRequired, graphErr)
		}
		if graphErr.Code == 10 || graphErr.Code == 200 {
			return fmt.Errorf("%w: %v", ErrPermission, graphErr)
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %v", ErrNotFound, graphErr)
		}
		return graphErr
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: decode Graph response: %v", ErrUnexpectedResponse, err)
		}
	}
	return nil
}

func (c *Client) currentToken(ctx context.Context) (Token, error) {
	token, err := c.tokenStore.Load(ctx)
	if err != nil || token.AccessToken == "" {
		return Token{}, &TokenError{Kind: "missing", Message: "connect a Meta account"}
	}
	if !token.Valid(c.now()) {
		return Token{}, &TokenError{Kind: "expired", Message: "Meta access token expired; reconnect or refresh it"}
	}
	return token, nil
}

func (c *Client) requireScopes(ctx context.Context, required ...Scope) error {
	token, err := c.currentToken(ctx)
	if err != nil {
		return err
	}
	return requireGrantedScopes(token.Scopes, required...)
}

func requireGrantedScopes(granted []Scope, required ...Scope) error {
	var missing []Scope
	for _, scope := range required {
		if !containsScope(granted, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return &ScopeError{Required: missing, Granted: append([]Scope(nil), granted...)}
	}
	return nil
}

func retryableGraphError(err error) bool {
	var graphErr *GraphError
	return errors.As(err, &graphErr) && (graphErr.IsTransient || graphErr.StatusCode == http.StatusTooManyRequests || graphErr.StatusCode >= 500)
}

func containsScope(scopes []Scope, expected Scope) bool {
	for _, scope := range scopes {
		if scope == expected {
			return true
		}
	}
	return false
}

func normalizeOrigin(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return origin
}

func decodeLimited(reader io.Reader, out any) error {
	dec := json.NewDecoder(io.LimitReader(reader, maxResponseBytes))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrUnexpectedResponse, err)
	}
	return nil
}

func graphErrorFromRaw(status int, raw json.RawMessage) *GraphError {
	err := &GraphError{StatusCode: status, Message: http.StatusText(status)}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, err)
	}
	if err.Message == "" {
		err.Message = "Graph API request failed"
	}
	return err
}

func redactGraphError(graphErr *GraphError, secrets ...string) {
	if graphErr == nil {
		return
	}
	redact := func(value string) string {
		for _, secret := range secrets {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
			}
		}
		return value
	}
	graphErr.Message = redact(graphErr.Message)
	graphErr.ErrorUserTitle = redact(graphErr.ErrorUserTitle)
	graphErr.ErrorUserMessage = redact(graphErr.ErrorUserMessage)
}
