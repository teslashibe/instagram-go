package main

import (
	"fmt"
	"strings"
)

func renderReport(report inventoryReport) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Instagram publishing protocol capture")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Captured at: `%s`  \n", report.CapturedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintln(&b, "Result: **complete burner photo, Reel, and video Story publish-and-delete flows**.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "> Generated from disposable burner content. Credential values, request values, binary bodies, captions, IDs, entity names, and response values are omitted.")
	fmt.Fprintln(&b)
	for _, flow := range report.Flows {
		fmt.Fprintf(&b, "## %s\n\n", strings.ToUpper(flow.Kind[:1])+flow.Kind[1:])
		for _, surface := range flow.Surfaces {
			fmt.Fprintf(&b, "### %s\n\n", surface.Stage)
			fmt.Fprintf(&b, "- Request: `%s %s%s`\n", surface.Method, surface.Host, surface.Path)
			fmt.Fprintf(&b, "- HTTP status: `%d`\n", surface.StatusCode)
			fmt.Fprintf(&b, "- Request header names: %s\n", markdownList(surface.RequestHeaders))
			fmt.Fprintf(&b, "- Request field names: %s\n", markdownList(surface.RequestFields))
			fmt.Fprintf(&b, "- Response field paths: %s\n\n", markdownList(surface.ResponseFields))
		}
	}
	fmt.Fprintln(&b, "## Safety boundary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- Each HAR must include successful deletion of the exact media created by that same burner flow.")
	fmt.Fprintln(&b, "- The inventory command never enumerates or deletes account media.")
	fmt.Fprintln(&b, "- Re-capture before changing endpoint, header, configure, processing, or status contracts.")
	return b.String()
}

func markdownList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = "`" + strings.ReplaceAll(item, "`", "'") + "`"
	}
	return strings.Join(quoted, ", ")
}
