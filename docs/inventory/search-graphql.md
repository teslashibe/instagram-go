# Instagram keyword search capture inventory

Status: **probe implemented; live artifact pending an authenticated burner
session and, for GraphQL, an operator-supplied Search HAR**.

This document is the durable capture contract for issue #6. It deliberately
does not claim a live result in this repository: no operator-supplied session
cookie or reachable social-login sidecar was available for this implementation
run, so authentication could not be completed without inventing evidence.
Successful date-stamped output from the probe belongs under
`docs/inventory/captures/` after human secret review.

## Scripted mobile inventory

The probe in [`cmd/instagram-search-inventory`](../../cmd/instagram-search-inventory/)
authenticates first with `GET /api/v1/accounts/current_user/?edit=true`, then
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

Mobile `fbsearch` requests cannot reveal web GraphQL persisted-query IDs. With
`-har`, the probe independently inventories search-related calls to
`/graphql/query` or `/api/graphql` and records:

- HTTP method, host, and path
- `x-fb-friendly-name` / `fb_api_req_friendly_name`
- live `doc_id`
- required top-level params and GraphQL variable names, never values
- response field paths and media-node paths
- pagination fields including `end_cursor` and `has_next_page`

Persisted `doc_id` values rotate. Every generated report is UTC date-stamped and
warns consumers to re-capture stale IDs. A run without a HAR explicitly says
GraphQL was not captured; it must not be interpreted as a complete GraphQL
inventory.

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
at least one media/post node. Any missing or invalid credential, challenge,
checkpoint, rejected tab, malformed response, missing media, or malformed HAR
aborts before the destination is created.

The command never serializes Cookie, `sessionid`, password, Authorization,
CSRF, access-token, raw header, cursor value, or full raw response data. Source
cookie files and HARs must remain outside the repository. Before committing a
generated report, still run a human review and a repository secret scan.

No production SDK method is added by this ticket. `Search` and `SearchUsers`
remain unchanged for compatibility.
