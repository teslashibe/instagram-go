package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// requestOptions tweaks a single request.
type requestOptions struct {
	// IsWrite marks the call as a write action — uses writeGap and is more
	// strictly classified as soft-blocked when the server redirects.
	IsWrite bool
	// XReferer overrides the Referer header (some endpoints want a tag/profile URL).
	Referer string
	// ExtraHeaders are merged on top of the defaults.
	ExtraHeaders map[string]string
	// FormBody, if non-nil, is sent as application/x-www-form-urlencoded.
	// It also forces method to POST when method is "" or "GET".
	FormBody url.Values
	// JSONBody, if non-nil, is sent as application/json.
	JSONBody any
}

// doJSON makes a request and decodes a JSON body into out. Pass nil out to
// discard the body.
func (c *Client) doJSON(ctx context.Context, method, path string, q url.Values, opts *requestOptions, out any) error {
	body, _, err := c.doRaw(ctx, method, path, q, opts)
	if err != nil {
		return err
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decoding %s: %v", ErrUnexpectedResponse, path, err)
	}
	return nil
}

// doRaw performs the HTTP call with retries, gap pacing, and rate-limit
// detection. It returns the response body and the *http.Response.
//
// Callers must not modify the returned response (the body is already drained).
func (c *Client) doRaw(ctx context.Context, method, path string, q url.Values, opts *requestOptions) ([]byte, *http.Response, error) {
	if opts == nil {
		opts = &requestOptions{}
	}
	if method == "" {
		method = http.MethodGet
	}
	if opts.FormBody != nil && (method == http.MethodGet) {
		method = http.MethodPost
	}

	// Build URL
	u := baseURL + path
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + q.Encode()
	}

	maxAttempts := c.maxRetries
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Honor any active cooldown before pacing — this is the circuit-breaker.
		// Returns immediately when no cooldown is in effect.
		if err := c.waitForCooldownInternal(ctx, opts.IsWrite); err != nil {
			return nil, nil, err
		}
		c.waitForGap(ctx, opts.IsWrite)

		req, err := c.buildRequest(ctx, method, u, opts)
		if err != nil {
			return nil, nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("instagram: http %s %s: %w", method, u, err)
			if !shouldRetryNetErr(err) || attempt == maxAttempts {
				return nil, nil, lastErr
			}
			c.sleepBackoff(ctx, attempt)
			continue
		}

		// Capture rate signals from headers regardless of status.
		c.recordSignals(resp, opts.IsWrite)
		body, classifyErr := c.classifyResponse(resp, opts.IsWrite, method, u)
		_ = resp.Body.Close()

		if classifyErr == nil {
			return body, resp, nil
		}

		lastErr = classifyErr
		if !shouldRetryStatus(resp.StatusCode, classifyErr) || attempt == maxAttempts {
			return body, resp, classifyErr
		}
		c.sleepBackoff(ctx, attempt)
	}
	return nil, nil, lastErr
}

