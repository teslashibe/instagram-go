package main

import (
	"fmt"
	"strings"
)

func renderReport(report inventoryReport) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Instagram account-administration read capture")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Captured at: `%s`  \n", report.CapturedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(&b, "Host: `%s`  \n", report.Host)
	fmt.Fprintf(&b, "Authenticated account: `%s` (redacted)  \n", redactID(report.AccountID))
	fmt.Fprintln(&b, "Result: **complete for the three allowlisted read projections**.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "> Generated from a read-only burner request. Values, raw bodies, cookies, CSRF, contact details, credentials, and security state are never retained.")
	fmt.Fprintln(&b)
	for _, surface := range report.Surfaces {
		fmt.Fprintf(&b, "## %s\n\n", surface.Name)
		fmt.Fprintf(&b, "- Request: `%s %s%s?edit=<redacted>`\n", surface.Method, report.Host, surface.Path)
		fmt.Fprintf(&b, "- Query parameter names: `%s`\n", strings.Join(surface.QueryNames, "`, `"))
		fmt.Fprintf(&b, "- Account ID path: `%s` (value matched `ds_user_id`)\n", surface.AccountIDPath)
		fmt.Fprintln(&b, "- Allowlisted response fields:")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```text")
		for _, field := range surface.ResponseFields {
			fmt.Fprintln(&b, field)
		}
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "## Mutation boundary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The capture command sends no writes. The SDK separately allowlists profile text fields, privacy, and professional category display settings. Password, username/email/phone, 2FA, deletion/deactivation, account conversion, ownership, and security changes are excluded.")
	return b.String()
}
