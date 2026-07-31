package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func renderReport(r report) (string, error) {
	var b strings.Builder
	fmt.Fprintln(&b, "# Instagram private search inventory")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Captured at: `%s`  \n", r.CapturedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(&b, "REST host: `%s`  \n", r.Host)
	fmt.Fprintf(&b, "Keyword: `%s`  \n", markdownCode(r.Query))
	fmt.Fprintf(&b, "Result: **complete for the four scripted mobile REST tabs**; %d media/post nodes observed.\n", r.MediaCount)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "> This file is generated from live calls. Cookie, Authorization, CSRF, and raw request-header values are never retained. Cursor values and CDN URL signatures are omitted. IDs and shortcodes in the sample are public media identifiers.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Captured surfaces")
	fmt.Fprintln(&b)
	for _, s := range r.REST {
		if err := renderSurface(&b, s); err != nil {
			return "", err
		}
	}

	fmt.Fprintln(&b, "## GraphQL / web capture")
	fmt.Fprintln(&b)
	if len(r.GraphQL) == 0 {
		fmt.Fprintln(&b, "No search GraphQL call was supplied in a HAR for this run. The scripted mobile calls use `i.instagram.com/api/v1/fbsearch/*`; they cannot discover rotating web `doc_id` values. Re-run with `-har <search-session.har>` after exercising Search in the app/web client. This absence is explicit and is not a claim that GraphQL was inventoried.")
		fmt.Fprintln(&b)
	} else {
		fmt.Fprintln(&b, "The following `doc_id` values are observations, not stable API constants. Re-capture before implementation if the capture date is stale.")
		fmt.Fprintln(&b)
		for _, s := range r.GraphQL {
			if err := renderSurface(&b, s); err != nil {
				return "", err
			}
		}
	}

	fmt.Fprintln(&b, "## Host and compatibility notes")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- Mobile Search tabs are served by `i.instagram.com/api/v1/fbsearch/*` (or the explicitly selected `-host`) with a mobile app ID/user-agent and an authenticated burner session.")
	fmt.Fprintln(&b, "- Existing SDK entity typeahead remains `www.instagram.com/api/v1/web/search/topsearch/`; it returns users, hashtags, and places and is not evidence of keyword-to-media search.")
	fmt.Fprintln(&b, "- Web search GraphQL is normally `www.instagram.com/graphql/query` or `/api/graphql`; its friendly names and `doc_id` values rotate independently of the mobile REST paths.")
	fmt.Fprintln(&b, "- `Search` and `SearchUsers` are unchanged. This inventory is research input for a later typed `SearchPosts` implementation.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## `Post` model candidate mapping")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Captured media field | `Post` candidate |")
	fmt.Fprintln(&b, "| --- | --- |")
	for _, row := range [][2]string{
		{"`pk` / `pk_id`", "`Post.PK`"}, {"`id`", "`Post.ID`"}, {"`code`", "`Post.Code`"},
		{"`media_type`", "`Post.MediaType`"}, {"`product_type`", "`Post.ProductType`"}, {"`taken_at`", "`Post.TakenAt`"},
		{"`caption.text`", "`Post.Caption`"}, {"`user`", "`Post.Owner`"}, {"`like_count`", "`Post.LikeCount`"},
		{"`comment_count`", "`Post.CommentCount`"}, {"`view_count`", "`Post.ViewCount`"}, {"`play_count`", "`Post.PlayCount`"},
		{"`image_versions2.candidates`", "`Post.ImageVersions`"}, {"`video_versions`", "`Post.VideoVersions`"},
		{"`carousel_media`", "`Post.CarouselMedia`"}, {"`clips_metadata`", "`Post.ClipsMetadata`"},
	} {
		fmt.Fprintf(&b, "| %s | %s |\n", row[0], row[1])
	}
	return b.String(), nil
}

func renderSurface(b *strings.Builder, s surface) error {
	fmt.Fprintf(b, "### %s\n\n", s.Name)
	fmt.Fprintf(b, "- Request: `%s %s%s`\n", s.Method, s.Host, s.Path)
	if s.FriendlyName != "" {
		fmt.Fprintf(b, "- Friendly name: `%s`\n", markdownCode(s.FriendlyName))
	}
	if s.DocID != "" {
		fmt.Fprintf(b, "- Captured `doc_id`: `%s`\n", markdownCode(s.DocID))
	}
	fmt.Fprintf(b, "- Live status: `%d`\n", s.StatusCode)
	fmt.Fprintf(b, "- Required/captured params: %s\n", codeList(s.RequiredParams))
	if len(s.PaginationFields) == 0 {
		fmt.Fprintln(b, "- Pagination fields observed: none in this response")
	} else {
		fmt.Fprintf(b, "- Pagination fields observed: %s\n", codeList(s.PaginationFields))
	}
	if len(s.MediaPaths) == 0 {
		fmt.Fprintln(b, "- Media node paths: none (expected for entity-only Accounts/typeahead responses)")
	} else {
		fmt.Fprintf(b, "- Media node paths: %s\n", codeList(s.MediaPaths))
	}
	fmt.Fprintln(b, "- Response field paths:")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```text")
	for _, path := range s.ResponseFields {
		fmt.Fprintln(b, path)
	}
	fmt.Fprintln(b, "```")
	if len(s.SampleMedia) > 0 {
		sample := scrubJSON(s.SampleMedia)
		raw, err := json.MarshalIndent(sample, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(b)
		fmt.Fprintln(b, "Scrubbed media sample:")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "```json")
		fmt.Fprintln(b, string(raw))
		fmt.Fprintln(b, "```")
	}
	fmt.Fprintln(b)
	return nil
}

func scrubJSON(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range node {
			lower := strings.ToLower(key)
			if sensitiveKey(lower) {
				out[key] = "<redacted>"
				continue
			}
			out[key] = scrubJSON(child)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = scrubJSON(child)
		}
		return out
	case string:
		if u := stripURLQuery(node); u != node {
			return u
		}
		return node
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	for _, needle := range []string{"session", "password", "authorization", "cookie", "csrf", "access_token", "secret"} {
		if strings.Contains(key, needle) {
			return true
		}
	}
	return false
}

func stripURLQuery(value string) string {
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return value
	}
	if idx := strings.IndexAny(value, "?#"); idx >= 0 {
		return value[:idx] + "?<redacted>"
	}
	return value
}

func codeList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	copyItems := append([]string(nil), items...)
	sort.Strings(copyItems)
	for i := range copyItems {
		copyItems[i] = "`" + markdownCode(copyItems[i]) + "`"
	}
	return strings.Join(copyItems, ", ")
}

func markdownCode(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}
