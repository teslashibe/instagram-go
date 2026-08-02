# Instagram publishing live capture required

This is a status marker, **not live protocol evidence**.

Issue #44 added a fail-closed capture command, typed SDK/MCP implementation,
offline response-shape fixtures, and an opt-in burner publish-and-cleanup test.
No burner credentials, disposable assets, or source HARs were available in the
implementation worktree, so no fabricated date-stamped live capture is
committed.

Generate the actual dated artifact with:

```bash
go run ./cmd/instagram-publish-inventory \
  -photo-har /secure/photo.har \
  -reel-har /secure/reel.har \
  -story-har /secure/story.har \
  -burner-ack DISPOSABLE_BURNER_CONTENT \
  -output docs/inventory/captures/YYYY-MM-DD-instagram-publishing.md
```

Review the generated file for secrets before replacing this status marker.
