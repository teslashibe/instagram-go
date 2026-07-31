# Instagram search inventory probe

This command captures the live, authenticated evidence required by
[`docs/inventory/search-graphql.md`](../../docs/inventory/search-graphql.md). It
does not add a production search API and does not change `Search` or
`SearchUsers`.

The probe fails closed unless all four mobile REST tabs respond and Top/Reels
contain at least one media node. It builds the complete report in memory and
uses an atomic write only after those checks pass. Request headers, cookies,
CSRF values, Authorization values, cursor values, raw response bodies, and CDN
URL signatures are never written.

## Authentication

Use a burner account and one of these inputs, in priority order:

1. `INSTAGRAM_COOKIES_FILE=/absolute/path/to/cookies.json` (recommended)
2. `INSTAGRAM_COOKIES_JSON='{"sessionid":"...", ...}'`
3. `INSTAGRAM_SESSIONID`, with optional `INSTAGRAM_CSRFTOKEN`,
   `INSTAGRAM_DS_USER_ID`, `INSTAGRAM_DATR`, `INSTAGRAM_MID`, and
   `INSTAGRAM_IG_DID`
4. `INSTAGRAM_USERNAME`, `INSTAGRAM_PASSWORD`, and
   `SOCIAL_LOGIN_SIDECAR_URL` for the existing browser-login sidecar

`INSTAGRAM_COOKIES_FILE` accepts a flat cookie object, an instagrapi-style
`{"cookies": {...}}` object, or a browser-export array of `{name,value}` items.
Keep credential files outside the repository. A residential proxy can be
provided with `INSTAGRAM_PROXY_URL`.

## Capture

Choose a date-stamped destination that does not already exist:

```bash
INSTAGRAM_COOKIES_FILE=/secure/burner-cookies.json \
go run ./cmd/instagram-search-inventory \
  -query "specialty coffee" \
  -output docs/inventory/captures/2026-07-31-specialty-coffee.md
```

The default host is `https://i.instagram.com`. Override it with `-host` only to
compare host behavior. The command refuses to overwrite an existing capture.

To inventory rotating web/app GraphQL operations, export a HAR after exercising
Search → Top, Reels, and Accounts, then add `-har /secure/search.har`. The HAR is
read locally and is never copied. Only search-related friendly names, `doc_id`
values, variable **names**, response field paths, pagination field names, and a
small media-model candidate sample are retained. Inspect the generated markdown
before committing it; never commit the source HAR. A GraphQL entry is retained
only when it returned 2xx, contains a usable `data` payload, and includes at
least one recognizable media/post node. Failed, error-only, and entity-only
entries cannot establish a captured GraphQL search surface.

## Failure behavior

- Missing credentials: exits before making requests or creating output.
- Invalid/challenged session: reports authentication rejection and creates no
  output.
- Missing tab, or no post media in either Top or Reels: reports the failed
  surface and creates no output, rather than labeling a partial capture
  complete.
- Malformed HAR: rejects the entire report and creates no output.

GraphQL calls cannot be discovered from the scripted mobile REST requests. A
report generated without `-har` explicitly records GraphQL as not captured.
