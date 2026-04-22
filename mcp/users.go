package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// MeInput is the typed input for instagram_me.
type MeInput struct{}

func me(ctx context.Context, c *instagram.Client, _ MeInput) (any, error) {
	return c.Me(ctx)
}

// GetProfileInput is the typed input for instagram_get_profile.
type GetProfileInput struct {
	Username string `json:"username" jsonschema:"description=Instagram username (without the @),required"`
}

func getProfile(ctx context.Context, c *instagram.Client, in GetProfileInput) (any, error) {
	return c.GetProfile(ctx, in.Username)
}

// GetProfileByIDInput is the typed input for instagram_get_profile_by_id.
type GetProfileByIDInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID,required"`
}

func getProfileByID(ctx context.Context, c *instagram.Client, in GetProfileByIDInput) (any, error) {
	return c.GetProfileByID(ctx, in.UserID)
}

// SearchUsersInput is the typed input for instagram_search_users.
type SearchUsersInput struct {
	Query string `json:"query" jsonschema:"description=free-text query (username or full-name fragment),required"`
	Count int    `json:"count,omitempty" jsonschema:"description=results to request from Instagram (server clamps to 50),minimum=1,maximum=50,default=12"`
}

func searchUsers(ctx context.Context, c *instagram.Client, in SearchUsersInput) (any, error) {
	res, err := c.SearchUsers(ctx, in.Query, in.Count)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", effectiveLimit(in.Count)), nil
}

// GetSuggestedUsersInput is the typed input for instagram_get_suggested_users.
type GetSuggestedUsersInput struct {
	TargetID string `json:"target_id" jsonschema:"description=seed user ID; pass own user ID for personal suggestions or any user ID to get accounts related to that user,required"`
}

func getSuggestedUsers(ctx context.Context, c *instagram.Client, in GetSuggestedUsersInput) (any, error) {
	res, err := c.GetSuggestedUsers(ctx, in.TargetID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

var userTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, MeInput](
		"instagram_me",
		"Fetch the authenticated Instagram user's profile",
		"Me",
		me,
	),
	mcptool.Define[*instagram.Client, GetProfileInput](
		"instagram_get_profile",
		"Fetch an Instagram profile by username",
		"GetProfile",
		getProfile,
	),
	mcptool.Define[*instagram.Client, GetProfileByIDInput](
		"instagram_get_profile_by_id",
		"Fetch an Instagram profile by numeric user ID",
		"GetProfileByID",
		getProfileByID,
	),
	mcptool.Define[*instagram.Client, SearchUsersInput](
		"instagram_search_users",
		"Search Instagram users by username or full-name fragment",
		"SearchUsers",
		searchUsers,
	),
	mcptool.Define[*instagram.Client, GetSuggestedUsersInput](
		"instagram_get_suggested_users",
		"Fetch accounts Instagram suggests as related to a seed user ID",
		"GetSuggestedUsers",
		getSuggestedUsers,
	),
}