func (c *Client) buildRequest(ctx context.Context, method, fullURL string, opts *requestOptions) (*http.Request, error) {
	var bodyReader io.Reader
	contentType := ""

	switch {
	case opts.JSONBody != nil:
		buf, err := json.Marshal(opts.JSONBody)
		if err != nil {
			return nil, fmt.Errorf("instagram: marshal json body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
		contentType = "application/json"
	case opts.FormBody != nil:
		bodyReader = strings.NewReader(opts.FormBody.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-IG-App-ID", c.appID)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-ASBD-ID", "129477")
	req.Header.Set("X-IG-WWW-Claim", "0")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	} else {
		req.Header.Set("Referer", baseURL+"/")
	}
	req.Header.Set("Origin", baseURL)
	req.Header.Set("X-CSRFToken", c.cookies.CSRFToken)
	if opts.IsWrite {
		req.Header.Set("X-Instagram-AJAX", "1")
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for k, v := range opts.ExtraHeaders {
		req.Header.Set(k, v)
	}

	req.Header.Set("Cookie", c.buildCookieHeader())

	return req, nil
}

// buildCookieHeader serialises Cookies into a single Cookie request header.
// Empty fields are omitted.
func (c *Client) buildCookieHeader() string {
	pairs := []string{}
	add := func(k, v string) {
		if v == "" {
			return
		}
		pairs = append(pairs, k+"="+v)
	}
	add("sessionid", c.cookies.SessionID)
	add("csrftoken", c.cookies.CSRFToken)
	add("ds_user_id", c.cookies.DSUserID)
	add("datr", c.cookies.Datr)
	add("mid", c.cookies.Mid)
	add("ig_did", c.cookies.IgDid)
	add("rur", c.cookies.Rur)
	add("ig_nrcb", c.cookies.IgNrcb)
	add("ps_l", c.cookies.PsL)
	add("ps_n", c.cookies.PsN)
	add("wd", c.cookies.Wd)
	return strings.Join(pairs, "; ")
}

// classifyResponse reads the body and maps non-2xx responses to typed errors.
func (c *Client) classifyResponse(resp *http.Response, isWrite bool, method, fullURL string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("instagram: read body: %w", err)
	}

	// Detect Instagram's "wipe sessionid" trick — a 302 with Set-Cookie wiping
	// sessionid means the request was rate-blocked or session-rejected.
	//
	// We distinguish by whether the session has already been successfully
	// validated. If yes, the session is healthy and this is a rate-limit
	// soft-block; trip the cooldown circuit-breaker. If no, the session
	// itself is dead.
	loc := resp.Header.Get("Location")
	wiped := isLoginRedirect(resp.StatusCode, loc)
	if wiped {
		c.validatedMu.Lock()
		validated := c.validated
		c.validatedMu.Unlock()

		if !validated {
			c.rateMu.Lock()
			c.rateState.LastBlockedAt = time.Now()
			c.rateState.BlockedReason = "302→login (pre-validation)"
			c.rateMu.Unlock()
			return body, fmt.Errorf("%w: redirect to %s", ErrSessionExpired, loc)
		}
		// Healthy session, so this is a behavioural rate-limit. Trip cooldown.
		cd := c.cooldownFor(isWrite)
		c.tripCooldown(isWrite, cd, "302→login")
		if isWrite {
			return body, fmt.Errorf("%w: cooldown for %s (redirect to %s)", ErrWriteSoftBlock, cd, loc)
		}
		return body, fmt.Errorf("%w: cooldown for %s (redirect to %s)", ErrRateLimited, cd, loc)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Instagram sometimes returns 200 with {"message":"...","status":"fail"}.
		if shaped := decodeStatusFail(body); shaped != nil {
			return body, c.mapMessage(shaped, resp.StatusCode, method, fullURL, body, isWrite)
		}
		// Any healthy response proves the session works. Mark validated so
		// that future 302→login responses are classified as rate limits
		// rather than session expiry.
		c.validatedMu.Lock()
		c.validated = true
		c.validatedMu.Unlock()
		return body, nil
	}

	if shaped := decodeStatusFail(body); shaped != nil {
		return body, c.mapMessage(shaped, resp.StatusCode, method, fullURL, body, isWrite)
	}

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Method:     method,
		URL:        fullURL,
		Body:       string(body),
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		// 401 from a previously-validated session is almost always rate-limit
		// behaviour ("require_login":true with no body cue), so trip cooldown.
		c.validatedMu.Lock()
		validated := c.validated
		c.validatedMu.Unlock()
		if validated {
			cd := c.cooldownFor(isWrite)
			c.tripCooldown(isWrite, cd, fmt.Sprintf("HTTP %d (validated session)", resp.StatusCode))
			return body, fmt.Errorf("%w: cooldown for %s (%s)", ErrRateLimited, cd, apiErr.Error())
		}
		return body, fmt.Errorf("%w: %s", ErrInvalidAuth, apiErr.Error())
	case http.StatusNotFound:
		return body, fmt.Errorf("%w: %s", ErrNotFound, apiErr.Error())
	case http.StatusTooManyRequests:
		retry := parseRetryAfter(resp)
		if retry <= 0 {
			retry = c.cooldownFor(isWrite)
		}
		c.tripCooldown(isWrite, retry, "HTTP 429")
		return body, fmt.Errorf("%w: cooldown for %s (%s)", ErrRateLimited, retry, apiErr.Error())
	}

	return body, apiErr
}

func (c *Client) mapMessage(shaped *statusFail, status int, method, fullURL string, body []byte, isWrite bool) error {
	msg := strings.ToLower(shaped.Message)
	apiErr := &APIError{
		StatusCode: status,
		Status:     http.StatusText(status),
		Method:     method,
		URL:        fullURL,
		Body:       string(body),
	}
	switch {
	case strings.Contains(msg, "checkpoint") || strings.Contains(msg, "challenge_required"):
		return fmt.Errorf("%w: %s", ErrChallengeRequired, apiErr.Error())
	case strings.Contains(msg, "csrf"):
		return fmt.Errorf("%w: %s", ErrCSRF, apiErr.Error())
	case strings.Contains(msg, "useragent"):
		return fmt.Errorf("%w: %s", ErrInvalidAuth, apiErr.Error())
	case strings.Contains(msg, "wait a few minutes") || strings.Contains(msg, "try again later"):
		cd := c.cooldownFor(isWrite)
		c.tripCooldown(isWrite, cd, "wait-a-few-minutes")
		return fmt.Errorf("%w: cooldown for %s (%s)", ErrRateLimited, cd, apiErr.Error())
	case strings.Contains(msg, "media not found") || strings.Contains(msg, "media_not_found"):
		return fmt.Errorf("%w: %s", ErrMediaUnavailable, apiErr.Error())
	case strings.Contains(msg, "user not found") || strings.Contains(msg, "user_not_found"):
		return fmt.Errorf("%w: %s", ErrNotFound, apiErr.Error())
	case strings.Contains(msg, "private"):
		return fmt.Errorf("%w: %s", ErrPrivateAccount, apiErr.Error())
	case strings.Contains(msg, "feedback_required"):
		cd := c.cooldownFor(isWrite)
		if cd < 5*time.Minute {
			cd = 5 * time.Minute
		}
		c.tripCooldown(isWrite, cd, "feedback_required")
		if isWrite {
			return fmt.Errorf("%w: cooldown for %s (%s)", ErrWriteSoftBlock, cd, apiErr.Error())
		}
		return fmt.Errorf("%w: cooldown for %s (%s)", ErrRateLimited, cd, apiErr.Error())
	}
	return apiErr
}

// statusFail is the standard Instagram failure envelope.
type statusFail struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	ErrorMsg string `json:"error_message"`
}

