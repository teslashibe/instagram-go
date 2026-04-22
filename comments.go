package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// GetComments iterates over the top-level comments on a post.
//
// mediaPK is the numeric pk of the post (Post.PK or Post.ID up to '_').
//
// Endpoint: GET /api/v1/media/<media_pk>/comments/
func (c *Client) GetComments(mediaPK string) *Iterator[*Comment] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Comment], error) {
		q := url.Values{}
		q.Set("can_support_threading", "true")
		q.Set("permalink_enabled", "false")
		if cursor != "" {
			q.Set("min_id", cursor)
		}
		var resp struct {
			Comments        []json.RawMessage `json:"comments"`
			NextMinID       string            `json:"next_min_id"`
			NextMaxID       string            `json:"next_max_id"`
			HasMoreComments bool              `json:"has_more_comments"`
			Status          string            `json:"status"`
		}
		if err := c.doJSON(ctx, "GET", "/api/v1/media/"+mediaPK+"/comments/", q, nil, &resp); err != nil {
			return Page[*Comment]{}, err
		}
		out, err := parseCommentList(resp.Comments)
		if err != nil {
			return Page[*Comment]{}, err
		}
		next := resp.NextMinID
		if next == "" {
			next = resp.NextMaxID
		}
		return Page[*Comment]{
			Items:      out,
			NextCursor: next,
			HasMore:    resp.HasMoreComments,
		}, nil
	})
}

// GetCommentReplies iterates over the child replies under a parent comment.
//
// Endpoint: GET /api/v1/media/<media_pk>/comments/<parent_id>/child_comments/
func (c *Client) GetCommentReplies(mediaPK, parentID string) *Iterator[*Comment] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Comment], error) {
		q := url.Values{}
		if cursor != "" {
			q.Set("max_id", cursor)
		}
		var resp struct {
			ChildComments []json.RawMessage `json:"child_comments"`
			NextMaxID     string            `json:"next_max_id"`
			HasMore       bool              `json:"has_more_head_child_comments"`
			Status        string            `json:"status"`
		}
		path := "/api/v1/media/" + mediaPK + "/comments/" + parentID + "/child_comments/"
		if err := c.doJSON(ctx, "GET", path, q, nil, &resp); err != nil {
			return Page[*Comment]{}, err
		}
		out, err := parseCommentList(resp.ChildComments)
		if err != nil {
			return Page[*Comment]{}, err
		}
		return Page[*Comment]{
			Items:      out,
			NextCursor: resp.NextMaxID,
			HasMore:    resp.HasMore,
		}, nil
	})
}

