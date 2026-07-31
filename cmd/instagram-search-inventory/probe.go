package main

import (
	"context"
	"crypto/rand"
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
	"strings"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

const (
	defaultMobileAppID     = "567067343352427"
	defaultMobileUserAgent = "Instagram 321.0.0.0.70 Android (33/13; 420dpi; 1080x2400; Google/google; Pixel 7; panther; panther; en_US; 502001959)"
)

type cookieSet map[string]string

type probe struct {
	baseURL    string
	query      string
	cookies    cookieSet
	httpClient *http.Client
	userAgent  string
	appID      string
	now        func() time.Time
}

type report struct {
	CapturedAt    time.Time
	Host          string
	Query         string
	Authenticated bool
	REST          []surface
	GraphQL       []surface
	MediaCount    int
}

type surface struct {
	Name              string
	Method            string
	Host              string
	Path              string
	DocID             string
	FriendlyName      string
	RequestParamNames []string
	PaginationFields  []string
	ResponseFields    []string
	MediaPaths        []string
	SampleMedia       map[string]any
	StatusCode        int
}

type endpoint struct {
	name   string
	path   string
	params url.Values
}

func (p probe) capture(ctx context.Context, harPath string) (report, error) {
	if p.httpClient == nil {
		p.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if p.now == nil {
		p.now = time.Now
	}
	p.baseURL = strings.TrimRight(p.baseURL, "/")
	if p.baseURL == "" || p.query == "" || p.cookies["sessionid"] == "" {
		return report{}, errors.New("host, query, and sessionid are required")
	}

	if err := p.authenticate(ctx); err != nil {
		return report{}, err
	}

	rankToken, err := randomUUID()
	if err != nil {
		return report{}, fmt.Errorf("rank token: %w", err)
	}
	_, timezoneOffset := time.Now().Zone()
	tz := fmt.Sprintf("%d", timezoneOffset)
	endpoints := []endpoint{
		{name: "Top", path: "/api/v1/fbsearch/top_serp/", params: values(
			"search_surface", "top_serp", "timezone_offset", tz, "query", p.query, "rank_token", rankToken)},
		{name: "Reels", path: "/api/v1/fbsearch/reels_serp/", params: values(
			"search_surface", "clips_search_page", "timezone_offset", tz, "query", p.query)},
		{name: "Accounts", path: "/api/v1/fbsearch/account_serp/", params: values(
			"search_surface", "account_serp", "timezone_offset", tz, "query", p.query)},
		{name: "Keyword typeahead", path: "/api/v1/fbsearch/typeahead_stream/", params: values(
			"search_surface", "typeahead_search_page", "timezone_offset", tz, "query", p.query, "context", "blended", "count", "30")},
	}

	result := report{CapturedAt: p.now().UTC(), Host: p.baseURL, Query: p.query, Authenticated: true}
	for _, ep := range endpoints {
		body, status, err := p.get(ctx, ep.path, ep.params)
		if err != nil {
			return report{}, fmt.Errorf("%s surface: %w", ep.name, err)
		}
		s, count, err := inspectSurface(ep.name, http.MethodGet, p.baseURL, ep.path, "", "", ep.params, status, body)
		if err != nil {
			return report{}, fmt.Errorf("%s surface: %w", ep.name, err)
		}
		result.REST = append(result.REST, s)
		result.MediaCount += count
		if (ep.name == "Top" || ep.name == "Reels") && count == 0 {
			return report{}, fmt.Errorf("%s returned no media/post nodes; refusing to claim a complete inventory", ep.name)
		}
	}

	if harPath != "" {
		gql, err := inspectHAR(harPath, p.query)
		if err != nil {
			return report{}, fmt.Errorf("GraphQL HAR: %w", err)
		}
		if len(gql) == 0 {
			return report{}, errors.New("GraphQL HAR contained no search-related call with a doc_id; refusing a partial HAR capture")
		}
		result.GraphQL = gql
	}
	return result, nil
}

func (p probe) authenticate(ctx context.Context) error {
	// Browser-minted sessions often serve i.instagram.com fbsearch while
	// accounts/current_user returns status=fail ("something went wrong").
	// Validate with the real authenticated search surface so dead cookies still
	// fail before any inventory can be written.
	rankToken, err := randomUUID()
	if err != nil {
		return fmt.Errorf("burner session validation: %w", err)
	}
	_, timezoneOffset := time.Now().Zone()
	body, _, err := p.get(ctx, "/api/v1/fbsearch/top_serp/", values(
		"search_surface", "top_serp",
		"timezone_offset", fmt.Sprintf("%d", timezoneOffset),
		"query", p.query,
		"rank_token", rankToken,
	))
	if err != nil {
		return fmt.Errorf("burner session validation: top_serp: %w", err)
	}
	var decoded map[string]any
	if json.Unmarshal(body, &decoded) != nil {
		return errors.New("burner session validation: top_serp returned non-JSON (authentication rejected or challenged)")
	}
	if _, ok := decoded["media_grid"]; !ok && decoded["status"] == "fail" {
		return errors.New("burner session validation: top_serp status=fail")
	}
	return nil
}

func (p probe) get(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	u := p.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("X-IG-App-ID", p.appID)
	req.Header.Set("X-IG-Capabilities", "3brTv10=")
	req.Header.Set("X-IG-Connection-Type", "WIFI")
	if token := p.cookies["csrftoken"]; token != "" {
		req.Header.Set("X-CSRFToken", token)
	}
	req.Header.Set("Cookie", cookieHeader(p.cookies))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := "request rejected"
		lower := strings.ToLower(string(body))
		location := strings.ToLower(resp.Header.Get("Location"))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
			(resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.Contains(location, "login")) ||
			strings.Contains(lower, "login_required") || strings.Contains(lower, "challenge_required") ||
			strings.Contains(lower, "checkpoint") {
			kind = "authentication rejected or challenged"
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d (%s)", resp.StatusCode, kind)
	}
	var status struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &status) == nil && status.Status == "fail" {
		kind := "API status=fail"
		lower := strings.ToLower(status.Message)
		if strings.Contains(lower, "login") || strings.Contains(lower, "challenge") || strings.Contains(lower, "checkpoint") {
			kind = "authentication rejected or challenged"
		}
		return nil, resp.StatusCode, errors.New(kind)
	}
	return body, resp.StatusCode, nil
}

func inspectSurface(name, method, host, path, docID, friendly string, params url.Values, status int, body []byte) (surface, int, error) {
	var decoded any
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return surface{}, 0, fmt.Errorf("decode JSON: %w", err)
	}
	fields := collectFieldPaths(decoded)
	media := collectMedia(decoded)
	paramNames := make([]string, 0, len(params))
	for key := range params {
		paramNames = append(paramNames, key)
	}
	sort.Strings(paramNames)
	s := surface{
		Name:              name,
		Method:            method,
		Host:              host,
		Path:              path,
		DocID:             docID,
		FriendlyName:      friendly,
		RequestParamNames: paramNames,
		PaginationFields:  paginationFields(fields),
		ResponseFields:    fields,
		StatusCode:        status,
	}
	if len(media) > 0 {
		s.SampleMedia = media[0].sample
		pathSet := map[string]struct{}{}
		for _, item := range media {
			pathSet[item.path] = struct{}{}
		}
		for itemPath := range pathSet {
			s.MediaPaths = append(s.MediaPaths, itemPath)
		}
		sort.Strings(s.MediaPaths)
	}
	return s, len(media), nil
}

