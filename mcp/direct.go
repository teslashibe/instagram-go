package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetDirectInboxInput is the typed input for instagram_get_direct_inbox.
type GetDirectInboxInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum inbox threads to return,minimum=1,maximum=50,default=12"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func getDirectInbox(ctx context.Context, c *instagram.Client, in GetDirectInboxInput) (any, error) {
	page, err := collectDirectPage(ctx, c.GetDirectInbox(), in.Cursor, in.Limit, "inbox", "")
	if err != nil {
		return nil, directToolError(err)
	}
	return page, nil
}

// GetDirectThreadInput is the typed input for instagram_get_direct_thread.
type GetDirectThreadInput struct {
	ThreadID string `json:"thread_id" jsonschema:"description=explicit numeric thread ID returned by the inbox tool,required"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=maximum thread items to return,minimum=1,maximum=50,default=12"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func getDirectThread(ctx context.Context, c *instagram.Client, in GetDirectThreadInput) (any, error) {
	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		return nil, directInvalidInput("thread_id is required")
	}
	page, err := collectDirectPage(ctx, c.GetDirectThread(threadID), in.Cursor, in.Limit, "thread", threadID)
	if err != nil {
		return nil, directToolError(err)
	}
	return page, nil
}

// SendDirectTextInput is the typed input for instagram_send_direct_text. The
// explicit confirmation field deliberately keeps this mutation separate from
// read-only discovery.
type SendDirectTextInput struct {
	RecipientID   string `json:"recipient_id" jsonschema:"description=explicit numeric Instagram user ID of the sole recipient,required"`
	Text          string `json:"text" jsonschema:"description=non-empty plain-text message body,required"`
	ConfirmSend   bool   `json:"confirm_send" jsonschema:"description=must be true to confirm this write mutation,required"`
	ThreadID      string `json:"thread_id,omitempty" jsonschema:"description=thread ID from a prior uncertain send; requires the matching client_context and skips thread creation"`
	ClientContext string `json:"client_context,omitempty" jsonschema:"description=optional idempotency context from a prior uncertain send"`
	RetryToken    string `json:"retry_token,omitempty" jsonschema:"description=authenticated token from a prior uncertain send; required with thread_id"`
}

func sendDirectText(ctx context.Context, c *instagram.Client, in SendDirectTextInput) (any, error) {
	if strings.TrimSpace(in.RecipientID) == "" {
		return nil, directInvalidInput("recipient_id is required")
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, directInvalidInput("text must not be empty")
	}
	if !in.ConfirmSend {
		return nil, directInvalidInput("confirm_send must be true for this write mutation")
	}
	if strings.TrimSpace(in.ThreadID) != "" && strings.TrimSpace(in.ClientContext) == "" {
		return nil, directInvalidInput("client_context is required when thread_id is supplied")
	}
	if strings.TrimSpace(in.ThreadID) != "" && strings.TrimSpace(in.RetryToken) == "" {
		return nil, directInvalidInput("retry_token is required when thread_id is supplied")
	}
	if strings.TrimSpace(in.RetryToken) != "" && strings.TrimSpace(in.ThreadID) == "" {
		return nil, directInvalidInput("thread_id is required when retry_token is supplied")
	}
	result, err := c.SendDirectText(ctx, instagram.DirectTextRequest{
		RecipientID: in.RecipientID, Text: in.Text, ThreadID: in.ThreadID,
		ClientContext: in.ClientContext, RetryToken: in.RetryToken,
	})
	if err != nil {
		return nil, directToolError(err)
	}
	return map[string]any{"ok": true, "send": result}, nil
}

const directPageCursorPrefix = "mcp-direct-v1."

type directPageCursor struct {
	Version    int    `json:"v"`
	Surface    string `json:"surface"`
	ThreadID   string `json:"thread_id,omitempty"`
	PageCursor string `json:"page_cursor,omitempty"`
	Offset     int    `json:"offset"`
}

func collectDirectPage[T any](ctx context.Context, it *instagram.Iterator[T], cursor string, limit int, surface, threadID string) (mcptool.Page[T], error) {
	pageCursor, offset, err := decodeDirectPageCursor(cursor, surface, threadID)
	if err != nil {
		return mcptool.Page[T]{}, err
	}
	if pageCursor != "" {
		it.WithCursor(pageCursor)
	}
	pageStart := it.Cursor()
	it.WithMaxPages(1)
	items := make([]T, 0)
	for it.Next(ctx) {
		items = append(items, it.Item())
	}
	if err := it.Err(); err != nil {
		return mcptool.Page[T]{}, err
	}
	if offset > len(items) {
		return mcptool.Page[T]{}, directInvalidInput("cursor offset exceeds page size")
	}
	limit = effectiveLimit(limit)
	end := min(offset+limit, len(items))
	pageItems := make([]T, end-offset)
	copy(pageItems, items[offset:end])
	page := mcptool.Page[T]{Items: pageItems, NextCursor: it.Cursor()}
	if end < len(items) {
		page.NextCursor, err = encodeDirectPageCursor(pageStart, end, surface, threadID)
		if err != nil {
			return mcptool.Page[T]{}, err
		}
		page.Truncated = true
	}
	return page, nil
}

func encodeDirectPageCursor(pageCursor string, offset int, surface, threadID string) (string, error) {
	raw, err := json.Marshal(directPageCursor{
		Version: 1, Surface: surface, ThreadID: threadID, PageCursor: pageCursor, Offset: offset,
	})
	if err != nil {
		return "", fmt.Errorf("encode direct cursor: %w", err)
	}
	return directPageCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeDirectPageCursor(cursor, surface, threadID string) (string, int, error) {
	if !strings.HasPrefix(cursor, directPageCursorPrefix) {
		return cursor, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, directPageCursorPrefix))
	if err != nil {
		return "", 0, directInvalidInput("direct cursor has invalid encoding")
	}
	var state directPageCursor
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", 0, directInvalidInput("direct cursor has invalid payload")
	}
	if state.Version != 1 || state.Offset <= 0 || state.Surface != surface || state.ThreadID != threadID {
		return "", 0, directInvalidInput("direct cursor does not belong to this resource")
	}
	return state.PageCursor, state.Offset, nil
}

func directInvalidInput(message string) error {
	return &mcptool.Error{Code: "invalid_input", Message: message}
}

func directToolError(err error) error {
	data := map[string]any{}
	var sendErr *instagram.DirectSendError
	if errors.As(err, &sendErr) {
		data["client_context"] = sendErr.ClientContext
		if sendErr.ThreadID != "" {
			data["thread_id"] = sendErr.ThreadID
		}
		if sendErr.RetryToken != "" {
			data["retry_token"] = sendErr.RetryToken
		}
	}
	toolErr := func(code, message string, retryable bool) error {
		if len(data) == 0 {
			data = nil
		}
		return &mcptool.Error{Code: code, Message: message, Retryable: retryable, Data: data}
	}
	switch {
	case errors.Is(err, instagram.ErrSessionExpired), errors.Is(err, instagram.ErrInvalidAuth):
		return toolErr("credential_expired", directRetryGuidance(sendErr,
			"Instagram session expired; reconnect Instagram"), false)
	case errors.Is(err, instagram.ErrChallengeRequired):
		return toolErr("challenge_required", directRetryGuidance(sendErr,
			"Instagram requires a security challenge before Direct can continue"), false)
	case errors.Is(err, instagram.ErrRateLimited), errors.Is(err, instagram.ErrWriteSoftBlock):
		return toolErr("rate_limited", directRetryGuidance(sendErr,
			"Instagram Direct is rate limited; wait for the write cooldown"), false)
	case errors.Is(err, instagram.ErrCSRF):
		return toolErr("csrf_rejected", directRetryGuidance(sendErr,
			"Instagram rejected the write CSRF token; reconnect before retrying"), false)
	case errors.Is(err, context.DeadlineExceeded):
		return toolErr("operation_timed_out", directRetryGuidance(sendErr,
			"Instagram Direct timed out and the outcome may be uncertain"), false)
	case errors.Is(err, context.Canceled):
		return toolErr("operation_canceled", directRetryGuidance(sendErr,
			"Instagram Direct operation was canceled"), false)
	case errors.Is(err, instagram.ErrUnexpectedResponse):
		if sendErr != nil {
			return toolErr("send_outcome_uncertain", directRetryGuidance(sendErr,
				"Instagram Direct returned an incomplete send result"), false)
		}
		return toolErr("unexpected_response", "Instagram Direct returned an unexpected response shape", false)
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cursor") || strings.Contains(message, "retry token") || strings.Contains(message, "required") ||
		strings.Contains(message, "must be") || strings.Contains(message, "must not be empty") ||
		strings.Contains(message, "exceeds 128") {
		return directInvalidInput(err.Error())
	}
	return err
}

func directRetryGuidance(sendErr *instagram.DirectSendError, prefix string) string {
	if sendErr == nil {
		return prefix
	}
	if sendErr.ThreadID != "" {
		return prefix + "; retry only the broadcast by supplying the returned thread_id, client_context, and retry_token"
	}
	return prefix + "; do not repeat thread creation until the outcome is reconciled"
}

func defineDirectSendTool() mcptool.Tool {
	tool := mcptool.Define[*instagram.Client, SendDirectTextInput](
		"instagram_send_direct_text",
		"Send confirmed plain text to one explicit Instagram recipient",
		"SendDirectText",
		sendDirectText,
	)
	tool.Tags = []string{"write"}
	return tool
}

var directTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetDirectInboxInput](
		"instagram_get_direct_inbox",
		"List the authenticated Instagram user's Direct inbox threads",
		"GetDirectInbox",
		getDirectInbox,
	),
	mcptool.Define[*instagram.Client, GetDirectThreadInput](
		"instagram_get_direct_thread",
		"Read text items from one explicit Instagram Direct thread",
		"GetDirectThread",
		getDirectThread,
	),
	defineDirectSendTool(),
}
