package main

import (
	"context"
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
)

const (
	defaultInventoryAppID     = "567067343352427"
	defaultInventoryUserAgent = "Instagram 321.0.0.0.70 Android (33/13; 420dpi; 1080x2400; Google/google; Pixel 7; panther; panther; en_US; 502001959)"
	accountInventoryPath      = "/api/v1/accounts/current_user/"
)

type cookieSet map[string]string

type probe struct {
	baseURL    string
	cookies    cookieSet
	httpClient *http.Client
	userAgent  string
	appID      string
	now        func() time.Time
}

type inventoryReport struct {
	CapturedAt time.Time
	Host       string
	AccountID  string
	Surfaces   []readSurface
}

type readSurface struct {
	Name           string
	Method         string
	Path           string
	QueryNames     []string
	AccountIDPath  string
	ResponseFields []string
}

func (p probe) capture(ctx context.Context) (inventoryReport, error) {
	p.baseURL = strings.TrimRight(strings.TrimSpace(p.baseURL), "/")
	u, err := url.Parse(p.baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return inventoryReport{}, errors.New("host must be an absolute HTTP(S) origin")
	}
	if p.cookies["sessionid"] == "" || p.cookies["csrftoken"] == "" || p.cookies["ds_user_id"] == "" {
		return inventoryReport{}, errors.New("sessionid, csrftoken, and ds_user_id are required")
	}
	if p.httpClient == nil {
		p.httpClient = newHTTPClient(30 * time.Second)
	}
	if p.now == nil {
		p.now = time.Now
	}
	body, err := p.get(ctx, accountInventoryPath, url.Values{"edit": {"true"}})
	if err != nil {
		return inventoryReport{}, err
	}
	var envelope struct {
		User map[string]json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.User == nil {
		return inventoryReport{}, errors.New("current-account response is malformed or missing user")
	}
	id, idPath := responseAccountID(envelope.User)
	if id == "" {
		return inventoryReport{}, errors.New("current-account response is missing an account ID")
	}
	if id != p.cookies["ds_user_id"] {
		return inventoryReport{}, fmt.Errorf("account mismatch: expected %s, got %s", redactID(p.cookies["ds_user_id"]), redactID(id))
	}

	contracts := []struct {
		name   string
		fields []string
	}{
		{name: "Current account", fields: []string{"pk", "username", "full_name", "biography", "external_url", "is_private", "is_professional_account", "account_type"}},
		{name: "Account settings", fields: []string{"pk", "full_name", "biography", "external_url", "is_private"}},
		{name: "Professional-account state", fields: []string{"pk", "is_professional_account", "is_business", "account_type", "category_id", "category_name", "should_show_category"}},
	}
	report := inventoryReport{CapturedAt: p.now().UTC(), Host: p.baseURL, AccountID: id}
	for _, contract := range contracts {
		missing := missingFields(envelope.User, contract.fields)
		if len(missing) > 0 {
			return inventoryReport{}, fmt.Errorf("%s contract missing fields: %s", contract.name, strings.Join(missing, ", "))
		}
		fields := make([]string, len(contract.fields))
		for i, field := range contract.fields {
			fields[i] = "$.user." + field
		}
		report.Surfaces = append(report.Surfaces, readSurface{
			Name: contract.name, Method: http.MethodGet, Path: accountInventoryPath,
			QueryNames: []string{"edit"}, AccountIDPath: idPath, ResponseFields: fields,
		})
	}
	return report, nil
}

func (p probe) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("X-IG-App-ID", p.appID)
	req.Header.Set("X-IG-Capabilities", "3brTv10=")
	req.Header.Set("X-IG-Connection-Type", "WIFI")
	req.Header.Set("X-CSRFToken", p.cookies["csrftoken"])
	req.Header.Set("Cookie", cookieHeader(p.cookies))
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(string(body))
	location := strings.ToLower(resp.Header.Get("Location"))
	if strings.Contains(lower, "challenge_required") || strings.Contains(lower, "checkpoint") {
		return nil, errors.New("challenge required")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		strings.Contains(lower, "login_required") || (resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.Contains(location, "/accounts/login")) {
		return nil, errors.New("authentication rejected")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var status struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &status) == nil && status.Status == "fail" {
		return nil, fmt.Errorf("API status=fail: %s", status.Message)
	}
	return body, nil
}

func responseAccountID(user map[string]json.RawMessage) (string, string) {
	for _, key := range []string{"pk_id", "pk", "id"} {
		raw := user[key]
		if len(raw) == 0 {
			continue
		}
		var value string
		if raw[0] == '"' {
			_ = json.Unmarshal(raw, &value)
		} else {
			value = strings.TrimSpace(string(raw))
		}
		if value != "" && value != "0" && value != "null" {
			return value, "$.user." + key
		}
	}
	return "", ""
}

func missingFields(user map[string]json.RawMessage, required []string) []string {
	var missing []string
	for _, field := range required {
		if _, ok := user[field]; !ok {
			missing = append(missing, field)
		}
	}
	return missing
}

func loadCookieSet(getenv func(string) string) (cookieSet, error) {
	values := cookieSet{}
	if path := getenv("INSTAGRAM_COOKIES_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("decode cookie file: %w", err)
		}
	} else {
		for env, key := range map[string]string{
			"INSTAGRAM_SESSIONID": "sessionid", "INSTAGRAM_CSRFTOKEN": "csrftoken",
			"INSTAGRAM_DS_USER_ID": "ds_user_id", "INSTAGRAM_DATR": "datr",
			"INSTAGRAM_MID": "mid", "INSTAGRAM_IG_DID": "ig_did",
		} {
			if value := getenv(env); value != "" {
				values[key] = value
			}
		}
	}
	if values["sessionid"] == "" || values["csrftoken"] == "" || values["ds_user_id"] == "" {
		return nil, errors.New("set INSTAGRAM_COOKIES_FILE or the sessionid/csrftoken/ds_user_id environment variables")
	}
	return values, nil
}

func cookieHeader(cookies cookieSet) string {
	keys := make([]string, 0, len(cookies))
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+cookies[key])
	}
	return strings.Join(parts, "; ")
}

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func writeAtomic(path, contents string) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("destination already exists; refusing to overwrite")
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".account-inventory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func redactID(id string) string {
	if len(id) <= 4 {
		return "<redacted>"
	}
	return "…" + id[len(id)-4:]
}
