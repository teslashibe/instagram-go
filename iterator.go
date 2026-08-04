package instagram

import (
	"context"
	"fmt"
)

const maxConsecutiveEmptyPages = 100

// Iterator is the generic paginating iterator returned by all list endpoints.
//
// Usage:
//
//	it := client.GetPosts(username)
//	for it.Next(ctx) {
//	    post := it.Item()
//	    fmt.Println(post.Code, post.LikeCount)
//	}
//	if err := it.Err(); err != nil {
//	    log.Fatal(err)
//	}
type Iterator[T any] struct {
	fetch     func(ctx context.Context, cursor string) (Page[T], error)
	page      Page[T]
	pageIdx   int
	cursor    string
	pagesSeen int
	err       error
	maxPages  int
}

// newIterator constructs an Iterator. fetch must be safe to call repeatedly.
func newIterator[T any](fetch func(ctx context.Context, cursor string) (Page[T], error)) *Iterator[T] {
	return &Iterator[T]{fetch: fetch}
}

// newIteratorWithCursor constructs an Iterator whose first fetch starts from
// cursor. It is used by endpoints whose initial request needs opaque session
// state that must remain resumable before the first page has been fetched.
func newIteratorWithCursor[T any](fetch func(ctx context.Context, cursor string) (Page[T], error), cursor string) *Iterator[T] {
	return &Iterator[T]{fetch: fetch, cursor: cursor}
}

// WithMaxPages caps the number of upstream requests the iterator will make.
// Returns the iterator for chaining. 0 means unlimited.
func (it *Iterator[T]) WithMaxPages(n int) *Iterator[T] {
	it.maxPages = n
	return it
}

// WithCursor starts the iterator from a cursor returned by Cursor. It must be
// called before Next or Collect. Cursor values are endpoint-specific and
// should be treated as opaque.
func (it *Iterator[T]) WithCursor(cursor string) *Iterator[T] {
	if it.pagesSeen == 0 && it.pageIdx == 0 {
		it.cursor = cursor
	}
	return it
}

// Next advances to the next item, fetching a new page if necessary. Returns
// false when there are no more items or an error occurred. Inspect Err.
func (it *Iterator[T]) Next(ctx context.Context) bool {
	if it.err != nil {
		return false
	}
	if it.pageIdx < len(it.page.Items) {
		it.pageIdx++
		return true
	}

	// Some heterogeneous connections can contain pages made entirely of units
	// filtered out by the endpoint mapper. Keep following a valid cursor until
	// an item page is found, while preserving maxPages as an upstream-request
	// limit and protecting unlimited iterators from cursor loops.
	emptyCursors := make(map[string]struct{})
	for emptyPages := 0; ; emptyPages++ {
		if it.pagesSeen > 0 && !it.page.HasMore {
			return false
		}
		if it.maxPages > 0 && it.pagesSeen >= it.maxPages {
			return false
		}
		if emptyPages >= maxConsecutiveEmptyPages {
			it.err = fmt.Errorf("%w: pagination exceeded %d consecutive empty pages", ErrUnexpectedResponse, maxConsecutiveEmptyPages)
			return false
		}

		previousCursor := it.cursor
		page, err := it.fetch(ctx, previousCursor)
		if err != nil {
			it.err = err
			return false
		}
		it.pagesSeen++
		it.page = page
		it.cursor = page.NextCursor
		it.pageIdx = 0
		if len(page.Items) > 0 {
			it.pageIdx = 1
			return true
		}
		if !page.HasMore {
			return false
		}
		if page.NextCursor == "" || page.NextCursor == previousCursor {
			it.err = fmt.Errorf("%w: pagination cursor did not advance after an empty page", ErrUnexpectedResponse)
			return false
		}
		if _, seen := emptyCursors[page.NextCursor]; seen {
			it.err = fmt.Errorf("%w: pagination cursor repeated after an empty page", ErrUnexpectedResponse)
			return false
		}
		emptyCursors[page.NextCursor] = struct{}{}
	}
}

// Item returns the current item. Only valid after Next returns true.
func (it *Iterator[T]) Item() T {
	if it.pageIdx < 1 || it.pageIdx > len(it.page.Items) {
		var zero T
		return zero
	}
	return it.page.Items[it.pageIdx-1]
}

// Err returns the error that caused iteration to stop, if any.
func (it *Iterator[T]) Err() error { return it.err }

// Cursor returns the endpoint-specific next-page cursor. It is opaque and can
// be passed to WithCursor on a fresh iterator to resume in a later process.
func (it *Iterator[T]) Cursor() string { return it.cursor }

// Collect drains the iterator into a slice. Stops at maxPages if set.
func (it *Iterator[T]) Collect(ctx context.Context) ([]T, error) {
	var out []T
	for it.Next(ctx) {
		out = append(out, it.Item())
	}
	return out, it.Err()
}
