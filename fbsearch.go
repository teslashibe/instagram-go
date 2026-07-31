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

	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		if query == "" {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: query required")
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
			// Top SERP can include placeholders in otherwise media-shaped units.
			// Keep only nodes with an identifier accepted by GetPost/GetPostByID.
			if post.PK == "" && post.Code == "" {
				continue
			}
			key := searchPostKey(post)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			posts = append(posts, post)
		}

		if !resp.MediaGrid.HasMore {
			return Page[*Post]{Items: posts}, nil
		}

		next := searchPostsCursor{
			Version:    searchPostsCursorVersion,
			NextMaxID:  firstNonEmpty(resp.MediaGrid.NextMaxID, resp.NextMaxID),
			ReelsMaxID: firstNonEmpty(resp.MediaGrid.ReelsMaxID, resp.ReelsMaxID),
			RankToken:  firstNonEmpty(resp.MediaGrid.RankToken, resp.RankToken, state.RankToken),
		}
		if next.NextMaxID == "" {
			return Page[*Post]{}, fmt.Errorf("%w: Top SERP has more results without next_max_id", ErrUnexpectedResponse)
		}
		nextCursor, err := encodeSearchPostsCursor(next)
		if err != nil {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchPosts: encode cursor: %w", err)
		}
		return Page[*Post]{Items: posts, NextCursor: nextCursor, HasMore: true}, nil
	})
}

type topSERPResponse struct {
	MediaGrid  *topSERPMediaGrid `json:"media_grid"`
	RankToken  string            `json:"rank_token"`
	NextMaxID  string            `json:"next_max_id"`
	ReelsMaxID string            `json:"reels_max_id"`
}

type topSERPMediaGrid struct {
	Sections   []topSERPSection `json:"sections"`
	HasMore    bool             `json:"has_more"`
	NextMaxID  string           `json:"next_max_id"`
	ReelsMaxID string           `json:"reels_max_id"`
	RankToken  string           `json:"rank_token"`
}

type topSERPSection struct {
	LayoutContent struct {
		Medias       []topSERPMediaWrapper `json:"medias"`
		FillItems    []topSERPMediaWrapper `json:"fill_items"`
		OneByTwoItem *struct {
			Media json.RawMessage `json:"media"`
			Clips *struct {
				Items []topSERPMediaWrapper `json:"items"`
			} `json:"clips"`
		} `json:"one_by_two_item"`
	} `json:"layout_content"`
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
	if cursor.Version != searchPostsCursorVersion || cursor.RankToken == "" || cursor.NextMaxID == "" {
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
