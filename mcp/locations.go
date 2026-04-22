package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetLocationInput is the typed input for instagram_get_location.
type GetLocationInput struct {
	ID string `json:"id" jsonschema:"description=numeric Instagram location ID,required"`
}

func getLocation(ctx context.Context, c *instagram.Client, in GetLocationInput) (any, error) {
	return c.GetLocation(ctx, in.ID)
}

// SearchLocationsInput is the typed input for instagram_search_locations.
type SearchLocationsInput struct {
	Query string `json:"query" jsonschema:"description=free-text query against Instagram's location index,required"`
}

func searchLocations(ctx context.Context, c *instagram.Client, in SearchLocationsInput) (any, error) {
	res, err := c.SearchLocations(ctx, in.Query)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", maxLimit), nil
}

// GetLocationPostsInput is the typed input for instagram_get_location_posts.
type GetLocationPostsInput struct {
	ID    string `json:"id" jsonschema:"description=numeric Instagram location ID,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=maximum recent posts to return,minimum=1,maximum=50,default=12"`
}

func getLocationPosts(ctx context.Context, c *instagram.Client, in GetLocationPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetLocationPosts(in.ID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

// GetLocationTopPostsInput is the typed input for instagram_get_location_top_posts.
type GetLocationTopPostsInput struct {
	ID    string `json:"id" jsonschema:"description=numeric Instagram location ID,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=maximum top-ranked posts to return,minimum=1,maximum=50,default=12"`
}

func getLocationTopPosts(ctx context.Context, c *instagram.Client, in GetLocationTopPostsInput) (any, error) {
	items, err := collectUpTo(ctx, c.GetLocationTopPosts(in.ID), in.Limit)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(items, "", effectiveLimit(in.Limit)), nil
}

var locationTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetLocationInput](
		"instagram_get_location",
		"Fetch metadata for an Instagram location by ID",
		"GetLocation",
		getLocation,
	),
	mcptool.Define[*instagram.Client, SearchLocationsInput](
		"instagram_search_locations",
		"Search Instagram's location index by free-text query",
		"SearchLocations",
		searchLocations,
	),
	mcptool.Define[*instagram.Client, GetLocationPostsInput](
		"instagram_get_location_posts",
		"Fetch recent posts tagged at an Instagram location",
		"GetLocationPosts",
		getLocationPosts,
	),
	mcptool.Define[*instagram.Client, GetLocationTopPostsInput](
		"instagram_get_location_top_posts",
		"Fetch top-ranked posts tagged at an Instagram location",
		"GetLocationTopPosts",
		getLocationTopPosts,
	),
}
