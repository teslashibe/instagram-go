package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GetPost fetches a single post by its shortcode (the bit between /p/ and /
// in the public URL).
//
// Endpoint: GET /api/v1/media/<media_id>/info/ via shortcode lookup using
// /graphql/query/?query_hash=...&variables=... is unstable, so we use the
// /api/v1/media/<media_pk>/info/ endpoint after resolving the shortcode via
// the GraphQL mapping. For simplicity and stability, we hit the public
// /p/<code>/?__a=1&__d=dis endpoint which still works for authenticated calls.
func (c *Client) GetPost(ctx context.Context, shortcode string) (*Post, error) {
	if shortcode == "" {
		return nil, fmt.Errorf("instagram: GetPost: shortcode required")
	}
	q := url.Values{}
	q.Set("__a", "1")
	q.Set("__d", "dis")
	var resp struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := c.doJSON(ctx, "GET", "/p/"+shortcode+"/", q, &requestOptions{
		Referer: baseURL + "/p/" + shortcode + "/",
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Items) == 0 {
		return nil, fmt.Errorf("%w: post %q", ErrNotFound, shortcode)
	}
	return parsePost(resp.Items[0])
}

// GetPosts iterates over the timeline posts of a user (most recent first).
//
// Endpoint: GET /api/v1/feed/user/<user_id>/?count=12&max_id=<cursor>
//
// The user_id can be obtained from GetProfile(username).ID.
func (c *Client) GetPosts(userID string) *Iterator[*Post] {
	return c.userMediaIterator(userID, "")
}

// GetReels iterates over the reels (clips) authored by a user.
//
// Endpoint: POST /api/v1/clips/user/ with form body {target_user_id, max_id, page_size}
func (c *Client) GetReels(userID string) *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		form := url.Values{}
		form.Set("target_user_id", userID)
		form.Set("page_size", "12")
		form.Set("include_feed_video", "true")
		if cursor != "" {
			form.Set("max_id", cursor)
		}
		var resp struct {
			Items  []struct {
				Media json.RawMessage `json:"media"`
			} `json:"items"`
			PagingInfo struct {
				MaxID         string `json:"max_id"`
				MoreAvailable bool   `json:"more_available"`
			} `json:"paging_info"`
			Status string `json:"status"`
		}
		if err := c.doJSON(ctx, "POST", "/api/v1/clips/user/", nil, &requestOptions{
			FormBody: form,
		}, &resp); err != nil {
			return Page[*Post]{}, err
		}
		out := make([]*Post, 0, len(resp.Items))
		for _, it := range resp.Items {
			p, err := parsePost(it.Media)
			if err != nil {
				return Page[*Post]{}, err
			}
			out = append(out, p)
		}
		return Page[*Post]{
			Items:      out,
			NextCursor: resp.PagingInfo.MaxID,
			HasMore:    resp.PagingInfo.MoreAvailable,
		}, nil
	})
}

// GetTaggedPosts iterates over posts the user has been tagged in.
//
// Endpoint: GET /api/v1/usertags/<user_id>/feed/
func (c *Client) GetTaggedPosts(userID string) *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		q := url.Values{}
		q.Set("count", "12")
		if cursor != "" {
			q.Set("max_id", cursor)
		}
		var resp struct {
			Items         []json.RawMessage `json:"items"`
			NextMaxID     string            `json:"next_max_id"`
			MoreAvailable bool              `json:"more_available"`
			Status        string            `json:"status"`
		}
		if err := c.doJSON(ctx, "GET", "/api/v1/usertags/"+userID+"/feed/", q, nil, &resp); err != nil {
			return Page[*Post]{}, err
		}
		return parsePostList(resp.Items, resp.NextMaxID, resp.MoreAvailable)
	})
}

