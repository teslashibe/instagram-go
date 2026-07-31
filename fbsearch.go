package instagram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const searchPostsCursorVersion = 1

// SearchPosts iterates over posts matching a free-text keyword from
// Instagram's mobile Top SERP. Results are ranked and personalized by
// Instagram; callers should deduplicate durable watches by Post.PK.
//
// Endpoint: GET /api/v1/fbsearch/top_serp/
//
// Cursor returns an opaque continuation value containing all of the Top SERP
// pagination state. Pass it to WithCursor on a fresh iterator to resume.
func (c *Client) SearchPosts(query string) *Iterator[*Post] {
	query = strings.TrimSpace(query)
	seen := make(map[string]struct{})
	var initialCursor string
	var initialCursorErr error
	if query != "" {
		rankToken, err := newSearchSessionID()
		if err != nil {
			initialCursorErr = fmt.Errorf("instagram: SearchPosts: create rank token: %w", err)
		} else {
			initialCursor, initialCursorErr = encodeSearchPostsCursor(searchPostsCursor{
				Version:   searchPostsCursorVersion,
				RankToken: rankToken,
			})
			if initialCursorErr != nil {
				initialCursorErr = fmt.Errorf("instagram: SearchPosts: encode initial cursor: %w", initialCursorErr)
			}
		}
	}

	return newIteratorWithCursor(func(ctx context.Context, cursor string) (Page[*Post], error) {
		if query == "" {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: query required")
		}
		if initialCursorErr != nil {
			return Page[*Post]{}, initialCursorErr
		}

		state, err := decodeSearchPostsCursor(cursor)
		if err != nil {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: %w", err)
		}
		if state.RankToken == "" {
			state.RankToken, err = newSearchSessionID()
			if err != nil {
				return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: create rank token: %w", err)
			}
		}

		q := mobileSearchQuery(query, "top_serp")
		q.Set("rank_token", state.RankToken)
		if state.NextMaxID != "" {
			q.Set("next_max_id", state.NextMaxID)
		}
		if state.ReelsMaxID != "" {
			q.Set("reels_max_id", state.ReelsMaxID)
		}

		var resp topSERPResponse
		if err := c.doJSON(ctx, http.MethodGet, "/api/v1/fbsearch/top_serp/", q, mobileSearchRequestOptions(), &resp); err != nil {
			return Page[*Post]{}, err
		}
		if resp.MediaGrid == nil {
			return Page[*Post]{}, fmt.Errorf("%w: Top SERP missing media_grid", ErrUnexpectedResponse)
		}

		raws := resp.MediaGrid.mediaItems()
		posts := make([]*Post, 0, len(raws))
		for _, raw := range raws {
			post, err := parsePost(raw)
			if err != nil {
				return Page[*Post]{}, err
			}
			// Top SERP can include placeholders and incomplete media-shaped units.
			// Every yielded result must have both a usable identifier and a URL.
			if (post.PK == "" && post.Code == "") || post.PermalinkURL == "" {
				continue
			}
			key := searchPostKey(post)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			posts = append(posts, post)
		}

		hasMore, embeddedReelsMaxID := resp.MediaGrid.pagination()
		if !hasMore {
			return Page[*Post]{Items: posts}, nil
		}

		next := searchPostsCursor{
			Version:    searchPostsCursorVersion,
			NextMaxID:  firstNonEmpty(resp.MediaGrid.NextMaxID, resp.NextMaxID),
			ReelsMaxID: firstNonEmpty(resp.MediaGrid.ReelsMaxID, resp.ReelsMaxID, embeddedReelsMaxID),
			RankToken:  firstNonEmpty(resp.MediaGrid.RankToken, resp.RankToken, state.RankToken),
		}
		if next.NextMaxID == "" && next.ReelsMaxID == "" {
			return Page[*Post]{}, fmt.Errorf("%w: Top SERP has more results without a continuation ID", ErrUnexpectedResponse)
		}
		nextCursor, err := encodeSearchPostsCursor(next)
		if err != nil {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: encode cursor: %w", err)
		}
		return Page[*Post]{Items: posts, NextCursor: nextCursor, HasMore: true}, nil
	}, initialCursor)
}

