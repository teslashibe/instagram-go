package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetStoryTrayInput is the typed input for instagram_get_story_tray.
type GetStoryTrayInput struct{}

func getStoryTray(ctx context.Context, c *instagram.Client, _ GetStoryTrayInput) (any, error) {
	res, err := c.GetStoryTray(ctx)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

// GetUserStoriesInput is the typed input for instagram_get_user_stories.
type GetUserStoriesInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose current story to fetch,required"`
}

func getUserStories(ctx context.Context, c *instagram.Client, in GetUserStoriesInput) (any, error) {
	return c.GetUserStories(ctx, in.UserID)
}

// GetHighlightsInput is the typed input for instagram_get_highlights.
type GetHighlightsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric Instagram user ID whose saved story highlights to fetch,required"`
}

func getHighlights(ctx context.Context, c *instagram.Client, in GetHighlightsInput) (any, error) {
	res, err := c.GetHighlights(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

// GetReelsMediaInput is the typed input for instagram_get_reels_media.
type GetReelsMediaInput struct {
	ReelIDs []string `json:"reel_ids" jsonschema:"description=story reel IDs (numeric user IDs for live stories or 'highlight:<id>' for highlights),required"`
}

func getReelsMedia(ctx context.Context, c *instagram.Client, in GetReelsMediaInput) (any, error) {
	return c.GetReelsMedia(ctx, in.ReelIDs)
}

// MarkStorySeenInput is the typed input for instagram_mark_story_seen.
type MarkStorySeenInput struct {
	ReelID  string `json:"reel_id" jsonschema:"description=story reel ID the media item belongs to,required"`
	MediaPK string `json:"media_pk" jsonschema:"description=numeric story media ID to mark as seen,required"`
	TakenAt int64  `json:"taken_at" jsonschema:"description=unix epoch (seconds) when the story media was posted,required"`
}

func markStorySeen(ctx context.Context, c *instagram.Client, in MarkStorySeenInput) (any, error) {
	if err := c.MarkStorySeen(ctx, in.ReelID, in.MediaPK, in.TakenAt); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "reel_id": in.ReelID, "media_pk": in.MediaPK}, nil
}

var storyTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetStoryTrayInput](
		"instagram_get_story_tray",
		"Fetch the authenticated user's home story tray (the row of profile circles)",
		"GetStoryTray",
		getStoryTray,
	),
	mcptool.Define[*instagram.Client, GetUserStoriesInput](
		"instagram_get_user_stories",
		"Fetch a single user's current story reel (returns null if no active story)",
		"GetUserStories",
		getUserStories,
	),
	mcptool.Define[*instagram.Client, GetHighlightsInput](
		"instagram_get_highlights",
		"Fetch a user's saved story highlight reels",
		"GetHighlights",
		getHighlights,
	),
	mcptool.Define[*instagram.Client, GetReelsMediaInput](
		"instagram_get_reels_media",
		"Fetch the media items inside one or more story reels (live or highlight)",
		"GetReelsMedia",
		getReelsMedia,
	),
	mcptool.Define[*instagram.Client, MarkStorySeenInput](
		"instagram_mark_story_seen",
		"Mark a story media item as seen by the authenticated viewer",
		"MarkStorySeen",
		markStorySeen,
	),
}
