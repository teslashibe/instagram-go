# Instagram publishing inventory

This command converts three successful burner-account HARs into a redacted,
reviewable publishing contract. It never performs a publish itself.

Capture exactly one disposable photo, one disposable Reel, and one disposable
video Story in the current Instagram client. Delete each newly created item by
its exact ID before ending its capture. Do not reuse a personal account or
pre-existing media.

```bash
go run ./cmd/instagram-publish-inventory \
  -photo-har /secure/photo.har \
  -reel-har /secure/reel.har \
  -story-har /secure/story.har \
  -burner-ack DISPOSABLE_BURNER_CONTENT \
  -output docs/inventory/captures/YYYY-MM-DD-instagram-publishing.md
```

The command fails closed unless each flow uses `https://i.instagram.com`, the
captured methods, ordered stages, required header/form/query names, correlated
upload/client/media IDs, known processing states ending in ready, its per-kind
delete discriminator, and a post-delete exact-ID readback proving the media is
unavailable. It also validates and reports the embedded rupload field set,
mobile request-profile values, entity value shapes, finish/configure constants,
Reel flags, and Story configure values used by the SDK. Photo capture includes
its status request; Reel and video Story captures include both pending and ready
processing observations.

HARs, cookies, binary content, captions, and IDs must remain outside the
repository. Reports retain only safe constants and redacted shapes for dynamic
values. Human review and a secret scan are still required before committing the
report and enabling `PublishingCaptureVersion`.
