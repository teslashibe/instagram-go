package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LoginParams configures a credential login via the social-login sidecar.
//
// Instagram gates login behind Bloks-encrypted, browser-only JavaScript that is
// impractical to reproduce in a pure-Go client. Rather than reimplement it, we
// delegate the interactive login to the headless-browser social-login sidecar
// (see sidecars/social-login), which drives the real web login and returns the
// session cookies. Those cookies are then used by the normal Client for all
// API calls.
type LoginParams struct {
	Username string
	Password string

	// SidecarURL is the base URL of the social-login sidecar (e.g.
	// "http://social-login:8090"). Required.
	SidecarURL string

	// ProxyURL, when set, is forwarded to the sidecar so the browser logs in
	// from a residential egress (Instagram challenges datacenter IPs).
	ProxyURL string

	// HTTPClient overrides the client used to talk to the sidecar. Optional.
	HTTPClient *http.Client
}

// LoginResult holds the session minted by a credential login.
type LoginResult struct {
	Cookies  Cookies
	FinalURL string
}

type sidecarLoginRequest struct {
	Platform string `json:"platform"`
	Username string `json:"username"`
	Password string `json:"password"`
	ProxyURL string `json:"proxyUrl,omitempty"`
}

type sidecarLoginResponse struct {
	OK       bool              `json:"ok"`
	FinalURL string            `json:"finalUrl"`
	Cookies  map[string]string `json:"cookies"`
	Hints    []string          `json:"hints"`
	Error    string            `json:"error"`
}

// Login performs a credential login through the social-login sidecar and
// returns the resulting session cookies. The caller passes the cookies to New
// to build an authenticated Client.
func Login(ctx context.Context, p LoginParams) (LoginResult, error) {
	if p.Username == "" || p.Password == "" {
		return LoginResult{}, fmt.Errorf("%w: username and password required", ErrInvalidAuth)
	}
	if p.SidecarURL == "" {
		return LoginResult{}, fmt.Errorf("%w: SidecarURL required for credential login", ErrInvalidAuth)
	}

	body, err := json.Marshal(sidecarLoginRequest{
		Platform: "instagram",
		Username: p.Username,
		Password: p.Password,
		ProxyURL: p.ProxyURL,
	})
	if err != nil {
		return LoginResult{}, err
	}

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}

	url := strings.TrimRight(p.SidecarURL, "/") + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return LoginResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return LoginResult{}, fmt.Errorf("social-login sidecar: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out sidecarLoginResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return LoginResult{}, fmt.Errorf("social-login sidecar: bad response (status %d): %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if !out.OK {
		detail := out.Error
		if detail == "" && len(out.Hints) > 0 {
			detail = strings.Join(out.Hints, "; ")
		}
		if detail == "" {
			detail = fmt.Sprintf("login failed (status %d)", resp.StatusCode)
		}
		return LoginResult{}, fmt.Errorf("%w: %s", ErrInvalidAuth, detail)
	}

	c := out.Cookies
	cookies := Cookies{
		SessionID: c["sessionid"],
		CSRFToken: c["csrftoken"],
		DSUserID:  c["ds_user_id"],
		Datr:      c["datr"],
		Mid:       c["mid"],
		IgDid:     c["ig_did"],
		Rur:       c["rur"],
		IgNrcb:    c["ig_nrcb"],
		PsL:       c["ps_l"],
		PsN:       c["ps_n"],
		Wd:        c["wd"],
	}
	if cookies.SessionID == "" || cookies.DSUserID == "" {
		return LoginResult{}, fmt.Errorf("%w: sidecar returned no session cookies", ErrInvalidAuth)
	}
	return LoginResult{Cookies: cookies, FinalURL: out.FinalURL}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
