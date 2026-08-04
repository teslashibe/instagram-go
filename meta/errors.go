// Package meta implements the official Meta Graph and Marketing APIs for
// Instagram professional accounts. It is deliberately independent from the
// cookie-authenticated private API in the repository root.
package meta

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidConfig           = errors.New("meta: invalid configuration")
	ErrInvalidInput            = errors.New("meta: invalid input")
	ErrTokenMissing            = errors.New("meta: OAuth token missing")
	ErrTokenExpired            = errors.New("meta: OAuth token expired")
	ErrReauthorizationRequired = errors.New("meta: reauthorization required")
	ErrPermission              = errors.New("meta: permission denied")
	ErrNotFound                = errors.New("meta: not found")
	ErrUnexpectedResponse      = errors.New("meta: unexpected response")
	ErrMutationNotAllowed      = errors.New("meta: ad mutations are not enabled")
	ErrConfirmationRequired    = errors.New("meta: explicit ad mutation confirmation required")
)

// GraphError is the structured error envelope returned by Meta Graph APIs.
// Access tokens and request URLs are intentionally omitted.
type GraphError struct {
	StatusCode       int    `json:"status_code"`
	Message          string `json:"message"`
	Type             string `json:"type,omitempty"`
	Code             int    `json:"code,omitempty"`
	ErrorSubcode     int    `json:"error_subcode,omitempty"`
	IsTransient      bool   `json:"is_transient,omitempty"`
	FacebookTraceID  string `json:"fbtrace_id,omitempty"`
	ErrorUserTitle   string `json:"error_user_title,omitempty"`
	ErrorUserMessage string `json:"error_user_msg,omitempty"`
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("meta: Graph API error %d/%d (HTTP %d): %s", e.Code, e.ErrorSubcode, e.StatusCode, e.Message)
}

// ScopeError reports a missing OAuth permission without revealing a token.
type ScopeError struct {
	Required []Scope `json:"required"`
	Granted  []Scope `json:"granted,omitempty"`
}

func (e *ScopeError) Error() string {
	return fmt.Sprintf("%v: required=%v granted=%v", ErrPermission, e.Required, e.Granted)
}

func (e *ScopeError) Unwrap() error { return ErrPermission }

// TokenError gives callers a stable reason for reconnecting an OAuth account.
type TokenError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e *TokenError) Error() string { return "meta: " + e.Kind + ": " + e.Message }

func (e *TokenError) Unwrap() error {
	switch e.Kind {
	case "missing":
		return ErrTokenMissing
	case "expired":
		return ErrTokenExpired
	default:
		return ErrReauthorizationRequired
	}
}
