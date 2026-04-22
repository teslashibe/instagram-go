package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetFollowersInput is the typed input for instagram_get_followers.
type GetFollowersInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose followers to fetch,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum followers to return,minimum=1,maximum=50,default=12"`
}

func getFollowers(ctx context.Context, c *instagram.Client, in GetFollowersInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetFollowers(in.UserID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetFollowingInput is the typed input for instagram_get_following.
type GetFollowingInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose following list to fetch,required"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum users to return,minimum=1,maximum=50,default=12"`
}

func getFollowing(ctx context.Context, c *instagram.Client, in GetFollowingInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetFollowing(in.UserID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetFriendshipInput is the typed input for instagram_get_friendship.
type GetFriendshipInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID to query relationship with,required"`
}

func getFriendship(ctx context.Context, c *instagram.Client, in GetFriendshipInput) (any, error) {
	return c.GetFriendship(ctx, in.UserID)
}

// GetFriendshipsInput is the typed input for instagram_get_friendships.
type GetFriendshipsInput struct {
	UserIDs []string `json:"user_ids" jsonschema:"description=numeric Instagram user IDs to bulk-query relationship status with,required"`
}

func getFriendships(ctx context.Context, c *instagram.Client, in GetFriendshipsInput) (any, error) {
	return c.GetFriendships(ctx, in.UserIDs)
}

// FollowInput is the typed input for instagram_follow.
type FollowInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID to follow,required"`
}

func follow(ctx context.Context, c *instagram.Client, in FollowInput) (any, error) {
	status, err := c.Follow(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID, "friendship": status}, nil
}

// UnfollowInput is the typed input for instagram_unfollow.
type UnfollowInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID to unfollow,required"`
}

func unfollow(ctx context.Context, c *instagram.Client, in UnfollowInput) (any, error) {
	status, err := c.Unfollow(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID, "friendship": status}, nil
}

// BlockInput is the typed input for instagram_block.
type BlockInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID to block,required"`
}

func block(ctx context.Context, c *instagram.Client, in BlockInput) (any, error) {
	status, err := c.Block(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID, "friendship": status}, nil
}

// UnblockInput is the typed input for instagram_unblock.
type UnblockInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID to unblock,required"`
}

func unblock(ctx context.Context, c *instagram.Client, in UnblockInput) (any, error) {
	status, err := c.Unblock(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID, "friendship": status}, nil
}

// MutePostsInput is the typed input for instagram_mute_posts.
type MutePostsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose posts to mute (without unfollowing),required"`
}

func mutePosts(ctx context.Context, c *instagram.Client, in MutePostsInput) (any, error) {
	if err := c.MutePosts(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

// UnmutePostsInput is the typed input for instagram_unmute_posts.
type UnmutePostsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose posts to unmute,required"`
}

func unmutePosts(ctx context.Context, c *instagram.Client, in UnmutePostsInput) (any, error) {
	if err := c.UnmutePosts(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

var followerTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetFollowersInput](
		"instagram_get_followers",
		"Fetch a user's followers (most recent first)",
		"GetFollowers",
		getFollowers,
	),
	mcptool.Define[*instagram.Client, GetFollowingInput](
		"instagram_get_following",
		"Fetch the accounts a user is following",
		"GetFollowing",
		getFollowing,
	),
	mcptool.Define[*instagram.Client, GetFriendshipInput](
		"instagram_get_friendship",
		"Fetch the viewer's relationship with one Instagram user",
		"GetFriendship",
		getFriendship,
	),
	mcptool.Define[*instagram.Client, GetFriendshipsInput](
		"instagram_get_friendships",
		"Bulk-fetch the viewer's relationship status with multiple Instagram users in one call",
		"GetFriendships",
		getFriendships,
	),
	mcptool.Define[*instagram.Client, FollowInput](
		"instagram_follow",
		"Follow an Instagram user",
		"Follow",
		follow,
	),
	mcptool.Define[*instagram.Client, UnfollowInput](
		"instagram_unfollow",
		"Unfollow an Instagram user",
		"Unfollow",
		unfollow,
	),
	mcptool.Define[*instagram.Client, BlockInput](
		"instagram_block",
		"Block an Instagram user",
		"Block",
		block,
	),
	mcptool.Define[*instagram.Client, UnblockInput](
		"instagram_unblock",
		"Unblock an Instagram user",
		"Unblock",
		unblock,
	),
	mcptool.Define[*instagram.Client, MutePostsInput](
		"instagram_mute_posts",
		"Mute the posts of an Instagram user without unfollowing",
		"MutePosts",
		mutePosts,
	),
	mcptool.Define[*instagram.Client, UnmutePostsInput](
		"instagram_unmute_posts",
		"Unmute the posts of a previously muted Instagram user",
		"UnmutePosts",
		unmutePosts,
	),
}
