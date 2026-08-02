package instagram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	directCursorVersion      = 1
	directPageSize           = 20
	directWriteTimeout       = 30 * time.Second
	directInboxCursorSurface = "inbox"
)

// DirectSendError preserves the idempotency context for a failed or uncertain
// send so callers can safely reconcile or retry the same logical broadcast.
type DirectSendError struct {
	ClientContext string
	ThreadID      string
	Err           error
}

func (e *DirectSendError) Error() string {
	if e == nil {
		return "instagram: direct send failed"
	}
	return e.Err.Error()
}

func (e *DirectSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type directCursor struct {
	Version  int    `json:"v"`
	Surface  string `json:"surface"`
	ThreadID string `json:"thread_id,omitempty"`
	Cursor   string `json:"cursor"`
}

// GetDirectInbox iterates over threads in the authenticated viewer's primary
// Direct inbox. Cursor values are versioned and opaque.
//
// Endpoint: GET i.instagram.com/api/v1/direct_v2/inbox/
func (c *Client) GetDirectInbox() *Iterator[*DirectThread] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*DirectThread], error) {
		upstreamCursor := ""
		if cursor != "" {
			state, err := decodeDirectCursor(cursor, directInboxCursorSurface, "")
			if err != nil {
				return Page[*DirectThread]{}, fmt.Errorf("instagram: GetDirectInbox: %w", err)
			}
			upstreamCursor = state.Cursor
		}

		q := url.Values{}
		q.Set("limit", strconv.Itoa(directPageSize))
		q.Set("thread_message_limit", "1")
		q.Set("visual_message_return_type", "unseen")
		q.Set("persistentBadging", "true")
		if upstreamCursor != "" {
			q.Set("cursor", upstreamCursor)
		}
		var resp struct {
			Inbox struct {
				Threads      []json.RawMessage `json:"threads"`
				OldestCursor string            `json:"oldest_cursor"`
				HasOlder     bool              `json:"has_older"`
			} `json:"inbox"`
			Status string `json:"status"`
		}
		if err := c.doJSON(ctx, http.MethodGet, "/api/v1/direct_v2/inbox/", q, &requestOptions{Host: requestHostAPI}, &resp); err != nil {
			return Page[*DirectThread]{}, err
		}
		threads := make([]*DirectThread, 0, len(resp.Inbox.Threads))
		for _, raw := range resp.Inbox.Threads {
			thread, err := parseDirectThread(raw, c.cookies.DSUserID)
			if err != nil {
				return Page[*DirectThread]{}, err
			}
			threads = append(threads, thread)
		}
		next, err := encodeDirectNextCursor(directInboxCursorSurface, "", resp.Inbox.OldestCursor, resp.Inbox.HasOlder)
		if err != nil {
			return Page[*DirectThread]{}, fmt.Errorf("instagram: GetDirectInbox: encode cursor: %w", err)
		}
		return Page[*DirectThread]{Items: threads, NextCursor: next, HasMore: resp.Inbox.HasOlder}, nil
	})
}

// GetDirectThread iterates over items in a selected Direct thread. Cursors are
// bound to threadID so a cursor cannot be replayed against another conversation.
//
// Endpoint: GET i.instagram.com/api/v1/direct_v2/threads/<thread_id>/
func (c *Client) GetDirectThread(threadID string) *Iterator[*DirectItem] {
	threadID = strings.TrimSpace(threadID)
	return newIterator(func(ctx context.Context, cursor string) (Page[*DirectItem], error) {
		if err := validateDirectID("threadID", threadID); err != nil {
			return Page[*DirectItem]{}, fmt.Errorf("instagram: GetDirectThread: %w", err)
		}
		upstreamCursor := ""
		if cursor != "" {
			state, err := decodeDirectCursor(cursor, "thread", threadID)
			if err != nil {
				return Page[*DirectItem]{}, fmt.Errorf("instagram: GetDirectThread: %w", err)
			}
			upstreamCursor = state.Cursor
		}
		q := url.Values{}
		q.Set("limit", strconv.Itoa(directPageSize))
		q.Set("visual_message_return_type", "unseen")
		if upstreamCursor != "" {
			q.Set("cursor", upstreamCursor)
		}
		var resp struct {
			Thread struct {
				Items        []json.RawMessage `json:"items"`
				OldestCursor string            `json:"oldest_cursor"`
				HasOlder     bool              `json:"has_older"`
			} `json:"thread"`
			Status string `json:"status"`
		}
		path := "/api/v1/direct_v2/threads/" + threadID + "/"
		if err := c.doJSON(ctx, http.MethodGet, path, q, &requestOptions{Host: requestHostAPI}, &resp); err != nil {
			return Page[*DirectItem]{}, err
		}
		items := make([]*DirectItem, 0, len(resp.Thread.Items))
		for _, raw := range resp.Thread.Items {
			item, err := parseDirectItem(raw, threadID, c.cookies.DSUserID)
			if err != nil {
				return Page[*DirectItem]{}, err
			}
			items = append(items, item)
		}
		next, err := encodeDirectNextCursor("thread", threadID, resp.Thread.OldestCursor, resp.Thread.HasOlder)
		if err != nil {
			return Page[*DirectItem]{}, fmt.Errorf("instagram: GetDirectThread: encode cursor: %w", err)
		}
		return Page[*DirectItem]{Items: items, NextCursor: next, HasMore: resp.Thread.HasOlder}, nil
	})
}

