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
	Continuation     bool
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

	found := map[string][]capturedDirectEntry{}
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
			found[name] = append(found[name], captured)
		}
	}
	for _, name := range []string{"Inbox pagination", "Thread retrieval", "Thread creation", "Text broadcast"} {
		if _, ok := found[name]; !ok {
			return directReport{}, fmt.Errorf("missing successful %s contract", strings.ToLower(name))
		}
	}

	inbox, inboxContinuation, inboxThreads, err := matchDirectInboxPagination(found["Inbox pagination"])
	if err != nil {
		return directReport{}, err
	}
	if err := validateDirectViewer(inbox.body, viewerID); err != nil {
		return directReport{}, err
	}
	thread, threadContinuation, err := matchDirectThreadPagination(found["Thread retrieval"], inboxThreads)
	if err != nil {
		return directReport{}, err
	}
	create := found["Thread creation"][len(found["Thread creation"])-1]
	recipients := parseJSONStrings(create.form.Get("recipient_users"))
	if len(recipients) != 1 || recipients[0] != approvedRecipient {
		return directReport{}, errors.New("thread creation recipient is not the sole explicitly approved burner/self target")
	}
	createdThreadID, ok := nestedScalarString(create.body, "thread", "thread_id")
	if !ok || validateNumeric("created thread ID", createdThreadID) != nil {
		return directReport{}, errors.New("thread creation response contained no thread_id")
	}
	broadcast := found["Text broadcast"][len(found["Text broadcast"])-1]
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
	if len(broadcastThreads) != 1 || broadcastThreads[0] != createdThreadID {
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
	report.Surfaces = append(report.Surfaces,
		mergeDirectReadSurfaces(inbox.surface, inboxContinuation.surface),
		mergeDirectReadSurfaces(thread.surface, threadContinuation.surface),
		create.surface,
		broadcast.surface,
	)
	return report, nil
}

func matchDirectInboxPagination(entries []capturedDirectEntry) (capturedDirectEntry, capturedDirectEntry, []string, error) {
	var firstErr error
	for _, initial := range entries {
		if initial.form.Get("cursor") != "" {
			continue
		}
		threadIDs, err := validateDirectInboxContract(initial.body)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		cursor, _ := directResponseCursor(initial.body, "inbox")
		for _, continuation := range entries {
			if continuation.form.Get("cursor") != cursor {
				continue
			}
			if err := validateDirectContinuation(continuation.body, "inbox", "threads", ""); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return initial, continuation, threadIDs, nil
		}
		if firstErr == nil {
			firstErr = errors.New("inbox pagination did not include a continuation GET whose cursor matched the preceding inbox.oldest_cursor")
		}
	}
	if firstErr != nil {
		return capturedDirectEntry{}, capturedDirectEntry{}, nil, firstErr
	}
	return capturedDirectEntry{}, capturedDirectEntry{}, nil,
		errors.New("inbox pagination omitted an initial GET without a cursor")
}

// validateDirectViewer requires a captured inbox response to identify the
// declared authenticated viewer. The HAR verifier must not accept a syntactic
// viewer ID that has no evidence that it belongs to the captured session.
func validateDirectViewer(body any, viewerID string) error {
	viewer, ok := nestedObject(body, "viewer")
	if !ok {
		return errors.New("inbox response omitted viewer identity")
	}
	for _, key := range []string{"pk_id", "pk", "id"} {
		if id, ok := scalarString(viewer[key]); ok && id == viewerID {
			return nil
		}
	}
	return errors.New("inbox viewer identity did not match the declared viewer ID")
}

