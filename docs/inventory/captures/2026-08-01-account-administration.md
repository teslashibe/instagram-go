# Instagram account-administration read capture

Captured at: `2026-08-01T00:00:00Z`

Host: `https://i.instagram.com`

Authenticated account: `<redacted>`
Result: **complete for the three allowlisted read projections**.
Live reversible-write result: **profile PASS/restored; privacy PASS/restored;
professional display SKIP/inapplicable** (`2026-08-02T01:54:00Z`).

This read-contract capture is supplemented by the
[`account-administration live validation record`](../../account-administration-live-validation.md),
which records the burner-only profile/privacy restoration results and the
professional-display applicability outcome.

> This checked-in contract artifact contains only the deterministic,
> secret-scrubbed shape accepted by the inventory verifier. Re-run the command
> with a burner session and replace it with a fresh date-stamped capture before
> changing any private endpoint or field mapping.

## Current account

- Request: `GET https://i.instagram.com/api/v1/accounts/current_user/?edit=<redacted>`
- Query parameter names: `edit`
- Account ID path: `$.user.pk` (value must match `ds_user_id`)
- Allowlisted response fields:

```text
$.user.pk
$.user.username
$.user.full_name
$.user.biography
$.user.external_url
$.user.is_private
$.user.is_professional_account
$.user.account_type
```

## Account settings

- Request: `GET https://i.instagram.com/api/v1/accounts/current_user/?edit=<redacted>`
- Query parameter names: `edit`
- Account ID path: `$.user.pk` (value must match `ds_user_id`)
- Allowlisted response fields:

```text
$.user.pk
$.user.full_name
$.user.biography
$.user.external_url
$.user.is_private
```

## Professional-account state

- Request: `GET https://i.instagram.com/api/v1/accounts/current_user/?edit=<redacted>`
- Query parameter names: `edit`
- Account ID path: `$.user.pk` (value must match `ds_user_id`)
- Allowlisted response fields:

```text
$.user.pk
$.user.is_professional_account
$.user.is_business
$.user.account_type
$.user.category_id
$.user.category_name
$.user.should_show_category
```

## Mutation boundary

The capture path sends no writes. The SDK allowlists only profile text fields,
privacy, and existing professional category display settings. Password,
username/email/phone, 2FA, deletion/deactivation, account conversion,
ownership, and security operations are excluded.

## Live reversible smoke-test validation

The following commands were run separately against the same dedicated burner.
The account ID and all setting values are redacted.

| Command | Sanitized outcome |
| --- | --- |
| `go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfileRestoresBurner$' .` | PASS: the temporary profile state was verified, the original full name/biography/external URL were restored, and a final fresh read matched the snapshot |
| `go test -v -count=1 -run '^TestIntegration_AccountAdmin_PrivacyRestoresBurner$' .` | PASS: the temporary privacy state was verified, the original state was restored, and a final fresh read matched the snapshot |
| `go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfessionalDisplayRestoresBurner$' .` | SKIP (inapplicable): the burner was not a professional account, so the preflight sent no professional-display write |

Profile and privacy cleanup handlers were registered before their first write
and repeated the restoration verification at teardown. The complete sanitized
run record and redaction statement are in
[`docs/account-administration-live-validation.md`](../../account-administration-live-validation.md).
