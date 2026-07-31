# Inventory probe live validation

AC1 has a successful live burner-session result. This record indexes the exact
probe and its full secret-scrubbed generated output, both committed in this
branch:

- Probe: [`cmd/instagram-search-inventory`](../cmd/instagram-search-inventory/)
- Full output: [`docs/inventory/captures/2026-07-31-coffee-rest.md`](inventory/captures/2026-07-31-coffee-rest.md)
- Capture contract: [`docs/inventory/search-graphql.md`](inventory/search-graphql.md)

The live run used a browser-minted burner cookie file kept outside the
repository:

```text
$ INSTAGRAM_COOKIES_FILE=<redacted> go run ./cmd/instagram-search-inventory -query coffee -output docs/inventory/captures/2026-07-31-coffee-rest.md
PASS: captured 4 REST and 0 GraphQL search surfaces with 32 media nodes -> docs/inventory/captures/2026-07-31-coffee-rest.md
```

| Field | Observed value |
| --- | --- |
| Captured at | `2026-07-31T20:50:17Z` |
| Authentication | Browser-minted burner session accepted by the probe's live `top_serp` authentication request |
| Keyword | `coffee` |
| Search request | `GET https://i.instagram.com/api/v1/fbsearch/top_serp/` |
| Live status | `200` |
| Media/post nodes | `32` across the four scripted mobile REST search tabs |
| Public sample shortcode | `DZ76pVrFrV9` |
| Public sample permalink | `https://www.instagram.com/reel/DZ76pVrFrV9/` |

The submitted probe authenticates by making a real `top_serp` request with the
minted burner session and fails closed on an Instagram `status=fail` response.
It then executes the four documented keyword surfaces and refuses to write the
report unless both Top and Reels return media/post nodes. Reaching the `PASS`
line therefore proves authentication, the configured `coffee` keyword search,
and a nonzero media result. The sample above is a public video/reel media item
(`media_type=2`, `product_type=clips`).

No username, password, cookies, authorization values, CSRF values, proxy
credentials, raw headers, cursor values, or signed CDN URLs are retained here.
IDs and shortcodes are public media identifiers.

The restored artifact and probe source are byte-for-byte identical to the live
capture commit `368b6c1ef278410e82a28096f4b8bf833a0949df` (capture SHA-256
`cfbf37f46f15766a2cde44164a86203c5939664f0eb3c63ac3d45d097bbc1bb6`).
`cmd/instagram-login-probe` remains a separate sidecar-login smoke path; it is
not cited as the source of this mobile SERP capture.
