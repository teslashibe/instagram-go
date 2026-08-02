package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	instagram "github.com/teslashibe/instagram-go"
	igmcp "github.com/teslashibe/instagram-go/mcp"
	"github.com/teslashibe/mcptool"
)

func TestPublishingToolsAreSeparatelyClassifiedAndConfirmed(t *testing.T) {
	for _, name := range []string{"instagram_publish_photo", "instagram_publish_reel", "instagram_publish_story"} {
		tool := findTool(t, name)
		if !reflect.DeepEqual(tool.Tags, []string{"write", "publishing", "mutation"}) {
			t.Errorf("%s tags = %#v", name, tool.Tags)
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties = %#v", name, tool.InputSchema)
		}
		if _, ok := properties["confirm_mutation"]; !ok {
			t.Errorf("%s lacks confirm_mutation schema", name)
		}
	}

	requests := 0
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{"status":"ok"}`), nil
	})
	for _, name := range []string{"instagram_publish_photo", "instagram_publish_reel", "instagram_publish_story"} {
		_, err := findTool(t, name).Invoke(context.Background(), client, json.RawMessage(`{}`))
		var toolErr *mcptool.Error
		if !errors.As(err, &toolErr) || toolErr.Code != "mutation_confirmation_required" {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("unconfirmed mutations made %d requests", requests)
	}
}

func TestPublishingToolRejectsOversizeAndInvalidBase64BeforeHTTP(t *testing.T) {
	requests := 0
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return mcpJSONResponse(req, http.StatusOK, `{}`), nil
	})
	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "declared oversize",
			body: `{"confirm_mutation":true,"media_base64":"eA==","filename":"x.jpg","mime_type":"image/jpeg","byte_size":8388609,"width":1,"height":1,"idempotency_key":"x"}`,
			code: "upload_too_large",
		},
		{
			name: "invalid base64",
			body: `{"confirm_mutation":true,"media_base64":"%%%","filename":"x.jpg","mime_type":"image/jpeg","byte_size":1,"width":1,"height":1,"idempotency_key":"x"}`,
			code: "invalid_input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findTool(t, "instagram_publish_photo").Invoke(context.Background(), client, json.RawMessage(tt.body))
			var toolErr *mcptool.Error
			if !errors.As(err, &toolErr) || toolErr.Code != tt.code {
				t.Fatalf("error = %#v, want %s", err, tt.code)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("invalid media made %d requests", requests)
	}
}

func TestPublishPhotoToolReturnsCreatedIdentifiers(t *testing.T) {
	raw := []byte("burner")
	client := newMCPClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/rupload_igphoto/") {
			var params struct {
				UploadID string `json:"upload_id"`
			}
			if err := json.Unmarshal([]byte(req.Header.Get("X-Instagram-Rupload-Params")), &params); err != nil {
				t.Fatal(err)
			}
			return mcpJSONResponse(req, http.StatusOK, `{"status":"ok","upload_id":"`+params.UploadID+`"}`), nil
		}
		if req.URL.Path == "/api/v1/media/configure/" {
			return mcpJSONResponse(req, http.StatusOK, `{"status":"ok","media":{"pk":"123","code":"SAFE","media_type":1}}`), nil
		}
		t.Fatalf("unexpected request %s", req.URL.Path)
		return nil, nil
	})
	input, err := json.Marshal(map[string]any{
		"confirm_mutation": true,
		"media_base64":     base64.StdEncoding.EncodeToString(raw),
		"filename":         "burner.jpg",
		"mime_type":        "image/jpeg",
		"byte_size":        len(raw),
		"width":            1,
		"height":           1,
		"idempotency_key":  "mcp-photo",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := findTool(t, "instagram_publish_photo").Invoke(context.Background(), client, input)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(*instagram.PublishResult)
	if !ok || result.MediaID != "123" || result.UploadID == "" || result.ClientID == "" {
		t.Fatalf("result = %#v (%T)", value, value)
	}
}

func TestDeleteMediaIsNotExposedThroughMCP(t *testing.T) {
	if reason := igmcp.Excluded["DeleteMedia"]; !strings.Contains(reason, "destructive") {
		t.Fatalf("DeleteMedia exclusion reason = %q", reason)
	}
	for _, tool := range (igmcp.Provider{}).Tools() {
		if tool.WrapsMethod == "DeleteMedia" {
			t.Fatalf("DeleteMedia exposed as %s", tool.Name)
		}
	}
}
