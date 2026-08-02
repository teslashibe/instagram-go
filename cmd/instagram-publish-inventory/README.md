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

The command fails closed if any flow omits upload, thumbnail where applicable,
media-processing/status, configure, or exact-media deletion. HARs, cookies,
binary content, captions, and IDs must remain outside the repository. Human
review and a secret scan are still required before committing the report.
