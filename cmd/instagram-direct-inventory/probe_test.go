package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testViewerID    = "100000000000000000"
	testRecipientID = "100000000000000001"
	testThreadID    = "340282366841710300949128199900000000001"
	testItemID      = "340282366841710300949128199900000000002"
)

func completeDirectHAR(t *testing.T, threadID, recipientID string) string {
	t.Helper()
	entry := func(method, rawURL, requestText, responseText string) map[string]any {
		return map[string]any{
			"request": map[string]any{
				"method": method, "url": rawURL,
				"headers": []any{
					map[string]any{"name": "Cookie", "value": "sessionid=never-retain-session"},
					map[string]any{"name": "X-CSRFToken", "value": "never-retain-csrf"},
					map[string]any{"name": "Authorization", "value": "never-retain-auth"},
				},
				"postData": map[string]any{"mimeType": "application/x-www-form-urlencoded", "text": requestText},
			},
			"response": map[string]any{"status": 200, "content": map[string]any{"text": responseText}},
		}
	}
	inbox := `{"inbox":{"threads":[{"thread_id":"` + threadID + `","users":[{"pk":"` + recipientID + `","username":"private-user"}],"items":[{"item_id":"private-item","item_type":"text","text":"private inbox text"}]}],"oldest_cursor":"private-inbox-cursor","has_older":true},"last_seen_at":{"` + recipientID + `":"private-timestamp"},"status":"ok"}`
	thread := `{"thread":{"thread_id":"` + threadID + `","items":[{"item_id":"` + testItemID + `","user_id":"` + recipientID + `","item_type":"text","text":"private thread text"}],"oldest_cursor":"private-thread-cursor","has_older":true},"status":"ok"}`
	created := `{"thread":{"thread_id":"` + threadID + `","users":[{"pk":"` + recipientID + `"}]},"status":"ok"}`
	broadcast := `{"payload":{"item_id":"` + testItemID + `","client_context":"private-context"},"status":"ok"}`
	createForm := "recipient_users=" + urlEscape(`["`+recipientID+`"]`) + "&_uuid=private-device"
	broadcastForm := "action=send_item&client_context=private-context&mutation_token=private-context&offline_threading_id=123&text=" + urlEscape("private outbound text") + "&thread_ids=" + urlEscape(`["`+threadID+`"]`)
	doc := map[string]any{"log": map[string]any{"entries": []any{
		entry("GET", "https://i.instagram.com/api/v1/direct_v2/inbox/?limit=20", "", inbox),
		entry("GET", "https://i.instagram.com/api/v1/direct_v2/threads/"+threadID+"/?limit=20", "", thread),
		entry("POST", "https://i.instagram.com/api/v1/direct_v2/create_group_thread/", createForm, created),
		entry("POST", "https://i.instagram.com/api/v1/direct_v2/threads/broadcast/text/", broadcastForm, broadcast),
	}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func urlEscape(value string) string {
	replacer := strings.NewReplacer("%", "%25", "[", "%5B", "]", "%5D", `"`, "%22", " ", "+")
	return replacer.Replace(value)
}

func writeHAR(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "direct.har")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func replaceDirectHARResponse(t *testing.T, content, pathFragment, response string) string {
	t.Helper()
	var doc harDocument
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	for i := range doc.Log.Entries {
		if strings.Contains(doc.Log.Entries[i].Request.URL, pathFragment) {
			doc.Log.Entries[i].Response.Content.Text = response
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			return string(raw)
		}
	}
	t.Fatalf("HAR contains no request matching %q", pathFragment)
	return ""
}

func TestInspectDirectHARCapturesAllContractsWithoutPrivateValues(t *testing.T) {
	path := writeHAR(t, completeDirectHAR(t, testThreadID, testRecipientID))
	report, err := inspectDirectHAR(context.Background(), path, testViewerID, testRecipientID, testRecipientID,
		func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Surfaces) != 4 {
		t.Fatalf("surfaces=%d", len(report.Surfaces))
	}
	rendered := renderDirectReport(report)
	for _, secret := range []string{
		"never-retain-session", "never-retain-csrf", "never-retain-auth",
		testViewerID, testRecipientID, testThreadID, testItemID, "private inbox text", "private outbound text",
		"private-inbox-cursor", "private-context", "private-user",
	} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("report leaked %q", secret)
		}
	}
	for _, expected := range []string{"Inbox pagination", "Thread retrieval", "{thread_id}", "{numeric_key}", "recipient_users", "client_context", "$.inbox.oldest_cursor"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("report missing %q", expected)
		}
	}
}

func TestInspectDirectHARRejectsUnownedThread(t *testing.T) {
	content := completeDirectHAR(t, testThreadID, testRecipientID)
	content = strings.Replace(content, "/threads/"+testThreadID+"/", "/threads/999/", 1)
	_, err := inspectDirectHAR(context.Background(), writeHAR(t, content), testViewerID, testRecipientID, testRecipientID, time.Now)
	if err == nil || !strings.Contains(err.Error(), "authenticated viewer's inbox") {
		t.Fatalf("error=%v", err)
	}
}

