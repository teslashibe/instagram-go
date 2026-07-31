package instagram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func genericAuthFailureTransport(status int, body string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
}

func TestGenericAuthStatusFailUsesAuthenticationSentinels(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "401 without message", status: http.StatusUnauthorized, body: `{"status":"fail"}`},
		{name: "403 with generic message", status: http.StatusForbidden, body: `{"message":"Something went wrong","status":"fail"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newHostTestClient(t, genericAuthFailureTransport(tt.status, tt.body))
			err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
				&requestOptions{Host: requestHostAPI}, nil)
			if !errors.Is(err, ErrInvalidAuth) {
				t.Fatalf("error = %v, want ErrInvalidAuth", err)
			}
		})
	}
}

func TestGenericAuthStatusFailPreservesValidatedSessionRateLimitHandling(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "without message", body: `{"status":"fail"}`},
		{name: "with generic message", body: `{"message":"Something went wrong","status":"fail"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newHostTestClient(t, genericAuthFailureTransport(http.StatusUnauthorized, tt.body))
			c.validatedMu.Lock()
			c.validated = true
			c.validatedMu.Unlock()

			err := c.doJSON(context.Background(), http.MethodGet, "/api/v1/fbsearch/top_serp/", nil,
				&requestOptions{Host: requestHostAPI}, nil)
			if !errors.Is(err, ErrRateLimited) {
				t.Fatalf("error = %v, want ErrRateLimited", err)
			}
			if errors.Is(err, ErrInvalidAuth) || errors.Is(err, ErrSessionExpired) {
				t.Fatalf("error = %v, must preserve validated-session rate-limit classification", err)
			}
		})
	}
}

func TestDiscoveryMethodsMapGenericAuthStatusFail(t *testing.T) {
	methods := []struct {
		name string
		call func(*Client) error
	}{
		{name: "keyword posts", call: func(c *Client) error {
			it := c.SearchKeywordPosts("coffee")
			it.Next(context.Background())
			return it.Err()
		}},
		{name: "accounts", call: func(c *Client) error {
			_, err := c.SearchAccounts(context.Background(), "coffee")
			return err
		}},
		{name: "typeahead", call: func(c *Client) error {
			_, err := c.SearchTypeaheadUsers(context.Background(), "coffee", 10)
			return err
		}},
	}
	failures := []struct {
		name   string
		status int
		body   string
	}{
		{name: "401 without message", status: http.StatusUnauthorized, body: `{"status":"fail"}`},
		{name: "403 with generic message", status: http.StatusForbidden, body: `{"message":"Something went wrong","status":"fail"}`},
	}
	for _, method := range methods {
		for _, failure := range failures {
			t.Run(method.name+"/"+failure.name, func(t *testing.T) {
				c := newHostTestClient(t, genericAuthFailureTransport(failure.status, failure.body))
				if err := method.call(c); !errors.Is(err, ErrInvalidAuth) {
					t.Fatalf("error = %v, want ErrInvalidAuth", err)
				}
			})
		}
	}
}