// GetTimeline fetches the home timeline feed (Following + recommendations).
//
// Endpoint: POST /api/v1/feed/timeline/
func (c *Client) GetTimeline() *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		form := url.Values{}
		form.Set("reason", "cold_start_fetch")
		form.Set("is_pull_to_refresh", "0")
		form.Set("phone_id", "00000000-0000-0000-0000-000000000000")
		form.Set("battery_level", "100")
		form.Set("is_charging", "1")
		form.Set("will_sound_on", "0")
		if cursor != "" {
			form.Set("max_id", cursor)
		}
		var resp struct {
			FeedItems []struct {
				MediaOrAd json.RawMessage `json:"media_or_ad"`
			} `json:"feed_items"`
			NextMaxID     string `json:"next_max_id"`
			MoreAvailable bool   `json:"more_available"`
			Status        string `json:"status"`
		}
		if err := c.doJSON(ctx, "POST", "/api/v1/feed/timeline/", nil, &requestOptions{
			FormBody: form,
			ExtraHeaders: map[string]string{
				"X-Ig-Connection-Type": "WIFI",
				"X-Ig-Capabilities":    "3brTvw==",
			},
		}, &resp); err != nil {
			return Page[*Post]{}, err
		}
		out := make([]*Post, 0, len(resp.FeedItems))
		for _, it := range resp.FeedItems {
			if len(it.MediaOrAd) == 0 || string(it.MediaOrAd) == "null" {
				continue
			}
			p, err := parsePost(it.MediaOrAd)
			if err != nil {
				continue // ignore unparseable items, often promotion blocks
			}
			out = append(out, p)
		}
		return Page[*Post]{
			Items:      out,
			NextCursor: resp.NextMaxID,
			HasMore:    resp.MoreAvailable,
		}, nil
	})
}

// GetExplore fetches the explore feed.
//
// Endpoint: GET /api/v1/discover/web/explore_grid/
func (c *Client) GetExplore() *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		q := url.Values{}
		if cursor != "" {
			q.Set("max_id", cursor)
		}
		var resp struct {
			SectionalItems []struct {
				LayoutContent struct {
					Medias []struct {
						Media json.RawMessage `json:"media"`
					} `json:"medias"`
				} `json:"layout_content"`
			} `json:"sectional_items"`
			NextMaxID     string `json:"next_max_id"`
			MoreAvailable bool   `json:"more_available"`
		}
		if err := c.doJSON(ctx, "GET", "/api/v1/discover/web/explore_grid/", q, nil, &resp); err != nil {
			return Page[*Post]{}, err
		}
		var raws []json.RawMessage
		for _, sec := range resp.SectionalItems {
			for _, m := range sec.LayoutContent.Medias {
				if len(m.Media) > 0 {
					raws = append(raws, m.Media)
				}
			}
		}
		return parsePostList(raws, resp.NextMaxID, resp.MoreAvailable)
	})
}

func (c *Client) userMediaIterator(userID, refererUsername string) *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		q := url.Values{}
		q.Set("count", "12")
		if cursor != "" {
			q.Set("max_id", cursor)
		}
		var resp struct {
			Items         []json.RawMessage `json:"items"`
			NextMaxID     string            `json:"next_max_id"`
			MoreAvailable bool              `json:"more_available"`
			Status        string            `json:"status"`
		}
		opts := &requestOptions{}
		if refererUsername != "" {
			opts.Referer = baseURL + "/" + refererUsername + "/"
		}
		if err := c.doJSON(ctx, "GET", "/api/v1/feed/user/"+userID+"/", q, opts, &resp); err != nil {
			return Page[*Post]{}, err
		}
		return parsePostList(resp.Items, resp.NextMaxID, resp.MoreAvailable)
	})
}

// parsePostList batches parsePost over a slice of raw items.
func parsePostList(raws []json.RawMessage, cursor string, more bool) (Page[*Post], error) {
	out := make([]*Post, 0, len(raws))
	for _, r := range raws {
		p, err := parsePost(r)
		if err != nil {
			return Page[*Post]{}, err
		}
		out = append(out, p)
	}
	return Page[*Post]{Items: out, NextCursor: cursor, HasMore: more}, nil
}

