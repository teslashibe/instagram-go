package mcp

import (
	"errors"
	"strings"

	"github.com/teslashibe/instagram-go/meta"
	"github.com/teslashibe/mcptool"
)

func toolError(err error) error {
	if err == nil {
		return nil
	}
	var scopeErr *meta.ScopeError
	if errors.As(err, &scopeErr) {
		return &mcptool.Error{
			Code: "missing_scope", Message: "Meta OAuth permission is missing", Retryable: false,
			Data: map[string]any{"required_scopes": scopeErr.Required, "granted_scopes": scopeErr.Granted},
		}
	}
	var tokenErr *meta.TokenError
	if errors.As(err, &tokenErr) {
		code := "reauthorization_required"
		if tokenErr.Kind == "expired" {
			code = "credential_expired"
		}
		return &mcptool.Error{Code: code, Message: tokenErr.Message, Retryable: false}
	}
	if errors.Is(err, meta.ErrReauthorizationRequired) {
		return &mcptool.Error{Code: "reauthorization_required", Message: "Reconnect the Meta OAuth account", Retryable: false}
	}
	if errors.Is(err, meta.ErrPermission) {
		return &mcptool.Error{Code: "permission_denied", Message: "Meta denied access to this resource", Retryable: false}
	}
	if errors.Is(err, meta.ErrInvalidInput) {
		return &mcptool.Error{Code: "invalid_input", Message: strings.TrimPrefix(err.Error(), "meta: invalid input: "), Retryable: false}
	}
	if errors.Is(err, meta.ErrNotFound) {
		return &mcptool.Error{Code: "not_found", Message: "The requested Meta resource was not found", Retryable: false}
	}
	var graphErr *meta.GraphError
	if errors.As(err, &graphErr) && graphErr.IsTransient {
		return &mcptool.Error{Code: "meta_transient_error", Message: graphErr.Message, Retryable: true}
	}
	return err
}