func decodeStatusFail(body []byte) *statusFail {
	if len(body) == 0 || body[0] != '{' {
		return nil
	}
	var sf statusFail
	if err := json.Unmarshal(body, &sf); err != nil {
		return nil
	}
	if sf.Status == "ok" || sf.Status == "" {
		return nil
	}
	if sf.Message == "" {
		sf.Message = sf.ErrorMsg
	}
	return &sf
}

// isLoginRedirect returns true if the response is a 3xx pointing at /accounts/login/.
func isLoginRedirect(status int, loc string) bool {
	if status < 300 || status >= 400 {
		return false
	}
	if loc == "" {
		return false
	}
	low := strings.ToLower(loc)
	return strings.Contains(low, "/accounts/login")
}

func parseRetryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := time.ParseDuration(v + "s"); err == nil {
		return secs
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func shouldRetryNetErr(err error) bool {
	if err == nil {
		return false
	}
	// Most net errors are transient.
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func shouldRetryStatus(status int, err error) bool {
	if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrWriteSoftBlock) ||
		errors.Is(err, ErrChallengeRequired) || errors.Is(err, ErrInvalidAuth) ||
		errors.Is(err, ErrNotFound) || errors.Is(err, ErrPrivateAccount) ||
		errors.Is(err, ErrMediaUnavailable) || errors.Is(err, ErrCSRF) {
		return false
	}
	// Once a cooldown is tripped, immediate retries are pointless and would
	// just hammer the same endpoint until cooldown clears. Surface the error.
	if errors.Is(err, ErrRateLimited) {
		return false
	}
	if status >= 500 && status < 600 {
		return true
	}
	return false
}

func (c *Client) sleepBackoff(ctx context.Context, attempt int) {
	base := c.retryBase
	if base <= 0 {
		base = defaultRetryBase
	}
	d := time.Duration(math.Pow(2, float64(attempt-1))) * base
	jitter := time.Duration(rand.Int63n(int64(base / 2)))
	d += jitter
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// waitForGap blocks until enough time has passed since the last request of
// the same kind.
func (c *Client) waitForGap(ctx context.Context, isWrite bool) {
	if isWrite {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if c.writeGap > 0 && !c.lastWriteAt.IsZero() {
			elapsed := time.Since(c.lastWriteAt)
			if elapsed < c.writeGap {
				select {
				case <-time.After(c.writeGap - elapsed):
				case <-ctx.Done():
					return
				}
			}
		}
		c.lastWriteAt = time.Now()
		return
	}
	c.gapMu.Lock()
	defer c.gapMu.Unlock()
	if c.minGap > 0 && !c.lastReqAt.IsZero() {
		elapsed := time.Since(c.lastReqAt)
		if elapsed < c.minGap {
			select {
			case <-time.After(c.minGap - elapsed):
			case <-ctx.Done():
				return
			}
		}
	}
	c.lastReqAt = time.Now()
}

// cooldownFor returns the configured cooldown duration for the request kind.
func (c *Client) cooldownFor(isWrite bool) time.Duration {
	if isWrite {
		if c.writeCooldown > 0 {
			return c.writeCooldown
		}
		return defaultWriteCooldown
	}
	if c.readCooldown > 0 {
		return c.readCooldown
	}
	return defaultReadCooldown
}

// tripCooldown sets the cooldown deadline for the request kind. Subsequent
// requests of the same kind will block until the deadline (see waitForCooldownInternal).
func (c *Client) tripCooldown(isWrite bool, d time.Duration, reason string) {
	if d <= 0 {
		return
	}
	until := time.Now().Add(d)
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	c.rateState.LastBlockedAt = time.Now()
	c.rateState.BlockedReason = reason
	if isWrite {
		c.cooldownWrt = until
		c.rateState.CooldownWriteUntil = until
		c.rateState.WriteBlocked = true
	} else {
		c.cooldownRead = until
		c.rateState.CooldownReadUntil = until
	}
}

// waitForCooldownInternal blocks the next request if the matching cooldown
// window is still active. Returns ctx.Err() on cancellation.
func (c *Client) waitForCooldownInternal(ctx context.Context, isWrite bool) error {
	for {
		c.rateMu.Lock()
		var until time.Time
		if isWrite {
			until = c.cooldownWrt
		} else {
			until = c.cooldownRead
		}
		c.rateMu.Unlock()
		if until.IsZero() {
			return nil
		}
		wait := time.Until(until)
		if wait <= 0 {
			return nil
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// recordSignals captures Instagram's soft rate-signal headers from any response.
//
// These headers are present on healthy and rate-limited responses alike and
// are useful for monitoring rather than blocking decisions:
//
//   - x-ig-capacity-level    integer 0–3 (3 = healthy)
//   - x-ig-peak-time         "1" if at peak
//   - x-ig-peak-v2           "1" if at peak (secondary)
//   - x-fb-connection-quality opaque string ("EXCELLENT; q=0.9, rtt=18, ...")
//   - x-ig-origin-region     server origin region
//   - x-ig-server-region     server pop region
//   - x-ig-request-elapsed-time-ms  server processing time
func (c *Client) recordSignals(resp *http.Response, isWrite bool) {
	cap := -1
	if v := resp.Header.Get("x-ig-capacity-level"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cap = n
		}
	}
	elapsed := 0
	if v := resp.Header.Get("x-ig-request-elapsed-time-ms"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			elapsed = n
		}
	}
	now := time.Now()
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if cap >= 0 {
		c.rateState.CapacityLevel = cap
	}
	c.rateState.PeakTime = strings.TrimSpace(resp.Header.Get("x-ig-peak-time")) == "1"
	c.rateState.PeakV2 = strings.TrimSpace(resp.Header.Get("x-ig-peak-v2")) == "1"
	if v := resp.Header.Get("x-fb-connection-quality"); v != "" {
		c.rateState.ConnectionQuality = v
	}
	if v := resp.Header.Get("x-ig-origin-region"); v != "" {
		c.rateState.OriginRegion = v
	}
	if v := resp.Header.Get("x-ig-server-region"); v != "" {
		c.rateState.ServerRegion = v
	}
	if elapsed > 0 {
		c.rateState.LastServerElapsedMs = elapsed
	}
	if isWrite {
		c.rateState.LastWriteAt = now
	} else {
		c.rateState.LastReadAt = now
	}
}
