# Instagram keyword-search GraphQL inventory

This inventory records the private web GraphQL contract observed on an
authenticated Instagram keyword-search page. It is capture evidence, not a
promise that the private endpoint or persisted document IDs will remain stable.

## Capture provenance

- Captured surface: `https://www.instagram.com/explore/search/keyword/?q=…`
- Transport: `POST https://www.instagram.com/graphql/query`
- Auth state: logged-in web session
- Capture date: 2026-06-11 UTC
- Query text, session IDs, viewer ID, cursors, media/account IDs, captions, and
  CDN URLs: scrubbed
- Source evidence: the saved production entrypoint and Relay artifacts in the
  same authenticated page snapshot, archived at commit
  [`4bd6d29`](https://github.com/DEEDSCOUT/HappyFaceLA/commit/4bd6d29d4ec119e120d860d4697c3e9777d25bd2)

The raw page snapshot is not copied into this repository because it contains a
large amount of unrelated application code and page data. The normalized media
sample committed at
[`testdata/keyword_search_graphql_response.json`](../testdata/keyword_search_graphql_response.json)
retains the observed field names, nesting, JSON types, and nullable values.

## Persisted operations

The first page and continuation page use different friendly names and
persisted documents.

| Request | `x-fb-friendly-name` / `fb_api_req_friendly_name` | `doc_id` |
| --- | --- | --- |
| Initial | `PolarisKeywordSearchExplorePageRelayQuery` | `26586987494245638` |
| Continuation | `PolarisKeywordSearchExplorePageRelayPaginationQuery` | `26577336451926911` |

These IDs were live at the capture date and are expected to rotate. A caller
must recapture both IDs when Instagram replaces either Relay artifact.

## Request inventory

The body is `application/x-www-form-urlencoded`. The observed GraphQL transport
field names were:

```text
__a
__d
doc_id
fb_api_caller_class
fb_api_req_friendly_name
server_timestamps
variables
```

Authenticated web requests can also carry `fb_dtsg` and `lsd`. Their values,
plus cookies and CSRF headers, are session credentials and must never be stored
in a capture.

Both persisted operations declare this exact top-level GraphQL variable-name
set:

```text
after
first
query
search_session_id
serp_session_id
```

The observed first-page values had these types and semantics:

```json
{
  "after": null,
  "first": 24,
  "query": "<scrubbed string>",
  "search_session_id": "<scrubbed string>",
  "serp_session_id": "<scrubbed string>"
}
```

The continuation request keeps the same variable names and changes `after` to
the preceding response's non-empty `page_info.end_cursor`. `first` remains an
integer and the query/session variables remain strings. Instagram's compiled
request object also contains `request_data.is_discovery_grid_enabled=false`
and `request_data.is_pagination_request=false` as document literals; they are
not client variable names and must not be added to the serialized variable set.

## Response shape

The response is a Relay connection. Edges are heterogeneous units, so code must
check `node.__typename` rather than assuming every edge contains media.

```text
data
├── xdt_viewer
│   └── user
│       ├── id: string
│       └── hide_like_and_view_counts: boolean
└── xdt_fbsearch__top_serp_graphql
    ├── edges: array
    │   └── []
    │       ├── cursor: string | null
    │       └── node
    │           ├── __typename: string
    │           ├── unit_type: string
    │           └── items: array                  # media-grid units only
    │               └── []: XDTMediaDict
    └── page_info
        ├── has_next_page: boolean
        └── end_cursor: string | null
```

Media results are found only at:

```text
$.data.xdt_fbsearch__top_serp_graphql.edges[]
  .node[__typename == "XDTTopSerpMediaGridUnit"].items[]
```

The captured media selection includes identifiers (`id`, `pk`, `code`), media
classification and dimensions, timestamps, image/video versions, owner fields,
caption text, audio/video flags, and nullable carousel fields. The committed
sample contains one scrubbed `XDTMediaDict` with those representative fields.
Other observed unit types include headers, accounts, inform modules, and Meta AI
units; their `items` must not be parsed as posts.

## Pagination contract

| Response field | JSON type | Meaning |
| --- | --- | --- |
| `page_info.has_next_page` | boolean | Whether the connection reports another page |
| `page_info.end_cursor` | string or null | Cursor passed as the next request's `after` variable |
| `edges[].cursor` | string or null | Per-edge Relay cursor; not the page continuation token |

Only continue when `has_next_page` is `true` and `end_cursor` is a non-empty
string. The next request uses the continuation friendly name and `doc_id` from
the operation table. The scrubbed cursor in the fixture is deliberately not a
replayable production value.

## Security and compatibility boundary

Do not commit raw HAR files, cookies, `sessionid`, CSRF values, `fb_dtsg`, `lsd`,
Authorization headers, session IDs, viewer IDs, real cursors, or signed CDN URL
query strings. Re-capture against a burner account after any schema or persisted
ID failure.

`SearchKeywordPosts` now wraps both captured persisted operations and maps only
media-grid units into typed `Post` values. Existing `Search` and `SearchUsers`
methods continue to use their documented REST endpoints unchanged. The full
Implemented in SDK vs Deferred checklist is maintained in
[`docs/inventory/search-graphql.md`](inventory/search-graphql.md#sdk-implementation-checklist).
