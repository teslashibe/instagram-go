# Instagram Direct burner verification

The Direct acceptance path is intentionally separate from ordinary integration
tests. The redacted contract record at
[`docs/inventory/captures/2026-08-01-direct.md`](inventory/captures/2026-08-01-direct.md)
was accepted only after the capture verifier established that the retrieved
thread came from the authenticated inbox and both operator recipient approvals
matched the sole captured write target.

No account, recipient, thread, item, cursor, text, client-context, cookie, CSRF,
authorization, or device value is retained in this record.

Verification status: **contract capture verified; SDK live read/send PASS not
yet recorded**. The commands below are a fail-closed procedure, not evidence
that they ran. This record must not claim the live acceptance criterion until an
operator runs both commands with local burner credentials and appends only
these sanitized outcomes:

```text
PASS: read <count> items from one thread selected only from the authenticated inbox
PASS: sent one confirmed text item to the explicitly approved burner/self target
```

To repeat the read verification:

```bash
IG_DIRECT_LIVE_TEST=1 \
go test -v -count=1 -run '^TestIntegration_DirectReadSelfOwnedThread$' .
```

To send exactly one approved plain-text message, set the matching IDs and the
message locally. A non-self target must also pass the explicit burner assertion:

```bash
IG_DIRECT_WRITE_TEST=1 \
IG_DIRECT_APPROVED_RECIPIENT_ID='<approved-id>' \
IG_DIRECT_CONFIRM_RECIPIENT_ID='<same-id>' \
IG_DIRECT_TEST_MESSAGE='<approved-text>' \
IG_DIRECT_TARGET_KIND='burner' \
IG_DIRECT_BURNER_TARGET_CONFIRM='I_CONFIRM_THIS_IS_A_BURNER' \
go test -v -count=1 -run '^TestIntegration_DirectSendApprovedBurnerOrSelf$' .
```

The read test never accepts an arbitrary thread ID; it selects from the current
authenticated inbox in-process. The write test fails before mutation unless all
approval gates are present. Self-target sends do not need the burner assertion,
but still require matching recipient IDs, the write opt-in, and non-empty text.
