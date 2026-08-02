package meta

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func validMutationRequest() UpdateAdStatusRequest {
	return UpdateAdStatusRequest{
		AdID: "ad-1", AdAccountID: "123", Status: "ACTIVE", BudgetMinor: 2500, Currency: "USD",
		StartDate: "2026-08-01", EndDate: "2026-08-31",
		Confirmation: AdMutationConfirmation{Approved: true, Phrase: AdMutationConfirmationPhrase, AdAccountID: "123",
			BudgetMinor: 2500, Currency: "USD", StartDate: "2026-08-01", EndDate: "2026-08-31"},
	}
}

func TestAdMutationsRequireSeparateEnablementAndExactConfirmation(t *testing.T) {
	requests := 0
	readOnly := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) { requests++; return jsonResponse(req, 200, `{}`), nil })
	if _, err := readOnly.UpdateAdStatus(context.Background(), validMutationRequest()); !errors.Is(err, ErrMutationNotAllowed) {
		t.Fatalf("error=%v", err)
	}

	scopes := append(allReadScopes(), ScopeAdsManagement)
	enabled := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) { requests++; return jsonResponse(req, 200, `{}`), nil })
	tests := []struct {
		name   string
		mutate func(*UpdateAdStatusRequest)
	}{
		{"not approved", func(r *UpdateAdStatusRequest) { r.Confirmation.Approved = false }},
		{"budget mismatch", func(r *UpdateAdStatusRequest) { r.Confirmation.BudgetMinor++ }},
		{"currency mismatch", func(r *UpdateAdStatusRequest) { r.Confirmation.Currency = "EUR" }},
		{"date mismatch", func(r *UpdateAdStatusRequest) { r.Confirmation.EndDate = "2026-09-01" }},
		{"phrase mismatch", func(r *UpdateAdStatusRequest) { r.Confirmation.Phrase = "yes" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validMutationRequest()
			tt.mutate(&r)
			if _, err := enabled.UpdateAdStatus(context.Background(), r); !errors.Is(err, ErrConfirmationRequired) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("failed guards made %d HTTP requests", requests)
	}
}

func TestAdMutationChecksAccountCurrencyBeforeWrite(t *testing.T) {
	scopes := append(allReadScopes(), ScopeAdsManagement)
	requests := 0
	c := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if req.Method != http.MethodGet || req.URL.Path != "/v23.0/act_123" {
				t.Fatalf("first request=%s %s", req.Method, req.URL)
			}
			return jsonResponse(req, 200, `{"id":"act_123","account_id":"123","currency":"USD"}`), nil
		}
		if req.Method != http.MethodPost || req.URL.Path != "/v23.0/ad-1" {
			t.Fatalf("write request=%s %s", req.Method, req.URL)
		}
		return jsonResponse(req, 200, `{"success":true}`), nil
	})
	result, err := c.UpdateAdStatus(context.Background(), validMutationRequest())
	if err != nil || !result.Success || requests != 2 {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, requests)
	}
}
