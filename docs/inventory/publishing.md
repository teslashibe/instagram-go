# Instagram publishing capture inventory

Status: **implementation and offline contract validation complete; a fresh live
burner capture must be generated and reviewed before claiming live verification**.

The SDK contract is versioned as `2026-08-01-v1` by
`instagram.PublishingCaptureVersion`. Instagram publishing is a private,
rotating protocol. Run
[`cmd/instagram-publish-inventory`](../../cmd/instagram-publish-inventory/)
against current disposable burner flows whenever an endpoint, header, form
field, response, or processing state changes. A generated report belongs under
`docs/inventory/captures/` only after human secret review.

No burner credentials, HARs, or disposable media were present in the issue #44
worktree. Consequently this repository does not mislabel an offline fixture as
live evidence. The live acceptance test remains disabled unless its explicit
burner acknowledgement, credentials, and disposable asset paths are supplied.

## Versioned transport contract

The current typed implementation and offline fixtures cover this ordered
contract:

| Flow | Ordered stages |
| --- | --- |
| Photo | `POST /rupload_igphoto/{entity}` → `POST /api/v1/media/configure/` |
| Reel | `POST /rupload_igvideo/{entity}` → thumbnail `POST /rupload_igphoto/{upload_id}_0` → `POST /api/v1/media/upload_finish/` → `GET /api/v1/media/upload_status/` → `POST /api/v1/media/configure_to_clips/` |
| Video Story | `POST /rupload_igvideo/{entity}` → thumbnail `POST /rupload_igphoto/{upload_id}_0` → `POST /api/v1/media/upload_finish/` → `GET /api/v1/media/upload_status/` → `POST /api/v1/media/configure_to_story/` |
| Photo Story | `POST /rupload_igphoto/{entity}` → `POST /api/v1/media/configure_to_story/` |
| Burner cleanup | `POST /api/v1/media/{exact_created_id}/delete/` |

Raw upload requests use the captured header-name contract:
`X-Entity-Type`, `X-Entity-Name`, `X-Entity-Length`, `Offset`,
`X-Instagram-Rupload-Params`, and `X_FB_PHOTO_WATERFALL_ID`. The inventory
retains names only; values, entity IDs, binary bodies, captions, cookies, CSRF
tokens, and response values are never written.

Configure requests correlate `upload_id` and deterministic `client_context`
with declared dimensions, duration for video, source type, and caption. Video
processing accepts only the inventoried pending states (`pending`,
`processing`, `transcoding`, `uploading`, `in_progress`) and ready states
(`ok`, `ready`, `finished`, `complete`, `completed`, `succeeded`, `uploaded`).
Unknown states fail closed. Terminal error states map to
`ErrProcessingFailed`; a bounded wait maps to `ErrProcessingTimeout`.

## Deterministic identifiers

`PublishPhoto`, `PublishReel`, and `PublishStory` require a caller
idempotency key. The SDK reads the bounded stream once, hashes the content and
metadata with the capture version and media kind, and deterministically derives
the numeric upload ID and UUID client context. Reusing the same key with
different content intentionally produces a different identifier. Upload bodies
are never automatically replayed; a failure after possible acceptance is
classified as `ErrPartialUpload`.

## Capture completeness and redaction

The inventory CLI requires three separate HARs:

- photo upload, photo configure, and exact-media delete;
- Reel video upload, thumbnail upload, upload-finish, status, Reel configure,
  and exact-media delete;
- video Story upload, thumbnail upload, upload-finish, status, Story configure,
  and exact-media delete.

Every retained response must be 2xx JSON with `status=ok`. The generated report
contains only method, redacted host/path, HTTP status, request header/field
names, and response field paths. It refuses partial flows and refuses to
overwrite an existing capture.

## Safety and live verification

`TestIntegration_PublishDisposableBurnerMedia` is one serial, opt-in test. It
requires `IG_PUBLISH_LIVE_TEST=1`,
`IG_PUBLISH_BURNER_ACK=DISPOSABLE_BURNER_CONTENT`, burner cookies, and five
disposable asset paths. Each returned media ID is registered immediately for
exact-ID cleanup. Cleanup never lists account media and never operates on an ID
not returned by that run.

## Unsupported features

The current capture contract is single-media only.

- Carousel publication is unsupported.
- Licensed music and music selection are unsupported; Reel audio is original
  audio embedded in the uploaded MP4 only.
- Stickers and interactive Story elements are unsupported.
- Structured mentions/tags are unsupported. Plain `@username` caption text is
  passed as text only and is not a captured mention contract.
- Locations are unsupported for publishing.
- Collaboration and paid-partnership invitations are unsupported.
- Scheduling and delayed publication are unsupported; calls publish
  immediately.

These features must remain absent from typed inputs and MCP schemas until each
has its own complete, redacted, burner-verified capture and cleanup record.
