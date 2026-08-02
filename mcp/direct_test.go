package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

func TestDirectToolsRegisteredAndSendIsSeparatelyClassified(t *testing.T) {
	for _, name := range []string{"instagram_get_direct_inbox", "instagram_get_direct_thread", "instagram_send_direct_text"} {
		tool := findTool(t, name)
		if len(tool.Description) > 120 {
			t.Fatalf("%s description too long", name)
		}
		if name == "instagram_send_direct_text" {
			if !reflect.DeepEqual(tool.Tags, []string{"write"}) {
				t.Fatalf("send tags = %#v", tool.Tags)
			}
		} else if len(tool.Tags) != 0 {
			t.Fatalf("read tool %s tags = %#v", name, tool.Tags)
		}
	}
	send := findTool(t, "instagram_send_direct_text")
	properties := send.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"recipient_id", "text", "confirm_send", "thread_id", "client_context"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("send schema lacks %q", field)
		}
	}
}

func TestDirectSendRequiresConfirmationRecipientAndTextWithoutHTTP(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{}`), nil
	})
	for _, input := range []string{
		`{"recipient_id":"123","text":"hello","confirm_send":false}`,
		`{"text":"hello","confirm_send":true}`,
		`{"recipient_id":"123","text":"  ","confirm_send":true}`,
		`{"recipient_id":"123","text":"hello","confirm_send":true,"thread_id":"456"}`,
	} {
		_, err := findTool(t, "instagram_send_direct_text").Invoke(context.Background(), client, json.RawMessage(input))
		var toolErr *mcptool.Error
		if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" {
			t.Fatalf("input=%s error=%v", input, err)
		}
	}
	if requests != 0 {
		t.Fatalf("unsafe input made %d HTTP requests", requests)
	}
}

func TestDirectInboxMCPPartialPageCursorDoesNotSkipItems(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{"inbox":{"threads":[
			{"thread_id":"1","users":[]},
			{"thread_id":"2","users":[]},
			{"thread_id":"3","users":[]}
		],"has_older":false},"status":"ok"}`), nil
	})
	tool := findTool(t, "instagram_get_direct_inbox")
	firstRaw, err := tool.Invoke(context.Background(), client, json.RawMessage(`{"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	first := firstRaw.(mcptool.Page[*instagram.DirectThread])
	if len(first.Items) != 2 || first.NextCursor == "" || !first.Truncated {
		t.Fatalf("first = %#v", first)
	}
	input, _ := json.Marshal(map[string]any{"limit": 2, "cursor": first.NextCursor})
	secondRaw, err := tool.Invoke(context.Background(), client, input)
	if err != nil {
		t.Fatal(err)
	}
	second := secondRaw.(mcptool.Page[*instagram.DirectThread])
	if len(second.Items) != 1 || second.Items[0].ID != "3" || second.NextCursor != "" {
		t.Fatalf("second = %#v", second)
	}
	if requests != 2 {
		t.Fatalf("requests=%d, want replay plus resume", requests)
	}
}

func TestDirectThreadMCPRejectsCrossThreadCursorWithoutHTTP(t *testing.T) {
	var requests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{"thread":{"items":[{"item_id":"1","user_id":"2","item_type":"text","text":"hello"}],"oldest_cursor":"secret","has_older":true},"status":"ok"}`), nil
	})
	tool := findTool(t, "instagram_get_direct_thread")
	firstRaw, err := tool.Invoke(context.Background(), client, json.RawMessage(`{"thread_id":"10","limit":12}`))
	if err != nil {
		t.Fatal(err)
	}
	first := firstRaw.(mcptool.Page[*instagram.DirectItem])
	if first.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	input, _ := json.Marshal(map[string]any{"thread_id": "11", "cursor": first.NextCursor})
	_, err = tool.Invoke(context.Background(), client, input)
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" || !strings.Contains(toolErr.Message, "cursor") {
		t.Fatalf("error=%v", err)
	}
	if requests != 1 {
		t.Fatalf("cross-thread cursor made request; requests=%d", requests)
	}
}

