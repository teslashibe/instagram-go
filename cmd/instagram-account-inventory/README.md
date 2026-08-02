# Instagram account-administration inventory

This read-only command captures the private contract used by the safe account
administration API. It makes one authenticated `current_user?edit=true` request
and emits three allowlisted projections: current account, reversible settings,
and professional-account state.

Use only a burner account. Keep cookies outside the repository:

```bash
INSTAGRAM_COOKIES_FILE=/secure/burner-cookies.json \
go run ./cmd/instagram-account-inventory \
  -output docs/inventory/captures/YYYY-MM-DD-account-administration.md
```

The command fails before writing if authentication is rejected, Instagram asks
for a challenge, the response account ID differs from `ds_user_id`, or any
required allowlisted field is missing. It refuses to overwrite an existing
capture. Reports contain paths and parameter names only—not raw values, bodies,
cookies, CSRF tokens, contact details, or security state.

The command never sends a mutation. Before committing a generated report,
perform a human secret review. Password, email, phone, 2FA,
deletion/deactivation, account conversion, ownership, and security operations
remain outside the approved contract.
