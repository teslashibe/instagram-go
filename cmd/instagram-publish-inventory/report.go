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
	fmt.Fprintln(&b, "> Generated from disposable burner content. Credentials, binary bodies, captions, and dynamic IDs/metadata are redacted. Safe protocol constants and value shapes are retained for implementation review.")
	fmt.Fprintln(&b)
	for _, flow := range report.Flows {
		fmt.Fprintf(&b, "## %s\n\n", strings.ToUpper(flow.Kind[:1])+flow.Kind[1:])
		for _, surface := range flow.Surfaces {
			fmt.Fprintf(&b, "### %s\n\n", surface.Stage)
			fmt.Fprintf(&b, "- Request: `%s %s%s`\n", surface.Method, surface.Host, surface.Path)
			fmt.Fprintf(&b, "- HTTP status: `%d`\n", surface.StatusCode)
			fmt.Fprintf(&b, "- Request header names: %s\n", markdownList(surface.RequestHeaders))
			fmt.Fprintf(&b, "- Request field names: %s\n", markdownList(surface.RequestFields))
			fmt.Fprintf(&b, "- Request query names: %s\n", markdownList(surface.RequestQueryFields))
			fmt.Fprintf(&b, "- Embedded rupload parameter names: %s\n", markdownList(surface.RuploadParamFields))
			fmt.Fprintf(&b, "- Captured protocol values/shapes: %s\n", markdownList(surface.ProtocolValues))
			fmt.Fprintf(&b, "- Response field paths: %s\n\n", markdownList(surface.ResponseFields))
		}
		fmt.Fprintf(&b, "- Captured processing states: %s\n", markdownList(flow.ProcessingStates))
		fmt.Fprintf(&b, "- Captured exact-delete `media_type`: `%s`\n\n", flow.DeleteMediaType)
	}
	fmt.Fprintln(&b, "## Safety boundary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- Each HAR must include successful deletion of the exact media created by that same burner flow.")
	fmt.Fprintln(&b, "- A post-delete readback of that exact ID must prove it is unavailable.")
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
