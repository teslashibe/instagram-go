// Package mcp exposes the instagram-go [instagram.Client] surface as a set
// of MCP (Model Context Protocol) tools that any host application can mount
// on its own MCP server.
//
// All tools wrap exported methods on *instagram.Client. Each tool is defined
// via [mcptool.Define] so the JSON input schema is reflected from the typed
// input struct — no hand-maintained schemas, no drift.
//
// Usage from a host application:
//
//	import (
//	    "github.com/teslashibe/mcptool"
//	    instagram "github.com/teslashibe/instagram-go"
//	    igmcp "github.com/teslashibe/instagram-go/mcp"
//	)
//
//	client, _ := instagram.New(instagram.Cookies{...})
//	for _, tool := range igmcp.Provider{}.Tools() {
//	    // register tool with your MCP server, passing client as the client arg
//	    // when invoking
//	}
//
// The [Excluded] map documents methods on *Client that are intentionally not
// exposed via MCP, with a one-line reason. The coverage test in mcp_test.go
// fails if a new exported method is added without either being wrapped by a
// tool or appearing in [Excluded].
package mcp

import "github.com/teslashibe/mcptool"

// Provider implements [mcptool.Provider] for instagram-go. The zero value is
// ready to use.
type Provider struct{}

// Platform returns "instagram".
func (Provider) Platform() string { return "instagram" }

// Tools returns every instagram-go MCP tool, in registration order.
func (Provider) Tools() []mcptool.Tool {
	out := make([]mcptool.Tool, 0,
		len(userTools)+
			len(postTools)+
			len(commentTools)+
			len(followerTools)+
			len(hashtagTools)+
			len(locationTools)+
			len(storyTools)+
			len(searchTools),
	)
	out = append(out, userTools...)
	out = append(out, postTools...)
	out = append(out, commentTools...)
	out = append(out, followerTools...)
	out = append(out, hashtagTools...)
	out = append(out, locationTools...)
	out = append(out, storyTools...)
	out = append(out, searchTools...)
	return out
}
