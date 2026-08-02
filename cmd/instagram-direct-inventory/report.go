package main

import (
	"fmt"
	"strings"
)

func renderDirectReport(report directReport) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Instagram Direct private-API capture")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Captured at: `%s`  \n", report.CapturedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintln(&b, "Result: **complete for inbox pagination, thread retrieval, thread creation, and text broadcast**.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "> Generated from an explicitly approved burner/self capture. Cookie, Authorization, CSRF, recipient, viewer, thread, item, cursor, client-context, message-text, and raw response values are not retained.")
	fmt.Fprintln(&b)
	for _, surface := range report.Surfaces {
		fmt.Fprintf(&b, "## %s\n\n", surface.Name)
		fmt.Fprintf(&b, "- Request: `%s %s%s`\n", surface.Method, surface.Host, surface.Path)
		if surface.Continuation {
			fmt.Fprintln(&b, "- Continuation request: captured with `cursor` equal to the preceding response's `oldest_cursor`")
		}
		fmt.Fprintf(&b, "- Request fields: %s\n", markdownList(surface.RequestFields))
		fmt.Fprintf(&b, "- Header names: %s\n", markdownList(surface.HeaderNames))
		fmt.Fprintf(&b, "- Pagination fields: %s\n", markdownList(surface.PaginationFields))
		fmt.Fprintln(&b, "- Response field paths:")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```text")
		for _, path := range surface.ResponseFields {
			fmt.Fprintln(&b, path)
		}
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b)
	}
	return b.String()
}

func markdownList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "`" + strings.ReplaceAll(value, "`", "'") + "`"
	}
	return strings.Join(quoted, ", ")
}
