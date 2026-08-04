// Command instagram-publish-inventory validates and redacts burner-only HAR
// captures for Instagram photo, Reel, and video Story publishing.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	var (
		photoHAR = flag.String("photo-har", "", "burner photo publish-and-delete HAR (required)")
		reelHAR  = flag.String("reel-har", "", "burner Reel publish-and-delete HAR (required)")
		storyHAR = flag.String("story-har", "", "burner video Story publish-and-delete HAR (required)")
		output   = flag.String("output", "", "new date-stamped markdown destination (required)")
		ack      = flag.String("burner-ack", "", `must equal "DISPOSABLE_BURNER_CONTENT"`)
	)
	flag.Parse()

	if *ack != "DISPOSABLE_BURNER_CONTENT" {
		fatalf("explicit -burner-ack DISPOSABLE_BURNER_CONTENT is required; nothing was written")
	}
	if *photoHAR == "" || *reelHAR == "" || *storyHAR == "" || *output == "" {
		fatalf("-photo-har, -reel-har, -story-har, and -output are required; nothing was written")
	}
	report, err := captureInventory(time.Now().UTC(), map[string]string{
		"photo": *photoHAR, "reel": *reelHAR, "story": *storyHAR,
	})
	if err != nil {
		fatalf("capture failed closed: %v; nothing was written", err)
	}
	if err := writeAtomic(*output, report); err != nil {
		fatalf("write inventory: %v", err)
	}
	fmt.Printf("PASS: redacted %d complete burner publishing flows -> %s\n", len(report.Flows), *output)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "instagram-publish-inventory: "+format+"\n", args...)
	os.Exit(1)
}
