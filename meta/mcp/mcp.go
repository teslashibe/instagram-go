// Package mcp exposes the official Meta Graph client through an independent
// MCP provider. It does not accept the private cookie-auth Instagram client.
package mcp

import "github.com/teslashibe/mcptool"

type Provider struct{}

func (Provider) Platform() string { return "instagram_meta" }

func (Provider) Tools() []mcptool.Tool {
	tools := make([]mcptool.Tool, 0, len(accountTools)+len(insightTools)+len(adTools))
	tools = append(tools, accountTools...)
	tools = append(tools, insightTools...)
	tools = append(tools, adTools...)
	return tools
}