// parsePost normalises a media item from any of the read endpoints.
func parsePost(raw json.RawMessage) (*Post, error) {
	var aux struct {
		ID          string `json:"id"`
		PK          any    `json:"pk"`
		PKID        string `json:"pk_id"`
		Code        string `json:"code"`
		MediaType   int    `json:"media_type"`
		ProductType string `json:"product_type"`
		TakenAt     int64  `json:"taken_at"`

		Caption *struct {
			Text   string `json:"text"`
			UserID any    `json:"user_id"`
		} `json:"caption"`

		User json.RawMessage `json:"user"`

		LikeCount      any `json:"like_count"`
		CommentCount   any `json:"comment_count"`
		ViewCount      any `json:"view_count"`
		PlayCount      any `json:"play_count"`
		IGTVViewCount  any `json:"igtv_view_count"`
		ReshareCount   any `json:"reshare_count"`
		SaveCount      any `json:"save_count"`
		OriginalWidth  any `json:"original_width"`
		OriginalHeight any `json:"original_height"`
		VideoDuration  any `json:"video_duration"`

		HasLiked      bool `json:"has_liked"`
		IsPinned      bool `json:"is_pinned"`
		IsPaidPartner bool `json:"is_paid_partnership"`

		ImageVersions2 *struct {
			Candidates []ImageVersion `json:"candidates"`
		} `json:"image_versions2"`
		VideoVersions []VideoVersion `json:"video_versions"`

		CarouselMedia []json.RawMessage `json:"carousel_media"`

		Location json.RawMessage `json:"location"`

		ClipsMetadata json.RawMessage `json:"clips_metadata"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse post: %v", ErrUnexpectedResponse, err)
	}

	pk := stringifyID(aux.PKID, aux.PK)
	if pk == "" && aux.ID != "" {
		// id is sometimes "<pk>_<owner_pk>"
		if idx := strings.IndexByte(aux.ID, '_'); idx > 0 {
			pk = aux.ID[:idx]
		} else {
			pk = aux.ID
		}
	}

	p := &Post{
		ID:             aux.ID,
		PK:             pk,
		Code:           aux.Code,
		MediaType:      MediaType(aux.MediaType),
		ProductType:    aux.ProductType,
		TakenAt:        aux.TakenAt,
		LikeCount:      anyToInt(aux.LikeCount),
		CommentCount:   anyToInt(aux.CommentCount),
		ViewCount:      anyToInt(aux.ViewCount),
		PlayCount:      anyToInt(aux.PlayCount),
		IGTVViewCount:  anyToInt(aux.IGTVViewCount),
		ReshareCount:   anyToInt(aux.ReshareCount),
		SaveCount:      anyToInt(aux.SaveCount),
		OriginalWidth:  anyToInt(aux.OriginalWidth),
		OriginalHeight: anyToInt(aux.OriginalHeight),
		VideoDurationS: anyToFloat(aux.VideoDuration),
		HasLiked:       aux.HasLiked,
		IsPinned:       aux.IsPinned,
		IsPaidPartner:  aux.IsPaidPartner,
		Raw:            raw,
	}
	if aux.Caption != nil {
		p.Caption = aux.Caption.Text
		p.CaptionUserID = stringifyID(aux.Caption.UserID)
	}
	if aux.ImageVersions2 != nil {
		p.ImageVersions = aux.ImageVersions2.Candidates
	}
	p.VideoVersions = aux.VideoVersions

	if len(aux.User) > 0 && string(aux.User) != "null" {
		if u, err := parseUser(aux.User); err == nil {
			p.Owner = u
		}
	}
	if len(aux.Location) > 0 && string(aux.Location) != "null" {
		var loc Location
		if err := json.Unmarshal(aux.Location, &loc); err == nil {
			loc.Raw = aux.Location
			p.Location = &loc
		}
	}
	if len(aux.ClipsMetadata) > 0 && string(aux.ClipsMetadata) != "null" {
		var cm ClipsMetadata
		if err := json.Unmarshal(aux.ClipsMetadata, &cm); err == nil {
			p.ClipsMetadata = &cm
		}
	}
	for _, child := range aux.CarouselMedia {
		cp, err := parsePost(child)
		if err != nil {
			continue
		}
		p.CarouselMedia = append(p.CarouselMedia, cp)
	}

	if p.Code != "" {
		path := "p"
		if p.ProductType == "clips" {
			path = "reel"
		}
		p.PermalinkURL = baseURL + "/" + path + "/" + p.Code + "/"
	}
	if p.Caption != "" {
		p.Hashtags = extractTokens(p.Caption, '#')
		p.Mentions = extractTokens(p.Caption, '@')
	}
	return p, nil
}

func anyToFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}

// extractTokens returns lowercased hashtag or mention tokens from text.
func extractTokens(text string, prefix byte) []string {
	if !strings.ContainsRune(text, rune(prefix)) {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for i := 0; i < len(text); i++ {
		if text[i] != prefix {
			continue
		}
		j := i + 1
		for j < len(text) && isTokenChar(text[j]) {
			j++
		}
		if j == i+1 {
			continue
		}
		tok := strings.ToLower(text[i+1 : j])
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
		i = j - 1
	}
	return out
}

func isTokenChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_' || b == '.':
		return true
	}
	return false
}
