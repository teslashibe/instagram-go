package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetPostInput is the typed input for instagram_get_post.
type GetPostInput struct {
	Shortcode string `json:"shortcode" jsonschema:"description=post shortcode (the slug between /p/ or /reel/ and the trailing slash),required"`
}

func getPost(ctx context.Context, c *instagram.Client, in GetPostInput) (any, error) {
	return c.GetPost(ctx, in.Shortcode)
}

// GetPostByIDInput is the typed input for instagram_get_post_by_id.
type GetPostByIDInput struct {
	MediaID string `json:"media_id" jsonschema:"description=numeric media ID (Post.PK),required"`
}

func getPostByID(ctx context.Context, c *instagram.Client, in GetPostByIDInput) (any, error) {
	return c.GetPostByID(ctx, in.MediaID)
}

// GetPostsInput is the typed input for instagram_get_posts.
type GetPostsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose timeline posts to fetch,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum posts to return,minimum=1,maximum=50,default=12"`
}

func getPosts(ctx context.Context, c *instagram.Client, in GetPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetPosts(in.UserID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetReelsInput is the typed input for instagram_get_reels.
type GetReelsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose reels to fetch,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum reels to return,minimum=1,maximum=50,default=12"`
}

func getReels(ctx context.Context, c *instagram.Client, in GetReelsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetReels(in.UserID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetTaggedPostsInput is the typed input for instagram_get_tagged_posts.
type GetTaggedPostsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID; returns posts the user is tagged in,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum posts to return,minimum=1,maximum=50,default=12"`
}

func getTaggedPosts(ctx context.Context, c *instagram.Client, in GetTaggedPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetTaggedPosts(in.UserID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetTimelineInput is the typed input for instagram_get_timeline.
type GetTimelineInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=maximum posts to return from the home timeline,minimum=1,maximum=50,default=12"`
}

func getTimeline(ctx context.Context, c *instagram.Client, in GetTimelineInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetTimeline(), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetExploreInput is the typed input for instagram_get_explore.
type GetExploreInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=maximum posts to return from the Explore feed,minimum=1,maximum=50,default=12"`
}

func getExplore(ctx context.Context, c *instagram.Client, in GetExploreInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetExplore(), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

var postTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetPostInput](
		"instagram_get_post",
		"Fetch an Instagram post by its shortcode",
		"GetPost",
		getPost,
	),
	mcptool.Define[*instagram.Client, GetPostByIDInput](
		"instagram_get_post_by_id",
		"Fetch an Instagram post by its numeric media ID",
		"GetPostByID",
		getPostByID,
	),
	mcptool.Define[*instagram.Client, GetPostsInput](
		"instagram_get_posts",
		"Fetch a user's recent timeline posts (most recent first)",
		"GetPosts",
		getPosts,
	),
	mcptool.Define[*instagram.Client, GetReelsInput](
		"instagram_get_reels",
		"Fetch a user's recent reels (clips)",
		"GetReels",
		getReels,
	),
	mcptool.Define[*instagram.Client, GetTaggedPostsInput](
		"instagram_get_tagged_posts",
		"Fetch posts the given user has been tagged in",
		"GetTaggedPosts",
		getTaggedPosts,
	),
	mcptool.Define[*instagram.Client, GetTimelineInput](
		"instagram_get_timeline",
		"Fetch the authenticated user's home timeline (Following + recommendations)",
		"GetTimeline",
		getTimeline,
	),
	mcptool.Define[*instagram.Client, GetExploreInput](
		"instagram_get_explore",
		"Fetch posts from the authenticated user's Explore feed",
		"GetExplore",
		getExplore,
	),
}