// SendDirectText creates/resolves a one-recipient thread and broadcasts a
// plain-text item. The entire mutation is capped at 30 seconds. Thread creation
// is never automatically retried; broadcast retries reuse the same client
// context, mutation token, and offline threading ID. To retry an uncertain
// broadcast without repeating non-idempotent thread creation, pass both the
// ThreadID and ClientContext from DirectSendError.
func (c *Client) SendDirectText(ctx context.Context, in DirectTextRequest) (*DirectSendResult, error) {
	recipientID := strings.TrimSpace(in.RecipientID)
	text := in.Text
	if err := validateDirectID("recipientID", recipientID); err != nil {
		return nil, fmt.Errorf("instagram: SendDirectText: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("instagram: SendDirectText: text must not be empty")
	}
	threadID := strings.TrimSpace(in.ThreadID)
	clientContext := strings.TrimSpace(in.ClientContext)
	if threadID != "" {
		if err := validateDirectID("threadID", threadID); err != nil {
			return nil, fmt.Errorf("instagram: SendDirectText: %w", err)
		}
		if clientContext == "" {
			return nil, fmt.Errorf("instagram: SendDirectText: clientContext required when threadID is supplied")
		}
	}
	if clientContext == "" {
		var err error
		clientContext, err = newDirectClientContext()
		if err != nil {
			return nil, fmt.Errorf("instagram: SendDirectText: client context: %w", err)
		}
	}
	if len(clientContext) > 128 {
		return nil, fmt.Errorf("instagram: SendDirectText: clientContext exceeds 128 characters")
	}

	writeCtx, cancel := context.WithTimeout(ctx, directWriteTimeout)
	defer cancel()

	if threadID == "" {
		var err error
		threadID, err = c.createDirectThread(writeCtx, recipientID)
		if err != nil {
			return nil, &DirectSendError{ClientContext: clientContext, Err: err}
		}
	}
	itemID, status, err := c.broadcastDirectText(writeCtx, threadID, text, clientContext)
	if err != nil {
		return nil, &DirectSendError{ClientContext: clientContext, ThreadID: threadID, Err: err}
	}
	return &DirectSendResult{
		RecipientID: recipientID, ThreadID: threadID, ItemID: itemID,
		ClientContext: clientContext, Status: status,
	}, nil
}

func (c *Client) createDirectThread(ctx context.Context, recipientID string) (string, error) {
	recipients, _ := json.Marshal([]string{recipientID})
	form := url.Values{"recipient_users": {string(recipients)}}
	if c.cookies.IgDid != "" {
		form.Set("_uuid", c.cookies.IgDid)
	}
	var resp struct {
		Thread struct {
			ID json.RawMessage `json:"thread_id"`
		} `json:"thread"`
		ThreadID json.RawMessage `json:"thread_id"`
		Status   string          `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/direct_v2/create_group_thread/", nil, &requestOptions{
		Host: requestHostAPI, IsWrite: true, FormBody: form, MaxAttempts: 1,
	}, &resp); err != nil {
		return "", err
	}
	if resp.Status != "ok" {
		return "", fmt.Errorf("%w: direct thread creation response omitted status=ok", ErrUnexpectedResponse)
	}
	threadID := stringifyID(resp.Thread.ID, resp.ThreadID)
	if err := validateDirectID("created thread ID", threadID); err != nil {
		return "", fmt.Errorf("%w: direct thread creation returned no usable thread ID", ErrUnexpectedResponse)
	}
	return threadID, nil
}

func (c *Client) broadcastDirectText(ctx context.Context, threadID, text, clientContext string) (string, string, error) {
	threadIDs, _ := json.Marshal([]string{threadID})
	form := url.Values{
		"action":               {"send_item"},
		"client_context":       {clientContext},
		"mutation_token":       {clientContext},
		"offline_threading_id": {directOfflineThreadingID(clientContext)},
		"text":                 {text},
		"thread_ids":           {string(threadIDs)},
	}
	if c.cookies.IgDid != "" {
		form.Set("_uuid", c.cookies.IgDid)
	}
	var resp struct {
		Payload struct {
			ItemID json.RawMessage `json:"item_id"`
		} `json:"payload"`
		ItemID json.RawMessage `json:"item_id"`
		Status string          `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/direct_v2/threads/broadcast/text/", nil, &requestOptions{
		Host: requestHostAPI, IsWrite: true, FormBody: form,
	}, &resp); err != nil {
		return "", "", err
	}
	if resp.Status != "ok" {
		return "", "", fmt.Errorf("%w: direct text broadcast response omitted status=ok", ErrUnexpectedResponse)
	}
	itemID := stringifyID(resp.Payload.ItemID, resp.ItemID)
	if err := validateDirectID("broadcast item ID", itemID); err != nil {
		return "", "", fmt.Errorf("%w: direct text broadcast returned no usable item ID", ErrUnexpectedResponse)
	}
	return itemID, resp.Status, nil
}

