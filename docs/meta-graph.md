# Official Meta Graph and Marketing API

The `meta` package is the official OAuth path for Instagram professional
(business or creator) account insights and Meta advertising. It is independent
from the root `instagram` package, which calls Instagram's private web/mobile
API with browser cookies.

## Choose the right client

| Capability | Root `instagram.Client` | `meta.Client` |
|------------|:-----------------------:|:-------------:|
| Credential | Instagram session cookies | Meta OAuth bearer token |
| Consumer profiles, feeds, comments, follows | Yes | No |
| Instagram professional account/media insights | No | Yes |
| Facebook Page and Instagram account linkage | No | Yes |
| Ad accounts, campaigns, ad sets, ads, creatives | No | Yes, read-only by default |
| Marketing delivery and performance insights | No | Yes |
| MCP platform | `instagram` | `instagram_meta` |

Credentials must never cross this boundary. Official requests send only an
`Authorization: Bearer ...` header; they do not send Cookie, CSRF, or private
Instagram app headers.

## OAuth scopes

`meta.DefaultReadScopes` contains the normal read path:

- `public_profile`
- `pages_show_list`
- `pages_read_engagement`
- `instagram_basic`
- `instagram_manage_insights`
- `ads_read`
- `business_management`

Meta App Review and business verification may be required before these
permissions work for people who do not have a role on the app. Request only the
permissions the deployment uses.

`ads_management` is not part of `DefaultReadScopes`. It can only be requested
when `Config.EnableAdMutations` is true, and `meta.New` rejects mutation-enabled
configuration unless that separately approved scope is present.
`Config.GrantedScopes` is the configured scope set the default authorization
URL requests; it is not treated as proof of a grant.

```go
store := meta.NewMemoryTokenStore(meta.Token{}) // use encrypted durable storage in production
client, err := meta.New(meta.Config{
    AppID:         os.Getenv("META_APP_ID"),
    AppSecret:     os.Getenv("META_APP_SECRET"),
    RedirectURI:   "https://app.example.com/oauth/meta/callback",
    TokenStore:    store,
    GrantedScopes: meta.DefaultReadScopes,
})
```

Create an authorization URL with an unpredictable state value, verify that
state in the callback, and exchange the returned code:

```go
authorizationURL, err := client.AuthorizationURL(state)
token, err := client.ExchangeCode(ctx, code)
```

## Token storage and renewal

Applications provide a concurrency-safe `meta.TokenStore`. Persist tokens in
encrypted secret storage and restrict access to the owning user or tenant.
`MemoryTokenStore` is suitable only for tests and short-lived processes.

`ExchangeCode` and `RefreshToken` save the resulting token only after a
successful exchange and a `/me/permissions` check. `Token.Scopes` therefore
contains only permissions whose current status is `granted`; requested or
declined permissions are never copied into stored grant state. Requests fail
before transport when a token is absent, expired, or lacks a required verified
scope. Meta does not guarantee that every long-lived user token can be silently
renewed indefinitely; `ErrReauthorizationRequired` means the user must complete
Facebook Login again. Token values are excluded from Graph errors, MCP errors,
pagination cursors, and `Token.String`.

The Graph API version defaults to `meta.DefaultAPIVersion`. Pin a different
supported version with `meta.WithAPIVersion` and validate metric availability
when upgrading.

## Page linkage and account selection

`ListPages` returns visible Facebook Pages and each Page's linked
`instagram_business_account`. Page access tokens are not requested or returned.
Selection is explicit and verified against Graph before it is stored:

```go
pages, err := client.ListPages(ctx, meta.ListOptions{Limit: 25})
account, err := client.SelectInstagramBusinessAccount(ctx, pageID, instagramAccountID)

adAccounts, err := client.ListAdAccounts(ctx, meta.ListOptions{Limit: 25})
adAccount, err := client.SelectAdAccount(ctx, adAccountID)
```

