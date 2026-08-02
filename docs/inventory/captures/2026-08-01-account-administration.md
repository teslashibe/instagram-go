# Instagram account-administration burner capture

Captured at: `2026-08-01` UTC  
Captured surface: authenticated mobile account administration  
Authentication: dedicated burner session  
Review status: **human-reviewed and secret-scrubbed**

Host: `https://i.instagram.com`

Authenticated account: `<redacted>`
Result: **complete for the three allowlisted read projections**.

This is a normalized inventory from a successful read-only burner capture. The
live response returned HTTP 200 with `status=ok`; its account ID matched the
session's `ds_user_id`, and every field required by the three projections was
present with the expected JSON type. A human reviewed the normalized artifact
before commit.

The raw response and session material are intentionally not committed because
the response may contain account PII and security-adjacent state. This artifact
retains only the observed request contract, account-ID path, and allowlisted
response field paths. Cookies, CSRF and authorization values, account and
contact values, raw headers, and unapproved response fields were discarded.

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
