package main

import (
	"context"
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

const maxDirectHARBytes = 64 << 20

type directReport struct {
	CapturedAt time.Time
	Surfaces   []directSurface
}

type directSurface struct {
	Name             string
	Method           string
	Host             string
	Path             string
	RequestFields    []string
	HeaderNames      []string
	ResponseFields   []string
	PaginationFields []string
}

type harDocument struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
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
			Text string `json:"text"`
		} `json:"content"`
	} `json:"response"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type capturedDirectEntry struct {
	surface directSurface
	pathID  string
	form    url.Values
	body    any
}

func inspectDirectHAR(ctx context.Context, path, viewerID, approvedRecipient, confirmedRecipient string, now func() time.Time) (directReport, error) {
	if err := validateNumeric("viewer ID", viewerID); err != nil {
		return directReport{}, err
	}
	if err := validateNumeric("approved recipient ID", approvedRecipient); err != nil {
		return directReport{}, err
	}
	if approvedRecipient != confirmedRecipient {
		return directReport{}, errors.New("approved and confirmed recipient IDs do not match")
	}
	file, err := os.Open(path)
	if err != nil {
		return directReport{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return directReport{}, err
	}
	if info.Size() > maxDirectHARBytes {
		return directReport{}, fmt.Errorf("HAR exceeds %d bytes", maxDirectHARBytes)
	}
	select {
	case <-ctx.Done():
		return directReport{}, ctx.Err()
	default:
	}
	var doc harDocument
	dec := json.NewDecoder(file)
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return directReport{}, fmt.Errorf("decode HAR: %w", err)
	}

	found := map[string]capturedDirectEntry{}
	for _, entry := range doc.Log.Entries {
		select {
		case <-ctx.Done():
			return directReport{}, ctx.Err()
		default:
		}
		captured, name, ok, err := inspectDirectEntry(entry)
		if err != nil {
			return directReport{}, err
		}
		if ok {
			found[name] = captured
		}
	}
	for _, name := range []string{"Inbox pagination", "Thread retrieval", "Thread creation", "Text broadcast"} {
		if _, ok := found[name]; !ok {
			return directReport{}, fmt.Errorf("missing successful %s contract", strings.ToLower(name))
		}
	}

	inboxThreads := collectStringValues(found["Inbox pagination"].body, "thread_id")
	threadID := found["Thread retrieval"].pathID
	if threadID == "" || !contains(inboxThreads, threadID) {
		return directReport{}, errors.New("thread retrieval did not read a thread from the authenticated viewer's inbox")
	}
	create := found["Thread creation"]
	recipients := parseJSONStrings(create.form.Get("recipient_users"))
	if len(recipients) != 1 || recipients[0] != approvedRecipient {
		return directReport{}, errors.New("thread creation recipient is not the sole explicitly approved burner/self target")
	}
	createdThreadIDs := collectStringValues(create.body, "thread_id")
	if len(createdThreadIDs) == 0 {
		return directReport{}, errors.New("thread creation response contained no thread_id")
	}
	broadcast := found["Text broadcast"]
	if strings.TrimSpace(broadcast.form.Get("text")) == "" {
		return directReport{}, errors.New("text broadcast contained empty text")
	}
	for _, required := range []string{"client_context", "mutation_token", "offline_threading_id"} {
		if strings.TrimSpace(broadcast.form.Get(required)) == "" {
			return directReport{}, fmt.Errorf("text broadcast omitted %s", required)
		}
	}
	if broadcast.form.Get("client_context") != broadcast.form.Get("mutation_token") {
		return directReport{}, errors.New("text broadcast client_context and mutation_token differ")
	}
	broadcastThreads := parseJSONStrings(broadcast.form.Get("thread_ids"))
	if len(broadcastThreads) != 1 || !contains(createdThreadIDs, broadcastThreads[0]) {
		return directReport{}, errors.New("text broadcast did not target the captured created thread")
	}
	broadcastItemID, ok := nestedScalarString(broadcast.body, "payload", "item_id")
	if !ok || validateNumeric("text broadcast item ID", broadcastItemID) != nil {
		return directReport{}, errors.New("text broadcast response contained no payload.item_id")
	}

	if now == nil {
		now = time.Now
	}
	report := directReport{CapturedAt: now().UTC()}
	for _, name := range []string{"Inbox pagination", "Thread retrieval", "Thread creation", "Text broadcast"} {
		report.Surfaces = append(report.Surfaces, found[name].surface)
	}
	return report, nil
}

func inspectDirectEntry(entry harEntry) (capturedDirectEntry, string, bool, error) {
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return capturedDirectEntry{}, "", false, nil
	}
	name, pathID := directSurfaceName(entry.Request.Method, u.Path)
	if name == "" {
		return capturedDirectEntry{}, "", false, nil
	}
	if entry.Response.Status < 200 || entry.Response.Status >= 300 {
		return capturedDirectEntry{}, "", false, fmt.Errorf("%s contract returned HTTP %d", strings.ToLower(name), entry.Response.Status)
	}
	var body any
	dec := json.NewDecoder(strings.NewReader(entry.Response.Content.Text))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		return capturedDirectEntry{}, "", false, fmt.Errorf("%s response is not JSON: %w", strings.ToLower(name), err)
	}
	status, ok := nestedScalarString(body, "status")
	if !ok || status != "ok" {
		return capturedDirectEntry{}, "", false, fmt.Errorf("%s response did not contain status=ok", strings.ToLower(name))
	}
	form := u.Query()
	requestFields := valueKeys(form)
	for _, param := range entry.Request.PostData.Params {
		form.Add(param.Name, param.Value)
	}
	if entry.Request.PostData.Text != "" {
		if parsed, err := url.ParseQuery(entry.Request.PostData.Text); err == nil {
			for key, values := range parsed {
				for _, value := range values {
					form.Add(key, value)
				}
			}
		}
	}
	requestFields = append(requestFields, valueKeys(form)...)
	requestFields = uniqueSorted(requestFields)
	headerNames := make([]string, 0, len(entry.Request.Headers))
	for _, header := range entry.Request.Headers {
		headerNames = append(headerNames, http.CanonicalHeaderKey(header.Name))
	}
	responseFields := collectFieldPaths(body, "$", nil)
	surface := directSurface{
		Name: name, Method: entry.Request.Method, Host: u.Scheme + "://" + u.Host,
		Path: redactDirectPath(u.Path), RequestFields: requestFields,
		HeaderNames: uniqueSorted(headerNames), ResponseFields: responseFields,
		PaginationFields: directPaginationFields(responseFields),
	}
	return capturedDirectEntry{surface: surface, pathID: pathID, form: form, body: body}, name, true, nil
}

func directSurfaceName(method, path string) (string, string) {
	switch {
	case method == http.MethodGet && path == "/api/v1/direct_v2/inbox/":
		return "Inbox pagination", ""
	case method == http.MethodPost && path == "/api/v1/direct_v2/create_group_thread/":
		return "Thread creation", ""
	case method == http.MethodPost && path == "/api/v1/direct_v2/threads/broadcast/text/":
		return "Text broadcast", ""
	case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/direct_v2/threads/"):
		rest := strings.TrimPrefix(path, "/api/v1/direct_v2/threads/")
		id := strings.Trim(rest, "/")
		if validateNumeric("thread ID", id) == nil {
			return "Thread retrieval", id
		}
	}
	return "", ""
}

func redactDirectPath(path string) string {
	if name, _ := directSurfaceName(http.MethodGet, path); name == "Thread retrieval" {
		return "/api/v1/direct_v2/threads/{thread_id}/"
	}
	return path
}

func collectFieldPaths(node any, path string, out []string) []string {
	switch value := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := path + "." + redactDirectResponseKey(key)
			out = append(out, childPath)
			out = collectFieldPaths(value[key], childPath, out)
		}
	case []any:
		arrayPath := path + "[]"
		out = append(out, arrayPath)
		if len(value) > 0 {
			out = collectFieldPaths(value[0], arrayPath, out)
		}
	}
	return uniqueSorted(out)
}

// redactDirectResponseKey prevents dynamic maps keyed by participant, thread,
// or item IDs from copying those private identifiers into the generated shape
// report. Static Direct response field names are retained.
func redactDirectResponseKey(key string) string {
	if validateNumeric("response object key", key) == nil {
		return "{numeric_key}"
	}
	return key
}

func nestedScalarString(node any, path ...string) (string, bool) {
	current := node
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[key]
		if !ok {
			return "", false
		}
	}
	switch value := current.(type) {
	case string:
		return value, value != ""
	case json.Number:
		return value.String(), value.String() != ""
	default:
		return "", false
	}
}

func collectStringValues(node any, key string) []string {
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for k, child := range typed {
				if k == key {
					switch scalar := child.(type) {
					case string:
						out = append(out, scalar)
					case json.Number:
						out = append(out, scalar.String())
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(node)
	return uniqueSorted(out)
}

func parseJSONStrings(value string) []string {
	var decoded any
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	if dec.Decode(&decoded) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			out = append(out, typed)
		case json.Number:
			out = append(out, typed.String())
		}
	}
	walk(decoded)
	return out
}

func directPaginationFields(paths []string) []string {
	var out []string
	for _, path := range paths {
		lower := strings.ToLower(path)
		if strings.Contains(lower, "cursor") || strings.HasSuffix(lower, ".has_older") || strings.HasSuffix(lower, ".has_more") {
			out = append(out, path)
		}
	}
	return uniqueSorted(out)
}

func valueKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateNumeric(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return fmt.Errorf("%s must be numeric", name)
		}
	}
	return nil
}

func writeDirectReport(path string, report directReport) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("destination already exists; refusing overwrite")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rendered := renderDirectReport(report)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".direct-inventory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(rendered); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