type mediaNode struct {
	path   string
	sample map[string]any
}

func collectMedia(v any) []mediaNode {
	var out []mediaNode
	var walk func(any, string)
	walk = func(value any, path string) {
		switch node := value.(type) {
		case map[string]any:
			_, hasPK := node["pk"]
			_, hasPKID := node["pk_id"]
			_, hasID := node["id"]
			_, hasMediaType := node["media_type"]
			_, hasCode := node["code"]
			if hasMediaType && hasCode && (hasPK || hasPKID || hasID) {
				out = append(out, mediaNode{path: path, sample: mediaSample(node)})
				return
			}
			for key, child := range node {
				walk(child, joinPath(path, key))
			}
		case []any:
			for _, child := range node {
				walk(child, path+"[]")
			}
		}
	}
	walk(v, "$")
	return out
}

func mediaSample(node map[string]any) map[string]any {
	// The composite media `id` commonly embeds the owner's account ID after an
	// underscore, so retain the media-only pk/pk_id instead.
	keys := []string{"pk", "pk_id", "code", "media_type", "product_type", "taken_at", "like_count", "comment_count", "view_count", "play_count", "original_width", "original_height"}
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := node[key]; ok && isScalar(value) {
			out[key] = value
		}
	}
	if caption, ok := node["caption"].(map[string]any); ok {
		if text, ok := caption["text"].(string); ok {
			out["caption_text_present"] = text != ""
		}
	}
	if user, ok := node["user"].(map[string]any); ok && len(user) > 0 {
		// Preserve structural evidence without retaining any owner account ID,
		// username, privacy state, or verification state.
		out["owner_present"] = true
	}
	return out
}

