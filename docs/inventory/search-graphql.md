# Instagram keyword search capture inventory

Status: **REST and GraphQL keyword-search inventory captured live**. Mobile
REST was captured on 2026-07-31; authenticated burner-session web GraphQL was
captured on 2026-06-11.

Live secret-scrubbed REST evidence:
[`docs/inventory/captures/2026-07-31-coffee-rest.md`](./captures/2026-07-31-coffee-rest.md)
— four mobile `fbsearch` tabs on `i.instagram.com`, keyword `coffee`, 32 media
nodes. Browser-minted sessions may fail `accounts/current_user` on the mobile
host while still serving SERP; the probe validates via `top_serp` instead.

Live secret-scrubbed GraphQL evidence:
[`docs/inventory/captures/2026-06-11-keyword-search-graphql.md`](./captures/2026-06-11-keyword-search-graphql.md)
— authenticated initial and continuation operations on `www.instagram.com`,
including the observed friendly names, `doc_id` values, variable names,
response paths, Relay pagination contract, and a normalized media sample.

This document remains the durable capture contract for issue #6. New captures
belong under `docs/inventory/captures/` after human secret review.

## SDK implementation checklist

The issue #11 vertical slice wraps four previously missing operations behind
three typed public methods. All calls use the client's existing read pacer,
retry policy, cooldown circuit breaker, and auth/error sentinels.

### Implemented in SDK

- [x] Mobile Top REST (`fbsearch/top_serp`) — `SearchPosts(query)` walks the
  captured media-grid layouts and preserves `next_max_id`, `reels_max_id`, and
  `rank_token` in its opaque resumable iterator cursor.
- [x] Web keyword GraphQL initial operation — `SearchKeywordPosts(query)` uses
  `PolarisKeywordSearchExplorePageRelayQuery` for the first iterator page.
- [x] Web keyword GraphQL continuation operation — the same iterator switches
  to `PolarisKeywordSearchExplorePageRelayPaginationQuery`; its versioned,
  query-bound opaque cursor preserves the preceding non-empty
  `page_info.end_cursor` plus both GraphQL session IDs for faithful resume on a
  fresh iterator.
- [x] Mobile Accounts SERP — `SearchAccounts(ctx, query)` maps account cards,
  page/rank tokens, friendship status, and search social context.
- [x] Mobile keyword typeahead — `SearchTypeaheadUsers(ctx, query, count)` maps
  its account suggestions and rank token; `KeywordTypeahead(ctx, query)` exposes
  those lightweight entities as suggestion strings.
- [x] Mobile Reels REST (`fbsearch/reels_serp`) — `SearchReels(query)` maps the
  captured `reels_serp_modules[].clips[].media` nodes through the shared `Post`
  parser. It remains first-page-only until a continuation request is captured.

### Deferred

- [ ] Mobile Reels REST (`fbsearch/reels_serp`) — deferred as duplicate media
  coverage; its next-page request parameter contract was not observed.
- [ ] Mobile Top REST GraphQL-only framing — Top REST remains available via
  `SearchPosts`; GraphQL iterators cover typed keyword pagination separately.
- [ ] Related-keyword / SERP-filter GraphQL — deferred because the inventory
  contains no successful persisted operation for either surface.
- [ ] Dedicated sound/audio search — deferred because no sound-search request
  was captured; audio fields nested on Reel media do not prove a search op.
- [ ] Search write actions — deferred because they are write-only and expressly
  outside the issue's read-discovery scope.

## Scripted mobile inventory

The probe in [`cmd/instagram-search-inventory`](../../cmd/instagram-search-inventory/)
validates the burner session against `GET /api/v1/fbsearch/top_serp/`, then
captures these four app surfaces using the same keyword. Every row is required
for a successful report.

| Tab | Method and path | Required first-page parameters | Pagination fields inventoried from the live response |
| --- | --- | --- | --- |
| Top | `GET /api/v1/fbsearch/top_serp/` | `query`, `search_surface=top_serp`, `timezone_offset`, `rank_token` | `next_max_id`, `reels_max_id`, `rank_token`, `has_more` wherever observed |
| Reels | `GET /api/v1/fbsearch/reels_serp/` | `query`, `search_surface=clips_search_page`, `timezone_offset` | `reels_max_id`, `rank_token`, `has_more` wherever observed |
| Accounts | `GET /api/v1/fbsearch/account_serp/` | `query`, `search_surface=account_serp`, `timezone_offset` | `page_token`, `next_page_token`, `paging_token`, `has_more` wherever observed |
| Keyword typeahead | `GET /api/v1/fbsearch/typeahead_stream/` | `query`, `search_surface=typeahead_search_page`, `timezone_offset`, `context=blended`, `count` | any cursor fields actually returned |

