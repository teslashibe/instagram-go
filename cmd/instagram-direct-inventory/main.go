// Command instagram-direct-inventory validates and redacts a burner-account
// HAR containing the four Instagram Direct contracts used by the SDK.
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
		harPath            = flag.String("har", "", "source burner-account HAR (required; never copied)")
		output             = flag.String("output", "", "destination markdown file (required; must not exist)")
		viewerID           = flag.String("viewer-id", os.Getenv("IG_DS_USER_ID"), "authenticated burner user ID")
		approvedRecipient  = flag.String("approved-recipient-id", os.Getenv("IG_DIRECT_APPROVED_RECIPIENT_ID"), "approved burner/self recipient ID")
		confirmedRecipient = flag.String("confirm-recipient-id", os.Getenv("IG_DIRECT_CONFIRM_RECIPIENT_ID"), "repeat recipient ID to confirm captured writes")
		timeout            = flag.Duration("timeout", 30*time.Second, "maximum local inspection time")
	)
	flag.Parse()
	if *harPath == "" || *output == "" || *viewerID == "" {
		fatalf("-har, -output, and -viewer-id/IG_DS_USER_ID are required; nothing was written")
	}
	if *approvedRecipient == "" || *approvedRecipient != *confirmedRecipient {
		fatalf("approved and confirmed recipient IDs must be non-empty and identical; nothing was written")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := inspectDirectHAR(ctx, *harPath, *viewerID, *approvedRecipient, *confirmedRecipient, time.Now)
	if err != nil {
		fatalf("capture rejected: %v; nothing was written", err)
	}
	if err := writeDirectReport(*output, report); err != nil {
		fatalf("write report: %v", err)
	}
	fmt.Printf("PASS: captured and redacted %d Instagram Direct contracts -> %s\n", len(report.Surfaces), *output)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "instagram-direct-inventory: "+format+"\n", args...)
	os.Exit(1)
}
