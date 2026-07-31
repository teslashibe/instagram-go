package mcp

import (
	"context"
	"errors"
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

// collectSearchPage returns at most one upstream page so the iterator cursor
// remains a true page boundary that can be resumed by a later MCP call.
func collectSearchPage[T any](ctx context.Context, it *instagram.Iterator[T], cursor string, limit int) (mcptool.Page[T], error) {
	it.WithCursor(cursor).WithMaxPages(1)
	items := make([]T, 0)
	for it.Next(ctx) {
		items = append(items, it.Item())
	}
	if err := it.Err(); err != nil {
		return mcptool.Page[T]{}, err
	}
	return mcptool.PageOf(items, it.Cursor(), effectiveLimit(limit)), nil
}

func invalidSearchQueryError() error {
	return &mcptool.Error{
		Code:    "invalid_input",
		Message: "query is required",
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
