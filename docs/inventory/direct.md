# Instagram Direct private mobile contract

Status: **captured and implemented for one-recipient plain text**. The
secret-reviewed shape record is
[`docs/inventory/captures/2026-08-01-direct.md`](./captures/2026-08-01-direct.md).
New captures must be produced by
[`cmd/instagram-direct-inventory`](../../cmd/instagram-direct-inventory/) and
must pass its same-inbox and matching-recipient gates before review.

## Safety and privacy boundary

Direct payloads contain private conversation text and participant data. Source
HARs and credentials stay outside the repository. Committed evidence retains
only request field names, header names, redacted paths, response field paths,
and pagination field names. It excludes cookies, authorization, CSRF values,
viewer/recipient/thread/item IDs, usernames, text, cursors, client contexts,
device identifiers, raw payloads, and CDN URLs.

The verifier accepts a read contract only when the selected thread ID appeared
in the authenticated viewer's captured inbox. It accepts write evidence only
when the thread creation contains exactly one recipient, the approved and
confirmed burner/self IDs match that recipient, and broadcast targets the
created thread with non-empty text and stable idempotency fields.

## Captured endpoints

All four endpoints use `https://i.instagram.com`, the mobile Android user agent,
mobile app ID, `X-IG-Capabilities`, `X-IG-Connection-Type`, authenticated cookie
header, and CSRF header. The SDK reuses its existing mobile request profile and
does not synthesize an authorization bearer token or WWW claim.

| Surface | Method and path | Captured request fields | Captured continuation |
|---|---|---|---|
| Inbox | `GET /api/v1/direct_v2/inbox/` | `limit`, `thread_message_limit`, `visual_message_return_type`, `persistentBadging`; continuation adds `cursor` | `inbox.has_older`, `inbox.oldest_cursor` |
| Thread | `GET /api/v1/direct_v2/threads/{thread_id}/` | `limit`, `visual_message_return_type`; continuation adds `cursor` | `thread.has_older`, `thread.oldest_cursor` |
| Create | `POST /api/v1/direct_v2/create_group_thread/` | `_uuid` when captured, `recipient_users` containing exactly one ID | returns `thread.thread_id` |
| Broadcast text | `POST /api/v1/direct_v2/threads/broadcast/text/` | `_uuid` when captured, `action=send_item`, `thread_ids`, non-empty `text`, `client_context`, matching `mutation_token`, stable `offline_threading_id` | returns `payload.item_id` |

SDK cursors are URL-safe base64 JSON envelopes with a version, surface, and
upstream cursor. Thread cursors also contain the thread ID. Unsupported versions,
malformed state, absent upstream cursors, and cross-thread cursors fail locally.
If a response says more history exists without a cursor, the SDK returns
`ErrUnexpectedResponse` rather than looping or replaying a page.

## Write behavior

`SendDirectText` validates the recipient and text before HTTP and places both
thread creation and text broadcast on the existing write pacer/cooldown budget.
The operation has a 30-second upper bound. Thread creation is attempted once.
Broadcast can use the normal bounded retry policy because every retry preserves
the same `client_context`, `mutation_token`, `offline_threading_id`, thread, and
text. A successful broadcast must contain captured `status=ok` and a usable
`payload.item_id`; an incomplete 2xx envelope is `ErrUnexpectedResponse`, not a
successful send. A failed send returns `DirectSendError` with the client context
and, once known, thread ID so an operator can reconcile an uncertain outcome.

External retries must not merely reuse `client_context`, because that would
repeat the unproven thread-creation mutation. When `DirectSendError.ThreadID` is
non-empty, pass `ThreadID`, `ClientContext`, and `RetryToken` in
`DirectTextRequest`; the SDK then skips creation and retries only the broadcast.
The retry token is authenticated with the current session and binds the
recipient ID, thread ID, exact text, and client context. Missing, altered, or
cross-recipient retry state is rejected before HTTP, so the explicit recipient
cannot merely label a send whose actual target is another thread. MCP exposes
the same three values as `thread_id`, `client_context`, and `retry_token`, and
mutation errors are not marked automatically retryable with unchanged input.

The MCP send tool requires `confirm_send=true`, is the only Direct tool tagged
`write`, and maps invalid input, expired authentication, security challenges,
rate limits/write soft-blocks, CSRF rejection, cancellation, and timeout into
structured errors. Read tools remain untagged.

## Live verification gates

Read verification is opt-in with `IG_DIRECT_LIVE_TEST=1` and selects the thread
ID only from the first page of the authenticated inbox before retrieving it.
Write verification additionally requires:

- `IG_DIRECT_WRITE_TEST=1`;
- identical non-empty `IG_DIRECT_APPROVED_RECIPIENT_ID` and
  `IG_DIRECT_CONFIRM_RECIPIENT_ID`;
- non-empty `IG_DIRECT_TEST_MESSAGE`;
- either a self target, or `IG_DIRECT_TARGET_KIND=burner` plus
  `IG_DIRECT_BURNER_TARGET_CONFIRM=I_CONFIRM_THIS_IS_A_BURNER`.

No live Direct test runs as part of ordinary `go test ./...`.
The commands are gates, not evidence of execution. A live run is accepted only
after its sanitized PASS lines are appended to
[`docs/direct-live-validation.md`](../direct-live-validation.md); credentials
and raw output remain local.

## Unsupported until separately captured

- attachments of every kind, including photos, videos, voice, links, shares,
  and disappearing visual media;
- reactions, likes, emoji acknowledgements, edits, and unsend;
- vanish mode and any ephemeral/disappearing-message state;
- group creation beyond the captured one-recipient thread resolution flow,
  participant changes, title/avatar changes, roles, and other administration.

Nested fields for these features may appear in private responses. Their presence
does not establish a safe request contract and the SDK intentionally leaves them
only in `Raw` model payloads.
