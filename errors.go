package instagram

import (
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is for matching.
var (
	// ErrInvalidAuth indicates missing or malformed cookies, or that
	// validateSession could not fetch the current user.
	ErrInvalidAuth = errors.New("instagram: invalid auth")

	// ErrSessionExpired is returned when Instagram redirects to /accounts/login/
	// or wipes the sessionid cookie, indicating the session is no longer valid.
	ErrSessionExpired = errors.New("instagram: session expired or invalidated")

	// ErrRateLimited is returned when Instagram throttles the request.
	// On reads this is a 429 or "Please wait a few minutes" body.
	// On writes it is most often a 302 redirect to the login page.
	ErrRateLimited = errors.New("instagram: rate limited")

	// ErrWriteSoftBlock is returned when a write action is rejected with a
	// 302-to-login that does not invalidate the read session. Try again later
	// or from a different IP / device.
	ErrWriteSoftBlock = errors.New("instagram: write soft-blocked")

	// ErrChallengeRequired is returned when Instagram requires the account to
	// complete a security challenge (checkpoint) before continuing.
	ErrChallengeRequired = errors.New("instagram: checkpoint / challenge required")

	// ErrNotFound is returned for 404s and for usernames/IDs that resolve to
	// a user_not_found response from Instagram.
	ErrNotFound = errors.New("instagram: not found")

	// ErrPrivateAccount is returned when the requested resource belongs to a
	// private account that the authenticated user does not follow.
	ErrPrivateAccount = errors.New("instagram: private account")

	// ErrMediaUnavailable is returned when a post has been deleted or hidden.
	ErrMediaUnavailable = errors.New("instagram: media unavailable")

	// ErrCSRF is returned when Instagram rejects a write with a CSRF error.
	ErrCSRF = errors.New("instagram: csrf token rejected")

	// ErrUnexpectedResponse is returned when the response is well-formed but
	// does not contain the expected fields. The wrapped error gives detail.
	ErrUnexpectedResponse = errors.New("instagram: unexpected response")
)

// APIError carries the raw status code and body from a non-2xx response.
type APIError struct {
	StatusCode int
	Status     string
	Method     string
	URL        string
	Body       string
}

func (e *APIError) Error() string {
	body := e.Body
	if len(body) > 240 {
		body = body[:240] + "…"
	}
	return fmt.Sprintf("instagram: %s %s -> %d %s: %s", e.Method, e.URL, e.StatusCode, e.Status, body)
}
