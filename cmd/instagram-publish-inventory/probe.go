package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	RuploadParamFields []string
	ProtocolValues     []string
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
	ruploadParams map[string]any
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
		}
	}
	if err := validateStageOrder(required, entries); err != nil {
		return flowCapture{}, err
	}

	var uploadID, clientContext, uploadWidth, uploadHeight, configuredID, deletedID, confirmedID string
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
			uploadWidth = mapString(item.ruploadParams, "upload_media_width")
			uploadHeight = mapString(item.ruploadParams, "upload_media_height")
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
			if item.form.Get("upload_media_width") != uploadWidth || item.form.Get("upload_media_height") != uploadHeight {
				return flowCapture{}, errors.New("configure dimensions do not match the primary rupload parameters")
			}
			configuredID = item.configuredID
		case "exact_media_delete":
			deletedID = item.deletedID
			out.DeleteMediaType = item.deleteType
		case "delete_confirmation":
			confirmedID = item.confirmedID
		}
		out.Surfaces = append(out.Surfaces, item.surface)
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
		if isPotentialPublishingRequest(entry.Request.Method, u) {
			return inspectedPublishingEntry{}, false, fmt.Errorf(
				"unrecognized publishing request %s %s://%s%s; refusing to claim a complete contract",
				strings.ToUpper(entry.Request.Method), u.Scheme, u.Host, u.EscapedPath(),
			)
		}
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
		decoder := json.NewDecoder(strings.NewReader(string(responseRaw)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			if stage != "delete_confirmation" {
				return inspectedPublishingEntry{}, false, fmt.Errorf("%s response JSON: %w", stage, err)
			}
		} else if err := requireJSONEOF(decoder); err != nil {
			return inspectedPublishingEntry{}, false, fmt.Errorf("%s response JSON: %w", stage, err)
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
	if err := validateMobileIdentity(item); err != nil {
		return err
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
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"path media_id=<configured_media_id>", "response media unavailable=true")
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
		decoder := json.NewDecoder(strings.NewReader(item.headers["x-instagram-rupload-params"]))
		decoder.UseNumber()
		if err := decoder.Decode(&item.ruploadParams); err != nil {
			return fmt.Errorf("decode X-Instagram-Rupload-Params: %w", err)
		}
		if err := requireJSONEOF(decoder); err != nil {
			return fmt.Errorf("decode X-Instagram-Rupload-Params: %w", err)
		}
		item.surface.RuploadParamFields = sortedMapKeys(item.ruploadParams)
		if err := requireNames(item.surface.RuploadParamFields, []string{
			"media_type", "upload_id", "upload_media_height", "upload_media_width", "xsharing_user_ids",
		}); err != nil {
			return fmt.Errorf("X-Instagram-Rupload-Params: %w", err)
		}
		if stage == "video_upload" {
			if err := requireNames(item.surface.RuploadParamFields, []string{"upload_media_duration_ms"}); err != nil {
				return fmt.Errorf("X-Instagram-Rupload-Params: %w", err)
			}
		}
		item.uploadID = mapString(item.ruploadParams, "upload_id")
		if item.uploadID == "" {
			return errors.New("rupload params have no upload_id")
		}
		item.clientContext = strings.TrimSpace(item.headers["x_fb_photo_waterfall_id"])
		if item.clientContext == "" {
			return errors.New("X_FB_PHOTO_WATERFALL_ID is empty")
		}
		wantMediaType := "1"
		wantEntityType := "image/jpeg"
		if stage == "video_upload" {
			wantMediaType = "2"
			wantEntityType = "video/mp4"
		}
		if got := mapString(item.ruploadParams, "media_type"); got != wantMediaType {
			return fmt.Errorf("rupload media_type=%q, want %q", got, wantMediaType)
		}
		if got := mapString(item.ruploadParams, "xsharing_user_ids"); got != "[]" {
			return fmt.Errorf("rupload xsharing_user_ids=%q, want []", got)
		}
		if !positiveMapInteger(item.ruploadParams, "upload_media_width") ||
			!positiveMapInteger(item.ruploadParams, "upload_media_height") {
			return errors.New("rupload dimensions must be positive integers")
		}
		if stage == "video_upload" && !positiveMapInteger(item.ruploadParams, "upload_media_duration_ms") {
			return errors.New("rupload video duration must be a positive integer")
		}
		if got := strings.ToLower(strings.TrimSpace(item.headers["x-entity-type"])); got != wantEntityType {
			return fmt.Errorf("X-Entity-Type=%q, want %q", got, wantEntityType)
		}
		if strings.TrimSpace(item.headers["offset"]) != "0" {
			return errors.New("Offset must be 0 for the captured single-request upload")
		}
		if !positiveInteger(item.headers["x-entity-length"]) {
			return errors.New("X-Entity-Length must be a positive integer")
		}
		wantEntityName := item.uploadID
		entityPlaceholder := "<upload_id>"
		if stage == "thumbnail_upload" {
			wantEntityName += "_0"
			entityPlaceholder += "_0"
		}
		if strings.TrimSpace(item.headers["x-entity-name"]) != wantEntityName {
			return errors.New("X-Entity-Name is not correlated with the captured upload_id")
		}
		if ack := mapString(item.response, "upload_id"); ack != "" && ack != item.uploadID {
			return errors.New("rupload response upload_id does not match request")
		}
		if !strings.HasSuffix(strings.TrimRight(item.url.Path, "/"), "/"+wantEntityName) {
			return errors.New("rupload entity path is not correlated with X-Entity-Name")
		}
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"header Offset=0", "header X-Entity-Type="+wantEntityType,
			"header X-Entity-Name="+entityPlaceholder,
			"header X-Entity-Length=<media_byte_length>",
			"header X_FB_PHOTO_WATERFALL_ID=<client_context>",
			"rupload media_type="+wantMediaType, "rupload upload_id=<upload_id>",
			"rupload upload_media_width=<positive_pixels>",
			"rupload upload_media_height=<positive_pixels>",
			`rupload xsharing_user_ids="[]"`)
		if stage == "video_upload" {
			item.surface.ProtocolValues = append(item.surface.ProtocolValues,
				"rupload upload_media_duration_ms=<positive_milliseconds>")
		}
	case "upload_finish":
		if err := requireNames(item.surface.RequestFields,
			[]string{"media_type", "source_type", "upload_id", "video"}); err != nil {
			return err
		}
		wantMediaType := map[string]string{"reel": "clips", "story": "story"}[kind]
		if item.form.Get("source_type") != "4" || item.form.Get("video") != "1" ||
			item.form.Get("media_type") != wantMediaType {
			return fmt.Errorf("upload_finish values must be source_type=4, video=1, media_type=%s", wantMediaType)
		}
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"form upload_id=<upload_id>", "form source_type=4", "form video=1",
			"form media_type="+wantMediaType)
	case "upload_status":
		if err := requireNames(item.surface.RequestQueryFields, []string{"upload_id"}); err != nil {
			return err
		}
		item.processing = strings.ToLower(strings.TrimSpace(nestedString(item.response, "processing_info", "state")))
		if !pendingProcessingStates[item.processing] && !readyProcessingStates[item.processing] {
			return fmt.Errorf("uncaptured processing state %q", item.processing)
		}
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"query upload_id=<upload_id>", "response processing_info.state="+item.processing)
	case "photo_configure", "reel_configure", "story_configure":
		required := []string{
			"caption", "client_context", "device_id", "source_type", "upload_id",
			"upload_media_height", "upload_media_width",
		}
		if stage == "reel_configure" {
			required = append(required, "clips_audio_type", "clips_share_preview_to_feed", "poster_frame_index")
		}
		if stage == "story_configure" {
			required = append(required, "configure_mode", "story_media_creation_date")
		}
		if err := requireNames(item.surface.RequestFields, required); err != nil {
			return err
		}
		if item.form.Get("source_type") != "4" {
			return errors.New("configure source_type must be 4")
		}
		if !positiveInteger(item.form.Get("upload_media_width")) || !positiveInteger(item.form.Get("upload_media_height")) {
			return errors.New("configure dimensions must be positive integers")
		}
		if deviceID := item.form.Get("device_id"); !strings.HasPrefix(deviceID, "android-") || len(deviceID) == len("android-") {
			return errors.New("configure device_id must use the captured android-<viewer_id> shape")
		}
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"form upload_id=<upload_id>", "form caption=<redacted>",
			"form client_context=<client_context>", "form device_id=android-<viewer_id>",
			"form source_type=4", "form upload_media_width=<positive_pixels>",
			"form upload_media_height=<positive_pixels>")
		if stage == "reel_configure" {
			if item.form.Get("clips_share_preview_to_feed") != "1" ||
				item.form.Get("clips_audio_type") != "original" || item.form.Get("poster_frame_index") != "0" {
				return errors.New("Reel configure values must be clips_share_preview_to_feed=1, clips_audio_type=original, poster_frame_index=0")
			}
			item.surface.ProtocolValues = append(item.surface.ProtocolValues,
				"form clips_share_preview_to_feed=1", "form clips_audio_type=original",
				"form poster_frame_index=0")
		}
		if stage == "story_configure" {
			if item.form.Get("configure_mode") != "1" || !positiveInteger(item.form.Get("story_media_creation_date")) {
				return errors.New("Story configure values must be configure_mode=1 and a positive creation timestamp")
			}
			item.surface.ProtocolValues = append(item.surface.ProtocolValues,
				"form configure_mode=1", "form story_media_creation_date=<unix_seconds>")
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
		wantDeleteType := map[string]string{"photo": "PHOTO", "reel": "VIDEO", "story": "STORY"}[kind]
		if item.deleteType != wantDeleteType {
			return fmt.Errorf("delete media_type=%q, want %q", item.deleteType, wantDeleteType)
		}
		item.surface.ProtocolValues = append(item.surface.ProtocolValues,
			"path media_id=<configured_media_id>", "form media_type="+wantDeleteType)
	}
	return nil
}

