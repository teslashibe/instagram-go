// Package instagram provides a Go client for Instagram's private web/mobile API.
//
// It supports authenticated profile lookup, post and reel feeds, comments,
// followers/following, stories, hashtags, locations, and search — giving
// programmatic access to Instagram's content graph from a logged-in browser
// session.
//
// Zero production dependencies — stdlib only.
//
// # Authentication
//
// Required cookies (obtained from a browser export of an authenticated session):
//
//   - sessionid    primary session credential
//   - csrftoken    CSRF token (also sent as X-CSRFToken header)
//   - ds_user_id   numeric user ID of the logged-in account
//   - datr         device auth token
//   - mid          machine ID
//   - ig_did       device ID (recommended)
//
// # User-Agent
//
// Instagram's API rejects desktop browser user-agents with
// {"message": "useragent mismatch"}. The default UA is the Instagram Android
// app's UA string. Override via WithUserAgent only if you have a known-good
// alternative.
//
// # Rate limiting
//
// Instagram does not return X-RateLimit headers. The client paces requests
// with a leaky-bucket minimum gap (default 1.5s) and exponential backoff on
// HTTP 429 / "Please wait a few minutes" responses.
//
// Write actions (Follow, Unfollow, Like, Comment, etc.) are subject to a
// stricter, separate rate limiter than reads. The client enforces a longer
// minimum gap (default 6s) between writes and applies aggressive backoff on
// any 302-to-login response, which Instagram uses to indicate a write soft
// block.
package instagram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	baseURL          = "https://www.instagram.com"
	defaultAppID     = "936619743392459"
	defaultUserAgent = "Mozilla/5.0 (Linux; Android 9; GM1903 Build/PKQ1.190110.001; wv) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/75.0.3770.143 Mobile Safari/537.36 " +
		"Instagram 103.1.0.15.119 Android (28/9; 420dpi; 1080x2260; OnePlus; GM1903; OnePlus7; qcom; sv_SE; 164094539)"
	defaultMinGap      = 1500 * time.Millisecond
	defaultMinWriteGap = 6 * time.Second
	defaultMaxRetries  = 3
	defaultRetryBase   = 750 * time.Millisecond
	defaultTimeout     = 30 * time.Second
)

// Cookies holds the Instagram session cookies obtained from a browser export.
// SessionID, CSRFToken, and DSUserID are required; the rest help mimic a real
// browser session and reduce the chance of the request being blocked.
type Cookies struct {
	SessionID string `json:"sessionid"`
	CSRFToken string `json:"csrftoken"`
	DSUserID  string `json:"ds_user_id"`
	Datr      string `json:"datr"`
	Mid       string `json:"mid"`
	IgDid     string `json:"ig_did"`
	Rur       string `json:"rur"`
	IgNrcb    string `json:"ig_nrcb"`
	PsL       string `json:"ps_l"`
	PsN       string `json:"ps_n"`
	Wd        string `json:"wd"`
}

// Client is an Instagram API client. It is safe for concurrent use.
type Client struct {
	cookies        Cookies
	httpClient     *http.Client
	userAgent      string
	appID          string
	maxRetries     int
	retryBase      time.Duration
	minGap         time.Duration
	writeGap       time.Duration
	skipValidation bool

	gapMu       sync.Mutex
	lastReqAt   time.Time
	writeMu     sync.Mutex
	lastWriteAt time.Time

	rateMu    sync.Mutex
	rateState RateLimitState

	viewer *User
}

// RateLimitState is a best-effort snapshot of the most recent rate-limit
// signal observed from Instagram. Instagram does not publish rate-limit
// headers, so RetryAfter is only set when the server returned 429 or a
// "wait a few minutes" body.
type RateLimitState struct {
	LastBlockedAt time.Time
	RetryAfter    time.Duration
	WriteBlocked  bool
}

// Option configures a Client.
type Option func(*Client)

