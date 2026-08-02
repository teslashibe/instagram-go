# Instagram account-administration contract

Status: **typed read and guarded reversible-write contract implemented**.

The durable, secret-scrubbed shape capture is
[`captures/2026-08-01-account-administration.md`](./captures/2026-08-01-account-administration.md).
Use [`cmd/instagram-account-inventory`](../../cmd/instagram-account-inventory/)
to refresh it against a dedicated burner after private API drift. A capture is
complete only when authentication succeeds, the returned account ID matches
`ds_user_id`, and all three safe projections are present.

## Read contract

One captured mobile read supplies three deliberately smaller public views. The
SDK does not retain the raw response because this endpoint may also return
contact or security-adjacent data.

| Public method | Request | Allowlisted state |
| --- | --- | --- |
| `GetCurrentAccount(ctx)` | `GET i.instagram.com/api/v1/accounts/current_user/?edit=true` | ID, username, full name, biography, external URL, privacy, professional flag, account type |
| `GetAccountSettings(ctx)` | same captured response | ID, full name, biography, external URL, privacy |
| `GetProfessionalAccountState(ctx)` | same captured response | ID, professional/business flags, account type, category ID/name, category visibility |

Every response ID is checked against the client's authenticated `ds_user_id`.
A missing or mismatched ID fails closed with `ErrAccountMismatch` or
`ErrUnexpectedResponse`.

## Mutation safety boundary

There is no generic request or arbitrary-field method. The only approved
administration writes are:

| Method | Endpoint | Complete allowlist |
| --- | --- | --- |
| `UpdateProfileFields` | `POST /api/v1/accounts/edit_profile/` | `first_name`, `biography`, `external_url` |
| `SetPrivacy` | `POST /api/v1/accounts/set_private/` or `set_public/` | requested privacy transition only |
| `UpdateProfessionalSettings` | `POST /api/v1/business/account/edit/` | `category_id`, `should_show_category` |

Each mutation requires `ExpectedAccountID`, an explicit `Before`, an explicit
`After`, and `Confirm: true`. The client reads fresh state first, rejects
account mismatches, no-ops, stale before-values, and professional changes on a
non-professional account, sends one write attempt, then reads again and returns
success only when the after-value is verified.

Password, username/email/phone, public contact data, 2FA,
deletion/deactivation, account conversion, ownership, and all other security
changes are excluded. They are absent from SDK and MCP mutation input types.

## Failure and cooldown contract

- Authentication and expired-session responses retain `ErrInvalidAuth` and
  `ErrSessionExpired`.
- Challenge/checkpoint responses retain `ErrChallengeRequired`.
- Identity mismatch uses `ErrAccountMismatch` plus `AccountMismatchError`.
- Missing confirmation, no-op, stale state, and failed post-write verification
  use `ErrMutationPrecondition` plus `MutationPreconditionError`.
- Administration writes are never retried after an ambiguous response.
- All writes use the shared write pacer and circuit breaker. Server-requested
  write cooldowns are capped at 30 minutes and waits remain context-cancellable.

## Burner smoke-test protocol

Live mutation tests are disabled by default. They require all existing cookie
variables plus these guards:

```bash
export INSTAGRAM_ACCOUNT_ADMIN_LIVE_TEST=1
export INSTAGRAM_ACCOUNT_ADMIN_BURNER_ID='<exact ds_user_id>'
export INSTAGRAM_ACCOUNT_ADMIN_CONFIRM='RESTORE_BURNER_SETTINGS'
```

Run one test at a time. Each test snapshots the original field, registers a
separate bounded cleanup before mutating, verifies the temporary value, restores
the original, and verifies restoration. Stop immediately and inspect the burner
if cleanup reports a challenge or cooldown.

```bash
go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfileRestoresBurner$' .
go test -v -count=1 -run '^TestIntegration_AccountAdmin_PrivacyRestoresBurner$' .
go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfessionalDisplayRestoresBurner$' .
```

A successful live run prints one sanitized `PASS: burner ... restored` marker
per applicable test. Do not create a validation record from skipped tests or
from offline fixtures. A committed record must include the UTC run time, the
three exact test commands, their PASS/skip outcome (professional display may be
inapplicable to a non-professional burner), and no account values or credentials.
When live mode is enabled, missing cookie configuration is a failure rather
than a skip. The professional-account inapplicable path emits a sanitized
`SKIP: burner professional-display smoke is inapplicable ...` marker and does
not send a write.
