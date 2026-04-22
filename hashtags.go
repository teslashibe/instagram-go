package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// GetHashtag fetches the metadata for a hashtag.
//
// Endpoint: GET /api/v1/tags/web_info/?tag_name=<name>
func (c *Client) GetHashtag(ctx context.Context, name string) (*Hashtag, error) {
	name = strings.TrimPrefix(strings.ToLower(name), "#")
	if name == "" {
		return nil, fmt.Errorf("instagram: GetHashtag: name required")
	}
	q := url.Values{}
	q.Set("tag_name", name)
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Status string          `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/tags/web_info/", q, &requestOptions{
		Referer: baseURL + "/explore/tags/" + name + "/",
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return nil, fmt.Errorf("%w: hashtag %q", ErrNotFound, name)
	}
	var tag Hashtag
	if err := json.Unmarshal(resp.Data, &tag); err != nil {
		return nil, fmt.Errorf("%w: parse hashtag: %v", ErrUnexpectedResponse, err)
	}
	if tag.Name == "" {
		tag.Name = name
	}
	tag.Raw = resp.Data
	return &tag, nil
}

// GetHashtagPosts iterates over the recent posts under a hashtag.
//
// Endpoint: GET /api/v1/tags/<tag>/sections/?tab=recent
func (c *Client) GetHashtagPosts(name string) *Iterator[*Post] {
	return c.hashtagPosts(name, "recent")
}

// GetHashtagTopPosts iterates over the top (algorithmically ranked) posts
// under a hashtag.
//
// Endpoint: GET /api/v1/tags/<tag>/sections/?tab=top
func (c *Client) GetHashtagTopPosts(name string) *Iterator[*Post] {
	return c.hashtagPosts(name, "top")
}

// GetHashtagClips iterates over reels (clips) under a hashtag.
//
// Endpoint: GET /api/v1/tags/<tag>/sections/?tab=clips
func (c *Client) GetHashtagClips(name string) *Iterator[*Post] {
	return c.hashtagPosts(name, "clips")
}

func (c *Client) hashtagPosts(name, tab string) *Iterator[*Post] {
	name = strings.TrimPrefix(strings.ToLower(name), "#")
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		form := url.Values{}
		form.Set("tab", tab)
		form.Set("include_persistent", "true")
		if cursor != "" {
			form.Set("max_id", cursor)
			form.Set("page", "1")
			form.Set("next_media_ids", "[]")
		}
		var resp struct {
			Sections []struct {
				LayoutContent struct {
					Medias []struct {
						Media json.RawMessage `json:"media"`
					} `json:"medias"`
				} `json:"layout_content"`
			} `json:"sections"`
			NextMaxID     string `json:"next_max_id"`
			MoreAvailable bool   `json:"more_available"`
			Status        string `json:"status"`
		}
		path := "/api/v1/tags/" + url.PathEscape(name) + "/sections/"
		if err := c.doJSON(ctx, "POST", path, nil, &requestOptions{
			FormBody: form,
			Referer:  baseURL + "/explore/tags/" + name + "/",
		}, &resp); err != nil {
			return Page[*Post]{}, err
		}
		var raws []json.RawMessage
		for _, sec := range resp.Sections {
			for _, m := range sec.LayoutContent.Medias {
				if len(m.Media) > 0 {
					raws = append(raws, m.Media)
				}
			}
		}
		return parsePostList(raws, resp.NextMaxID, resp.MoreAvailable)
	})
}

// FollowHashtag starts following a hashtag.
//
// Endpoint: POST /api/v1/web/tags/follow/<name>/
//
// Note: subject to write rate-limiting; see ErrWriteSoftBlock.
func (c *Client) FollowHashtag(ctx context.Context, name string) error {
	name = strings.TrimPrefix(strings.ToLower(name), "#")
	return c.doJSON(ctx, "POST", "/api/v1/web/tags/follow/"+name+"/", nil, &requestOptions{IsWrite: true}, nil)
}

// UnfollowHashtag stops following a hashtag.
//
// Endpoint: POST /api/v1/web/tags/unfollow/<name>/
func (c *Client) UnfollowHashtag(ctx context.Context, name string) error {
	name = strings.TrimPrefix(strings.ToLower(name), "#")
	return c.doJSON(ctx, "POST", "/api/v1/web/tags/unfollow/"+name+"/", nil, &requestOptions{IsWrite: true}, nil)
}