The expected media envelope is not hard-coded as proof. The capture walks the
live response and records every actual field path. It recognizes media only
when a node has `media_type`, `code`, and one of `pk`, `pk_id`, or `id`. This
guards against mistakenly treating the Accounts response as keyword-to-post
evidence. Known layouts such as
`media_grid.sections[].layout_content.fill_items[].media`,
`one_by_two_item.media`, `one_by_two_item.clips.items[].media`, and `medias[]`
are therefore discovered without assuming that one remains canonical.

## GraphQL inventory

The authenticated web capture observed two live persisted operations. The
separate initial and continuation documents are important: using the initial
`doc_id` for pagination is not the captured contract.

| Request | Method and path | Friendly name | Live `doc_id` |
| --- | --- | --- | --- |
| Initial | `POST https://www.instagram.com/graphql/query` | `PolarisKeywordSearchExplorePageRelayQuery` | `26586987494245638` |
| Continuation | `POST https://www.instagram.com/graphql/query` | `PolarisKeywordSearchExplorePageRelayPaginationQuery` | `26577336451926911` |

Both successful responses used the Relay connection at
`data.xdt_fbsearch__top_serp_graphql`. Media-grid units contained recognized
post nodes at `edges[].node.items[]`; `page_info.has_next_page` and
`page_info.end_cursor` provide continuation state. The exact transport fields,
GraphQL variable-name set, heterogeneous edge shape, pagination behavior, and
scrubbed media sample are retained in the linked capture. The persisted IDs are
dated observations and must be recaptured after a schema or persisted-query
failure.

Mobile `fbsearch` requests cannot reveal web GraphQL persisted-query IDs. For a
fresh recapture, `-har` makes the probe independently inventory search-related
calls to `/graphql/query` or `/api/graphql` and record:

- HTTP method, host, and path
- `x-fb-friendly-name` / `fb_api_req_friendly_name`
- live `doc_id`
- required top-level params and GraphQL variable names, never values
- response field paths and media-node paths
- pagination fields including `end_cursor` and `has_next_page`

The verifier accepts a GraphQL surface only when the matching HAR entry has a
2xx response, a non-null `data` payload, and at least one recognized media/post
node. Non-2xx calls, GraphQL error-only responses, and entity-only response
shapes are rejected as capture evidence. A failed duplicate also cannot hide a
later successful response for the same friendly-name/`doc_id` pair.

Persisted `doc_id` values rotate. Every generated report is UTC date-stamped and
warns consumers to re-capture stale IDs. A generated REST report without a HAR
does not supersede the dated GraphQL capture above and is not fresh GraphQL
evidence.

## Host differences

- App Search tabs use `https://i.instagram.com/api/v1/fbsearch/*` with a mobile
  app ID/user-agent and authenticated burner session.
- The existing SDK `Search` uses
  `https://www.instagram.com/api/v1/web/search/topsearch/` and returns only
  users, hashtags, and places. It is entity typeahead, not keyword media SERP.
- Web persisted GraphQL normally uses
  `https://www.instagram.com/graphql/query` or `/api/graphql`. Its friendly
  names, variables, and `doc_id` values must come from the HAR from the same live
  search session.

## Media node → `Post` candidates

| Live field | Existing model candidate |
| --- | --- |
| `pk` / `pk_id` | `Post.PK` |
| `id` | `Post.ID` |
| `code` | `Post.Code` and permalink construction |
| `media_type` | `Post.MediaType` |
| `product_type` | `Post.ProductType` |
| `taken_at` | `Post.TakenAt` |
| `caption.text` | `Post.Caption` |
| `user` | `Post.Owner` |
| `like_count`, `comment_count` | `Post.LikeCount`, `Post.CommentCount` |
| `view_count`, `play_count` | `Post.ViewCount`, `Post.PlayCount` |
| `image_versions2.candidates` | `Post.ImageVersions` |
| `video_versions` | `Post.VideoVersions` |
| `carousel_media` | `Post.CarouselMedia` |
| `clips_metadata` | `Post.ClipsMetadata` |

The generated report includes a small live sample containing public media
PK/shortcode and scalar model candidates. Captions are reduced to a boolean
presence flag; CDN query strings are removed.

## Completeness and secret boundary

A capture is labeled complete for mobile REST only after session validation,
HTTP success for Top/Reels/Accounts/typeahead, valid JSON for every surface, and
at least one media/post node in each of Top and Reels. Any missing or invalid
credential, challenge, checkpoint, rejected tab, malformed response, missing
media, or malformed HAR aborts before the destination is created.

The command never serializes Cookie, `sessionid`, password, Authorization,
CSRF, access-token, raw header, cursor value, or full raw response data. Source
cookie files and HARs must remain outside the repository. Before committing a
generated report, still run a human review and a repository secret scan.

Issue #11 adds the typed discovery methods listed in the checklist while
leaving the signatures and behavior of `Search` and `SearchUsers` unchanged.