// GetLikers fetches the users who liked a post.
//
// Endpoint: GET /api/v1/media/<media_pk>/likers/
//
// Note: this is a one-shot endpoint — Instagram does not paginate likers.
// For posts with very many likes, only the first ~1000 are returned.
func (c *Client) GetLikers(ctx context.Context, mediaPK string) ([]*User, error) {
	var resp struct {
		Users  []json.RawMessage `json:"users"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/media/"+mediaPK+"/likers/", nil, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(resp.Users))
	for _, raw := range resp.Users {
		u, err := parseUser(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// GetCommentLikers fetches the users who liked a specific comment on a post.
//
// Endpoint: GET /api/v1/media/<media_pk>/comment_likers/?comment_id=<id>
func (c *Client) GetCommentLikers(ctx context.Context, mediaPK, commentID string) ([]*User, error) {
	if mediaPK == "" || commentID == "" {
		return nil, fmt.Errorf("instagram: GetCommentLikers: mediaPK and commentID required")
	}
	q := url.Values{}
	q.Set("comment_id", commentID)
	var resp struct {
		Users  []json.RawMessage `json:"users"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/media/"+mediaPK+"/comment_likers/", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(resp.Users))
	for _, raw := range resp.Users {
		u, err := parseUser(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func parseCommentList(raws []json.RawMessage) ([]*Comment, error) {
	out := make([]*Comment, 0, len(raws))
	for _, r := range raws {
		c, err := parseComment(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseComment(raw json.RawMessage) (*Comment, error) {
	var aux struct {
		PK              any               `json:"pk"`
		UserID          any               `json:"user_id"`
		User            json.RawMessage   `json:"user"`
		Text            string            `json:"text"`
		CreatedAt       int64             `json:"created_at"`
		LikeCount       any               `json:"comment_like_count"`
		HasLiked        bool              `json:"has_liked_comment"`
		ChildCount      any               `json:"child_comment_count"`
		ParentCommentID any               `json:"parent_comment_id"`
		ChildComments   []json.RawMessage `json:"preview_child_comments"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse comment: %v", ErrUnexpectedResponse, err)
	}
	c := &Comment{
		ID:                stringifyID(aux.PK),
		UserID:            stringifyID(aux.UserID),
		Text:              aux.Text,
		CreatedAt:         aux.CreatedAt,
		LikeCount:         anyToInt(aux.LikeCount),
		HasLikedComment:   aux.HasLiked,
		ChildCommentCount: anyToInt(aux.ChildCount),
		ParentCommentID:   stringifyID(aux.ParentCommentID),
		Raw:               raw,
	}
	if c.ID == "" {
		var aux2 struct {
			PK string `json:"pk"`
		}
		_ = json.Unmarshal(raw, &aux2)
		if aux2.PK != "" {
			c.ID = aux2.PK
		}
	}
	if len(aux.User) > 0 && string(aux.User) != "null" {
		if u, err := parseUser(aux.User); err == nil {
			c.User = u
		}
	}
	for _, child := range aux.ChildComments {
		cc, err := parseComment(child)
		if err == nil {
			c.Replies = append(c.Replies, cc)
		}
	}
	return c, nil
}

// PostComment leaves a top-level comment on a post.
//
// Endpoint: POST /api/v1/media/<media_pk>/comment/
//
// Subject to write rate-limiting; Instagram is aggressive about silent
// soft-blocks here. See ErrWriteSoftBlock.
func (c *Client) PostComment(ctx context.Context, mediaPK, text string) (*Comment, error) {
	if mediaPK == "" || text == "" {
		return nil, fmt.Errorf("instagram: PostComment: mediaPK and text required")
	}
	form := url.Values{}
	form.Set("comment_text", text)
	form.Set("idempotence_token", strconv.FormatInt(int64(len(text))*7919, 10))
	form.Set("nav_chain", "Profile:profile:1:profile_action_bar")
	form.Set("delivery_class", "organic")
	form.Set("device_id", "android-"+c.cookies.DSUserID)

	var resp struct {
		Comment json.RawMessage `json:"comment"`
		Status  string          `json:"status"`
	}
	if err := c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/comment/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Comment) == 0 {
		return nil, fmt.Errorf("%w: comment endpoint returned no comment body", ErrUnexpectedResponse)
	}
	return parseComment(resp.Comment)
}

// LikeComment likes a comment.
//
// Endpoint: POST /api/v1/media/<comment_id>/comment_like/
func (c *Client) LikeComment(ctx context.Context, commentID string) error {
	return c.doJSON(ctx, "POST", "/api/v1/media/"+commentID+"/comment_like/", nil, &requestOptions{IsWrite: true}, nil)
}

// UnlikeComment removes a like from a comment.
//
// Endpoint: POST /api/v1/media/<comment_id>/comment_unlike/
func (c *Client) UnlikeComment(ctx context.Context, commentID string) error {
	return c.doJSON(ctx, "POST", "/api/v1/media/"+commentID+"/comment_unlike/", nil, &requestOptions{IsWrite: true}, nil)
}

// DeleteComment deletes a comment authored by the viewer (or on the viewer's post).
//
// Endpoint: POST /api/v1/media/<media_pk>/comment/<comment_id>/delete/
func (c *Client) DeleteComment(ctx context.Context, mediaPK, commentID string) error {
	return c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/comment/"+commentID+"/delete/", nil, &requestOptions{IsWrite: true}, nil)
}

// LikePost likes a post.
//
// Endpoint: POST /api/v1/media/<media_pk>/like/
func (c *Client) LikePost(ctx context.Context, mediaPK string) error {
	form := url.Values{}
	form.Set("media_id", mediaPK)
	form.Set("nav_chain", "Profile:profile:1:profile_action_bar,Feed:feed_timeline:1:swipe")
	form.Set("delivery_class", "organic")
	return c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/like/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, nil)
}

// UnlikePost removes a like from a post.
//
// Endpoint: POST /api/v1/media/<media_pk>/unlike/
func (c *Client) UnlikePost(ctx context.Context, mediaPK string) error {
	form := url.Values{}
	form.Set("media_id", mediaPK)
	return c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/unlike/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, nil)
}

// SavePost saves a post to the user's collection.
//
// Endpoint: POST /api/v1/media/<media_pk>/save/
func (c *Client) SavePost(ctx context.Context, mediaPK string) error {
	return c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/save/", nil, &requestOptions{IsWrite: true}, nil)
}

// UnsavePost removes a post from the user's saved collection.
//
// Endpoint: POST /api/v1/media/<media_pk>/unsave/
func (c *Client) UnsavePost(ctx context.Context, mediaPK string) error {
	return c.doJSON(ctx, "POST", "/api/v1/media/"+mediaPK+"/unsave/", nil, &requestOptions{IsWrite: true}, nil)
}
