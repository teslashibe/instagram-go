package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureInventoryRequiresCompleteFlowsAndRedactsValues(t *testing.T) {
	t.Parallel()
	photo := writeHAR(t, []string{
		"/rupload_igphoto/secret-photo", "/api/v1/media/configure/", "/api/v1/media/response-secret/delete/",
	})
	reel := writeHAR(t, []string{
		"/rupload_igvideo/secret-video", "/rupload_igphoto/secret-video_0",
		"/api/v1/media/upload_finish/", "/api/v1/media/upload_status/",
		"/api/v1/media/configure_to_clips/", "/api/v1/media/response-secret/delete/",
	})
	story := writeHAR(t, []string{
		"/rupload_igvideo/secret-story", "/rupload_igphoto/secret-story_0",
		"/api/v1/media/upload_finish/", "/api/v1/media/upload_status/",
		"/api/v1/media/configure_to_story/", "/api/v1/media/response-secret/delete/",
	})
	report, err := captureInventory(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), map[string]string{
		"photo": photo, "reel": reel, "story": story,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Flows) != 3 {
		t.Fatalf("flows = %#v", report.Flows)
	}
	rendered := renderReport(report)
	for _, secret := range []string{"cookie-secret", "caption-secret", "response-secret"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("report leaked %q:\n%s", secret, rendered)
		}
	}
	for _, want := range []string{"/rupload_igphoto/<entity>", "/api/v1/media/<media_id>/delete/", "`cookie`", "`caption`", "`$.media.pk`"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("report lacks %q:\n%s", want, rendered)
		}
	}
}

func TestCaptureInventoryRejectsMissingStatusContract(t *testing.T) {
	t.Parallel()
	incomplete := writeHAR(t, []string{
		"/rupload_igvideo/v", "/rupload_igphoto/v_0", "/api/v1/media/upload_finish/",
		"/api/v1/media/configure_to_clips/", "/api/v1/media/response-secret/delete/",
	})
	_, err := inspectPublishingHAR("reel", incomplete)
	if err == nil || !strings.Contains(err.Error(), "upload_status") {
		t.Fatalf("error = %v", err)
	}
}

func TestCaptureInventoryRejectsDeletingDifferentMedia(t *testing.T) {
	t.Parallel()
	path := writeHAR(t, []string{
		"/rupload_igphoto/photo", "/api/v1/media/configure/", "/api/v1/media/unrelated-media/delete/",
	})
	_, err := inspectPublishingHAR("photo", path)
	if err == nil || !strings.Contains(err.Error(), "exact media ID") {
		t.Fatalf("error = %v", err)
	}
}

func TestWriteAtomicRefusesPublishingCaptureOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "capture.md")
	report := inventoryReport{CapturedAt: time.Now(), Flows: []flowCapture{{Kind: "photo"}}}
	if err := writeAtomic(path, report); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, report); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func writeHAR(t *testing.T, paths []string) string {
	t.Helper()
	entries := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, map[string]any{
			"request": map[string]any{
				"method": "POST",
				"url":    "https://i.instagram.test" + path,
				"headers": []map[string]string{
					{"name": "Cookie", "value": "cookie-secret"},
					{"name": "X-Instagram-Rupload-Params", "value": "upload-secret"},
				},
				"postData": map[string]any{
					"mimeType": "application/x-www-form-urlencoded",
					"text":     "caption=caption-secret&upload_id=upload-secret",
				},
			},
			"response": map[string]any{
				"status": 200,
				"content": map[string]any{
					"text": `{"status":"ok","media":{"pk":"response-secret"}}`,
				},
			},
		})
	}
	raw, err := json.Marshal(map[string]any{"log": map[string]any{"entries": entries}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "capture.har")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
