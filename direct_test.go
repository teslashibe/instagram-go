package instagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type directRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn directRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func newDirectTestClient(t *testing.T, fn directRoundTripFunc, opts ...Option) *Client {
	t.Helper()
	options := []Option{
		WithHTTPClient(&http.Client{Transport: fn}),
		WithSkipSessionValidation(),
		WithMinRequestGap(0),
		WithMinWriteGap(0),
		WithRetry(1, time.Millisecond),
	}
	options = append(options, opts...)
	c, err := New(Cookies{SessionID: "session", CSRFToken: "csrf", DSUserID: "100000000000000000", IgDid: "device-id"}, options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func directResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), Request: req,
	}
}

func readDirectFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestGetDirectInboxParsesTypedThreadsAndResumesOpaqueCursor(t *testing.T) {
	fixture := readDirectFixture(t, "direct_inbox_response.json")
	var requests int
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/direct_v2/inbox/" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("X-IG-App-ID") != defaultAPIAppID {
			t.Fatalf("mobile app header = %q", req.Header.Get("X-IG-App-ID"))
		}
		if requests == 1 {
			if req.URL.Query().Get("cursor") != "" {
				t.Fatalf("first cursor = %q", req.URL.Query().Get("cursor"))
			}
			return directResponse(req, http.StatusOK, fixture), nil
		}
		if req.URL.Query().Get("cursor") != "<redacted-cursor>" {
			t.Fatalf("continuation cursor = %q", req.URL.Query().Get("cursor"))
		}
		return directResponse(req, http.StatusOK, `{"inbox":{"threads":[],"has_older":false},"status":"ok"}`), nil
	})

	first := c.GetDirectInbox().WithMaxPages(1)
	threads, err := first.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != "340282366841710300949128199900000000001" || len(threads[0].Users) != 1 {
		t.Fatalf("threads = %#v", threads)
	}
	if len(threads[0].Items) != 1 || threads[0].Items[0].Text != "<redacted>" {
		t.Fatalf("preview items = %#v", threads[0].Items)
	}
	cursor := first.Cursor()
	if cursor == "" || strings.Contains(cursor, "redacted-cursor") {
		t.Fatalf("cursor is not opaque: %q", cursor)
	}
	second, err := c.GetDirectInbox().WithCursor(cursor).WithMaxPages(1).Collect(context.Background())
	if err != nil || len(second) != 0 || requests != 2 {
		t.Fatalf("second=%#v requests=%d err=%v", second, requests, err)
	}
}

func TestGetDirectThreadParsesItemsAndRejectsCrossThreadCursorLocally(t *testing.T) {
	fixture := readDirectFixture(t, "direct_thread_response.json")
	var requests int
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return directResponse(req, http.StatusOK, fixture), nil
	})
	threadID := "340282366841710300949128199900000000001"
	it := c.GetDirectThread(threadID).WithMaxPages(1)
	items, err := it.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "340282366841710300949128199900000000003" || items[0].ThreadID != threadID {
		t.Fatalf("items = %#v", items)
	}
	cross := c.GetDirectThread("999").WithCursor(it.Cursor()).WithMaxPages(1)
	if cross.Next(context.Background()) || cross.Err() == nil || !strings.Contains(cross.Err().Error(), "cursor thread mismatch") {
		t.Fatalf("cross-thread error = %v", cross.Err())
	}
	if requests != 1 {
		t.Fatalf("cross-thread cursor made HTTP request; requests=%d", requests)
	}
}

func TestDirectPaginationRejectsMalformedAndIncompleteCursorsWithoutHTTP(t *testing.T) {
	badCursor := "not-base64"
	var requests int
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return directResponse(req, http.StatusOK, `{}`), nil
	})
	for _, cursor := range []string{badCursor, "e30"} {
		it := c.GetDirectInbox().WithCursor(cursor)
		if it.Next(context.Background()) || it.Err() == nil {
			t.Fatalf("cursor %q error = %v", cursor, it.Err())
		}
	}
	if requests != 0 {
		t.Fatalf("invalid cursors made %d HTTP requests", requests)
	}
}