func validateMobileIdentity(item *inspectedPublishingEntry) error {
	required := []string{
		"cookie", "user-agent", "x-csrftoken", "x-ig-app-id",
		"x-ig-capabilities", "x-ig-connection-type",
	}
	if err := requireNames(item.surface.RequestHeaders, required); err != nil {
		return fmt.Errorf("mobile identity headers: %w", err)
	}
	for _, name := range []string{"user-agent", "x-ig-app-id", "x-ig-capabilities", "x-ig-connection-type"} {
		if strings.TrimSpace(item.headers[name]) == "" {
			return fmt.Errorf("mobile identity header %s is empty", name)
		}
	}
	if strings.TrimSpace(item.headers["cookie"]) == "" || strings.TrimSpace(item.headers["x-csrftoken"]) == "" {
		return errors.New("capture lacks authenticated Cookie or X-CSRFToken values")
	}
	item.surface.ProtocolValues = append(item.surface.ProtocolValues,
		"header User-Agent="+strings.TrimSpace(item.headers["user-agent"]),
		"header X-IG-App-ID="+strings.TrimSpace(item.headers["x-ig-app-id"]),
		"header X-IG-Capabilities="+strings.TrimSpace(item.headers["x-ig-capabilities"]),
		"header X-IG-Connection-Type="+strings.TrimSpace(item.headers["x-ig-connection-type"]),
		"header Cookie=<redacted>", "header X-CSRFToken=<redacted>")
	return nil
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mapString(values map[string]any, key string) string {
	switch value := values[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func positiveMapInteger(values map[string]any, key string) bool {
	return positiveInteger(mapString(values, key))
}

func positiveInteger(value string) bool {
	number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return err == nil && number > 0
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

// isPotentialPublishingRequest identifies requests that could be part of the
// captured mutation protocol but are not understood by publishingStage. A HAR
// may contain unrelated reads and telemetry, so those remain ignorable. Upload,
// processing, and configure paths are always relevant; unknown mutations under
// media/clip/story surfaces are also relevant because silently dropping one
// could make an incomplete protocol look complete.
func isPotentialPublishingRequest(method string, u *url.URL) bool {
	if u == nil {
		return false
	}
	path := strings.ToLower(u.Path)
	for _, marker := range []string{"rupload", "upload", "configure", "processing", "publish"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	mutating := method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
	if !mutating {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "instagram.com" && !strings.HasSuffix(host, ".instagram.com") {
		return false
	}
	for _, prefix := range []string{
		"/api/v1/media/", "/api/v1/clips/", "/api/v1/story/", "/api/v1/stories/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func requestFormValues(entry harEntry) url.Values {
	values := url.Values{}
	for _, param := range entry.Request.PostData.Params {
		values.Add(param.Name, param.Value)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(entry.Request.PostData.MimeType, ";")[0]))
	if entry.Request.PostData.Text != "" && mediaType == "application/x-www-form-urlencoded" {
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