func matchDirectThreadPagination(entries []capturedDirectEntry, inboxThreadIDs []string) (capturedDirectEntry, capturedDirectEntry, error) {
	var firstErr error
	for _, initial := range entries {
		if initial.form.Get("cursor") != "" {
			continue
		}
		if !contains(inboxThreadIDs, initial.pathID) {
			if firstErr == nil {
				firstErr = errors.New("thread retrieval did not read a thread from inbox.threads[] in the authenticated viewer's inbox")
			}
			continue
		}
		if err := validateDirectThreadContract(initial.body, initial.pathID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		cursor, _ := directResponseCursor(initial.body, "thread")
		for _, continuation := range entries {
			if continuation.pathID != initial.pathID || continuation.form.Get("cursor") != cursor {
				continue
			}
			if err := validateDirectContinuation(continuation.body, "thread", "items", initial.pathID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return initial, continuation, nil
		}
		if firstErr == nil {
			firstErr = errors.New("thread pagination did not include a continuation GET whose cursor matched the preceding thread.oldest_cursor")
		}
	}
	if firstErr != nil {
		return capturedDirectEntry{}, capturedDirectEntry{}, firstErr
	}
	return capturedDirectEntry{}, capturedDirectEntry{},
		errors.New("thread retrieval omitted an initial GET without a cursor")
}

func validateDirectContinuation(body any, containerName, itemsName, threadID string) error {
	container, ok := nestedObject(body, containerName)
	if !ok {
		return fmt.Errorf("%s continuation response omitted %s object", containerName, containerName)
	}
	if _, ok := container[itemsName].([]any); !ok {
		return fmt.Errorf("%s continuation response omitted %s.%s array", containerName, containerName, itemsName)
	}
	if threadID != "" {
		responseThreadID, ok := scalarString(container["thread_id"])
		if !ok || responseThreadID != threadID {
			return errors.New("thread continuation response thread.thread_id did not match the requested thread")
		}
	}
	hasOlder, ok := container["has_older"].(bool)
	if !ok {
		return fmt.Errorf("%s continuation response omitted boolean %s.has_older", containerName, containerName)
	}
	if hasOlder {
		cursor, ok := scalarString(container["oldest_cursor"])
		if !ok || strings.TrimSpace(cursor) == "" {
			return fmt.Errorf("%s continuation response has_older without %s.oldest_cursor", containerName, containerName)
		}
	}
	return nil
}

func directResponseCursor(body any, containerName string) (string, bool) {
	container, ok := nestedObject(body, containerName)
	if !ok {
		return "", false
	}
	return scalarString(container["oldest_cursor"])
}

func mergeDirectReadSurfaces(initial, continuation directSurface) directSurface {
	initial.Continuation = true
	initial.RequestFields = uniqueSorted(append(initial.RequestFields, continuation.RequestFields...))
	initial.HeaderNames = uniqueSorted(append(initial.HeaderNames, continuation.HeaderNames...))
	initial.ResponseFields = uniqueSorted(append(initial.ResponseFields, continuation.ResponseFields...))
	initial.PaginationFields = uniqueSorted(append(initial.PaginationFields, continuation.PaginationFields...))
	return initial
}

// validateDirectInboxContract requires the captured inbox response to prove
// the exact collection and continuation shape used by GetDirectInbox. Thread
// ownership must be derived only from inbox.threads[].thread_id; a thread_id in
// an unrelated nested object is not evidence that the viewer owns that thread.
func validateDirectInboxContract(body any) ([]string, error) {
	inbox, ok := nestedObject(body, "inbox")
	if !ok {
		return nil, errors.New("inbox pagination response omitted inbox object")
	}
	threads, ok := inbox["threads"].([]any)
	if !ok || len(threads) == 0 {
		return nil, errors.New("inbox pagination response omitted non-empty inbox.threads array")
	}
	if err := validateCapturedPagination(inbox, "inbox"); err != nil {
		return nil, err
	}

	threadIDs := make([]string, 0, len(threads))
	for _, value := range threads {
		thread, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("inbox pagination response contained a non-object inbox.threads item")
		}
		threadID, ok := scalarString(thread["thread_id"])
		if !ok || validateNumeric("inbox thread ID", threadID) != nil {
			return nil, errors.New("inbox pagination response contained an inbox.threads item without a numeric thread_id")
		}
		threadIDs = append(threadIDs, threadID)
	}
	return uniqueSorted(threadIDs), nil
}

// validateDirectThreadContract requires both the selected thread identity and
// the item/pagination structures consumed by GetDirectThread. This prevents a
// generic status=ok envelope from being certified as a thread-read contract.
func validateDirectThreadContract(body any, pathThreadID string) error {
	if validateNumeric("thread retrieval path ID", pathThreadID) != nil {
		return errors.New("thread retrieval path contained no numeric thread ID")
	}
	thread, ok := nestedObject(body, "thread")
	if !ok {
		return errors.New("thread retrieval response omitted thread object")
	}
	responseThreadID, ok := scalarString(thread["thread_id"])
	if !ok || responseThreadID != pathThreadID {
		return errors.New("thread retrieval response thread.thread_id did not match the requested thread")
	}
	items, ok := thread["items"].([]any)
	if !ok || len(items) == 0 {
		return errors.New("thread retrieval response omitted non-empty thread.items array")
	}
	if err := validateCapturedPagination(thread, "thread"); err != nil {
		return err
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return errors.New("thread retrieval response contained a non-object thread.items item")
		}
		itemID, itemIDOK := scalarString(item["item_id"])
		userID, userIDOK := scalarString(item["user_id"])
		itemType, itemTypeOK := scalarString(item["item_type"])
		if !itemIDOK || validateNumeric("thread item ID", itemID) != nil ||
			!userIDOK || validateNumeric("thread item user ID", userID) != nil ||
			!itemTypeOK || strings.TrimSpace(itemType) == "" {
			return errors.New("thread retrieval response contained a thread.items item without item_id, user_id, or item_type")
		}
	}
	return nil
}

func validateCapturedPagination(container map[string]any, name string) error {
	hasOlder, ok := container["has_older"].(bool)
	if !ok {
		return fmt.Errorf("%s pagination response omitted boolean %s.has_older", name, name)
	}
	cursor, cursorOK := scalarString(container["oldest_cursor"])
	if !cursorOK || strings.TrimSpace(cursor) == "" {
		return fmt.Errorf("%s pagination response omitted non-empty %s.oldest_cursor", name, name)
	}
	if !hasOlder {
		return fmt.Errorf("%s pagination response did not prove continuation because %s.has_older was false", name, name)
	}
	return nil
}

func nestedObject(node any, path ...string) (map[string]any, bool) {
	current, ok := node.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range path {
		next, exists := current[key]
		if !exists {
			return nil, false
		}
		current, ok = next.(map[string]any)
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func scalarString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case json.Number:
		return typed.String(), typed.String() != ""
	default:
		return "", false
	}
}

func inspectDirectEntry(entry harEntry) (capturedDirectEntry, string, bool, error) {
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return capturedDirectEntry{}, "", false, nil
	}
	if u.Scheme != "https" || (u.Hostname() != "i.instagram.com" && u.Hostname() != "www.instagram.com") {
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
