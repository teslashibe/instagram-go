package instagram

import "context"

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

// WithMaxPages caps the number of upstream requests the iterator will make.
// Returns the iterator for chaining. 0 means unlimited.
func (it *Iterator[T]) WithMaxPages(n int) *Iterator[T] {
	it.maxPages = n
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
	if it.pagesSeen > 0 && !it.page.HasMore {
		return false
	}
	if it.maxPages > 0 && it.pagesSeen >= it.maxPages {
		return false
	}
	page, err := it.fetch(ctx, it.cursor)
	if err != nil {
		it.err = err
		return false
	}
	it.pagesSeen++
	it.page = page
	it.cursor = page.NextCursor
	it.pageIdx = 0
	if len(page.Items) == 0 {
		return false
	}
	it.pageIdx = 1
	return true
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

// Cursor returns the next page cursor (next_max_id). Useful for resuming
// iteration in a later process.
func (it *Iterator[T]) Cursor() string { return it.cursor }

// Collect drains the iterator into a slice. Stops at maxPages if set.
func (it *Iterator[T]) Collect(ctx context.Context) ([]T, error) {
	var out []T
	for it.Next(ctx) {
		out = append(out, it.Item())
	}
	return out, it.Err()
}
