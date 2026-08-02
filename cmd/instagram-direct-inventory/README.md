# Instagram Direct inventory verifier

This command inspects a locally exported HAR from an authenticated burner
account and writes only a response-shape contract for Instagram Direct. It
never performs a network request or sends a message itself.

The verifier fails closed unless the HAR contains successful contracts for:

- inbox pagination;
- retrieval of a thread that was present in that same authenticated inbox;
- one-recipient thread creation for the explicitly approved burner/self ID;
- non-empty text broadcast to the created thread with matching
  `client_context`/`mutation_token` and an `offline_threading_id`.

Use an isolated burner conversation or the authenticated account's self-chat.
Export the HAR only after approving the exact recipient, keep the HAR outside
the repository, and repeat the approved ID as confirmation:

```bash
IG_DS_USER_ID='<burner-viewer-id>' \
IG_DIRECT_APPROVED_RECIPIENT_ID='<approved-burner-or-self-id>' \
IG_DIRECT_CONFIRM_RECIPIENT_ID='<same-id>' \
go run ./cmd/instagram-direct-inventory \
  -har /secure/direct-session.har \
  -output docs/inventory/captures/YYYY-MM-DD-direct.md
```

The generated report contains method/host/redacted path, request field names,
header names, response field paths, and pagination field names. It does not
retain header values, cookies, authorization, CSRF, IDs, cursors, usernames,
message text, client contexts, or raw payloads. The destination must not exist.
Human secret review is still required before committing a generated report.

Attachments, reactions, vanish mode, and group administration are outside this
capture and must not be inferred from fields that happen to appear in a payload.
