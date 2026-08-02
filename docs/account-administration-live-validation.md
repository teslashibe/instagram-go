# Account-administration live validation

Run completed at: `2026-08-02T01:54:00Z`

Target: dedicated burner account (`account ID redacted`)

Result: **PASS for profile and privacy; professional display inapplicable**

This is the sanitized record of the opt-in live mutation smoke tests. The
burner account's values were held only in memory by the tests. No profile
value, account ID, cookie, CSRF token, credential, raw response, request
header, proxy value, or contact/security field is retained in this record.

The live-test guard variables were set in the invoking shell. Their values are
intentionally omitted; the tests fail closed unless live mode, the dedicated
burner ID, the exact restoration confirmation phrase, and all required cookie
variables are present and the authenticated account matches the burner ID.

## Profile fields — PASS

Command:

```bash
go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfileRestoresBurner$' .
```

Sanitized outcome:

```text
PASS: temporary burner profile mutation matched the requested after-state
PASS: burner profile mutation verified and original profile restored
PASS: final fresh read matched the original full name, biography, and external URL
```

The test snapshotted all three approved profile fields, registered bounded
cleanup before the first write, verified the temporary after-state, restored
the snapshot, and compared every field with a subsequent independent read.

## Privacy — PASS

Command:

```bash
go test -v -count=1 -run '^TestIntegration_AccountAdmin_PrivacyRestoresBurner$' .
```

Sanitized outcome:

```text
PASS: temporary burner privacy mutation matched the requested after-state
PASS: burner privacy mutation verified and original privacy restored
PASS: final fresh read matched the original burner privacy state
```

The test snapshotted the original public/private state, registered bounded
cleanup before the first write, verified the temporary inverse state, restored
the snapshot, and confirmed equality with a subsequent independent read.

## Professional category display — SKIP (inapplicable)

Command:

```bash
go test -v -count=1 -run '^TestIntegration_AccountAdmin_ProfessionalDisplayRestoresBurner$' .
```

Sanitized outcome:

```text
SKIP: configured burner is not a professional account
```

The live `GetProfessionalAccountState` preflight reported that this burner was
not already professional. Category-display settings therefore do not apply,
and the test skipped before registering or sending a professional mutation.
The test deliberately does not convert account type.

## Restoration and redaction review

The profile and privacy commands completed only after their explicit restore
writes and final fresh-read equality assertions passed. Their registered
cleanup handlers repeated the restoration checks at test teardown. The
professional-display command made no write because its applicability preflight
failed.

Before commit, this document was checked for session cookies, CSRF and
authorization material, passwords, proxy configuration, raw account values,
usernames, account IDs, and contact/security data; none are present.
