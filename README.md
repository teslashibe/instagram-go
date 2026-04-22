# instagram-go

A Go client for Instagram's private web/mobile API (`api/v1/*`). Authenticated, stdlib-only,
zero production dependencies. Mirrors the conventions of [`x-go`](https://github.com/teslashibe/x-go),
[`linkedin-go`](https://github.com/teslashibe/linkedin-go), and the rest of the teslashibe scraper family.

```go
import "github.com/teslashibe/instagram-go"
```

## Status

| Surface              | Read | Write | Tested live |
|----------------------|:----:|:-----:|:-----------:|
| Profiles & search    | ✅   | —     | ✅          |
| Posts / reels / feed | ✅   | ✅    | ✅ (read)   |
| Comments / likers    | ✅   | ✅    | ✅ (read)   |
| Followers / friendship| ✅  | ✅    | ✅ (read)   |
| Stories / highlights | ✅   | ✅    | ✅ (read)   |
| Hashtags             | ✅   | ✅    | ✅ (read)   |
| Locations            | ✅   | —     | ✅          |
| Topical explore      | ✅   | —     | (offline)   |
| Home timeline        | ✅   | —     | (offline)   |

Write endpoints are implemented and shape-checked, but their integration tests are
disabled by default — Instagram is aggressive about silent soft-blocks on write
actions from server-side IPs. See [Rate limiting](#rate-limiting) below.

## Install

```bash
go get github.com/teslashibe/instagram-go
```

Requires Go 1.25 or newer (uses generics for `Iterator[T]`).

## Quick start

```go
package main

import (
    "context"
    "fmt"

    instagram "github.com/teslashibe/instagram-go"
)

func main() {
    c, err := instagram.New(instagram.Cookies{
        SessionID: "...",  // sessionid cookie
        CSRFToken: "...",  // csrftoken cookie
        DSUserID:  "...",  // ds_user_id cookie (numeric)
        Datr:      "...",  // datr cookie
        Mid:       "...",  // mid cookie
        IgDid:     "...",  // ig_did cookie
    })
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    user, err := c.GetProfile(ctx, "natgeo")
    if err != nil {
        panic(err)
    }
    fmt.Printf("@%s — %d followers\n", user.Username, user.FollowerCount)

    it := c.GetPosts(user.ID).WithMaxPages(3)
    for it.Next(ctx) {
        p := it.Item()
        fmt.Printf("%s  %s  likes=%d comments=%d\n", p.Code, p.PermalinkURL, p.LikeCount, p.CommentCount)
    }
    if err := it.Err(); err != nil {
        panic(err)
    }
}
```

## Authentication

Required cookies (export from a logged-in browser session):

| Field       | Cookie name   | Required | Notes                                            |
|-------------|---------------|----------|--------------------------------------------------|
| `SessionID` | `sessionid`   | yes      | Primary credential                               |
| `CSRFToken` | `csrftoken`   | yes      | Also sent as `X-CSRFToken` header                |
| `DSUserID`  | `ds_user_id`  | yes      | Numeric user ID; used for session validation     |
| `Datr`      | `datr`        | recommended | Device auth token; reduces challenge prompts  |
| `Mid`       | `mid`         | recommended | Machine ID                                    |
| `IgDid`     | `ig_did`      | recommended | Device ID                                     |
| `Rur`       | `rur`         | optional | Region routing                                   |
| `IgNrcb`    | `ig_nrcb`     | optional | Notification opt-in flag                         |
| `PsL`/`PsN` | `ps_l`/`ps_n` | optional | Persistent session telemetry                     |
| `Wd`        | `wd`          | optional | Viewport size (`<width>x<height>`)               |

`New()` validates the session on construction by fetching `/api/v1/users/<DSUserID>/info/`.
Pass `WithSkipSessionValidation()` to defer validation (useful in tests).

### User-Agent

The default `User-Agent` is the Instagram Android app's UA string (`Instagram 103.1.0.15.119
Android …`). Desktop browser UAs are rejected with `{"message": "useragent mismatch"}` —
override only if you have a known-good alternative.

## Endpoint catalogue

All read endpoints below have been **end-to-end verified** with a live session.
Write endpoints are implemented but not exercised in the integration suite.

### Profiles & search

| Method                                        | Endpoint                                                |
|-----------------------------------------------|---------------------------------------------------------|
| `Me(ctx)`                                     | (cached from `New()`)                                   |
| `GetProfile(ctx, username)`                   | `GET  /api/v1/users/web_profile_info/?username=`        |
| `GetProfileByID(ctx, userID)`                 | `GET  /api/v1/users/{id}/info/`                         |
| `SearchUsers(ctx, query, count)`              | `GET  /api/v1/users/search/?q=&count=`                  |
| `Search(ctx, query)`                          | `GET  /api/v1/web/search/topsearch/`                    |
| `GetSuggestedUsers(ctx, targetID)`            | `GET  /api/v1/discover/chaining/?target_id=`            |

### Posts & feeds

| Method                                | Endpoint                                                       |
|---------------------------------------|----------------------------------------------------------------|
| `GetPosts(userID)` (iterator)         | `GET  /api/v1/feed/user/{id}/?count=&max_id=`                  |
| `GetReels(userID)` (iterator)         | `POST /api/v1/clips/user/`                                     |
| `GetTaggedPosts(userID)` (iterator)   | `GET  /api/v1/usertags/{id}/feed/`                             |
| `GetPost(ctx, shortcode)`             | shortcode → media_id, then `GET /api/v1/media/{id}/info/`       |
| `GetPostByID(ctx, mediaID)`           | `GET  /api/v1/media/{id}/info/`                                |
| `GetTimeline()` (iterator)            | `POST /api/v1/feed/timeline/`                                  |
| `GetExplore()` (iterator)             | `GET  /api/v1/discover/topical_explore/`                       |

### Comments & likers

| Method                                                  | Endpoint                                                      |
|---------------------------------------------------------|---------------------------------------------------------------|
| `GetComments(mediaPK)` (iterator)                       | `GET  /api/v1/media/{pk}/comments/`                            |
| `GetCommentReplies(mediaPK, parentID)` (iterator)       | `GET  /api/v1/media/{pk}/comments/{parent}/child_comments/`    |
| `GetLikers(ctx, mediaPK)`                               | `GET  /api/v1/media/{pk}/likers/`                              |
| `GetCommentLikers(ctx, mediaPK, commentID)`             | `GET  /api/v1/media/{pk}/comment_likers/?comment_id=`          |

### Followers, following, friendship

| Method                                       | Endpoint                                            |
|----------------------------------------------|-----------------------------------------------------|
| `GetFollowers(userID)` (iterator)            | `GET  /api/v1/friendships/{id}/followers/`          |
| `GetFollowing(userID)` (iterator)            | `GET  /api/v1/friendships/{id}/following/`          |
| `GetFriendship(ctx, userID)`                 | `GET  /api/v1/friendships/show/{id}/`               |
| `GetFriendships(ctx, userIDs)`               | `POST /api/v1/friendships/show_many/`               |

### Stories & highlights

| Method                                            | Endpoint                                                       |
|---------------------------------------------------|----------------------------------------------------------------|
| `GetStoryTray(ctx)`                               | `GET  /api/v1/feed/reels_tray/`                                |
| `GetUserStories(ctx, userID)`                     | `GET  /api/v1/feed/user/{id}/story/`                           |
| `GetHighlights(ctx, userID)`                      | `GET  /api/v1/highlights/{id}/highlights_tray/`                |
| `GetReelsMedia(ctx, reelIDs)`                     | `POST /api/v1/feed/reels_media/`                               |

### Hashtags

| Method                                  | Endpoint                                            |
|-----------------------------------------|-----------------------------------------------------|
| `GetHashtag(ctx, name)`                 | `GET  /api/v1/tags/web_info/?tag_name=`             |
| `GetHashtagPosts(name)` (iterator)      | `POST /api/v1/tags/{name}/sections/` `tab=recent`   |
| `GetHashtagTopPosts(name)` (iterator)   | `POST /api/v1/tags/{name}/sections/` `tab=top`      |
| `GetHashtagClips(name)` (iterator)      | `POST /api/v1/tags/{name}/sections/` `tab=clips`    |

### Locations

| Method                                | Endpoint                                                |
|---------------------------------------|---------------------------------------------------------|
| `GetLocation(ctx, id)`                | `GET  /api/v1/locations/{id}/info/`                     |
| `SearchLocations(ctx, query)`         | `GET  /api/v1/location_search/?search_query=`           |
| `GetLocationPosts(id)` (iterator)     | `POST /api/v1/locations/{id}/sections/` `tab=recent`    |
| `GetLocationTopPosts(id)` (iterator)  | `POST /api/v1/locations/{id}/sections/` `tab=ranked`    |

### Write actions

All writes are subject to a stricter rate-limit budget than reads. They share a 12 s
minimum gap and a 15 m circuit-breaker cooldown when Instagram returns a `302→login`
soft-block. See [Rate limiting](#rate-limiting).

| Action category | Methods                                                                                |
|-----------------|----------------------------------------------------------------------------------------|
| Posts           | `LikePost`, `UnlikePost`, `SavePost`, `UnsavePost`                                     |
| Comments        | `PostComment`, `LikeComment`, `UnlikeComment`, `DeleteComment`                         |
| Friendship      | `Follow`, `Unfollow`, `Block`, `Unblock`, `MutePosts`, `UnmutePosts`                   |
| Hashtags        | `FollowHashtag`, `UnfollowHashtag`                                                     |
| Stories         | `MarkStorySeen`                                                                        |

## Pagination

All list endpoints return an `Iterator[T]`:

```go
it := c.GetFollowers(userID).WithMaxPages(5)
for it.Next(ctx) {
    u := it.Item()
    // ...
}
if err := it.Err(); err != nil { … }
```

`Next` advances one item; the iterator transparently fetches the next page on
exhaustion using Instagram's `next_max_id` (or, for comments, `next_min_id`).
Use `WithMaxPages(n)` to cap how many pages are fetched. `WithLimit(n)` caps
total items returned.

## Rate limiting

Instagram does **not** publish standard `X-RateLimit-*` / `Retry-After` headers.
Instead it rate-limits behaviourally and signals pressure via three channels:

1. **Body messages** — `{"message": "Please wait a few minutes before you try again.", "status": "fail"}`
2. **`302→/accounts/login/` soft-block** — Instagram returns a 302 redirect to the
   login page even with a healthy `sessionid`. This pattern indicates a rate
   limit, **not** session expiry, once the session has been validated at least once.
3. **Soft signal headers** — `x-ig-capacity-level` (0 = degraded, 3 = healthy),
   `x-ig-peak-time`, `x-ig-peak-v2`, `x-fb-connection-quality`.

The client handles all three:

- A leaky-bucket pacer enforces a minimum gap between requests
  (default **4 s** for reads = ~15 reads/min, **12 s** for writes).
- On any of the three signals above, a global circuit-breaker trips a cooldown
  (default **5 m** for reads, **15 m** for writes). Subsequent calls block until
  the cooldown clears (or the context is cancelled).
- `RateLimit()` exposes the most recent observation; `WaitForCooldown(ctx)` blocks
  until all cooldowns are clear.
- Retries are skipped while a cooldown is active (so we never burn attempts).

```go
state := c.RateLimit()
fmt.Printf("capacity=%d peak=%v conn=%q\n", state.CapacityLevel, state.PeakTime, state.ConnectionQuality)

if err := c.WaitForCooldown(ctx); err != nil {
    return err
}
```

Tune via:

```go
c, _ := instagram.New(cookies,
    instagram.WithMinRequestGap(6*time.Second),
    instagram.WithMinWriteGap(20*time.Second),
    instagram.WithRateLimitCooldown(10*time.Minute, 30*time.Minute),
)
```

### Recommended budgets

Empirically observed safe ceilings on a single residential session (your mileage
will vary):

| Action            | Conservative | Aggressive | Notes                                       |
|-------------------|:------------:|:----------:|---------------------------------------------|
| Reads (per min)   | 8            | 15         | Above 15/min triggers `wait a few minutes`. |
| Reads (per hour)  | 400          | 800        | Capacity drops to 1–2 above this.           |
| Writes (per hour) | 30           | 60         | Above this risks a 24 h soft-block.         |

## Error handling

All errors wrap one of the package sentinels — match with `errors.Is`:

| Sentinel                | Meaning                                                         |
|-------------------------|-----------------------------------------------------------------|
| `ErrInvalidAuth`        | Missing/malformed cookies, or session validation failed         |
| `ErrSessionExpired`     | 302→login on an unvalidated session, or `sessionid=""` server-set |
| `ErrRateLimited`        | 429, `wait a few minutes`, or 302→login on a validated session  |
| `ErrWriteSoftBlock`     | 302→login on a write action; read session still works           |
| `ErrChallengeRequired`  | Account flagged for security checkpoint                         |
| `ErrNotFound`           | 404 or `user_not_found` response                                |
| `ErrPrivateAccount`     | Resource belongs to a private account viewer doesn't follow     |
| `ErrMediaUnavailable`   | Post deleted or hidden                                          |
| `ErrCSRF`               | CSRF token rejected on a write                                  |
| `ErrUnexpectedResponse` | Well-formed JSON missing the expected fields                    |

For non-2xx HTTP responses, the wrapped error is also an `*APIError` with
`StatusCode`, `Body`, etc. — useful for logging:

```go
var apiErr *instagram.APIError
if errors.As(err, &apiErr) {
    log.Printf("instagram %d: %s", apiErr.StatusCode, apiErr.Body)
}
```

## Concurrency

`Client` is safe for concurrent use by multiple goroutines. The pacer and
circuit-breaker are global to the client, so concurrent goroutines share a
single rate-limit budget.

## Testing

```bash
# Offline unit tests (no cookies required)
go test ./...

# Integration suite — needs cookies in env
source .env.test.local
go test -count=1 -run '^TestIntegration_' .
```

`.env.test.local` shape (gitignored):

```bash
export IG_SESSIONID='...'
export IG_CSRFTOKEN='...'
export IG_DS_USER_ID='...'
export IG_DATR='...'
export IG_MID='...'
export IG_DID='...'
# Optional but recommended:
export IG_RUR='...'
export IG_NRCB='1'
export IG_WD='948x1384'
```

The integration suite uses a **single shared client across all tests** so the
rate-limit circuit-breaker is honoured globally. To stay under Instagram's
~15 reads/min ceiling, run individual tests with explicit pauses rather than
the full suite as a burst:

```bash
go test -v -count=1 -run '^TestIntegration_GetProfile$' .
sleep 30
go test -v -count=1 -run '^TestIntegration_GetPosts$' .
# ...
```

## MCP support

This package ships an [MCP](https://modelcontextprotocol.io/) tool surface in
`./mcp` for use with [`teslashibe/mcptool`](https://github.com/teslashibe/mcptool)-compatible
hosts (e.g. [`teslashibe/agent-setup`](https://github.com/teslashibe/agent-setup)).
50 tools cover the full client API: profile lookup and search, post/reel/timeline/explore
feeds, comments and likes, followers/following and friendship reads + writes
(follow/unfollow/block/mute), hashtag and location reads + follow/unfollow,
stories and highlights, and top-search.

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

A coverage test in `mcp/mcp_test.go` fails if a new exported method is added
to `*Client` without either being wrapped by an MCP tool or being added to
`mcp.Excluded` with a reason — keeping the MCP surface in lockstep with the
package API is enforced by CI rather than convention.

## Conventions

- **Stdlib only in the SDK.** `instagram` package itself has zero third-party deps.
  The `./mcp` subpackage pulls in `teslashibe/mcptool` (and its transitive deps)
  for the MCP tool surface — opt in by importing `./mcp`, otherwise unaffected.
- **Errors as values.** Sentinel errors with `errors.Is`; `*APIError` for HTTP context.
- **Iterators for lists.** Anything paginated returns `*Iterator[T]`; one-shot
  results return `[]T` directly.
- **Numeric IDs as strings.** Instagram mixes numeric and string IDs in the same
  payloads (`pk` / `pk_id`). All IDs are normalised to `string` on the way out.
- **`Raw` field on every model.** Each `User`, `Post`, etc. carries `Raw json.RawMessage`
  so callers can fish out fields the typed view doesn't expose.

## License

MIT — see [LICENSE](LICENSE).
