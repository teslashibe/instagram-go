package meta

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestListPagesBearerIsolationAndBoundCursor(t *testing.T) {
	fixture, err := os.ReadFile("testdata/pages_response.json")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Header.Get("Authorization") != "Bearer token-secret" {
			t.Errorf("authorization=%q", req.Header.Get("Authorization"))
		}
		if req.Header.Get("Cookie") != "" || req.Header.Get("X-CSRFToken") != "" {
			t.Fatal("official request included private auth")
		}
		if requests == 2 && req.URL.Query().Get("after") != "graph-after-secret" {
			t.Errorf("after=%q", req.URL.Query().Get("after"))
		}
		return jsonResponse(req, http.StatusOK, string(fixture)), nil
	})
	first, err := c.ListPages(context.Background(), ListOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].InstagramBusinessAccount.ID != "ig-1" || first.NextCursor == "" || strings.Contains(first.NextCursor, "graph-after-secret") {
		t.Fatalf("page=%#v", first)
	}
	if _, err := c.ListPages(context.Background(), ListOptions{Limit: 1, Cursor: first.NextCursor}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAdAccounts(context.Background(), ListOptions{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-query cursor error=%v", err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d want 2", requests)
	}
}

func TestSelectInstagramBusinessAccountVerifiesLink(t *testing.T) {
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusOK, `{"id":"page-1","instagram_business_account":{"id":"ig-1","username":"business"}}`), nil
	})
	if _, err := c.SelectInstagramBusinessAccount(context.Background(), "page-1", "wrong"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error=%v", err)
	}
	account, err := c.SelectInstagramBusinessAccount(context.Background(), "page-1", "ig-1")
	if err != nil || account.Username != "business" || c.SelectedAccounts().InstagramAccountID != "ig-1" {
		t.Fatalf("account=%#v err=%v", account, err)
	}
}
