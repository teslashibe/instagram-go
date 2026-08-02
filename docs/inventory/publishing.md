# Instagram publishing capture inventory

Status: **publishing disabled; a fresh live burner capture must be generated,
reviewed, committed, and compiled into the SDK before any mutation can run**.

`instagram.PublishingCaptureVersion` is intentionally empty. Every SDK
publishing/cleanup method returns `ErrPublishingCaptureRequired` before
consuming media or making an HTTP request, and MCP returns
`publishing_capture_required`. Instagram publishing is a private, rotating
protocol, so offline fixtures cannot unlock a production mutation path.

No burner credentials, HARs, or disposable media were present in the issue #44
worktree. Enabling publishing requires one code review that:

1. Generates a date-stamped report with
   [`cmd/instagram-publish-inventory`](../../cmd/instagram-publish-inventory/).
2. Human-reviews the report and source HARs for completeness and secrets.
3. Commits the redacted report under `docs/inventory/captures/`.
4. Aligns the implementation and fixtures to exactly that evidence.
5. Sets `PublishingCaptureVersion` to the reviewed capture version.
6. Runs the burner-only live publish, verification, deletion, and readback test.

## Required ordered evidence

The capture validator requires these ordered flows:

| Flow | Ordered stages |
| --- | --- |
| Photo | `POST /rupload_igphoto/{entity}` → `GET /api/v1/media/upload_status/` → `POST /api/v1/media/configure/` |
| Reel | `POST /rupload_igvideo/{entity}` → thumbnail `POST /rupload_igphoto/{upload_id}_0` → `POST /api/v1/media/upload_finish/` → one or more `GET /api/v1/media/upload_status/` → `POST /api/v1/media/configure_to_clips/` |
| Video Story | `POST /rupload_igvideo/{entity}` → thumbnail `POST /rupload_igphoto/{upload_id}_0` → `POST /api/v1/media/upload_finish/` → one or more `GET /api/v1/media/upload_status/` → `POST /api/v1/media/configure_to_story/` |
| Burner cleanup | per-kind `POST /api/v1/media/{exact_created_id}/delete/` → exact-ID `GET /api/v1/media/{id}/info/` proving unavailable |

The capture tool validates:

- HTTPS requests to `i.instagram.com` with the stage-specific method;
- ordered, non-duplicated mutation stages (status polling may repeat);
- raw upload header names: `X-Entity-Type`, `X-Entity-Name`,
  `X-Entity-Length`, `Offset`, `X-Instagram-Rupload-Params`, and
  `X_FB_PHOTO_WATERFALL_ID`;
- required form/query field names for finish, status, configure, and delete;
- the same upload ID across primary upload, thumbnail, finish, status, and
  configure;
- the same client context across the upload waterfall and configure;
- known processing-state values, a terminal ready state, and at least one
  pending/processing observation for video;
- the exact configured media ID across delete and post-delete readback;
- the exact non-empty per-flow delete `media_type` values, retained as safe enum
  evidence so the implementation can be aligned without assuming one value;
- successful JSON envelopes, except the expected missing-media readback.

Generic `status=ok` responses, incorrect methods/hosts, unknown states,
uncorrelated IDs, incorrect delete types, and unconfirmed cleanup all fail
closed. Generated reports contain only methods, redacted paths, status codes,
request header/form/query names, response field paths, processing-state enum
values, and delete-type enum values. They never retain credential values,
binary bodies, captions, upload IDs, client IDs, or media IDs.

## Typed draft and deterministic identifiers

The fail-closed draft defines `UploadSource`, `PublishPhotoInput`,
`PublishReelInput`, and video-only `PublishStoryInput` around an `io.Reader`,
filename, MIME type, exact byte length, dimensions, duration, caption, and
caller idempotency key. It derives a deterministic numeric upload ID and UUID
client context from the bounded content hash and metadata.

Upload bodies are not automatically replayed. A failure after possible
acceptance is classified as `ErrPartialUpload`. Challenge, feedback, processing
failure, processing timeout, and upload-size failures have separate sentinels.
All network stages use the write pacer/cooldown. Publishing request contexts,
not the client's 30-second default HTTP timeout, control the configured upload
and processing deadlines.

## Safety and live verification

`TestIntegration_PublishDisposableBurnerMedia` is one serial, opt-in test and
skips while `PublishingCaptureVersion` is empty. It requires
`IG_PUBLISH_LIVE_TEST=1`,
`IG_PUBLISH_BURNER_ACK=DISPOSABLE_BURNER_CONTENT`, burner cookies, and five
disposable asset paths.

Each returned media ID and `PublishedMediaKind` is registered immediately for
cleanup. Cleanup sends the captured per-kind delete discriminator, then reads
that exact ID until Instagram confirms it is unavailable. It never lists
account media and never operates on an ID not returned by that run.

## Unsupported features

The draft surface is single-media only.

- Photo Story publication is unsupported; only a video Story draft exists.
- Carousel publication is unsupported.
- Licensed music and music selection are unsupported; Reel audio is original
  audio embedded in the uploaded MP4 only.
- Stickers and interactive Story elements are unsupported.
- Structured mentions/tags are unsupported. Plain `@username` caption text is
  text only and is not a captured mention contract.
- Locations are unsupported for publishing.
- Collaboration and paid-partnership invitations are unsupported.
- Scheduling and delayed publication are unsupported.

These features must remain absent from typed inputs and MCP schemas until each
has its own complete, redacted, burner-verified capture and cleanup record.