func collectFieldPaths(v any) []string {
	set := map[string]struct{}{}
	var walk func(any, string, int)
	walk = func(value any, path string, depth int) {
		if depth > 12 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				childPath := joinPath(path, key)
				set[childPath] = struct{}{}
				walk(child, childPath, depth+1)
			}
		case []any:
			arrayPath := path + "[]"
			set[arrayPath] = struct{}{}
			for _, child := range node {
				walk(child, arrayPath, depth+1)
			}
		}
	}
	walk(v, "$", 0)
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func paginationFields(paths []string) []string {
	names := []string{"next_max_id", "reels_max_id", "rank_token", "page_token", "next_page_token", "paging_token", "has_more", "more_available", "end_cursor", "has_next_page"}
	var out []string
	for _, path := range paths {
		for _, name := range names {
			if strings.HasSuffix(path, "."+name) {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, bool, string, json.Number, float64:
		return true
	default:
		return false
	}
}

func values(kv ...string) url.Values {
	result := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		result.Set(kv[i], kv[i+1])
	}
	return result
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func cookieHeader(cookies cookieSet) string {
	keys := make([]string, 0, len(cookies))
	for key, value := range cookies {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+cookies[key])
	}
	return strings.Join(pairs, "; ")
}

func loadCookies(ctx context.Context, getenv func(string) string) (cookieSet, error) {
	if path := getenv("INSTAGRAM_COOKIES_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read INSTAGRAM_COOKIES_FILE: %w", err)
		}
		return decodeCookies(raw)
	}
	if raw := getenv("INSTAGRAM_COOKIES_JSON"); raw != "" {
		return decodeCookies([]byte(raw))
	}
	if sessionID := getenv("INSTAGRAM_SESSIONID"); sessionID != "" {
		return cookieSet{
			"sessionid":  sessionID,
			"csrftoken":  getenv("INSTAGRAM_CSRFTOKEN"),
			"ds_user_id": getenv("INSTAGRAM_DS_USER_ID"),
			"datr":       getenv("INSTAGRAM_DATR"),
			"mid":        getenv("INSTAGRAM_MID"),
			"ig_did":     getenv("INSTAGRAM_IG_DID"),
			"rur":        getenv("INSTAGRAM_RUR"),
		}, nil
	}
	if getenv("INSTAGRAM_USERNAME") != "" || getenv("INSTAGRAM_PASSWORD") != "" || getenv("SOCIAL_LOGIN_SIDECAR_URL") != "" {
		login, err := instagram.Login(ctx, instagram.LoginParams{
			Username:   getenv("INSTAGRAM_USERNAME"),
			Password:   getenv("INSTAGRAM_PASSWORD"),
			SidecarURL: getenv("SOCIAL_LOGIN_SIDECAR_URL"),
			ProxyURL:   getenv("INSTAGRAM_PROXY_URL"),
		})
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(login.Cookies)
		if err != nil {
			return nil, err
		}
		return decodeCookies(raw)
	}
	return nil, errors.New("provide INSTAGRAM_SESSIONID, INSTAGRAM_COOKIES_FILE/JSON, or username/password plus SOCIAL_LOGIN_SIDECAR_URL")
}

func decodeCookies(raw []byte) (cookieSet, error) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err == nil {
		if nested, ok := object["cookies"].(map[string]any); ok {
			object = nested
		}
		out := cookieSet{}
		for key, value := range object {
			if text, ok := value.(string); ok {
				out[key] = text
			}
		}
		if out["sessionid"] != "" {
			return out, nil
		}
	}
	var list []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("cookie JSON must be an object, a {cookies:{...}} object, or a browser cookie array")
	}
	out := cookieSet{}
	for _, item := range list {
		out[item.Name] = item.Value
	}
	if out["sessionid"] == "" {
		return nil, errors.New("cookie JSON contains no sessionid")
	}
	return out, nil
}