type topSERPResponse struct {
	MediaGrid  *topSERPMediaGrid `json:"media_grid"`
	RankToken  string            `json:"rank_token"`
	NextMaxID  string            `json:"next_max_id"`
	ReelsMaxID string            `json:"reels_max_id"`
}

type topSERPMediaGrid struct {
	Sections     []topSERPSection `json:"sections"`
	HasMore      bool             `json:"has_more"`
	HasMoreReels bool             `json:"has_more_reels"`
	NextMaxID    string           `json:"next_max_id"`
	ReelsMaxID   string           `json:"reels_max_id"`
	RankToken    string           `json:"rank_token"`
}

type topSERPSection struct {
	LayoutContent struct {
		Medias       []topSERPMediaWrapper `json:"medias"`
		FillItems    []topSERPMediaWrapper `json:"fill_items"`
		OneByTwoItem *struct {
			Media json.RawMessage `json:"media"`
			Clips *topSERPClips   `json:"clips"`
		} `json:"one_by_two_item"`
	} `json:"layout_content"`
}

type topSERPClips struct {
	Items         []topSERPMediaWrapper `json:"items"`
	MoreAvailable bool                  `json:"more_available"`
	MaxID         string                `json:"max_id"`
}

type topSERPMediaWrapper struct {
	Media json.RawMessage `json:"media"`
}

func (g *topSERPMediaGrid) mediaItems() []json.RawMessage {
	var raws []json.RawMessage
	appendMedia := func(raw json.RawMessage) {
		if len(raw) != 0 && string(raw) != "null" {
			raws = append(raws, raw)
		}
	}
	for _, section := range g.Sections {
		layout := section.LayoutContent
		for _, item := range layout.Medias {
			appendMedia(item.Media)
		}
		for _, item := range layout.FillItems {
			appendMedia(item.Media)
		}
		if layout.OneByTwoItem == nil {
			continue
		}
		appendMedia(layout.OneByTwoItem.Media)
		if layout.OneByTwoItem.Clips != nil {
			for _, item := range layout.OneByTwoItem.Clips.Items {
				appendMedia(item.Media)
			}
		}
	}
	return raws
}

func (g *topSERPMediaGrid) pagination() (hasMore bool, embeddedReelsMaxID string) {
	hasMore = g.HasMore || g.HasMoreReels
	for _, section := range g.Sections {
		item := section.LayoutContent.OneByTwoItem
		if item == nil || item.Clips == nil {
			continue
		}
		if item.Clips.MoreAvailable {
			hasMore = true
		}
		if embeddedReelsMaxID == "" {
			embeddedReelsMaxID = item.Clips.MaxID
		}
	}
	return hasMore, embeddedReelsMaxID
}

type searchPostsCursor struct {
	Version    int    `json:"v"`
	NextMaxID  string `json:"max_id,omitempty"`
	ReelsMaxID string `json:"reels_max_id,omitempty"`
	RankToken  string `json:"rank_token"`
}

func encodeSearchPostsCursor(cursor searchPostsCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSearchPostsCursor(encoded string) (searchPostsCursor, error) {
	if encoded == "" {
		return searchPostsCursor{Version: searchPostsCursorVersion}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return searchPostsCursor{}, fmt.Errorf("invalid cursor: %w", err)
	}
	var cursor searchPostsCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return searchPostsCursor{}, fmt.Errorf("invalid cursor: %w", err)
	}
	// A rank-token-only cursor identifies the initial page. SearchPosts seeds
	// iterators with one so consumers can replay a partially consumed first
	// page without starting a different ranked search session.
	if cursor.Version != searchPostsCursorVersion || cursor.RankToken == "" {
		return searchPostsCursor{}, fmt.Errorf("invalid cursor state")
	}
	return cursor, nil
}

func searchPostKey(post *Post) string {
	if post.PK != "" {
		return "pk:" + post.PK
	}
	if post.ID != "" {
		return "id:" + post.ID
	}
	return "code:" + post.Code
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
