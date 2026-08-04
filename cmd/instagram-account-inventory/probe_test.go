package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func probeResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func accountInventoryFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/account_current_user_response.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProbeCapturesOnlyAllowlistedReadContracts(t *testing.T) {
	secret := "session-secret-never-report"
	var requests int
	p := probe{
		baseURL: "https://i.instagram.com", cookies: cookieSet{"sessionid": secret, "csrftoken": "csrf-secret", "ds_user_id": accountFixtureIDForProbe},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet || req.URL.Path != accountInventoryPath || req.URL.Query().Get("edit") != "true" {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			}
			return probeResponse(req, http.StatusOK, accountInventoryFixture(t)), nil
		})}, now: func() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) },
	}
	report, err := p.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(report.Surfaces) != 3 {
		t.Fatalf("requests=%d surfaces=%d", requests, len(report.Surfaces))
	}
	rendered := renderReport(report)
	for _, forbidden := range []string{secret, "csrf-secret", "$.user.phone_number", "$.user.email"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, rendered)
		}
	}
}

const accountFixtureIDForProbe = "1234567890123456789"

func TestProbeFailsClosedOnMismatchChallengeAndMissingShape(t *testing.T) {
	tests := []struct{ name, body string }{
		{name: "mismatch", body: strings.Replace(accountInventoryFixture(t), accountFixtureIDForProbe, "999", 1)},
		{name: "challenge", body: `{"message":"challenge_required","status":"fail"}`},
		{name: "missing field", body: strings.Replace(accountInventoryFixture(t), `"biography": "reversible smoke-test profile",`, "", 1)},
		{name: "null field", body: strings.Replace(accountInventoryFixture(t), `"is_private": true`, `"is_private": null`, 1)},
		{name: "wrong field type", body: strings.Replace(accountInventoryFixture(t), `"account_type": 2`, `"account_type": false`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := probe{
				baseURL: "https://i.instagram.com", cookies: cookieSet{"sessionid": "s", "csrftoken": "c", "ds_user_id": accountFixtureIDForProbe},
				httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return probeResponse(req, http.StatusOK, tt.body), nil
				})},
			}
			if _, err := p.capture(context.Background()); err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}

func TestProbeRejectsUntrustedOriginBeforeSendingCookies(t *testing.T) {
	for _, baseURL := range []string{
		"http://i.instagram.com",
		"https://instagram.com",
		"https://i.instagram.com.evil.example",
		"https://evil.example",
	} {
		t.Run(baseURL, func(t *testing.T) {
			called := false
			p := probe{
				baseURL: baseURL,
				cookies: cookieSet{"sessionid": "s", "csrftoken": "c", "ds_user_id": accountFixtureIDForProbe},
				httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					called = true
					return nil, errors.New("transport must not be called")
				})},
			}
			if _, err := p.capture(context.Background()); err == nil {
				t.Fatal("expected untrusted origin rejection")
			}
			if called {
				t.Fatal("capture sent cookies to an untrusted origin")
			}
		})
	}
}

func TestWriteAtomicRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.md")
	if err := writeAtomic(path, "first"); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, "second"); err == nil {
		t.Fatal("expected overwrite rejection")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "first" {
		t.Fatalf("contents = %q", b)
	}
}
