package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetFollowers iterates over a user's followers.
//
// Endpoint: GET /api/v1/friendships/<user_id>/followers/?count=12&max_id=<cursor>
//
// Note: pagination is best-effort. Instagram caps deep follower lists for
// large accounts, and very high page indices are silently truncated.
func (c *Client) GetFollowers(userID string) *Iterator[*User] {
	return c.friendshipListIterator(userID, "followers")
}

// GetFollowing iterates over the accounts a user is following.
//
// Endpoint: GET /api/v1/friendships/<user_id>/following/
func (c *Client) GetFollowing(userID string) *Iterator[*User] {
	return c.friendshipListIterator(userID, "following")
}

func (c *Client) friendshipListIterator(userID, kind string) *Iterator[*User] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*User], error) {
		q := url.Values{}
		q.Set("count", "12")
		q.Set("search_surface", "follow_list_page")
		if cursor != "" {
			q.Set("max_id", cursor)
		}
		var resp struct {
			Users     []json.RawMessage `json:"users"`
			NextMaxID any               `json:"next_max_id"`
			BigList   bool              `json:"big_list"`
			PageSize  int               `json:"page_size"`
			Status    string            `json:"status"`
		}
		path := "/api/v1/friendships/" + userID + "/" + kind + "/"
		if err := c.doJSON(ctx, "GET", path, q, nil, &resp); err != nil {
			return Page[*User]{}, err
		}
		out := make([]*User, 0, len(resp.Users))
		for _, raw := range resp.Users {
			u, err := parseUser(raw)
			if err != nil {
				return Page[*User]{}, err
			}
			out = append(out, u)
		}
		next := stringifyID(resp.NextMaxID)
		return Page[*User]{
			Items:      out,
			NextCursor: next,
			HasMore:    next != "",
		}, nil
	})
}

// GetFriendship returns the viewer's relationship with one user.
//
// Endpoint: GET /api/v1/friendships/show/<user_id>/
func (c *Client) GetFriendship(ctx context.Context, userID string) (*FriendshipStatus, error) {
	var resp FriendshipStatus
	if err := c.doJSON(ctx, "GET", "/api/v1/friendships/show/"+userID+"/", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetFriendships returns relationship status with many users in one call.
//
// Endpoint: POST /api/v1/friendships/show_many/  with form: user_ids=1,2,3
func (c *Client) GetFriendships(ctx context.Context, userIDs []string) (map[string]*FriendshipStatus, error) {
	if len(userIDs) == 0 {
		return map[string]*FriendshipStatus{}, nil
	}
	form := url.Values{}
	form.Set("_csrftoken", c.cookies.CSRFToken)
	form.Set("user_ids", joinStrings(userIDs, ","))
	var resp struct {
		FriendshipStatuses map[string]*FriendshipStatus `json:"friendship_statuses"`
		Status             string                       `json:"status"`
	}
	if err := c.doJSON(ctx, "POST", "/api/v1/friendships/show_many/", nil, &requestOptions{
		FormBody: form,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.FriendshipStatuses, nil
}

// Follow follows a user.
//
// Endpoint: POST /api/v1/friendships/create/<user_id>/
func (c *Client) Follow(ctx context.Context, userID string) (*FriendshipStatus, error) {
	return c.friendshipWrite(ctx, "create", userID)
}

// Unfollow unfollows a user.
//
// Endpoint: POST /api/v1/friendships/destroy/<user_id>/
func (c *Client) Unfollow(ctx context.Context, userID string) (*FriendshipStatus, error) {
	return c.friendshipWrite(ctx, "destroy", userID)
}

// Block blocks a user.
//
// Endpoint: POST /api/v1/friendships/block/<user_id>/
//
// Note: this and the other tier-2 friendship writes (Mute, SetBesties) are
// often soft-blocked on web sessions. See ErrWriteSoftBlock.
func (c *Client) Block(ctx context.Context, userID string) (*FriendshipStatus, error) {
	return c.friendshipWrite(ctx, "block", userID)
}

// Unblock unblocks a user.
//
// Endpoint: POST /api/v1/friendships/unblock/<user_id>/
func (c *Client) Unblock(ctx context.Context, userID string) (*FriendshipStatus, error) {
	return c.friendshipWrite(ctx, "unblock", userID)
}

// MutePosts mutes the posts of a user without unfollowing.
//
// Endpoint: POST /api/v1/friendships/mute_posts_or_story_from_follow/
func (c *Client) MutePosts(ctx context.Context, userID string) error {
	form := url.Values{}
	form.Set("target_posts_author_id", userID)
	return c.doJSON(ctx, "POST", "/api/v1/friendships/mute_posts_or_story_from_follow/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, nil)
}

// UnmutePosts undoes a previous MutePosts.
//
// Endpoint: POST /api/v1/friendships/unmute_posts_or_story_from_follow/
func (c *Client) UnmutePosts(ctx context.Context, userID string) error {
	form := url.Values{}
	form.Set("target_posts_author_id", userID)
	return c.doJSON(ctx, "POST", "/api/v1/friendships/unmute_posts_or_story_from_follow/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, nil)
}

func (c *Client) friendshipWrite(ctx context.Context, action, userID string) (*FriendshipStatus, error) {
	if userID == "" {
		return nil, fmt.Errorf("instagram: %s: userID required", action)
	}
	form := url.Values{}
	form.Set("user_id", userID)
	form.Set("radio_type", "wifi-none")
	form.Set("device_id", "android-"+c.cookies.DSUserID)
	var resp struct {
		FriendshipStatus *FriendshipStatus `json:"friendship_status"`
		Status           string            `json:"status"`
	}
	path := "/api/v1/friendships/" + action + "/" + userID + "/"
	if err := c.doJSON(ctx, "POST", path, nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.FriendshipStatus, nil
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += sep + s
	}
	return out
}