// WithUserAgent overrides the default Instagram mobile User-Agent string.
// Most desktop browser UAs are rejected with "useragent mismatch".
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithAppID overrides the X-IG-App-ID header. The default (936619743392459)
// is Instagram Web's registered app ID and works for all documented endpoints.
func WithAppID(id string) Option {
	return func(c *Client) {
		if id != "" {
			c.appID = id
		}
	}
}

// WithRetry configures retry behaviour. Set maxAttempts to 0 to disable retries.
// Default: 3 attempts, 750ms exponential base.
func WithRetry(maxAttempts int, base time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = maxAttempts
		c.retryBase = base
	}
}

// WithHTTPClient replaces the default http.Client. Nil is ignored.
//
// IMPORTANT: When supplying a custom client, leave the cookie jar nil.
// Instagram's 302-to-login responses include Set-Cookie: sessionid=""
// directives that will wipe the session if you use a CookieJar. All cookies
// are sent via the explicit Cookie header instead.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithProxy routes all HTTP traffic through the given proxy URL.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsed)
		c.httpClient = &http.Client{
			Timeout:       c.httpClient.Timeout,
			Transport:     transport,
			CheckRedirect: noFollowRedirect,
		}
	}
}

// WithMinRequestGap sets the minimum time between consecutive read requests.
// Default: 1.5s. Lower values risk triggering Instagram's behavioural limiter.
func WithMinRequestGap(d time.Duration) Option {
	return func(c *Client) { c.minGap = d }
}

// WithMinWriteGap sets the minimum time between consecutive write requests
// (Follow, Like, Comment, Save, etc.). Default: 6s. Writes share a separate
// rate-limit budget from reads on Instagram's backend.
func WithMinWriteGap(d time.Duration) Option {
	return func(c *Client) { c.writeGap = d }
}

// WithSkipSessionValidation disables the initial session check inside New.
// Useful for offline tests or when the caller wants to defer validation.
func WithSkipSessionValidation() Option {
	return func(c *Client) { c.skipValidation = true }
}

// New creates a Client and validates the session by fetching the current user.
// Returns ErrInvalidAuth if SessionID, CSRFToken, or DSUserID are empty.
func New(cookies Cookies, opts ...Option) (*Client, error) {
	if cookies.SessionID == "" {
		return nil, fmt.Errorf("%w: SessionID must not be empty", ErrInvalidAuth)
	}
	if cookies.CSRFToken == "" {
		return nil, fmt.Errorf("%w: CSRFToken must not be empty", ErrInvalidAuth)
	}
	if cookies.DSUserID == "" {
		return nil, fmt.Errorf("%w: DSUserID must not be empty", ErrInvalidAuth)
	}

	c := &Client{
		cookies: cookies,
		httpClient: &http.Client{
			Timeout:       defaultTimeout,
			CheckRedirect: noFollowRedirect,
		},
		userAgent:  defaultUserAgent,
		appID:      defaultAppID,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
		minGap:     defaultMinGap,
		writeGap:   defaultMinWriteGap,
	}

	for _, o := range opts {
		o(c)
	}

	if c.httpClient.CheckRedirect == nil {
		c.httpClient.CheckRedirect = noFollowRedirect
	}

	if !c.skipValidation {
		if err := c.validateSession(context.Background()); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// validateSession fetches the authenticated user via /api/v1/accounts/current_user/.
// On success the result is cached on the client and returned by Me.
func (c *Client) validateSession(ctx context.Context) error {
	u, err := c.currentUser(ctx)
	if err != nil {
		return err
	}
	c.viewer = u
	return nil
}

// Me returns the authenticated user's profile. The result is cached at
// New() time; subsequent calls return the cached value.
func (c *Client) Me(ctx context.Context) (*User, error) {
	if c.viewer != nil {
		return c.viewer, nil
	}
	u, err := c.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	c.viewer = u
	return u, nil
}

// RateLimit returns the most recent rate-limit observation.
func (c *Client) RateLimit() RateLimitState {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	return c.rateState
}

// noFollowRedirect makes the HTTP client surface 3xx responses as-is.
// Instagram uses 302 to /accounts/login/ to indicate auth/rate failures —
// following them silently would obscure the real error.
func noFollowRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
