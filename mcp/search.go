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

// SearchInput is the typed input for instagram_search.
type SearchInput struct {
	Query string `json:"query" jsonschema:"description=free-text query; returns blended users, hashtags, and places,required"`
}

func search(ctx context.Context, c *instagram.Client, in SearchInput) (any, error) {
	return c.Search(ctx, in.Query)
}

// SearchPostsInput is the typed input for instagram_search_posts.
type SearchPostsInput struct {
	Query  string `json:"query" jsonschema:"description=keyword query used to find matching posts,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum posts to return,minimum=1,maximum=50,default=12"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func searchPosts(ctx context.Context, c *instagram.Client, in SearchPostsInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	page, err := collectSearchPage(ctx, c.SearchPosts(in.Query), in.Cursor, in.Limit)
	if err != nil {
		return nil, searchToolError(err)
	}
	return page, nil
}

// SearchReelsInput is the typed input for instagram_search_reels.
type SearchReelsInput struct {
	Query  string `json:"query" jsonschema:"description=keyword query used to find matching reels,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum reels to return,minimum=1,maximum=50,default=12"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func searchReels(ctx context.Context, c *instagram.Client, in SearchReelsInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	page, err := collectSearchPage(ctx, c.SearchReels(in.Query), in.Cursor, in.Limit)
	if err != nil {
		return nil, searchToolError(err)
	}
	return page, nil
}

const searchPageCursorPrefix = "mcp-search-v1."

type searchPageCursor struct {
	Version    int    `json:"v"`
	PageCursor string `json:"page_cursor,omitempty"`
	Offset     int    `json:"offset"`
}

// collectSearchPage returns at most one upstream page. When limit ends inside
// that page, the MCP cursor records its starting cursor and the consumed
// offset, allowing the next call to replay the page without skipping items.
func collectSearchPage[T any](ctx context.Context, it *instagram.Iterator[T], cursor string, limit int) (mcptool.Page[T], error) {
	pageCursor, offset, err := decodeSearchPageCursor(cursor)
	if err != nil {
		return mcptool.Page[T]{}, err
	}
	if pageCursor != "" {
		it.WithCursor(pageCursor)
	}
	pageCursor = it.Cursor()
	it.WithMaxPages(1)
	items := make([]T, 0)
	for it.Next(ctx) {
		items = append(items, it.Item())
	}
	if err := it.Err(); err != nil {
		return mcptool.Page[T]{}, err
	}
	if offset > len(items) {
		return mcptool.Page[T]{}, invalidSearchCursorError("offset exceeds page size")
	}

	limit = effectiveLimit(limit)
	end := min(offset+limit, len(items))
	pageItems := make([]T, end-offset)
	copy(pageItems, items[offset:end])
	page := mcptool.Page[T]{
		Items:      pageItems,
		NextCursor: it.Cursor(),
	}
	if end < len(items) {
		page.NextCursor, err = encodeSearchPageCursor(pageCursor, end)
		if err != nil {
			return mcptool.Page[T]{}, err
		}
		page.Truncated = true
	}
	return page, nil
}

func encodeSearchPageCursor(pageCursor string, offset int) (string, error) {
	raw, err := json.Marshal(searchPageCursor{
		Version:    1,
		PageCursor: pageCursor,
		Offset:     offset,
	})
	if err != nil {
		return "", fmt.Errorf("encode search cursor: %w", err)
	}
	return searchPageCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSearchPageCursor(cursor string) (pageCursor string, offset int, err error) {
	if !strings.HasPrefix(cursor, searchPageCursorPrefix) {
		return cursor, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, searchPageCursorPrefix))
	if err != nil {
		return "", 0, invalidSearchCursorError("invalid encoding")
	}
	var state searchPageCursor
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", 0, invalidSearchCursorError("invalid payload")
	}
	if state.Version != 1 || state.Offset <= 0 {
		return "", 0, invalidSearchCursorError("invalid state")
	}
	return state.PageCursor, state.Offset, nil
}

func invalidSearchQueryError() error {
	return &mcptool.Error{
		Code:    "invalid_input",
		Message: "query is required",
	}
}

func invalidSearchCursorError(detail string) error {
	return &mcptool.Error{
		Code:    "invalid_input",
		Message: "invalid search cursor: " + detail,
	}
}

func searchToolError(err error) error {
	if errors.Is(err, instagram.ErrSessionExpired) || errors.Is(err, instagram.ErrInvalidAuth) {
		return &mcptool.Error{
			Code:    "credential_expired",
			Message: "Instagram session expired; reconnect Instagram",
		}
	}
	return err
}

var searchTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, SearchInput](
		"instagram_search",
		"Run an Instagram top-search for users, hashtags, and places matching a query",
		"Search",
		search,
	),
	mcptool.Define[*instagram.Client, SearchPostsInput](
		"instagram_search_posts",
		"Search Instagram posts by keyword and return an opaque cursor for the next page",
		"SearchPosts",
		searchPosts,
	),
	mcptool.Define[*instagram.Client, SearchReelsInput](
		"instagram_search_reels",
		"Search Instagram reels by keyword",
		"SearchReels",
		searchReels,
	),
}