func newHTTPClient(proxy string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, errors.New("invalid INSTAGRAM_PROXY_URL")
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
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
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		PostData struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Params   []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
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

func inspectHAR(path, query string) ([]surface, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var har harFile
	if err := json.Unmarshal(raw, &har); err != nil {
		return nil, fmt.Errorf("decode HAR: %w", err)
	}
	var out []surface
	seen := map[string]struct{}{}
	var rejected []string
	for _, entry := range har.Log.Entries {
		u, err := url.Parse(entry.Request.URL)
		if err != nil || !strings.Contains(u.Path, "graphql") {
			continue
		}
		params := u.Query()
		for _, item := range entry.Request.PostData.Params {
			params.Set(item.Name, item.Value)
		}
		if entry.Request.PostData.Text != "" && len(entry.Request.PostData.Params) == 0 {
			if parsed, err := url.ParseQuery(entry.Request.PostData.Text); err == nil {
				for key, vals := range parsed {
					for _, value := range vals {
						params.Add(key, value)
					}
				}
			} else {
				var body map[string]any
				if json.Unmarshal([]byte(entry.Request.PostData.Text), &body) == nil {
					for key, value := range body {
						if text, ok := value.(string); ok {
							params.Set(key, text)
						}
					}
				}
			}
		}
		friendly := headerValue(entry.Request.Headers, "x-fb-friendly-name")
		docID := params.Get("doc_id")
		needle := strings.ToLower(friendly + " " + params.Get("fb_api_req_friendly_name") + " " + params.Get("variables"))
		if docID == "" || (!strings.Contains(needle, "search") && !strings.Contains(needle, "serp") &&
			!strings.Contains(needle, "explore") && !strings.Contains(needle, "keyword") &&
			(query == "" || !strings.Contains(needle, strings.ToLower(query)))) {
			continue
		}
		if friendly == "" {
			friendly = params.Get("fb_api_req_friendly_name")
		}
		key := friendly + ":" + docID
		if _, ok := seen[key]; ok {
			continue
		}
		if entry.Response.Status < http.StatusOK || entry.Response.Status >= http.StatusMultipleChoices {
			rejected = append(rejected, fmt.Sprintf("%s (%s): HTTP %d", friendly, docID, entry.Response.Status))
			continue
		}
		responseBody := entry.Response.Content.Text
		if entry.Response.Content.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(responseBody)
			if err != nil {
				return nil, fmt.Errorf("decode base64 response for %s: %w", friendly, err)
			}
			responseBody = string(decoded)
		}
		requestParams := url.Values{}
		for key := range params {
			if key == "variables" {
				var vars map[string]any
				if json.Unmarshal([]byte(params.Get(key)), &vars) == nil {
					for varName := range vars {
						requestParams.Set("variables."+varName, "<captured>")
					}
				}
				continue
			}
			requestParams.Set(key, "<captured>")
		}
		responseRaw := []byte(responseBody)
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(responseRaw, &envelope); err != nil {
			return nil, fmt.Errorf("inspect %s (%s): decode GraphQL response: %w", friendly, docID, err)
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			rejected = append(rejected, fmt.Sprintf("%s (%s): response has no usable data", friendly, docID))
			continue
		}
		s, mediaCount, err := inspectSurface("GraphQL search", entry.Request.Method, u.Scheme+"://"+u.Host, u.Path, docID, friendly, requestParams, entry.Response.Status, responseRaw)
		if err != nil {
			return nil, fmt.Errorf("inspect %s (%s): %w", friendly, docID, err)
		}
		if mediaCount == 0 {
			rejected = append(rejected, fmt.Sprintf("%s (%s): response contains no media/post node", friendly, docID))
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 && len(rejected) > 0 {
		return nil, fmt.Errorf("no usable successful search media call: %s", strings.Join(rejected, "; "))
	}
	return out, nil
}

func headerValue(headers []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func writeAtomic(path string, r report) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("destination already exists (choose a new date-stamped path)")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content, err := renderReport(r)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".search-inventory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
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
	return os.Rename(tmpName, path)
}
