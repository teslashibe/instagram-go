// Command instagram-search-inventory captures a secret-scrubbed inventory of
// Instagram's private keyword-search surfaces. It is a research tool, not a
// production SDK API.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	var (
		query   = flag.String("query", os.Getenv("INSTAGRAM_SEARCH_QUERY"), "keyword to search (or INSTAGRAM_SEARCH_QUERY)")
		output  = flag.String("output", "", "destination markdown file (required)")
		harPath = flag.String("har", "", "optional app/browser HAR containing search GraphQL calls")
		host    = flag.String("host", envOr("INSTAGRAM_API_HOST", "https://i.instagram.com"), "Instagram API host")
		timeout = flag.Duration("timeout", 2*time.Minute, "total probe timeout")
	)
	flag.Parse()

	if *query == "" {
		fatalf("query required: pass -query or set INSTAGRAM_SEARCH_QUERY")
	}
	if *output == "" {
		fatalf("output required: pass -output; nothing was written")
	}

	cookies, err := loadCookies(context.Background(), os.Getenv)
	if err != nil {
		fatalf("authentication configuration: %v; nothing was written", err)
	}

	httpClient, err := newHTTPClient(os.Getenv("INSTAGRAM_PROXY_URL"), *timeout)
	if err != nil {
		fatalf("proxy configuration: %v; nothing was written", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	probe := probe{
		baseURL:    *host,
		query:      *query,
		cookies:    cookies,
		httpClient: httpClient,
		userAgent:  envOr("INSTAGRAM_USER_AGENT", defaultMobileUserAgent),
		appID:      envOr("INSTAGRAM_APP_ID", defaultMobileAppID),
		now:        time.Now,
	}
	report, err := probe.capture(ctx, *harPath)
	if err != nil {
		fatalf("capture failed closed: %v; nothing was written", err)
	}
	if err := writeAtomic(*output, report); err != nil {
		fatalf("write inventory: %v", err)
	}

	fmt.Printf("PASS: captured %d REST and %d GraphQL search surfaces with %d media nodes -> %s\n",
		len(report.REST), len(report.GraphQL), report.MediaCount, *output)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "instagram-search-inventory: "+format+"\n", args...)
	os.Exit(1)
}