func TestInspectDirectHARRejectsIncompleteReadContracts(t *testing.T) {
	complete := completeDirectHAR(t, testThreadID, testRecipientID)
	tests := []struct {
		name         string
		pathFragment string
		response     string
		want         string
	}{
		{
			name:         "inbox object absent despite unrelated thread ID",
			pathFragment: "/direct_v2/inbox/",
			response:     `{"metadata":{"thread_id":"` + testThreadID + `"},"status":"ok"}`,
			want:         "omitted inbox object",
		},
		{
			name:         "thread ID outside empty inbox threads is not ownership proof",
			pathFragment: "/direct_v2/inbox/",
			response:     `{"inbox":{"threads":[],"oldest_cursor":"cursor","has_older":true},"metadata":{"thread_id":"` + testThreadID + `"},"status":"ok"}`,
			want:         "non-empty inbox.threads",
		},
		{
			name:         "inbox pagination absent",
			pathFragment: "/direct_v2/inbox/",
			response:     `{"inbox":{"threads":[{"thread_id":"` + testThreadID + `"}]},"status":"ok"}`,
			want:         "inbox.has_older",
		},
		{
			name:         "inbox continuation not proven",
			pathFragment: "/direct_v2/inbox/",
			response:     `{"inbox":{"threads":[{"thread_id":"` + testThreadID + `"}],"oldest_cursor":"cursor","has_older":false},"status":"ok"}`,
			want:         "did not prove continuation",
		},
		{
			name:         "thread object absent",
			pathFragment: "/direct_v2/threads/" + testThreadID + "/",
			response:     `{"status":"ok"}`,
			want:         "omitted thread object",
		},
		{
			name:         "thread response ID mismatch",
			pathFragment: "/direct_v2/threads/" + testThreadID + "/",
			response:     `{"thread":{"thread_id":"999","items":[{"item_id":"1","user_id":"2","item_type":"text"}],"oldest_cursor":"cursor","has_older":true},"status":"ok"}`,
			want:         "did not match",
		},
		{
			name:         "thread items absent",
			pathFragment: "/direct_v2/threads/" + testThreadID + "/",
			response:     `{"thread":{"thread_id":"` + testThreadID + `","oldest_cursor":"cursor","has_older":true},"status":"ok"}`,
			want:         "non-empty thread.items",
		},
		{
			name:         "thread item shape absent",
			pathFragment: "/direct_v2/threads/" + testThreadID + "/",
			response:     `{"thread":{"thread_id":"` + testThreadID + `","items":[{}],"oldest_cursor":"cursor","has_older":true},"status":"ok"}`,
			want:         "without item_id, user_id, or item_type",
		},
		{
			name:         "thread pagination absent",
			pathFragment: "/direct_v2/threads/" + testThreadID + "/",
			response:     `{"thread":{"thread_id":"` + testThreadID + `","items":[{"item_id":"1","user_id":"2","item_type":"text"}]},"status":"ok"}`,
			want:         "thread.has_older",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := replaceDirectHARResponse(t, complete, tt.pathFragment, tt.response)
			_, err := inspectDirectHAR(context.Background(), writeHAR(t, content), testViewerID, testRecipientID, testRecipientID, time.Now)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestInspectDirectHARRejectsRecipientMismatch(t *testing.T) {
	path := writeHAR(t, completeDirectHAR(t, testThreadID, testRecipientID))
	_, err := inspectDirectHAR(context.Background(), path, testViewerID, "999", "999", time.Now)
	if err == nil || !strings.Contains(err.Error(), "approved") {
		t.Fatalf("error=%v", err)
	}
	_, err = inspectDirectHAR(context.Background(), path, testViewerID, testRecipientID, "999", time.Now)
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("confirmation error=%v", err)
	}
}

func TestInspectDirectHARRejectsIncompleteOrNonIdempotentBroadcast(t *testing.T) {
	complete := completeDirectHAR(t, testThreadID, testRecipientID)
	missingSurface := strings.Replace(complete, "/threads/broadcast/text/", "/threads/broadcast/link/", 1)
	_, err := inspectDirectHAR(context.Background(), writeHAR(t, missingSurface), testViewerID, testRecipientID, testRecipientID, time.Now)
	if err == nil || !strings.Contains(err.Error(), "missing successful text broadcast") {
		t.Fatalf("missing error=%v", err)
	}
	mismatchedContext := strings.Replace(complete, "mutation_token=private-context", "mutation_token=other-context", 1)
	_, err = inspectDirectHAR(context.Background(), writeHAR(t, mismatchedContext), testViewerID, testRecipientID, testRecipientID, time.Now)
	if err == nil || !strings.Contains(err.Error(), "differ") {
		t.Fatalf("context error=%v", err)
	}
}

func TestInspectDirectHARRejects2xxFailureAndBroadcastWithoutItemID(t *testing.T) {
	complete := completeDirectHAR(t, testThreadID, testRecipientID)
	statusMarker := `\"status\":\"ok\"`
	statusIndex := strings.LastIndex(complete, statusMarker)
	if statusIndex < 0 {
		t.Fatal("test HAR has no broadcast status marker")
	}
	statusFail := complete[:statusIndex] + `\"status\":\"fail\"` + complete[statusIndex+len(statusMarker):]
	_, err := inspectDirectHAR(context.Background(), writeHAR(t, statusFail), testViewerID, testRecipientID, testRecipientID, time.Now)
	if err == nil || !strings.Contains(err.Error(), "status=ok") {
		t.Fatalf("2xx status=fail error=%v", err)
	}

	missingItemID := replaceDirectHARResponse(t, complete, "/threads/broadcast/text/", `{"payload":{},"status":"ok"}`)
	_, err = inspectDirectHAR(context.Background(), writeHAR(t, missingItemID), testViewerID, testRecipientID, testRecipientID, time.Now)
	if err == nil || !strings.Contains(err.Error(), "payload.item_id") {
		t.Fatalf("missing item ID error=%v", err)
	}
}

func TestWriteDirectReportRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.md")
	report := directReport{CapturedAt: time.Now()}
	if err := writeDirectReport(path, report); err != nil {
		t.Fatal(err)
	}
	if err := writeDirectReport(path, report); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}