Calls that omit an account ID use the selected account and otherwise fail with
`ErrInvalidInput`. This prevents an ambiguous Page or Business Manager linkage
from silently choosing an account.

## Insights

Account and media reads use typed metric and period constants. Account insight
timeframes require `since < until` and are bounded to 93 days per request.
Account requests also select `MetricTypeTimeSeries` or `MetricTypeTotalValue`;
the latter is required for `accounts_engaged`, `total_interactions`, and
`follows_and_unfollows`. Metrics from the two response families cannot be mixed
in one request. Metric/period combinations are validated explicitly. Media
insights require a typed media surface (`image`, `carousel_album`, `video`,
`reel`, or `story`), use `lifetime`, and reject metrics unavailable for that
surface. Unknown, duplicate, or incompatible metrics fail before an HTTP
request.

```go
insights, err := client.GetAccountInsights(ctx, meta.AccountInsightsRequest{
    Metrics:    []meta.AccountMetric{meta.AccountMetricReach, meta.AccountMetricProfileViews},
    MetricType: meta.MetricTypeTimeSeries,
    Period:     meta.PeriodDay,
    Timeframe: meta.Timeframe{Since: since, Until: until},
})
```

All list and insight methods accept `ListOptions`. Returned cursors are opaque,
versioned, and bound to the resource, selected account, fields, filters, and
limit. A malformed cursor or one reused for another query is rejected before
transport.

## Read-only advertising

The initial public surface reads:

- ad accounts and their currency/timezone/status;
- campaigns and effective delivery state;
- ad sets, budgets, schedules, optimization, and targeting;
- ads and creative references;
- creative metadata;
- configured/effective delivery state; and
- account/campaign/ad-set/ad-level delivery insights.

Fields, metrics, reporting levels, statuses, date ranges, and pagination are
validated allowlists. The client does not expose arbitrary Graph paths or
caller-selected fields.

## Mutation safety

Ad mutation methods are disabled unless all of these conditions hold:

1. `Config.EnableAdMutations` was explicitly set.
2. The separately approved `ads_management` scope is granted.
3. A positive budget in minor currency units, ISO currency, start date, and end
   date are supplied.
4. `AdMutationConfirmation` exactly repeats the account, budget amount, budget
   kind (`daily` or `lifetime`), budget owner type and ID, currency, and dates,
   sets `Approved`, and uses `meta.AdMutationConfirmationPhrase`.
5. The target ad, its ad set, and any campaign-level budget/schedule resolve to
   the confirmed ad account.
6. The confirmed budget amount, kind, owner, currency, and dates exactly match
   the effective budget/schedule and account currency returned by Graph.

Malformed or self-inconsistent confirmations fail before any Graph request.
Ownership, budget kind/owner, and effective spend/schedule mismatches fail after
read verification but before the write. Mutations are deliberately
excluded from the initial `meta/mcp` provider; the SDK guard exists so a future
write surface cannot bypass the policy.

## Independent MCP binding

Mount `meta/mcp.Provider{}` with a `*meta.Client`. Its platform and tool names
use the `instagram_meta` prefix and cannot accept `*instagram.Client`.

The provider maps expected authorization failures to structured tool errors:

| Code | Meaning |
|------|---------|
| `credential_expired` | Stored OAuth token expired |
| `reauthorization_required` | Facebook Login must be completed again |
| `missing_scope` | One or more required OAuth permissions were not granted |
| `permission_denied` | Graph denied the selected resource |
| `invalid_input` | Metric, period, date, account, status, or cursor validation failed |

No mutation tool is registered in this initial provider.

## Testing

All committed tests are offline and use sanitized fixtures:

```bash
go test ./...
go test -race -count=1 ./meta/... ./mcp/...
go vet ./...
go build ./...
```

Keep live Meta credentials in environment-backed secret storage. Never commit
access tokens, app secrets, authorization codes, raw Graph paging URLs, or
production response bodies.
