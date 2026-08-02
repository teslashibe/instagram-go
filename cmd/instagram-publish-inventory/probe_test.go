package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureInventoryRequiresStrictCompleteFlowsAndRedactsValues(t *testing.T) {
	t.Parallel()
	report, err := captureInventory(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), map[string]string{
		"photo": writeHAR(t, completeFlowEntries("photo")),
		"reel":  writeHAR(t, completeFlowEntries("reel")),
		"story": writeHAR(t, completeFlowEntries("story")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Flows) != 3 {
		t.Fatalf("flows = %#v", report.Flows)
	}
	rendered := renderReport(report)
	for _, secret := range []string{
		"cookie-secret", "caption-secret", "123456789", "client-secret", "999_42",
	} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("report leaked %q:\n%s", secret, rendered)
		}
	}
	for _, want := range []string{
		"/rupload_igphoto/<entity>",
		"/api/v1/media/<media_id>/delete/",
		"/api/v1/media/<media_id>/info/",
		"`cookie`",
		"`caption`",
		"`$.media.pk`",
		"`processing`, `ready`",
		"`VIDEO`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("report lacks %q:\n%s", want, rendered)
		}
	}
}

func TestPublishingInventoryRejectsIncompleteOrUncorrelatedContracts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
		want   string
	}{
		{
			name: "missing status",
			mutate: func(entries []map[string]any) []map[string]any {
				return append(entries[:3], entries[5:]...)
			},
			want: "upload_status",
		},
		{
			name: "wrong order",
			mutate: func(entries []map[string]any) []map[string]any {
				entries[2], entries[3] = entries[3], entries[2]
				return entries
			},
			want: "out-of-order",
		},
		{
			name: "wrong method",
			mutate: func(entries []map[string]any) []map[string]any {
				request(entries[0])["method"] = "GET"
				return entries
			},
			want: "want POST",
		},
		{
			name: "wrong host",
			mutate: func(entries []map[string]any) []map[string]any {
				request(entries[0])["url"] = "https://www.instagram.com/rupload_igvideo/123456789"
				return entries
			},
			want: "unexpected host",
		},
		{
			name: "generic status response",
			mutate: func(entries []map[string]any) []map[string]any {
				responseContent(entries[3])["text"] = `{"status":"ok"}`
				return entries
			},
			want: "processing state",
		},
		{
			name: "configure upload mismatch",
			mutate: func(entries []map[string]any) []map[string]any {
				request(entries[5])["postData"] = formPostData("upload_id=other&client_context=client-secret&source_type=4&upload_media_width=1080&upload_media_height=1920&caption=caption-secret")
				return entries
			},
			want: "configure upload_id",
		},
		{
			name: "delete target mismatch",
			mutate: func(entries []map[string]any) []map[string]any {
				request(entries[6])["url"] = "https://i.instagram.com/api/v1/media/unrelated/delete/"
				return entries
			},
			want: "do not match",
		},
		{
			name: "delete type missing",
			mutate: func(entries []map[string]any) []map[string]any {
				request(entries[6])["postData"] = formPostData("")
				return entries
			},
			want: "media_type",
		},
		{
			name: "cleanup not confirmed",
			mutate: func(entries []map[string]any) []map[string]any {
				response(entries[7])["status"] = 200
				responseContent(entries[7])["text"] = `{"status":"ok","items":[{"pk":"999_42"}]}`
				return entries
			},
			want: "did not confirm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := tt.mutate(completeFlowEntries("reel"))
			_, err := inspectPublishingHAR("reel", writeHAR(t, entries))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPublishingInventoryRequiresPhotoStatusContract(t *testing.T) {
	t.Parallel()
	entries := completeFlowEntries("photo")
	entries = append(entries[:1], entries[2:]...)
	_, err := inspectPublishingHAR("photo", writeHAR(t, entries))
	if err == nil || !strings.Contains(err.Error(), "upload_status") {
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

func completeFlowEntries(kind string) []map[string]any {
	const (
		uploadID = "123456789"
		clientID = "client-secret"
		mediaID  = "999_42"
	)
	rawHeaders := []map[string]string{
		{"name": "Cookie", "value": "cookie-secret"},
		{"name": "Offset", "value": "0"},
		{"name": "X-Entity-Length", "value": "5"},
		{"name": "X-Entity-Name", "value": uploadID},
		{"name": "X-Entity-Type", "value": "video/mp4"},
		{"name": "X-Instagram-Rupload-Params", "value": `{"upload_id":"` + uploadID + `"}`},
		{"name": "X_FB_PHOTO_WATERFALL_ID", "value": clientID},
	}
	photoHeaders := cloneHeaders(rawHeaders)
	for _, header := range photoHeaders {
		if header["name"] == "X-Entity-Type" {
			header["value"] = "image/jpeg"
		}
	}
	uploadResponse := `{"status":"ok","upload_id":"` + uploadID + `"}`
	statusReady := entry("GET", "/api/v1/media/upload_status/?upload_id="+uploadID, nil, nil,
		200, `{"status":"ok","processing_info":{"state":"ready"}}`)
	deleteType := map[string]string{"photo": "PHOTO", "reel": "VIDEO", "story": "STORY"}[kind]
	deleteEntry := entry("POST", "/api/v1/media/"+mediaID+"/delete/", nil,
		formPostData("media_type="+deleteType), 200, `{"status":"ok"}`)
	confirmEntry := entry("GET", "/api/v1/media/"+mediaID+"/info/", nil, nil,
		404, `{"status":"fail","message":"media_not_found"}`)

	if kind == "photo" {
		return []map[string]any{
			entry("POST", "/rupload_igphoto/"+uploadID, photoHeaders, nil, 200, uploadResponse),
			statusReady,
			entry("POST", "/api/v1/media/configure/", nil,
				formPostData("upload_id="+uploadID+"&client_context="+clientID+"&source_type=4&upload_media_width=1080&upload_media_height=1350&caption=caption-secret"),
				200, `{"status":"ok","media":{"pk":"`+mediaID+`"}}`),
			deleteEntry,
			confirmEntry,
		}
	}

	configurePath := "/api/v1/media/configure_to_clips/"
	configureFields := "upload_id=" + uploadID + "&client_context=" + clientID +
		"&source_type=4&upload_media_width=1080&upload_media_height=1920&caption=caption-secret"
	if kind == "story" {
		configurePath = "/api/v1/media/configure_to_story/"
		configureFields += "&configure_mode=1&story_media_creation_date=1"
	}
	thumbHeaders := cloneHeaders(photoHeaders)
	for _, header := range thumbHeaders {
		if header["name"] == "X-Entity-Name" {
			header["value"] = uploadID + "_0"
		}
	}
	return []map[string]any{
		entry("POST", "/rupload_igvideo/"+uploadID, rawHeaders, nil, 200, uploadResponse),
		entry("POST", "/rupload_igphoto/"+uploadID+"_0", thumbHeaders, nil, 200, uploadResponse),
		entry("POST", "/api/v1/media/upload_finish/", nil,
			formPostData("upload_id="+uploadID+"&source_type=4&video=1&media_type="+kind),
			200, `{"status":"ok"}`),
		entry("GET", "/api/v1/media/upload_status/?upload_id="+uploadID, nil, nil,
			200, `{"status":"ok","processing_info":{"state":"processing"}}`),
		statusReady,
		entry("POST", configurePath, nil, formPostData(configureFields),
			200, `{"status":"ok","media":{"pk":"`+mediaID+`"}}`),
		deleteEntry,
		confirmEntry,
	}
}

func cloneHeaders(headers []map[string]string) []map[string]string {
	out := make([]map[string]string, len(headers))
	for index, header := range headers {
		out[index] = map[string]string{"name": header["name"], "value": header["value"]}
	}
	return out
}

func entry(method, path string, headers []map[string]string, postData map[string]any, status int, body string) map[string]any {
	if headers == nil {
		headers = []map[string]string{{"name": "Cookie", "value": "cookie-secret"}}
	}
	if postData == nil {
		postData = map[string]any{}
	}
	return map[string]any{
		"request": map[string]any{
			"method": method, "url": "https://i.instagram.com" + path,
			"headers": headers, "postData": postData,
		},
		"response": map[string]any{
			"status":  status,
			"content": map[string]any{"text": body},
		},
	}
}

func formPostData(text string) map[string]any {
	return map[string]any{
		"mimeType": "application/x-www-form-urlencoded",
		"text":     text,
	}
}

func request(entry map[string]any) map[string]any {
	return entry["request"].(map[string]any)
}

func response(entry map[string]any) map[string]any {
	return entry["response"].(map[string]any)
}

func responseContent(entry map[string]any) map[string]any {
	return response(entry)["content"].(map[string]any)
}

func writeHAR(t *testing.T, entries []map[string]any) string {
	t.Helper()
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
