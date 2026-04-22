package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
)

// defaultLimit is the number of items returned when the caller doesn't
// specify a limit. Instagram's per-page count is typically 12; this matches
// "one page" worth of results without forcing the caller to think about it.
const defaultLimit = 12

// maxLimit caps the number of items any iterator-backed tool will return in
// a single MCP call, regardless of what the caller requests. Higher values
// inflate response payloads (and drive Instagram's rate limiter harder) for
// little benefit; agents that need more should call again with a refined
// query.
const maxLimit = 50

// maxPagesBackstop bounds the worst-case number of upstream Instagram
// requests one tool call can trigger. With Instagram's typical page size of
// ~12 items, 5 pages is enough to fill the maxLimit cap.
const maxPagesBackstop = 5

// collectUpTo drains an [instagram.Iterator] until it has at least limit
// items or the iterator is exhausted, applying maxPagesBackstop as a safety
// net. limit is clamped to [1, maxLimit].
func collectUpTo[T any](ctx context.Context, it *instagram.Iterator[T], limit int) ([]T, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	it.WithMaxPages(maxPagesBackstop)
	out := make([]T, 0, limit)
	for it.Next(ctx) {
		out = append(out, it.Item())
		if len(out) >= limit {
			break
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// effectiveLimit returns the caller's requested limit clamped to the
// [defaultLimit, maxLimit] range; used when calling [mcptool.PageOf] so the
// Page's truncation flag is computed against the same cap collectUpTo used.
func effectiveLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
