package instagram_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

// fakeServer wraps an httptest.Server with helpers for swapping the response
// mid-test so we can simulate cooldown trips deterministically.
type fakeServer struct {
	srv  *httptest.Server
	hits atomic.Int32
	mode atomic.Int32 // 0=ok, 1=wait-a-few-minutes, 2=302-to-login
	body string
}

func newFakeServer() *fakeServer {
	f := &fakeServer{body: `{"user":{"pk":"123","username":"tester"}}`}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		w.Header().Set("x-ig-capacity-level", "2")
		w.Header().Set("x-ig-peak-time", "1")
		w.Header().Set("x-ig-peak-v2", "0")
		w.Header().Set("x-fb-connection-quality", "GOOD; q=0.5, rtt=42")
		w.Header().Set("x-ig-origin-region", "test-origin")
		w.Header().Set("x-ig-server-region", "test-server")
		w.Header().Set("x-ig-request-elapsed-time-ms", "33")
		switch f.mode.Load() {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, `{"message":"Please wait a few minutes before you try again.","status":"fail"}`)
		case 2:
			w.Header().Set("Location", "https://www.instagram.com/accounts/login/")
			w.WriteHeader(http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, f.body)
		}
	}))
	return f
}

// newFakeClient builds a Client whose HTTP traffic is redirected to the fake
// server by replacing the http.Transport with one that rewrites the URL host.
func newFakeClient(t *testing.T, srv *httptest.Server, opts ...instagram.Option) *instagram.Client {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&fakeDialer{addr: strings.TrimPrefix(srv.URL, "http://")}).DialContext
	hc := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &rewriteTransport{
			inner: tr,
			host:  strings.TrimPrefix(srv.URL, "http://"),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	all := []instagram.Option{
		instagram.WithHTTPClient(hc),
		instagram.WithSkipSessionValidation(),
		instagram.WithMinRequestGap(0),
		instagram.WithMinWriteGap(0),
		instagram.WithRetry(1, 0), // disable retries to keep call counts deterministic
	}
	all = append(all, opts...)
	c, err := instagram.New(instagram.Cookies{
		SessionID: "x", CSRFToken: "y", DSUserID: "z",
	}, all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

type rewriteTransport struct {
	inner http.RoundTripper
	host  string
}

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = r.host
	req.Host = r.host
	return r.inner.RoundTrip(req)
}

type fakeDialer struct{ addr string }

func (d *fakeDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var dial net.Dialer
	return dial.DialContext(ctx, network, d.addr)
}

func TestRateLimit_WaitFewMinutes_TripsCooldown(t *testing.T) {
	srv := newFakeServer()
	defer srv.srv.Close()

	cooldown := 250 * time.Millisecond
	c := newFakeClient(t, srv.srv, instagram.WithRateLimitCooldown(cooldown, cooldown))

	srv.mode.Store(1)
	ctx := context.Background()
	_, err := c.GetProfileByID(ctx, "123")
	if err == nil {
		t.Fatal("expected ErrRateLimited, got nil")
	}
	if !errors.Is(err, instagram.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	rs := c.RateLimit()
	if !rs.CooldownReadUntil.After(time.Now()) {
		t.Errorf("expected CooldownReadUntil in the future, got %v", rs.CooldownReadUntil)
	}
	if rs.BlockedReason == "" {
		t.Error("expected BlockedReason populated")
	}
	if rs.CapacityLevel != 2 {
		t.Errorf("expected CapacityLevel=2 from fake server, got %d", rs.CapacityLevel)
	}

	// Subsequent call should block until cooldown clears, then succeed.
	srv.mode.Store(0)
	startHits := srv.hits.Load()
	t0 := time.Now()
	if _, err := c.GetProfileByID(ctx, "123"); err != nil {
		t.Fatalf("post-cooldown call failed: %v", err)
	}
	elapsed := time.Since(t0)
	if elapsed < cooldown-50*time.Millisecond {
		t.Errorf("cooldown not honoured: waited %v, expected ~%v", elapsed, cooldown)
	}
	if srv.hits.Load() <= startHits {
		t.Error("expected post-cooldown call to actually hit the server")
	}
	t.Logf("PASS: cooldown blocked next call for ~%v then released", elapsed.Round(10*time.Millisecond))
}

func TestRateLimit_LoginRedirect_TripsCooldown_PostValidation(t *testing.T) {
	srv := newFakeServer()
	defer srv.srv.Close()

	cooldown := 200 * time.Millisecond
	c := newFakeClient(t, srv.srv,
		instagram.WithRateLimitCooldown(cooldown, cooldown),
	)

	// First, a healthy call to "validate" the session (well — the client was
	// built with WithSkipSessionValidation, so we manually validate by Me).
	ctx := context.Background()
	srv.mode.Store(0)
	if _, err := c.Me(ctx); err != nil {
		t.Fatalf("Me: %v", err)
	}

	// Now flip the server to 302→login. Because the session was validated,
	// this should trip cooldown (not session-expiry).
	srv.mode.Store(2)
	_, err := c.GetProfileByID(ctx, "123")
	if err == nil {
		t.Fatal("expected error on 302→login")
	}
	if !errors.Is(err, instagram.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited (validated session 302), got %v", err)
	}
	if errors.Is(err, instagram.ErrSessionExpired) {
		t.Fatal("expected ErrRateLimited not ErrSessionExpired after successful validation")
	}
	rs := c.RateLimit()
	if !rs.CooldownReadUntil.After(time.Now()) {
		t.Error("CooldownReadUntil not set after 302→login on validated session")
	}
	if rs.BlockedReason == "" || !strings.Contains(rs.BlockedReason, "302") {
		t.Errorf("expected BlockedReason to mention 302, got %q", rs.BlockedReason)
	}
	t.Logf("PASS: 302→login on validated session classified as ErrRateLimited (reason=%q)", rs.BlockedReason)
}

func TestRateLimit_LoginRedirect_PreValidation_IsSessionExpiry(t *testing.T) {
	srv := newFakeServer()
	defer srv.srv.Close()

	c := newFakeClient(t, srv.srv)

	srv.mode.Store(2)
	_, err := c.GetProfileByID(context.Background(), "123")
	if err == nil {
		t.Fatal("expected error on 302→login")
	}
	if !errors.Is(err, instagram.ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired (no prior validation), got %v", err)
	}
	t.Log("PASS: 302→login pre-validation is classified as session expiry")
}

func TestRateLimit_WaitForCooldown(t *testing.T) {
	srv := newFakeServer()
	defer srv.srv.Close()

	cooldown := 150 * time.Millisecond
	c := newFakeClient(t, srv.srv, instagram.WithRateLimitCooldown(cooldown, cooldown))

	srv.mode.Store(1)
	if _, err := c.GetProfileByID(context.Background(), "123"); err == nil {
		t.Fatal("expected rate limit error")
	}

	t0 := time.Now()
	if err := c.WaitForCooldown(context.Background()); err != nil {
		t.Fatalf("WaitForCooldown: %v", err)
	}
	elapsed := time.Since(t0)
	if elapsed < cooldown-50*time.Millisecond {
		t.Errorf("WaitForCooldown returned too early: %v < %v", elapsed, cooldown)
	}
	t.Logf("PASS: WaitForCooldown blocked for ~%v then released", elapsed.Round(10*time.Millisecond))
}
