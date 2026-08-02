package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	Kind             string
	Surfaces         []capturedSurface
	ProcessingStates []string
	DeleteMediaType  string
}

type capturedSurface struct {
	Stage              string
	Method             string
	Host               string
	Path               string
	RequestHeaders     []string
	RequestFields      []string
	RequestQueryFields []string
	ResponseFields     []string
	StatusCode         int
}

type harFile struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harEntry struct {
	Request struct {
		Method   string         `json:"method"`
		URL      string         `json:"url"`
		Headers  []harNameValue `json:"headers"`
		PostData struct {
			MimeType string         `json:"mimeType"`
			Text     string         `json:"text"`
			Params   []harNameValue `json:"params"`
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

type inspectedPublishingEntry struct {
	entry         harEntry
	surface       capturedSurface
	url           *url.URL
	headers       map[string]string
	form          url.Values
	response      map[string]any
	processing    string
	configuredID  string
	deletedID     string
	confirmedID   string
	deleteType    string
	uploadID      string
	clientContext string
}

var flowStages = map[string][]string{
	"photo": {
		"photo_upload", "upload_status", "photo_configure",
		"exact_media_delete", "delete_confirmation",
	},
	"reel": {
		"video_upload", "thumbnail_upload", "upload_finish", "upload_status",
		"reel_configure", "exact_media_delete", "delete_confirmation",
	},
	"story": {
		"video_upload", "thumbnail_upload", "upload_finish", "upload_status",
		"story_configure", "exact_media_delete", "delete_confirmation",
	},
}

var rawUploadHeaders = []string{
	"offset",
	"x-entity-length",
	"x-entity-name",
	"x-entity-type",
	"x-instagram-rupload-params",
	"x_fb_photo_waterfall_id",
}

var pendingProcessingStates = map[string]bool{
	"pending": true, "processing": true, "transcoding": true,
	"uploading": true, "in_progress": true,
}

var readyProcessingStates = map[string]bool{
	"ok": true, "ready": true, "finished": true, "complete": true,
	"completed": true, "succeeded": true, "uploaded": true,
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
	required := flowStages[kind]
	if required == nil {
		return flowCapture{}, fmt.Errorf("unsupported flow kind %q", kind)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return flowCapture{}, err
	}
	var har harFile
	if err := json.Unmarshal(raw, &har); err != nil {
		return flowCapture{}, fmt.Errorf("decode HAR: %w", err)
	}

	out := flowCapture{Kind: kind}
	var entries []inspectedPublishingEntry
	for _, entry := range har.Log.Entries {
		inspected, ok, err := inspectPublishingEntry(entry)
		if err != nil {
			return flowCapture{}, err
		}
		if ok {
			entries = append(entries, inspected)
			out.Surfaces = append(out.Surfaces, inspected.surface)
		}
	}
	if err := validateStageOrder(required, entries); err != nil {
		return flowCapture{}, err
	}

	var uploadID, clientContext, configuredID, deletedID, confirmedID string
	for index := range entries {
		item := &entries[index]
		if err := validatePublishingSurface(kind, item); err != nil {
			return flowCapture{}, fmt.Errorf("%s: %w", item.surface.Stage, err)
		}
		switch item.surface.Stage {
		case "photo_upload", "video_upload":
			if uploadID != "" {
				return flowCapture{}, errors.New("multiple primary uploads are not a complete single-media flow")
			}
			uploadID = item.uploadID
			clientContext = item.clientContext
		case "thumbnail_upload":
			if item.uploadID != uploadID {
				return flowCapture{}, errors.New("thumbnail upload_id does not match the primary upload")
			}
			if item.clientContext != clientContext {
				return flowCapture{}, errors.New("thumbnail waterfall ID does not match the primary upload")
			}
			if !strings.HasSuffix(strings.TrimRight(item.url.Path, "/"), "/"+uploadID+"_0") {
				return flowCapture{}, errors.New("thumbnail entity name is not correlated as <upload_id>_0")
			}
		case "upload_finish":
			if item.form.Get("upload_id") != uploadID {
				return flowCapture{}, errors.New("upload_finish upload_id does not match the primary upload")
			}
		case "upload_status":
			if item.url.Query().Get("upload_id") != uploadID {
				return flowCapture{}, errors.New("upload_status upload_id does not match the primary upload")
			}
			out.ProcessingStates = append(out.ProcessingStates, item.processing)
		case "photo_configure", "reel_configure", "story_configure":
			if item.form.Get("upload_id") != uploadID {
				return flowCapture{}, errors.New("configure upload_id does not match the primary upload")
			}
			if item.form.Get("client_context") != clientContext {
				return flowCapture{}, errors.New("configure client_context does not match the upload waterfall ID")
			}
			configuredID = item.configuredID
		case "exact_media_delete":
			deletedID = item.deletedID
			out.DeleteMediaType = item.deleteType
		case "delete_confirmation":
			confirmedID = item.confirmedID
		}
	}
	if uploadID == "" || clientContext == "" {
		return flowCapture{}, errors.New("primary upload lacks correlated upload_id or client context")
	}
	if len(out.ProcessingStates) == 0 ||
		!readyProcessingStates[out.ProcessingStates[len(out.ProcessingStates)-1]] {
		return flowCapture{}, errors.New("status contract has no captured terminal ready processing state")
	}
	if kind != "photo" {
		hasPending := false
		for _, state := range out.ProcessingStates {
			hasPending = hasPending || pendingProcessingStates[state]
		}
		if !hasPending {
			return flowCapture{}, errors.New("video status contract has no captured pending/processing state")
		}
	}
	if configuredID == "" || deletedID != configuredID || confirmedID != configuredID {
		return flowCapture{}, errors.New("configure, exact delete, and post-delete confirmation IDs do not match")
	}
	return out, nil
}

func validateStageOrder(required []string, entries []inspectedPublishingEntry) error {
	expected := 0
	for _, item := range entries {
		stage := item.surface.Stage
		if expected > 0 && stage == "upload_status" && required[expected-1] == "upload_status" {
			continue
		}
		if expected >= len(required) || stage != required[expected] {
			want := "<none>"
			if expected < len(required) {
				want = required[expected]
			}
			return fmt.Errorf("out-of-order stage %q; expected %q", stage, want)
		}
		expected++
	}
	if expected != len(required) {
		return fmt.Errorf("incomplete ordered contract; missing %s", strings.Join(required[expected:], ", "))
	}
	return nil
}

func inspectPublishingEntry(entry harEntry) (inspectedPublishingEntry, bool, error) {
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return inspectedPublishingEntry{}, false, fmt.Errorf("parse request URL: %w", err)
	}
	stage := publishingStage(u.Path)
	if stage == "" {
		return inspectedPublishingEntry{}, false, nil
	}
	if stage == "photo_upload" && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "_0") {
		stage = "thumbnail_upload"
	}
	headers := make(map[string]string, len(entry.Request.Headers))
	headerNames := make([]string, 0, len(entry.Request.Headers))
	for _, header := range entry.Request.Headers {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		headers[name] = header.Value
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)

	form := requestFormValues(entry)
	fieldNames := sortedValueKeys(form)
	queryNames := sortedValueKeys(u.Query())
	responseRaw, err := responseBody(entry)
	if err != nil {
		return inspectedPublishingEntry{}, false, fmt.Errorf("%s response: %w", stage, err)
	}
	var decoded any
	response := map[string]any{}
	if len(responseRaw) > 0 {
		if err := json.Unmarshal(responseRaw, &decoded); err != nil {
			if stage != "delete_confirmation" {
				return inspectedPublishingEntry{}, false, fmt.Errorf("%s response JSON: %w", stage, err)
			}
		} else if object, ok := decoded.(map[string]any); ok {
			response = object
		}
	}
	item := inspectedPublishingEntry{
		entry:    entry,
		url:      u,
		headers:  headers,
		form:     form,
		response: response,
		surface: capturedSurface{
			Stage: stage, Method: strings.ToUpper(entry.Request.Method),
			Host: u.Scheme + "://" + u.Host, Path: redactEntityPath(u.Path),
			RequestHeaders: unique(headerNames), RequestFields: fieldNames,
			RequestQueryFields: queryNames, ResponseFields: collectPaths(decoded),
			StatusCode: entry.Response.Status,
		},
	}
	return item, true, nil
}

func validatePublishingSurface(kind string, item *inspectedPublishingEntry) error {
	stage := item.surface.Stage
	if item.url.Scheme != "https" || !strings.EqualFold(item.url.Host, "i.instagram.com") {
		return fmt.Errorf("unexpected host %q; captures must use https://i.instagram.com", item.surface.Host)
	}
	wantMethod := http.MethodPost
	if stage == "upload_status" || stage == "delete_confirmation" {
		wantMethod = http.MethodGet
	}
	if item.surface.Method != wantMethod {
		return fmt.Errorf("method=%s, want %s", item.surface.Method, wantMethod)
	}
	if stage == "delete_confirmation" {
		if item.entry.Response.Status != http.StatusNotFound && !responseConfirmsMissing(item.response) {
			return errors.New("post-delete readback did not confirm the exact media is unavailable")
		}
		id, err := mediaIDFromPath(item.url.Path, "/info/")
		if err != nil {
			return err
		}
		item.confirmedID = id
		return nil
	}
	if item.entry.Response.Status < 200 || item.entry.Response.Status >= 300 {
		return fmt.Errorf("HTTP %d", item.entry.Response.Status)
	}
	if statusString(item.response, "status") != "ok" {
		return errors.New("response was not JSON status=ok")
	}

	switch stage {
	case "photo_upload", "video_upload", "thumbnail_upload":
		if err := requireNames(item.surface.RequestHeaders, rawUploadHeaders); err != nil {
			return fmt.Errorf("headers: %w", err)
		}
		var params struct {
			UploadID string `json:"upload_id"`
		}
		if err := json.Unmarshal([]byte(item.headers["x-instagram-rupload-params"]), &params); err != nil {
			return fmt.Errorf("decode X-Instagram-Rupload-Params: %w", err)
		}
		if params.UploadID == "" {
			return errors.New("rupload params have no upload_id")
		}
		item.uploadID = params.UploadID
		item.clientContext = strings.TrimSpace(item.headers["x_fb_photo_waterfall_id"])
		if item.clientContext == "" {
			return errors.New("X_FB_PHOTO_WATERFALL_ID is empty")
		}
		if ack := statusString(item.response, "upload_id"); ack != "" && ack != item.uploadID {
			return errors.New("rupload response upload_id does not match request")
		}
		if stage != "thumbnail_upload" &&
			!strings.Contains(strings.TrimRight(item.url.Path, "/"), item.uploadID) {
			return errors.New("primary rupload entity path is not correlated with upload_id")
		}
	case "upload_finish":
		if err := requireNames(item.surface.RequestFields,
			[]string{"media_type", "source_type", "upload_id", "video"}); err != nil {
			return err
		}
	case "upload_status":
		if err := requireNames(item.surface.RequestQueryFields, []string{"upload_id"}); err != nil {
			return err
		}
		item.processing = strings.ToLower(strings.TrimSpace(nestedString(item.response, "processing_info", "state")))
		if !pendingProcessingStates[item.processing] && !readyProcessingStates[item.processing] {
			return fmt.Errorf("uncaptured processing state %q", item.processing)
		}
	case "photo_configure", "reel_configure", "story_configure":
		required := []string{
			"caption", "client_context", "source_type", "upload_id",
			"upload_media_height", "upload_media_width",
		}
		if stage == "story_configure" {
			required = append(required, "configure_mode", "story_media_creation_date")
		}
		if err := requireNames(item.surface.RequestFields, required); err != nil {
			return err
		}
		item.configuredID = nestedString(item.response, "media", "pk")
		if item.configuredID == "" {
			return errors.New("configure response has no media.pk")
		}
	case "exact_media_delete":
		if err := requireNames(item.surface.RequestFields, []string{"media_type"}); err != nil {
			return err
		}
		id, err := mediaIDFromPath(item.url.Path, "/delete/")
		if err != nil {
			return err
		}
		item.deletedID = id
		item.deleteType = item.form.Get("media_type")
		if item.deleteType == "" {
			return errors.New("delete media_type is empty")
		}
	}
	return nil
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
	case strings.Contains(path, "/api/v1/media/") && strings.HasSuffix(path, "/info/"):
		return "delete_confirmation"
	default:
		return ""
	}
}

