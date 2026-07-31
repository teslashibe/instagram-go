package mcp

// Excluded enumerates exported methods on *instagram.Client that are
// intentionally not exposed via MCP. Each entry must have a non-empty reason.
//
// The coverage test in mcp_test.go fails if any exported method on *Client is
// neither wrapped by a Tool nor present in this map (or vice-versa: if an
// entry here doesn't correspond to a real method).
//
// When the underlying client gains a new method:
//   - prefer to add an MCP tool for it (see users.go / posts.go / etc.)
//   - if the method is unsuitable for an agent (internal observability,
//     auth-only helper, etc.), add it here with a reason
var Excluded = map[string]string{
	"KeywordTypeahead":     "SDK keyword-suggestion helper; MCP exposure is expressly out of scope for issue #9",
	"RateLimit":            "internal observability; surfaced via the host application's MCP middleware, not as a callable tool",
	"SearchAccounts":       "SDK discovery wrapper; MCP exposure is intentionally deferred until an agent workflow requires this account SERP",
	"SearchKeywordPosts":   "SDK discovery wrapper; MCP pagination and result-budget semantics are intentionally deferred to a focused follow-up",
	"SearchPosts":          "SDK keyword-post wrapper; MCP registration is explicitly out of scope for issue #8 and deferred to its follow-up",
	"SearchReels":          "SDK Reels-search wrapper; MCP exposure is expressly out of scope for issue #9",
	"SearchTypeaheadUsers": "SDK discovery wrapper; MCP exposure is intentionally deferred until an agent workflow requires mobile typeahead",
	"WaitForCooldown":      "internal flow-control primitive; the host should manage rate-limit cooldowns at the request layer rather than expose blocking calls to the agent",
}