func TestSendDirectTextUsesOneRecipientAndStableClientContextAcrossRetry(t *testing.T) {
	createFixture := readDirectFixture(t, "direct_create_thread_response.json")
	broadcastFixture := readDirectFixture(t, "direct_text_broadcast_response.json")
	var mu sync.Mutex
	var createBodies, broadcastBodies []string
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		payload, _ := io.ReadAll(req.Body)
		mu.Lock()
		defer mu.Unlock()
		switch req.URL.Path {
		case "/api/v1/direct_v2/create_group_thread/":
			createBodies = append(createBodies, string(payload))
			return directResponse(req, http.StatusOK, createFixture), nil
		case "/api/v1/direct_v2/threads/broadcast/text/":
			broadcastBodies = append(broadcastBodies, string(payload))
			if len(broadcastBodies) == 1 {
				return nil, errors.New("temporary transport failure")
			}
			return directResponse(req, http.StatusOK, broadcastFixture), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	}, WithRetry(2, time.Millisecond))

	result, err := c.SendDirectText(context.Background(), DirectTextRequest{
		RecipientID: "100000000000000001", Text: " hello burner ", ClientContext: "fixed-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ThreadID == "" || result.ItemID == "" || result.ClientContext != "fixed-context" {
		t.Fatalf("result = %#v", result)
	}
	if len(createBodies) != 1 || len(broadcastBodies) != 2 || broadcastBodies[0] != broadcastBodies[1] {
		t.Fatalf("create=%d broadcast=%#v", len(createBodies), broadcastBodies)
	}
	createForm, _ := url.ParseQuery(createBodies[0])
	if createForm.Get("recipient_users") != `["100000000000000001"]` {
		t.Fatalf("recipient_users = %q", createForm.Get("recipient_users"))
	}
	broadcastForm, _ := url.ParseQuery(broadcastBodies[0])
	if broadcastForm.Get("client_context") != "fixed-context" || broadcastForm.Get("mutation_token") != "fixed-context" || broadcastForm.Get("text") != " hello burner " {
		t.Fatalf("broadcast form = %v", broadcastForm)
	}
}

func TestSendDirectTextDoesNotRetryThreadCreation(t *testing.T) {
	var requests int
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("uncertain create outcome")
	}, WithRetry(3, time.Millisecond))
	_, err := c.SendDirectText(context.Background(), DirectTextRequest{RecipientID: "123", Text: "hello"})
	var sendErr *DirectSendError
	if !errors.As(err, &sendErr) || sendErr.ClientContext == "" {
		t.Fatalf("error = %#v", err)
	}
	if requests != 1 {
		t.Fatalf("thread creation requests = %d, want 1", requests)
	}
}

func TestSendDirectTextRejectsUnsafeInputBeforeHTTP(t *testing.T) {
	var requests int
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return directResponse(req, http.StatusOK, `{}`), nil
	})
	for _, in := range []DirectTextRequest{
		{RecipientID: "", Text: "hello"},
		{RecipientID: "not-numeric", Text: "hello"},
		{RecipientID: "123", Text: " \n\t "},
	} {
		if _, err := c.SendDirectText(context.Background(), in); err == nil {
			t.Fatalf("input %#v succeeded", in)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid input made %d HTTP requests", requests)
	}
}

func TestDirectWritePreservesChallengeRateAuthAndCSRFSentinels(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		want         error
		wantCooldown bool
	}{
		{name: "challenge", status: http.StatusOK, body: `{"message":"challenge_required","status":"fail"}`, want: ErrChallengeRequired},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"message":"Please wait a few minutes","status":"fail"}`, want: ErrRateLimited, wantCooldown: true},
		{name: "auth", status: http.StatusUnauthorized, body: `{"message":"login_required","status":"fail"}`, want: ErrInvalidAuth},
		{name: "csrf", status: http.StatusForbidden, body: `{"message":"CSRF token missing or incorrect","status":"fail"}`, want: ErrCSRF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
				return directResponse(req, tt.status, tt.body), nil
			}, WithRateLimitCooldown(time.Millisecond, time.Minute))
			_, err := c.SendDirectText(context.Background(), DirectTextRequest{RecipientID: "123", Text: "hello", ClientContext: "same-logical-send"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want errors.Is(%v)", err, tt.want)
			}
			state := c.RateLimit()
			if tt.wantCooldown && (!state.WriteBlocked || !state.CooldownWriteUntil.After(time.Now())) {
				t.Fatalf("write cooldown state=%#v", state)
			}
		})
	}
}

func TestDirectWriteHonorsCallerCancellationAndReturnsClientContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newDirectTestClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	_, err := c.SendDirectText(ctx, DirectTextRequest{RecipientID: "123", Text: "hello", ClientContext: "reconcile-this"})
	var sendErr *DirectSendError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &sendErr) || sendErr.ClientContext != "reconcile-this" {
		t.Fatalf("error=%#v", err)
	}
}
