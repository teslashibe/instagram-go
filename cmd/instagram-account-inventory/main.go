// Command instagram-account-inventory captures a secret-scrubbed, read-only
// contract for the authenticated account administration surface.
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
		output  = flag.String("output", "", "new markdown destination (required; existing files are never overwritten)")
		host    = flag.String("host", envOr("INSTAGRAM_API_HOST", "https://i.instagram.com"), "Instagram mobile API host")
		timeout = flag.Duration("timeout", 90*time.Second, "total capture timeout")
	)
	flag.Parse()
	if *output == "" {
		fatalf("output required; nothing was written")
	}
	cookies, err := loadCookieSet(os.Getenv)
	if err != nil {
		fatalf("authentication configuration: %v; nothing was written", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	p := probe{
		baseURL: *host, cookies: cookies, httpClient: newHTTPClient(*timeout), now: time.Now,
		userAgent: envOr("INSTAGRAM_USER_AGENT", defaultInventoryUserAgent),
		appID:     envOr("INSTAGRAM_APP_ID", defaultInventoryAppID),
	}
	report, err := p.capture(ctx)
	if err != nil {
		fatalf("capture failed closed: %v; nothing was written", err)
	}
	rendered := renderReport(report)
	if err := writeAtomic(*output, rendered); err != nil {
		fatalf("write inventory: %v", err)
	}
	fmt.Printf("PASS: captured %d safe account-administration read contracts for burner %s -> %s\n", len(report.Surfaces), redactID(report.AccountID), *output)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "instagram-account-inventory: "+format+"\n", args...)
	os.Exit(1)
}
