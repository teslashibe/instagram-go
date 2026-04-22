package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetCommentsInput is the typed input for instagram_get_comments.
type GetCommentsInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID (Post.PK),required"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=maximum top-level comments to return,minimum=1,maximum=50,default=12"`
}

func getComments(ctx context.Context, c *instagram.Client, in GetCommentsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetComments(in.MediaPK), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetCommentRepliesInput is the typed input for instagram_get_comment_replies.
type GetCommentRepliesInput struct {
	MediaPK  string `json:"media_pk" jsonschema:"description=numeric post ID (Post.PK),required"`
	ParentID string `json:"parent_id" jsonschema:"description=parent comment ID to fetch replies under,required"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=maximum reply comments to return,minimum=1,maximum=50,default=12"`
}

func getCommentReplies(ctx context.Context, c *instagram.Client, in GetCommentRepliesInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetCommentReplies(in.MediaPK, in.ParentID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetLikersInput is the typed input for instagram_get_likers.
type GetLikersInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID (Post.PK),required"`
}

func getLikers(ctx context.Context, c *instagram.Client, in GetLikersInput) (any, error) {
	res, err := c.GetLikers(ctx, in.MediaPK)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

// GetCommentLikersInput is the typed input for instagram_get_comment_likers.
type GetCommentLikersInput struct {
	MediaPK   string `json:"media_pk" jsonschema:"description=numeric post ID (Post.PK),required"`
	CommentID string `json:"comment_id" jsonschema:"description=comment ID whose likers to fetch,required"`
}

func getCommentLikers(ctx context.Context, c *instagram.Client, in GetCommentLikersInput) (any, error) {
	res, err := c.GetCommentLikers(ctx, in.MediaPK, in.CommentID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

// PostCommentInput is the typed input for instagram_post_comment.
type PostCommentInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID to comment on,required"`
	Text    string `json:"text" jsonschema:"description=comment body (plain text),required"`
}

func postComment(ctx context.Context, c *instagram.Client, in PostCommentInput) (any, error) {
	res, err := c.PostComment(ctx, in.MediaPK, in.Text)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"ok": true, "media_pk": in.MediaPK}
	if res != nil {
		out["comment_id"] = res.ID
		out["comment"] = res
	}
	return out, nil
}

// LikeCommentInput is the typed input for instagram_like_comment.
type LikeCommentInput struct {
	CommentID string `json:"comment_id" jsonschema:"description=comment ID to like,required"`
}

func likeComment(ctx context.Context, c *instagram.Client, in LikeCommentInput) (any, error) {
	if err := c.LikeComment(ctx, in.CommentID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "comment_id": in.CommentID}, nil
}

// UnlikeCommentInput is the typed input for instagram_unlike_comment.
type UnlikeCommentInput struct {
	CommentID string `json:"comment_id" jsonschema:"description=comment ID to unlike,required"`
}

func unlikeComment(ctx context.Context, c *instagram.Client, in UnlikeCommentInput) (any, error) {
	if err := c.UnlikeComment(ctx, in.CommentID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "comment_id": in.CommentID}, nil
}

// DeleteCommentInput is the typed input for instagram_delete_comment.
type DeleteCommentInput struct {
	MediaPK   string `json:"media_pk" jsonschema:"description=numeric post ID the comment belongs to,required"`
	CommentID string `json:"comment_id" jsonschema:"description=comment ID to delete (must be authored by the viewer or on the viewer's post),required"`
}

func deleteComment(ctx context.Context, c *instagram.Client, in DeleteCommentInput) (any, error) {
	if err := c.DeleteComment(ctx, in.MediaPK, in.CommentID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "media_pk": in.MediaPK, "comment_id": in.CommentID}, nil
}

// LikePostInput is the typed input for instagram_like_post.
type LikePostInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID to like,required"`
}

func likePost(ctx context.Context, c *instagram.Client, in LikePostInput) (any, error) {
	if err := c.LikePost(ctx, in.MediaPK); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "media_pk": in.MediaPK}, nil
}

// UnlikePostInput is the typed input for instagram_unlike_post.
type UnlikePostInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID to unlike,required"`
}

func unlikePost(ctx context.Context, c *instagram.Client, in UnlikePostInput) (any, error) {
	if err := c.UnlikePost(ctx, in.MediaPK); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "media_pk": in.MediaPK}, nil
}

// SavePostInput is the typed input for instagram_save_post.
type SavePostInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID to save to the viewer's collection,required"`
}

func savePost(ctx context.Context, c *instagram.Client, in SavePostInput) (any, error) {
	if err := c.SavePost(ctx, in.MediaPK); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "media_pk": in.MediaPK}, nil
}

// UnsavePostInput is the typed input for instagram_unsave_post.
type UnsavePostInput struct {
	MediaPK string `json:"media_pk" jsonschema:"description=numeric post ID to remove from the viewer's saved collection,required"`
}

func unsavePost(ctx context.Context, c *instagram.Client, in UnsavePostInput) (any, error) {
	if err := c.UnsavePost(ctx, in.MediaPK); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "media_pk": in.MediaPK}, nil
}

var commentTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetCommentsInput](
		"instagram_get_comments",
		"Fetch top-level comments on an Instagram post",
		"GetComments",
		getComments,
	),
	mcptool.Define[*instagram.Client, GetCommentRepliesInput](
		"instagram_get_comment_replies",
		"Fetch reply comments under a parent Instagram comment",
		"GetCommentReplies",
		getCommentReplies,
	),
	mcptool.Define[*instagram.Client, GetLikersInput](
		"instagram_get_likers",
		"Fetch the users who liked a given Instagram post",
		"GetLikers",
		getLikers,
	),
	mcptool.Define[*instagram.Client, GetCommentLikersInput](
		"instagram_get_comment_likers",
		"Fetch the users who liked a specific comment on an Instagram post",
		"GetCommentLikers",
		getCommentLikers,
	),
	mcptool.Define[*instagram.Client, PostCommentInput](
		"instagram_post_comment",
		"Post a top-level comment on an Instagram post",
		"PostComment",
		postComment,
	),
	mcptool.Define[*instagram.Client, LikeCommentInput](
		"instagram_like_comment",
		"Like an Instagram comment",
		"LikeComment",
		likeComment,
	),
	mcptool.Define[*instagram.Client, UnlikeCommentInput](
		"instagram_unlike_comment",
		"Remove a like from an Instagram comment",
		"UnlikeComment",
		unlikeComment,
	),
	mcptool.Define[*instagram.Client, DeleteCommentInput](
		"instagram_delete_comment",
		"Delete a comment authored by the viewer or on the viewer's own post",
		"DeleteComment",
		deleteComment,
	),
	mcptool.Define[*instagram.Client, LikePostInput](
		"instagram_like_post",
		"Like an Instagram post",
		"LikePost",
		likePost,
	),
	mcptool.Define[*instagram.Client, UnlikePostInput](
		"instagram_unlike_post",
		"Remove a like from an Instagram post",
		"UnlikePost",
		unlikePost,
	),
	mcptool.Define[*instagram.Client, SavePostInput](
		"instagram_save_post",
		"Save an Instagram post to the viewer's saved collection",
		"SavePost",
		savePost,
	),
	mcptool.Define[*instagram.Client, UnsavePostInput](
		"instagram_unsave_post",
		"Remove an Instagram post from the viewer's saved collection",
		"UnsavePost",
		unsavePost,
	),
}
