# instagram-go

A Go client for Instagram's private web/mobile API (`api/v1/*`), plus a separate official Meta
Graph/Marketing API client for professional insights and advertising. Authenticated, stdlib-only,
zero production dependencies. Mirrors the conventions of [`x-go`](https://github.com/teslashibe/x-go),
[`linkedin-go`](https://github.com/teslashibe/linkedin-go), and the rest of the teslashibe scraper family.

```go
import "github.com/teslashibe/instagram-go"
```

## Status

| Surface              | Read | Write | Tested live |
|----------------------|:----:|:-----:|:-----------:|
| Profiles & search    | ✅   | —     | ✅          |
| Account administration | ✅ | ✅    | (offline; burner opt-in) |
| Posts / reels / feed | ✅   | ✅    | ✅ (read)   |
| Comments / likers    | ✅   | ✅    | ✅ (read)   |
| Followers / friendship| ✅  | ✅    | ✅ (read)   |
| Stories / highlights | ✅   | ✅    | ✅ (read)   |
| Hashtags             | ✅   | ✅    | ✅ (read)   |
| Locations            | ✅   | —     | ✅          |
| Keyword discovery    | ✅   | —     | ✅          |
| Topical explore      | ✅   | —     | (offline)   |
| Home timeline        | ✅   | —     | (offline)   |

Write endpoints are implemented and shape-checked, but their integration tests are
disabled by default — Instagram is aggressive about silent soft-blocks on write
actions from server-side IPs. See [Rate limiting](#rate-limiting) below.

Account-administration writes have an additional fail-closed contract: no raw
request escape hatch, approved fields only, and an expected account ID plus
explicit before/after values and confirmation on every call.

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

This repository has two deliberately separate clients and credential types:

| Client | Package | Credential | Intended capabilities |
|--------|---------|------------|-----------------------|
| Private Instagram API | `instagram` (module root) | Browser session cookies | Consumer profiles, feeds, search, social actions |
| Official Meta Graph API | `instagram/meta` | Facebook Login OAuth bearer token | Professional account/media insights and read-only advertising |

Never put a Meta OAuth token in `Cookies`, and never provide Instagram cookies to
`meta.New`. The transports, models, errors, and MCP providers are independent.
See [Official Meta Graph and Marketing API](docs/meta-graph.md) for OAuth scopes,
token storage, Page linkage, account selection, insight validation, and ad safety.

### Private cookie authentication

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

### Burner credential probes

`cmd/instagram-login-probe` verifies the complete credential-to-media path. It
asks the social-login sidecar to mint cookies, validates the authenticated user,
runs a blended keyword search, resolves one of the returned hashtags, and fails
unless that hashtag returns at least one media post.

```bash
INSTAGRAM_USERNAME='burner@example.com' \
INSTAGRAM_PASSWORD='...' \
SOCIAL_LOGIN_SIDECAR_URL='http://localhost:8190' \
INSTAGRAM_PROXY_URL='http://residential-proxy.example:8080' \
INSTAGRAM_SEARCH_QUERY='nature' \
go run ./cmd/instagram-login-probe
```

`INSTAGRAM_SEARCH_QUERY` is optional and defaults to `nature`. A residential
proxy is recommended because Instagram commonly challenges browser logins from
datacenter addresses. A successful run prints sanitized evidence in this form:

```text
PASS: authenticated as @burner_account (id=123456789)
PASS: keyword search query="nature" hashtag=#nature posts=12 first_post=ABC123 permalink=https://www.instagram.com/p/ABC123/
```

The same path is available as an explicit live acceptance test:

```bash
INSTAGRAM_LIVE_TEST=1 \
INSTAGRAM_USERNAME='burner@example.com' \
INSTAGRAM_PASSWORD='...' \
SOCIAL_LOGIN_SIDECAR_URL='http://localhost:8190' \
INSTAGRAM_SEARCH_QUERY='nature' \
go test -v -run TestLiveInventoryProbe ./cmd/instagram-login-probe
```

When `INSTAGRAM_LIVE_TEST=1`, missing live configuration is a test failure rather
than a skip. Never commit passwords, proxy credentials, session cookies, CSRF
tokens, or raw sidecar responses. See the
[redacted live validation record](docs/inventory-probe-live-validation.md) for
the latest committed run.

For the auditable keyword-to-media acceptance capture, use
`cmd/instagram-search-inventory`. Unlike blended web typeahead, this command
queries Instagram's mobile keyword SERP directly, fails closed unless both Top
and Reels contain media nodes, and writes only secret-scrubbed response-shape
evidence:

```bash
INSTAGRAM_COOKIES_FILE=/secure/burner-cookies.json \
go run ./cmd/instagram-search-inventory \
  -query coffee \
  -output docs/inventory/captures/YYYY-MM-DD-coffee-rest.md
```

The submitted probe, its capture contract, and the full live artifact are
committed at [`cmd/instagram-search-inventory`](cmd/instagram-search-inventory/),
[`docs/inventory/search-graphql.md`](docs/inventory/search-graphql.md), and
[`docs/inventory/captures/2026-07-31-coffee-rest.md`](docs/inventory/captures/2026-07-31-coffee-rest.md).

### User-Agent

The default `User-Agent` is the Instagram Android app's UA string (`Instagram 103.1.0.15.119
Android …`). Desktop browser UAs are rejected with `{"message": "useragent mismatch"}` —
override only if you have a known-good alternative.

### Web and mobile request routing

One `Client` carries the same explicit cookie header and `http.Client` across
two Instagram origins. Existing profile, feed, hashtag, entity-search, and web
GraphQL calls use `https://www.instagram.com`. Mobile keyword SERP calls use
`https://i.instagram.com` only when the endpoint wrapper explicitly selects the
mobile request profile; switching hosts does not create or maintain a second
session.

The profiles intentionally have different defaults:

| Surface | Host | App/header profile |
|---------|------|--------------------|
| Existing web API and GraphQL | `www.instagram.com` | Web app ID `936619743392459`, `X-IG-WWW-Claim: 0`, browser fetch/origin headers |
| Mobile `fbsearch` SERP | `i.instagram.com` | Mobile app ID `567067343352427`, current Android UA, `X-IG-Capabilities: 3brTv10=`, `X-IG-Connection-Type: WIFI` |

The live inventory succeeded with browser session cookies and did not require a
synthesized `Authorization` bearer value or a web WWW-Claim on the mobile host,
so the client does not invent either. Web GraphQL remains on the WWW host and
retains its existing claim/header behavior. Override origins with
`WithWWWHost` and `WithAPIHost`; override only the mobile identity with
`WithAPIUserAgent` and `WithAPIAppID`. `WithUserAgent` and `WithAppID` retain
their existing web behavior.

## Endpoint catalogue

Read endpoints with enabled integration coverage have been **end-to-end
verified** with a live session; surfaces marked `(offline)` above remain
fixture-verified only. Write endpoints are implemented but not exercised in the
integration suite.

### Profiles & search

| Method                                        | Endpoint                                                |
|-----------------------------------------------|---------------------------------------------------------|
| `Me(ctx)`                                     | (cached from `New()`)                                   |
| `GetProfile(ctx, username)`                   | `GET  /api/v1/users/web_profile_info/?username=`        |
| `GetProfileByID(ctx, userID)`                 | `GET  /api/v1/users/{id}/info/`                         |
| `SearchUsers(ctx, query, count)`              | `GET  /api/v1/users/search/?q=&count=`                  |
| `Search(ctx, query)`                          | `GET  /api/v1/web/search/topsearch/`                    |
| `GetSuggestedUsers(ctx, targetID)`            | `GET  /api/v1/discover/chaining/?target_id=`            |
| `SearchPosts(query)` (iterator)               | `GET  i.instagram.com/api/v1/fbsearch/top_serp/`         |
| `SearchKeywordPosts(query)` (iterator)        | `POST /graphql/query` (initial + pagination documents)  |
| `SearchReels(query)` (single-page iterator)   | `GET  i.instagram.com/api/v1/fbsearch/reels_serp/`       |
| `SearchAccounts(ctx, query)`                  | `GET  i.instagram.com/api/v1/fbsearch/account_serp/`     |
| `SearchTypeaheadUsers(ctx, query, count)`     | `GET  i.instagram.com/api/v1/fbsearch/typeahead_stream/` |
| `KeywordTypeahead(ctx, query)`                | `GET  i.instagram.com/api/v1/fbsearch/typeahead_stream/` |

`Search` and `SearchUsers` remain the compatible REST entity searches.
`SearchPosts` uses the mobile Top SERP and preserves its complete pagination
state in a versioned opaque cursor bound to the trimmed query. Version-1
SearchPosts cursors are rejected because they contain no query binding;
malformed, unsupported, or query-mismatched cursors fail before an HTTP request
is made. `SearchKeywordPosts` uses the web
keyword-to-media connection, transparently switches from the captured initial
GraphQL document to the distinct pagination document, and preserves the Relay
cursor plus both GraphQL search session IDs in its versioned opaque cursor.
Keyword cursors are bound to the trimmed query; malformed, unsupported, or
query-mismatched cursors fail before an HTTP request is made.
`SearchReels` returns Reel media as ordinary `Post` values, including media PKs
needed by commenting helpers. It intentionally fetches only the first page: the
live inventory proved response cursor fields but not their continuation request
parameters. The iterator shape allows pagination to be added compatibly after a
continuation request is captured; until then, passing a cursor to the iterator
fails before an HTTP request. The `instagram_search_reels` MCP tool follows the
same terminal contract: `limit` can truncate that first page, but the tool does
not return a continuation cursor, and it rejects any supplied cursor before
making an HTTP request.
`SearchAccounts` returns the richer account SERP context (including friendship
and social-context fields), while `SearchTypeaheadUsers` returns the lighter
account suggestions shown during keyword entry. `KeywordTypeahead` is a compact
string helper over those entities, returning usernames (or display-name
fallbacks). An empty slice is a valid response when Instagram has no suggestion
for a partial query. The private contracts, status
checklist, rotating `doc_id` values, and scrubbed evidence are documented in the
[search inventory](docs/inventory/search-graphql.md).

```go
it := client.SearchPosts("specialty coffee").WithMaxPages(2)
for it.Next(ctx) {
    post := it.Item()
    fmt.Printf("%s %s\n", post.Code, post.PermalinkURL)
}
if err := it.Err(); err != nil {
    // Includes the existing auth/rate-limit/challenge sentinels.
    return err
}

// Persist after a page, then resume the same trimmed query later without
// losing Top SERP state.
cursor := it.Cursor()
if cursor != "" {
    resumed := client.SearchPosts("specialty coffee").WithCursor(cursor)
    _ = resumed
}

// Keyword GraphQL cursors also resume faithfully on a fresh iterator.
keyword := client.SearchKeywordPosts("specialty coffee").WithMaxPages(1)
_, err := keyword.Collect(ctx)
if err == nil && keyword.Cursor() != "" {
    resumed := client.SearchKeywordPosts("specialty coffee").WithCursor(keyword.Cursor())
    _ = resumed
}

accounts, err := client.SearchAccounts(ctx, "specialty coffee")

reels, err := client.SearchReels("specialty coffee").Collect(ctx)
suggestions, err := client.KeywordTypeahead(ctx, "specialty cof")
```

Top SERP ranking is personalized and can change between runs. Durable watches
should deduplicate results by `Post.PK` (falling back to `Post.Code`).

### Safe account administration

The authenticated account read is projected into three safe models. Raw current
account responses are not exposed because Instagram may include contact or
security-adjacent fields alongside editable settings.

| Method | Endpoint |
| --- | --- |
| `GetCurrentAccount(ctx)` | `GET i.instagram.com/api/v1/accounts/current_user/?edit=true` |
| `GetAccountSettings(ctx)` | same captured read, reversible-settings projection |
| `GetProfessionalAccountState(ctx)` | same captured read, professional-state projection |
| `UpdateProfileFields(ctx, params)` | `POST i.instagram.com/api/v1/accounts/edit_profile/` |
| `SetPrivacy(ctx, params)` | `POST i.instagram.com/api/v1/accounts/set_private/` or `set_public/` |
| `UpdateProfessionalSettings(ctx, params)` | `POST i.instagram.com/api/v1/business/account/edit/` |

Each mutation checks `ExpectedAccountID` against both the authenticated
`ds_user_id` and a fresh read, requires `Confirm: true`, rejects a no-op or stale
`Before`, sends exactly one write attempt, and re-reads to verify `After` before
returning success. Profile mutation is limited to full name, biography, and
external URL. Professional mutation is limited to category ID and category
visibility on an account that is already professional.

Password, username/email/phone, public contact details, 2FA,
deletion/deactivation, account conversion, ownership, and security changes are
not implemented. The same fields are absent from MCP input schemas. The
captured contract and burner-only verification protocol are documented in
[`docs/inventory/account-administration.md`](docs/inventory/account-administration.md).

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
| `ErrAccountMismatch`    | Authenticated account differs from the requested target         |
| `ErrMutationPrecondition` | Confirmation, before-state, no-op, or verification failed      |
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

Account-administration smoke tests have stronger guards and must use a dedicated
burner. Each test registers cleanup before writing and verifies restoration:

```bash
export INSTAGRAM_ACCOUNT_ADMIN_LIVE_TEST=1
export INSTAGRAM_ACCOUNT_ADMIN_BURNER_ID="$IG_DS_USER_ID"
export INSTAGRAM_ACCOUNT_ADMIN_CONFIRM='RESTORE_BURNER_SETTINGS'
go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfileRestoresBurner$' .
```

Run one administration smoke test at a time. Privacy and professional-display
test names are listed in the account-administration inventory document.

## MCP support

This package ships an [MCP](https://modelcontextprotocol.io/) tool surface in
`./mcp` for use with [`teslashibe/mcptool`](https://github.com/teslashibe/mcptool)-compatible
hosts (e.g. [`teslashibe/agent-setup`](https://github.com/teslashibe/agent-setup)).
58 tools cover the full client API: profile lookup and search, safe account administration, post/reel/timeline/explore
feeds, comments and likes, followers/following and friendship reads + writes
(follow/unfollow/block/mute), hashtag and location reads + follow/unfollow,
stories and highlights, blended top-search, and keyword post/reel search.

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
