# instagram-go

A lean, zero-dependency Go client for Instagram's private web/mobile API. Authenticated profile lookup, post and reel feeds, comments, followers/following, stories, hashtags, locations, and search — programmatic access to Instagram's content graph from a logged-in browser session.

```go
import instagram "github.com/teslashibe/instagram-go"
```

## Install

```bash
go get github.com/teslashibe/instagram-go
```

Requires Go 1.25+.

## Auth

Instagram session cookies obtained from a browser export of an authenticated session:

| Cookie       | Required | Where to find                    |
| ------------ | -------- | -------------------------------- |
| `sessionid`  | yes      | DevTools > Application > Cookies |
| `csrftoken`  | yes      | DevTools > Application > Cookies |
| `ds_user_id` | yes      | DevTools > Application > Cookies |
| `datr`       | optional | DevTools > Application > Cookies |
| `mid`        | optional | DevTools > Application > Cookies |
| `ig_did`     | optional | DevTools > Application > Cookies |

## Quick start

```go
client, err := instagram.New(instagram.Cookies{
    SessionID: os.Getenv("IG_SESSIONID"),
    CSRFToken: os.Getenv("IG_CSRFTOKEN"),
    DSUserID:  os.Getenv("IG_DS_USER_ID"),
})
if err != nil {
    log.Fatal(err)
}

// Profile lookup
user, _ := client.GetProfile(ctx, "instagram")
fmt.Println(user.Username, user.FollowerCount)

// Iterate a user's posts
it := client.GetPosts(user.ID).WithMaxPages(3)
for it.Next(ctx) {
    p := it.Item()
    fmt.Println(p.Code, p.LikeCount)
}
if err := it.Err(); err != nil {
    log.Fatal(err)
}
```

## Pagination

List endpoints return an `Iterator[T]` you drive with `Next` / `Item`, with optional `WithMaxPages` to bound upstream calls. Use `Collect(ctx)` to drain into a slice.

## Rate limiting

Instagram does not return standard rate-limit headers. The client paces reads with a leaky-bucket minimum gap (default 4s) and applies a stricter, separate budget for writes (default 12s gap, 15 minute cooldown after a soft-block). Inspect with `client.RateLimit()` and block until clear with `client.WaitForCooldown(ctx)`.

## Options

```go
client, _ := instagram.New(cookies,
    instagram.WithRetry(5, time.Second),       // 5 attempts, 1s base backoff
    instagram.WithMinRequestGap(3*time.Second),
    instagram.WithMinWriteGap(10*time.Second),
    instagram.WithUserAgent("custom-agent/1.0"),
    instagram.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
)
```

## MCP support

This package ships an [MCP](https://modelcontextprotocol.io/) tool surface in `./mcp` for use with [`teslashibe/mcptool`](https://github.com/teslashibe/mcptool)-compatible hosts (e.g. [`teslashibe/agent-setup`](https://github.com/teslashibe/agent-setup)). 50 tools cover the full client API: profile lookup and search, post/reel/timeline/explore feeds, comments and likes, followers/following and friendship reads + writes (follow/unfollow/block/mute), hashtag and location reads + follow/unfollow, stories and highlights, and top-search.

```go
import (
    "github.com/teslashibe/mcptool"
    instagram "github.com/teslashibe/instagram-go"
    igmcp "github.com/teslashibe/instagram-go/mcp"
)

client, _ := instagram.New(instagram.Cookies{...})
provider := igmcp.Provider{}
for _, tool := range provider.Tools() {
    // register tool with your MCP server, passing client as the
    // opaque client argument when invoking
}
```

A coverage test in `mcp/mcp_test.go` fails if a new exported method is added to `*Client` without either being wrapped by an MCP tool or being added to `mcp.Excluded` with a reason — keeping the MCP surface in lockstep with the package API is enforced by CI rather than convention.

## License

MIT
