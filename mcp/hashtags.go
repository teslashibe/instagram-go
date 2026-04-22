package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetHashtagInput is the typed input for instagram_get_hashtag.
type GetHashtagInput struct {
	Name string `json:"name" jsonschema:"description=hashtag name (without the leading #),required"`
}

func getHashtag(ctx context.Context, c *instagram.Client, in GetHashtagInput) (any, error) {
	return c.GetHashtag(ctx, in.Name)
}

// GetHashtagPostsInput is the typed input for instagram_get_hashtag_posts.
type GetHashtagPostsInput struct {
	Name  string `json:"name" jsonschema:"description=hashtag name (without the leading #),required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=maximum recent posts to return,minimum=1,maximum=50,default=12"`
}

func getHashtagPosts(ctx context.Context, c *instagram.Client, in GetHashtagPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetHashtagPosts(in.Name), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetHashtagTopPostsInput is the typed input for instagram_get_hashtag_top_posts.
type GetHashtagTopPostsInput struct {
	Name  string `json:"name" jsonschema:"description=hashtag name (without the leading #),required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=maximum top-ranked posts to return,minimum=1,maximum=50,default=12"`
}

func getHashtagTopPosts(ctx context.Context, c *instagram.Client, in GetHashtagTopPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetHashtagTopPosts(in.Name), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetHashtagClipsInput is the typed input for instagram_get_hashtag_clips.
type GetHashtagClipsInput struct {
	Name  string `json:"name" jsonschema:"description=hashtag name (without the leading #),required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=maximum reels to return,minimum=1,maximum=50,default=12"`
}

func getHashtagClips(ctx context.Context, c *instagram.Client, in GetHashtagClipsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetHashtagClips(in.Name), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// FollowHashtagInput is the typed input for instagram_follow_hashtag.
type FollowHashtagInput struct {
	Name string `json:"name" jsonschema:"description=hashtag name to follow (without the leading #),required"`
}

func followHashtag(ctx context.Context, c *instagram.Client, in FollowHashtagInput) (any, error) {
	if err := c.FollowHashtag(ctx, in.Name); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": in.Name}, nil
}

// UnfollowHashtagInput is the typed input for instagram_unfollow_hashtag.
type UnfollowHashtagInput struct {
	Name string `json:"name" jsonschema:"description=hashtag name to unfollow (without the leading #),required"`
}

func unfollowHashtag(ctx context.Context, c *instagram.Client, in UnfollowHashtagInput) (any, error) {
	if err := c.UnfollowHashtag(ctx, in.Name); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": in.Name}, nil
}

var hashtagTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetHashtagInput](
		"instagram_get_hashtag",
		"Fetch metadata for an Instagram hashtag",
		"GetHashtag",
		getHashtag,
	),
	mcptool.Define[*instagram.Client, GetHashtagPostsInput](
		"instagram_get_hashtag_posts",
		"Fetch recent posts under an Instagram hashtag",
		"GetHashtagPosts",
		getHashtagPosts,
	),
	mcptool.Define[*instagram.Client, GetHashtagTopPostsInput](
		"instagram_get_hashtag_top_posts",
		"Fetch top (algorithmically ranked) posts under an Instagram hashtag",
		"GetHashtagTopPosts",
		getHashtagTopPosts,
	),
	mcptool.Define[*instagram.Client, GetHashtagClipsInput](
		"instagram_get_hashtag_clips",
		"Fetch reels (clips) tagged with an Instagram hashtag",
		"GetHashtagClips",
		getHashtagClips,
	),
	mcptool.Define[*instagram.Client, FollowHashtagInput](
		"instagram_follow_hashtag",
		"Follow an Instagram hashtag",
		"FollowHashtag",
		followHashtag,
	),
	mcptool.Define[*instagram.Client, UnfollowHashtagInput](
		"instagram_unfollow_hashtag",
		"Unfollow an Instagram hashtag",
		"UnfollowHashtag",
		unfollowHashtag,
	),
}
