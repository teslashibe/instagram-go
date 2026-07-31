# Inventory probe live validation

AC1 has a successful live burner-session result. This record is a concise,
secret-scrubbed summary of the full capture committed as `368b6c1` before the
repository was consolidated into its current history.

| Field | Observed value |
| --- | --- |
| Captured at | `2026-07-31T20:50:17Z` |
| Authentication | Browser-minted burner session accepted by Instagram search |
| Keyword | `coffee` |
| Search request | `GET https://i.instagram.com/api/v1/fbsearch/top_serp/` |
| Live status | `200` |
| Media/post nodes | `32` across the four scripted mobile REST search tabs |
| Public sample shortcode | `DZ76pVrFrV9` |
| Public sample permalink | `https://www.instagram.com/reel/DZ76pVrFrV9/` |

The probe authenticated by making a real search request with the minted burner
session and failed closed on an Instagram `status=fail` response. The successful
response contained media grids; the capture then counted parsed media nodes and
failed if the count was zero. The sample above was a public video/reel media item
(`media_type=2`, `product_type=clips`).

No username, password, cookies, authorization values, CSRF values, proxy
credentials, raw headers, cursor values, or signed CDN URLs are retained here.
IDs and shortcodes are public media identifiers.

The current `cmd/instagram-login-probe` reproduces the acceptance path from
credentials: sidecar login, session validation, blended keyword lookup, hashtag
selection, and a one-page media fetch. It only prints `PASS` for the keyword
stage after at least one post is observed. Run the opt-in test documented in the
README when a sidecar and burner credentials are available.
