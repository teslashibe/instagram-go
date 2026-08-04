# Instagram account-administration read capture

Captured at: `2026-08-01T00:00:00Z`

Host: `https://i.instagram.com`

Authenticated account: `<redacted>`
Result: **complete for the three allowlisted read projections**.

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