func TestDirectToolsReturnStructuredChallengeRateAndAuthErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{name: "challenge", status: http.StatusOK, body: `{"message":"challenge_required","status":"fail"}`, code: "challenge_required"},
		{name: "rate", status: http.StatusTooManyRequests, body: `{"message":"Please wait a few minutes","status":"fail"}`, code: "rate_limited"},
		{name: "auth", status: http.StatusUnauthorized, body: `{"message":"login_required","status":"fail"}`, code: "credential_expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
				return mcpJSONResponse(req, tt.status, tt.body), nil
			})
			_, err := findTool(t, "instagram_get_direct_inbox").Invoke(context.Background(), client, json.RawMessage(`{}`))
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != tt.code {
				t.Fatalf("error=%v, want %s", err, tt.code)
			}
		})
	}
}

func TestDirectSendStructuredErrorIncludesClientContext(t *testing.T) {
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		return mcpJSONResponse(req, http.StatusOK, `{"message":"challenge_required","status":"fail"}`), nil
	})
	_, err := findTool(t, "instagram_send_direct_text").Invoke(context.Background(), client,
		json.RawMessage(`{"recipient_id":"123","text":"hello","confirm_send":true,"client_context":"retry-me"}`))
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "challenge_required" || toolErr.Data["client_context"] != "retry-me" {
		t.Fatalf("error=%#v", err)
	}
}

func TestDirectSendMissingItemIDIsStructuredAndRetrySkipsThreadCreation(t *testing.T) {
	var createRequests, broadcastRequests int
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/direct_v2/create_group_thread/":
			createRequests++
			return mcpJSONResponse(req, http.StatusOK, `{"thread":{"thread_id":"456"},"status":"ok"}`), nil
		case "/api/v1/direct_v2/threads/broadcast/text/":
			broadcastRequests++
			if broadcastRequests == 1 {
				return mcpJSONResponse(req, http.StatusOK, `{"payload":{},"status":"ok"}`), nil
			}
			return mcpJSONResponse(req, http.StatusOK, `{"payload":{"item_id":"789"},"status":"ok"}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})
	tool := findTool(t, "instagram_send_direct_text")
	_, err := tool.Invoke(context.Background(), client,
		json.RawMessage(`{"recipient_id":"123","text":"hello","confirm_send":true,"client_context":"retry-me"}`))
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "send_outcome_uncertain" || toolErr.Retryable {
		t.Fatalf("error=%#v", err)
	}
	if toolErr.Data["thread_id"] != "456" || toolErr.Data["client_context"] != "retry-me" ||
		!strings.Contains(toolErr.Message, "retry only the broadcast") {
		t.Fatalf("structured reconciliation error=%#v", toolErr)
	}

	retryRaw, err := tool.Invoke(context.Background(), client,
		json.RawMessage(`{"recipient_id":"123","text":"hello","confirm_send":true,"thread_id":"456","client_context":"retry-me"}`))
	if err != nil {
		t.Fatalf("broadcast-only retry: %v", err)
	}
	retry := retryRaw.(map[string]any)["send"].(*instagram.DirectSendResult)
	if retry.ItemID != "789" || createRequests != 1 || broadcastRequests != 2 {
		t.Fatalf("retry=%#v create=%d broadcast=%d", retry, createRequests, broadcastRequests)
	}
}

func TestDirectSendMapsCSRFAndCancellationErrors(t *testing.T) {
	t.Run("csrf", func(t *testing.T) {
		client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
			return mcpJSONResponse(req, http.StatusForbidden, `{"message":"CSRF token missing","status":"fail"}`), nil
		})
		_, err := findTool(t, "instagram_send_direct_text").Invoke(context.Background(), client,
			json.RawMessage(`{"recipient_id":"123","text":"hello","confirm_send":true,"client_context":"csrf-context"}`))
		var toolErr *mcptool.Error
		if !errors.As(err, &toolErr) || toolErr.Code != "csrf_rejected" || toolErr.Data["client_context"] != "csrf-context" {
			t.Fatalf("error=%#v", err)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := findTool(t, "instagram_send_direct_text").Invoke(ctx, client,
			json.RawMessage(`{"recipient_id":"123","text":"hello","confirm_send":true,"client_context":"cancel-context"}`))
		var toolErr *mcptool.Error
		if !errors.As(err, &toolErr) || toolErr.Code != "operation_canceled" || toolErr.Data["client_context"] != "cancel-context" {
			t.Fatalf("error=%#v", err)
		}
	})
}
