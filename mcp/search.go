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
	Cursor string `json:"cursor,omitempty" jsonschema:"description=reserved for future continuation; currently must be omitted"`
}

func searchReels(ctx context.Context, c *instagram.Client, in SearchReelsInput) (any, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, invalidSearchQueryError()
	}
	if in.Cursor != "" {
		return nil, invalidSearchCursorError("reels continuation is unavailable")
	}

	it := c.SearchReels(query).WithMaxPages(1)
	items := make([]*instagram.Post, 0)
	for it.Next(ctx) {
		items = append(items, it.Item())
	}
	if err := it.Err(); err != nil {
		return nil, searchToolError(err)
	}

	limit := effectiveLimit(in.Limit)
	end := min(limit, len(items))
	pageItems := make([]*instagram.Post, end)
	copy(pageItems, items[:end])
	return mcptool.Page[*instagram.Post]{
		Items:     pageItems,
		Truncated: end < len(items),
	}, nil
}

// SearchKeywordPostsInput is the typed input for
// instagram_search_keyword_posts.
type SearchKeywordPostsInput struct {
	Query  string `json:"query" jsonschema:"description=keyword query used for Instagram's web GraphQL search,required"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func searchKeywordPosts(ctx context.Context, c *instagram.Client, in SearchKeywordPostsInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	it := c.SearchKeywordPosts(in.Query).WithMaxPages(1)
	if in.Cursor != "" {
		it.WithCursor(in.Cursor)
	}
	items := make([]*instagram.Post, 0)
	for it.Next(ctx) {
		items = append(items, it.Item())
	}
	if err := it.Err(); err != nil {
		return nil, searchToolError(err)
	}
	return mcptool.Page[*instagram.Post]{
		Items:      items,
		NextCursor: it.Cursor(),
		Truncated:  it.Cursor() != "",
	}, nil
}

// SearchAccountsInput is the typed input for instagram_search_accounts.
type SearchAccountsInput struct {
	Query string `json:"query" jsonschema:"description=account name or username query,required"`
}

func searchAccounts(ctx context.Context, c *instagram.Client, in SearchAccountsInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	result, err := c.SearchAccounts(ctx, in.Query)
	if err != nil {
		return nil, searchToolError(err)
	}
	return result, nil
}

// SearchTypeaheadUsersInput is the typed input for
// instagram_search_typeahead_users.
type SearchTypeaheadUsersInput struct {
	Query string `json:"query" jsonschema:"description=partial account name or username,required"`
	Count int    `json:"count,omitempty" jsonschema:"description=maximum account suggestions,minimum=1,maximum=50,default=30"`
}

func searchTypeaheadUsers(ctx context.Context, c *instagram.Client, in SearchTypeaheadUsersInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	count := in.Count
	if count <= 0 {
		count = 30
	}
	result, err := c.SearchTypeaheadUsers(ctx, in.Query, count)
	if err != nil {
		return nil, searchToolError(err)
	}
	return result, nil
}

// KeywordTypeaheadInput is the typed input for instagram_keyword_typeahead.
type KeywordTypeaheadInput struct {
	Query string `json:"query" jsonschema:"description=partial keyword used to suggest Instagram accounts,required"`
}

func keywordTypeahead(ctx context.Context, c *instagram.Client, in KeywordTypeaheadInput) (any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, invalidSearchQueryError()
	}
	suggestions, err := c.KeywordTypeahead(ctx, in.Query)
	if err != nil {
		return nil, searchToolError(err)
	}
	return map[string]any{"suggestions": suggestions}, nil
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
	if strings.TrimSpace(state.PageCursor) == "" {
		return "", 0, invalidSearchCursorError("missing page cursor")
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
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cursor query mismatch") {
		return invalidSearchCursorError("cursor query mismatch")
	}
	if strings.Contains(message, "invalid cursor") ||
		strings.Contains(message, "unsupported cursor version") {
		return invalidSearchCursorError("cursor is malformed or belongs to another query")
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
		"Search the first page of Instagram reels by keyword without continuation",
		"SearchReels",
		searchReels,
	),
	mcptool.Define[*instagram.Client, SearchKeywordPostsInput](
		"instagram_search_keyword_posts",
		"Search Instagram posts through the web keyword index with opaque pagination",
		"SearchKeywordPosts",
		searchKeywordPosts,
	),
	mcptool.Define[*instagram.Client, SearchAccountsInput](
		"instagram_search_accounts",
		"Search Instagram accounts and return rich account SERP context",
		"SearchAccounts",
		searchAccounts,
	),
	mcptool.Define[*instagram.Client, SearchTypeaheadUsersInput](
		"instagram_search_typeahead_users",
		"Suggest Instagram accounts for a partial name or username",
		"SearchTypeaheadUsers",
		searchTypeaheadUsers,
	),
	mcptool.Define[*instagram.Client, KeywordTypeaheadInput](
		"instagram_keyword_typeahead",
		"Return lightweight Instagram account suggestions for a partial keyword",
		"KeywordTypeahead",
		keywordTypeahead,
	),
}
