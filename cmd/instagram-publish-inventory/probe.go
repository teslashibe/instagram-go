package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type inventoryReport struct {
	CapturedAt time.Time
	Flows      []flowCapture
}

type flowCapture struct {
	Kind     string
	Surfaces []capturedSurface
}

type capturedSurface struct {
	Stage          string
	Method         string
	Host           string
	Path           string
	RequestHeaders []string
	RequestFields  []string
	ResponseFields []string
	StatusCode     int
}

type harFile struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harEntry struct {
	Request struct {
		Method  string `json:"method"`
		URL     string `json:"url"`
		Headers []struct {
			Name string `json:"name"`
		} `json:"headers"`
		PostData struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Params   []struct {
				Name string `json:"name"`
			} `json:"params"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int `json:"status"`
		Content struct {
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

func captureInventory(now time.Time, files map[string]string) (inventoryReport, error) {
	result := inventoryReport{CapturedAt: now.UTC()}
	for _, kind := range []string{"photo", "reel", "story"} {
		path := files[kind]
		if path == "" {
			return inventoryReport{}, fmt.Errorf("%s HAR required", kind)
		}
		flow, err := inspectPublishingHAR(kind, path)
		if err != nil {
			return inventoryReport{}, fmt.Errorf("%s HAR: %w", kind, err)
		}
		result.Flows = append(result.Flows, flow)
	}
	return result, nil
}

func inspectPublishingHAR(kind, path string) (flowCapture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return flowCapture{}, err
	}
	var har harFile
	if err := json.Unmarshal(raw, &har); err != nil {
		return flowCapture{}, fmt.Errorf("decode HAR: %w", err)
	}
	out := flowCapture{Kind: kind}
	seen := map[string]bool{}
	var configuredID string
	var deletedID string
	for _, entry := range har.Log.Entries {
		surface, ok, err := inspectPublishingEntry(entry)
		if err != nil {
			return flowCapture{}, err
		}
		if !ok {
			continue
		}
		if entry.Response.Status < 200 || entry.Response.Status >= 300 {
			return flowCapture{}, fmt.Errorf("%s returned HTTP %d", surface.Stage, entry.Response.Status)
		}
		if !responseIsOK(entry) {
			return flowCapture{}, fmt.Errorf("%s response was not status=ok", surface.Stage)
		}
		out.Surfaces = append(out.Surfaces, surface)
		seen[surface.Stage] = true
		switch surface.Stage {
		case "photo_configure", "reel_configure", "story_configure":
			configuredID, err = configuredMediaID(entry)
			if err != nil {
				return flowCapture{}, fmt.Errorf("%s: %w", surface.Stage, err)
			}
		case "exact_media_delete":
			deletedID, err = deletedMediaID(entry.Request.URL)
			if err != nil {
				return flowCapture{}, fmt.Errorf("exact_media_delete: %w", err)
			}
		}
	}
	required := map[string][]string{
		"photo": {"photo_upload", "photo_configure", "exact_media_delete"},
		"reel":  {"video_upload", "thumbnail_upload", "upload_finish", "upload_status", "reel_configure", "exact_media_delete"},
		"story": {"video_upload", "thumbnail_upload", "upload_finish", "upload_status", "story_configure", "exact_media_delete"},
	}[kind]
	if required == nil {
		return flowCapture{}, fmt.Errorf("unsupported flow kind %q", kind)
	}
	var missing []string
	for _, stage := range required {
		if !seen[stage] {
			missing = append(missing, stage)
		}
	}
	if len(missing) > 0 {
		return flowCapture{}, fmt.Errorf("incomplete contract; missing %s", strings.Join(missing, ", "))
	}
	if configuredID == "" || deletedID == "" || configuredID != deletedID {
		return flowCapture{}, errors.New("delete target does not match the exact media ID returned by configure")
	}
	return out, nil
}

func inspectPublishingEntry(entry harEntry) (capturedSurface, bool, error) {
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return capturedSurface{}, false, fmt.Errorf("parse request URL: %w", err)
	}
	stage := publishingStage(u.Path)
	if stage == "" {
		return capturedSurface{}, false, nil
	}
	if stage == "photo_upload" && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "_0") {
		stage = "thumbnail_upload"
	}
	headers := make([]string, 0, len(entry.Request.Headers))
	for _, header := range entry.Request.Headers {
		headers = append(headers, strings.ToLower(header.Name))
	}
	fields := make([]string, 0, len(entry.Request.PostData.Params))
	for _, param := range entry.Request.PostData.Params {
		fields = append(fields, param.Name)
	}
	if len(fields) == 0 && strings.Contains(strings.ToLower(entry.Request.PostData.MimeType), "form") {
		if values, err := url.ParseQuery(entry.Request.PostData.Text); err == nil {
			for key := range values {
				fields = append(fields, key)
			}
		}
	}
	responseRaw, err := responseBody(entry)
	if err != nil {
		return capturedSurface{}, false, fmt.Errorf("%s response: %w", stage, err)
	}
	var decoded any
	if len(responseRaw) > 0 {
		if err := json.Unmarshal(responseRaw, &decoded); err != nil {
			return capturedSurface{}, false, fmt.Errorf("%s response JSON: %w", stage, err)
		}
	}
	sort.Strings(headers)
	sort.Strings(fields)
	return capturedSurface{
		Stage: stage, Method: entry.Request.Method, Host: u.Scheme + "://" + u.Host,
		Path: redactEntityPath(u.Path), RequestHeaders: unique(headers), RequestFields: unique(fields),
		ResponseFields: collectPaths(decoded), StatusCode: entry.Response.Status,
	}, true, nil
}

func publishingStage(path string) string {
	switch {
	case strings.Contains(path, "/rupload_igvideo/"):
		return "video_upload"
	case strings.Contains(path, "/rupload_igphoto/"):
		return "photo_upload"
	case strings.HasSuffix(path, "/api/v1/media/configure/"):
		return "photo_configure"
	case strings.HasSuffix(path, "/api/v1/media/configure_to_clips/"):
		return "reel_configure"
	case strings.HasSuffix(path, "/api/v1/media/configure_to_story/"):
		return "story_configure"
	case strings.HasSuffix(path, "/api/v1/media/upload_finish/"):
		return "upload_finish"
	case strings.HasSuffix(path, "/api/v1/media/upload_status/"):
		return "upload_status"
	case strings.Contains(path, "/api/v1/media/") && strings.HasSuffix(path, "/delete/"):
		return "exact_media_delete"
	default:
		return ""
	}
}

func responseBody(entry harEntry) ([]byte, error) {
	if entry.Response.Content.Text == "" {
		return nil, nil
	}
	if strings.EqualFold(entry.Response.Content.Encoding, "base64") {
		return base64.StdEncoding.DecodeString(entry.Response.Content.Text)
	}
	return []byte(entry.Response.Content.Text), nil
}

func responseIsOK(entry harEntry) bool {
	raw, err := responseBody(entry)
	if err != nil || len(raw) == 0 {
		return false
	}
	var envelope struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Status == "ok"
}

func configuredMediaID(entry harEntry) (string, error) {
	raw, err := responseBody(entry)
	if err != nil {
		return "", err
	}
	var envelope struct {
		Media struct {
			PK json.RawMessage `json:"pk"`
		} `json:"media"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	id := strings.Trim(strings.TrimSpace(string(envelope.Media.PK)), `"`)
	if id == "" || id == "null" {
		return "", errors.New("configure response has no media.pk")
	}
	return id, nil
}

func deletedMediaID(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	const prefix = "/api/v1/media/"
	index := strings.Index(u.Path, prefix)
	if index < 0 || !strings.HasSuffix(u.Path, "/delete/") {
		return "", errors.New("delete request has no exact media path")
	}
	id := strings.TrimSuffix(u.Path[index+len(prefix):], "/delete/")
	id, err = url.PathUnescape(id)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", errors.New("delete request has an invalid exact media ID")
	}
	return id, nil
}

func redactEntityPath(path string) string {
	for _, marker := range []string{"/rupload_igphoto/", "/rupload_igvideo/"} {
		if index := strings.Index(path, marker); index >= 0 {
			return path[:index] + marker + "<entity>"
		}
	}
	if index := strings.Index(path, "/api/v1/media/"); index >= 0 && strings.HasSuffix(path, "/delete/") {
		return path[:index] + "/api/v1/media/<media_id>/delete/"
	}
	return path
}

func collectPaths(value any) []string {
	set := map[string]struct{}{}
	var walk func(any, string)
	walk = func(current any, path string) {
		switch node := current.(type) {
		case map[string]any:
			for key, child := range node {
				next := "$." + key
				if path != "$" {
					next = path + "." + key
				}
				set[next] = struct{}{}
				walk(child, next)
			}
		case []any:
			for _, child := range node {
				walk(child, path+"[]")
			}
		}
	}
	if value != nil {
		walk(value, "$")
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func unique(items []string) []string {
	if len(items) < 2 {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if len(out) == 0 || item != out[len(out)-1] {
			out = append(out, item)
		}
	}
	return out
}

func writeAtomic(path string, report inventoryReport) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := renderReport(report)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".publish-inventory-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