func parseDirectThread(raw json.RawMessage, viewerID string) (*DirectThread, error) {
	var aux struct {
		ThreadID     json.RawMessage   `json:"thread_id"`
		ThreadTitle  string            `json:"thread_title"`
		Users        []json.RawMessage `json:"users"`
		Items        []json.RawMessage `json:"items"`
		LastActivity json.RawMessage   `json:"last_activity_at"`
		IsGroup      bool              `json:"is_group"`
		IsPending    bool              `json:"is_pending"`
		Muted        bool              `json:"muted"`
		ReadState    any               `json:"read_state"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse direct thread: %v", ErrUnexpectedResponse, err)
	}
	thread := &DirectThread{
		ID: stringifyID(aux.ThreadID), Title: aux.ThreadTitle,
		LastActivityAt: anyToInt64(aux.LastActivity), IsGroup: aux.IsGroup,
		IsPending: aux.IsPending, Muted: aux.Muted, ReadState: anyToInt(aux.ReadState), Raw: raw,
	}
	if thread.ID == "" {
		return nil, fmt.Errorf("%w: direct thread missing thread_id", ErrUnexpectedResponse)
	}
	for _, userRaw := range aux.Users {
		user, err := parseUser(userRaw)
		if err != nil {
			return nil, err
		}
		thread.Users = append(thread.Users, user)
	}
	for _, itemRaw := range aux.Items {
		item, err := parseDirectItem(itemRaw, thread.ID, viewerID)
		if err != nil {
			return nil, err
		}
		thread.Items = append(thread.Items, item)
	}
	return thread, nil
}

func parseDirectItem(raw json.RawMessage, threadID, viewerID string) (*DirectItem, error) {
	var aux struct {
		ItemID        json.RawMessage `json:"item_id"`
		UserID        json.RawMessage `json:"user_id"`
		ItemType      string          `json:"item_type"`
		Text          string          `json:"text"`
		Timestamp     json.RawMessage `json:"timestamp"`
		ClientContext string          `json:"client_context"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse direct item: %v", ErrUnexpectedResponse, err)
	}
	item := &DirectItem{
		ID: stringifyID(aux.ItemID), ThreadID: threadID, UserID: stringifyID(aux.UserID),
		ItemType: aux.ItemType, Text: aux.Text, Timestamp: anyToInt64(aux.Timestamp),
		ClientContext: aux.ClientContext, Raw: raw,
	}
	item.IsSentByViewer = viewerID != "" && item.UserID == viewerID
	if item.ID == "" {
		return nil, fmt.Errorf("%w: direct item missing item_id", ErrUnexpectedResponse)
	}
	return item, nil
}

func anyToInt64(v any) int64 {
	s := stringifyID(v)
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func encodeDirectNextCursor(surface, threadID, cursor string, hasMore bool) (string, error) {
	if !hasMore {
		return "", nil
	}
	if strings.TrimSpace(cursor) == "" {
		return "", fmt.Errorf("%w: direct response has_older without oldest_cursor", ErrUnexpectedResponse)
	}
	raw, err := json.Marshal(directCursor{Version: directCursorVersion, Surface: surface, ThreadID: threadID, Cursor: cursor})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeDirectCursor(value, surface, threadID string) (directCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return directCursor{}, fmt.Errorf("invalid cursor: %w", err)
	}
	var state directCursor
	if err := json.Unmarshal(raw, &state); err != nil {
		return directCursor{}, fmt.Errorf("invalid cursor: %w", err)
	}
	if state.Version != directCursorVersion {
		return directCursor{}, fmt.Errorf("unsupported cursor version %d", state.Version)
	}
	if state.Surface != surface || strings.TrimSpace(state.Cursor) == "" {
		return directCursor{}, fmt.Errorf("invalid cursor state")
	}
	if state.ThreadID != threadID {
		return directCursor{}, fmt.Errorf("cursor thread mismatch")
	}
	return state, nil
}

func validateDirectID(name, id string) error {
	if id == "" {
		return fmt.Errorf("%s required", name)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("%s must be a numeric Instagram ID", name)
		}
	}
	return nil
}

func newDirectClientContext() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func directOfflineThreadingID(clientContext string) string {
	sum := sha256.Sum256([]byte(clientContext))
	n := binary.BigEndian.Uint64(sum[:8]) & ((1 << 63) - 1)
	return strconv.FormatUint(n, 10)
}
