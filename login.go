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

	// VerificationCode is the email/SMS code for Instagram's login challenge
	// (interposed from unfamiliar IPs). When empty, VerificationProvider is
	// consulted after the challenge is detected.
	VerificationCode string

	// VerificationProvider, when set, is called to fetch the login challenge
	// code on demand (e.g. read from the user's connected Gmail). It is only
	// invoked if the sidecar reports a verification challenge and no
	// VerificationCode was pre-supplied.
	VerificationProvider func(ctx context.Context) (string, error)

	// HTTPClient overrides the client used to talk to the sidecar. Optional.
	HTTPClient *http.Client
}

// LoginResult holds the session minted by a credential login.
type LoginResult struct {
	Cookies  Cookies
	FinalURL string
}

type sidecarLoginRequest struct {
	Platform         string `json:"platform"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	VerificationCode string `json:"verificationCode,omitempty"`
	ProxyURL         string `json:"proxyUrl,omitempty"`
}

type sidecarLoginResponse struct {
	OK        bool              `json:"ok"`
	Challenge bool              `json:"challenge"`
	FinalURL  string            `json:"finalUrl"`
	Cookies   map[string]string `json:"cookies"`
	Hints     []string          `json:"hints"`
	Error     string            `json:"error"`
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

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	endpoint := strings.TrimRight(p.SidecarURL, "/") + "/login"

	out, err := callSidecar(ctx, httpClient, endpoint, sidecarLoginRequest{
		Platform:         "instagram",
		Username:         p.Username,
		Password:         p.Password,
		VerificationCode: p.VerificationCode,
		ProxyURL:         p.ProxyURL,
	})
	if err != nil {
		return LoginResult{}, err
	}

	// Instagram interposes an email/SMS code challenge from unfamiliar IPs. If
	// the sidecar reports one and we have a way to fetch the code, submit it.
	if !out.OK && isChallenge(out) && p.VerificationCode == "" && p.VerificationProvider != nil {
		code, perr := p.VerificationProvider(ctx)
		if perr != nil {
			return LoginResult{}, fmt.Errorf("instagram login challenge: fetching code: %w", perr)
		}
		if code != "" {
			out, err = callSidecar(ctx, httpClient, endpoint, sidecarLoginRequest{
				Platform:         "instagram",
				Username:         p.Username,
				Password:         p.Password,
				VerificationCode: code,
				ProxyURL:         p.ProxyURL,
			})
			if err != nil {
				return LoginResult{}, err
			}
		}
	}

	if !out.OK {
		if isChallenge(out) {
			return LoginResult{}, fmt.Errorf("%w: Instagram requires an email/SMS verification code for this login", ErrChallengeRequired)
		}
		detail := out.Error
		if detail == "" && len(out.Hints) > 0 {
			detail = strings.Join(out.Hints, "; ")
		}
		if detail == "" {
			detail = "login failed"
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

// callSidecar performs one POST /login round-trip and decodes the response.
func callSidecar(ctx context.Context, httpClient *http.Client, endpoint string, body sidecarLoginRequest) (sidecarLoginResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return sidecarLoginResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return sidecarLoginResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return sidecarLoginResponse{}, fmt.Errorf("social-login sidecar: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out sidecarLoginResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return sidecarLoginResponse{}, fmt.Errorf("social-login sidecar: bad response (status %d): %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return out, nil
}

// isChallenge reports whether the sidecar response indicates a verification
// challenge (email/SMS code) rather than a hard auth failure.
func isChallenge(out sidecarLoginResponse) bool {
	if out.Challenge {
		return true
	}
	for _, h := range out.Hints {
		if h == "verification_code_required" {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
