package mcp

import (
	"context"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// SearchInput is the typed input for instagram_search.
type SearchInput struct {
	Query string `json:"query" jsonschema:"description=free-text query; returns blended users, hashtags, and places,required"`
}

func search(ctx context.Context, c *instagram.Client, in SearchInput) (any, error) {
	return c.Search(ctx, in.Query)
}

var searchTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, SearchInput](
		"instagram_search",
		"Run an Instagram top-search for users, hashtags, and places matching a query",
		"Search",
		search,
	),
}