func requestFormValues(entry harEntry) url.Values {
	values := url.Values{}
	for _, param := range entry.Request.PostData.Params {
		values.Add(param.Name, param.Value)
	}
	if entry.Request.PostData.Text != "" {
		if parsed, err := url.ParseQuery(entry.Request.PostData.Text); err == nil {
			for key, items := range parsed {
				values[key] = append([]string(nil), items...)
			}
		}
	}
	return values
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

func responseConfirmsMissing(response map[string]any) bool {
	status := strings.ToLower(statusString(response, "status"))
	message := strings.ToLower(statusString(response, "message"))
	return status == "fail" &&
		(strings.Contains(message, "media not found") || strings.Contains(message, "media_not_found"))
}

func statusString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func nestedString(object map[string]any, path ...string) string {
	var current any = object
	for _, key := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = node[key]
	}
	switch value := current.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		return fmt.Sprintf("%.0f", value)
	default:
		return ""
	}
}

func mediaIDFromPath(path, suffix string) (string, error) {
	const prefix = "/api/v1/media/"
	index := strings.Index(path, prefix)
	if index < 0 || !strings.HasSuffix(path, suffix) {
		return "", errors.New("request has no exact media path")
	}
	id := strings.TrimSuffix(path[index+len(prefix):], suffix)
	id, err := url.PathUnescape(id)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", errors.New("request has an invalid exact media ID")
	}
	return id, nil
}

func redactEntityPath(path string) string {
	for _, marker := range []string{"/rupload_igphoto/", "/rupload_igvideo/"} {
		if index := strings.Index(path, marker); index >= 0 {
			return path[:index] + marker + "<entity>"
		}
	}
	if index := strings.Index(path, "/api/v1/media/"); index >= 0 {
		switch {
		case strings.HasSuffix(path, "/delete/"):
			return path[:index] + "/api/v1/media/<media_id>/delete/"
		case strings.HasSuffix(path, "/info/"):
			return path[:index] + "/api/v1/media/<media_id>/info/"
		}
	}
	return path
}

func requireNames(got, required []string) error {
	set := make(map[string]bool, len(got))
	for _, name := range got {
		set[strings.ToLower(name)] = true
	}
	var missing []string
	for _, name := range required {
		if !set[strings.ToLower(name)] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required names: %s", strings.Join(missing, ", "))
	}
	return nil
}

func sortedValueKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
