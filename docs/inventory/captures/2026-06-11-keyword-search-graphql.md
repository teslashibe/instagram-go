# Instagram authenticated keyword-search GraphQL capture

Captured at: `2026-06-11` UTC  
Host: `https://www.instagram.com`  
Surface: authenticated Search → Top keyword results  
Auth state: logged-in burner web session  
Query: scrubbed  
Result: **complete for the observed initial and continuation GraphQL calls**;
both calls returned HTTP 200 with a non-null `data` payload and recognized
media/post nodes.

This is a normalized, secret-reviewed inventory from the authenticated page
capture. The source page snapshot and raw HAR are intentionally not committed:
they contained the burner session, viewer ID, request tokens, query text,
cursors, raw response data, and signed CDN URLs. The inventory retains only the
operation identities, parameter and variable names, response field paths,
pagination field names, JSON types, and a scrubbed media-model sample.

Persisted `doc_id` values rotate. These values prove the observed surface and
were live on the capture date; recapture them before production use if either
operation returns a persisted-query or schema error.

## Initial page

- Request: `POST https://www.instagram.com/graphql/query`
- Friendly name: `PolarisKeywordSearchExplorePageRelayQuery`
- Captured `doc_id`: `26586987494245638`
- Live status: `200`
- Content type: `application/x-www-form-urlencoded`
- Captured transport parameter names: `__a`, `__d`, `doc_id`,
  `fb_api_caller_class`, `fb_api_req_friendly_name`, `server_timestamps`,
  `variables`
- GraphQL variable names: `after`, `first`, `query`, `search_session_id`,
  `serp_session_id`
- Observed variable types: `after=null`, `first=integer`, and the query and two
  session identifiers as strings; every value is omitted
- Pagination fields: `data.xdt_fbsearch__top_serp_graphql.page_info.has_next_page`,
  `data.xdt_fbsearch__top_serp_graphql.page_info.end_cursor`, and
  `data.xdt_fbsearch__top_serp_graphql.edges[].cursor`
- Media node path:
  `data.xdt_fbsearch__top_serp_graphql.edges[].node.items[]` when
  `node.__typename == "XDTTopSerpMediaGridUnit"`

## Continuation page

- Request: `POST https://www.instagram.com/graphql/query`
- Friendly name: `PolarisKeywordSearchExplorePageRelayPaginationQuery`
- Captured `doc_id`: `26577336451926911`
- Live status: `200`
- Content type and transport parameter names: identical to the initial request
- GraphQL variable names: `after`, `first`, `query`, `search_session_id`,
  `serp_session_id`
- Observed continuation behavior: `after` is the preceding response's non-empty
  `page_info.end_cursor`; the other variable types remain unchanged
- Pagination fields and media node path: identical Relay connection contract to
  the initial page

Only `page_info.end_cursor` is the next-page token. `edges[].cursor` is
per-edge state and was not used as the continuation variable. The raw values of
all cursor and session fields were discarded.

## Response field paths

The successful response selection included these paths. Arrays are denoted by
`[]`; values are not retained.

```text
$.data
$.data.xdt_viewer
$.data.xdt_viewer.user
$.data.xdt_viewer.user.id
$.data.xdt_viewer.user.hide_like_and_view_counts
$.data.xdt_fbsearch__top_serp_graphql
$.data.xdt_fbsearch__top_serp_graphql.edges
$.data.xdt_fbsearch__top_serp_graphql.edges[]
$.data.xdt_fbsearch__top_serp_graphql.edges[].cursor
$.data.xdt_fbsearch__top_serp_graphql.edges[].node
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.__typename
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.unit_type
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[]
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].__typename
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].id
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].pk
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].code
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].media_type
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].product_type
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].taken_at
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].original_width
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].original_height
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].image_versions2.candidates[]
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].image_versions2.candidates[].height
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].image_versions2.candidates[].url
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].image_versions2.candidates[].width
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].video_versions[]
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].video_versions[].height
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].video_versions[].type
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].video_versions[].url
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].video_versions[].width
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].caption.text
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.id
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.pk
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.username
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.full_name
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.profile_pic_url
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.is_verified
$.data.xdt_fbsearch__top_serp_graphql.edges[].node.items[].user.is_private
$.data.xdt_fbsearch__top_serp_graphql.page_info
$.data.xdt_fbsearch__top_serp_graphql.page_info.has_next_page
$.data.xdt_fbsearch__top_serp_graphql.page_info.end_cursor
$.extensions.is_final
```

Edges are heterogeneous. Only an `XDTTopSerpMediaGridUnit` was treated as
keyword-to-post evidence; account, header, information, and Meta AI units were
not counted as media.

## Scrubbed media sample

The normalized sample preserves observed JSON field names and types. Public
identifiers, account data, caption content, CDN URLs, and viewer/session state
are redacted or reduced to presence flags.

```json
{
  "id_present": true,
  "pk_present": true,
  "code_present": true,
  "media_type": 2,
  "product_type": "clips",
  "taken_at_present": true,
  "original_width": 1080,
  "original_height": 1920,
  "image_versions2": {
    "candidates_present": true
  },
  "video_versions_present": true,
  "video_dash_manifest_present": false,
  "is_dash_eligible": false,
  "number_of_qualities": 1,
  "has_audio": true,
  "caption_text_present": true,
  "user": {
    "id_present": true,
    "pk_present": true,
    "username_present": true,
    "full_name_present": true,
    "profile_pic_url_present": true,
    "is_verified": false,
    "is_private": false
  },
  "carousel_media_count": null,
  "carousel_media": null
}
```

## `Post` candidate mapping

| Captured GraphQL media field | Existing model candidate |
| --- | --- |
| `pk` | `Post.PK` |
| `id` | `Post.ID` |
| `code` | `Post.Code` and permalink construction |
| `media_type` | `Post.MediaType` |
| `product_type` | `Post.ProductType` |
| `taken_at` | `Post.TakenAt` |
| `caption.text` | `Post.Caption` |
| `user` | `Post.Owner` |
| `image_versions2.candidates` | `Post.ImageVersions` |
| `video_versions` | `Post.VideoVersions` |
| `carousel_media` | `Post.CarouselMedia` |

This capture changes no production API. `Search` and `SearchUsers` retain their
existing entity-typeahead behavior.
