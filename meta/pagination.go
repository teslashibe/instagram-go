package meta

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const cursorPrefix = "meta-page-v1."

type ListOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type graphPage[T any] struct {
	Data   []T `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

type cursorState struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	After   string `json:"a"`
}

func validateListOptions(options ListOptions) error {
	if options.Limit < 0 || options.Limit > 100 {
		return fmt.Errorf("%w: limit must be 0 (default) or between 1 and 100", ErrInvalidInput)
	}
	return nil
}

func preparePageQuery(path string, query url.Values, options ListOptions) (url.Values, string, error) {
	if err := validateListOptions(options); err != nil {
		return nil, "", err
	}
	q := cloneValues(query)
	limit := options.Limit
	if limit == 0 {
		limit = 25
	}
	q.Set("limit", strconv.Itoa(limit))
	binding := queryBinding(path, q)
	if options.Cursor != "" {
		after, err := decodeCursor(options.Cursor, binding)
		if err != nil {
			return nil, "", err
		}
		q.Set("after", after)
	}
	return q, binding, nil
}

func finishPage[T any](raw graphPage[T], binding string) (Page[T], error) {
	page := Page[T]{Items: raw.Data}
	if page.Items == nil {
		page.Items = []T{}
	}
	if raw.Paging.Next == "" {
		return page, nil
	}
	if raw.Paging.Cursors.After == "" {
		return Page[T]{}, fmt.Errorf("%w: paging.next without paging.cursors.after", ErrUnexpectedResponse)
	}
	cursor, err := encodeCursor(binding, raw.Paging.Cursors.After)
	if err != nil {
		return Page[T]{}, err
	}
	page.NextCursor = cursor
	page.HasMore = true
	return page, nil
}

func queryBinding(path string, query url.Values) string {
	q := cloneValues(query)
	q.Del("after")
	sum := sha256.Sum256([]byte(strings.Trim(path, "/") + "?" + q.Encode()))
	return hex.EncodeToString(sum[:])
}

func encodeCursor(binding, after string) (string, error) {
	raw, err := json.Marshal(cursorState{Version: 1, Query: binding, After: after})
	if err != nil {
		return "", fmt.Errorf("%w: encode cursor: %v", ErrUnexpectedResponse, err)
	}
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(cursor, binding string) (string, error) {
	if !strings.HasPrefix(cursor, cursorPrefix) {
		return "", fmt.Errorf("%w: malformed Meta pagination cursor", ErrInvalidInput)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, cursorPrefix))
	if err != nil {
		return "", fmt.Errorf("%w: malformed Meta pagination cursor", ErrInvalidInput)
	}
	var state cursorState
	if err := json.Unmarshal(raw, &state); err != nil || state.Version != 1 || state.After == "" {
		return "", fmt.Errorf("%w: malformed Meta pagination cursor", ErrInvalidInput)
	}
	if state.Query != binding {
		return "", fmt.Errorf("%w: pagination cursor belongs to another account or query", ErrInvalidInput)
	}
	return state.After, nil
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, value := range values {
		clone[key] = append([]string(nil), value...)
	}
	return clone
}
