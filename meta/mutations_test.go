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

func TestAdMutationBindsOwnershipBudgetCurrencyAndScheduleBeforeWrite(t *testing.T) {
	scopes := append(allReadScopes(), ScopeAdsManagement)
	requests := 0
	c := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			if req.Method != http.MethodGet || req.URL.Path != "/v23.0/ad-1" {
				t.Fatalf("first request=%s %s", req.Method, req.URL)
			}
			return jsonResponse(req, 200, `{"id":"ad-1","account_id":"123","adset_id":"adset-1"}`), nil
		case 2:
			if req.Method != http.MethodGet || req.URL.Path != "/v23.0/adset-1" {
				t.Fatalf("second request=%s %s", req.Method, req.URL)
			}
			return jsonResponse(req, 200, `{"id":"adset-1","account_id":"123","campaign_id":"campaign-1","daily_budget":"2500","start_time":"2026-08-01T00:00:00-0700","end_time":"2026-08-31T23:59:59-0700"}`), nil
		case 3:
			if req.Method != http.MethodGet || req.URL.Path != "/v23.0/act_123" {
				t.Fatalf("third request=%s %s", req.Method, req.URL)
			}
			return jsonResponse(req, 200, `{"id":"act_123","account_id":"123","currency":"USD"}`), nil
		case 4:
			if req.Method != http.MethodPost || req.URL.Path != "/v23.0/ad-1" {
				t.Fatalf("write request=%s %s", req.Method, req.URL)
			}
			return jsonResponse(req, 200, `{"success":true}`), nil
		default:
			t.Fatalf("unexpected request %d: %s %s", requests, req.Method, req.URL)
		}
		return nil, nil
	})
	result, err := c.UpdateAdStatus(context.Background(), validMutationRequest())
	if err != nil || !result.Success || requests != 4 {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, requests)
	}
}

func TestAdMutationRejectsCrossAccountTargetBeforeWrite(t *testing.T) {
	scopes := append(allReadScopes(), ScopeAdsManagement)
	requests := 0
	c := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet || req.URL.Path != "/v23.0/ad-1" {
			t.Fatalf("request=%s %s", req.Method, req.URL)
		}
		return jsonResponse(req, 200, `{"id":"ad-1","account_id":"999","adset_id":"adset-1"}`), nil
	})
	if _, err := c.UpdateAdStatus(context.Background(), validMutationRequest()); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("ownership error=%v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestAdMutationRejectsActualBudgetOrScheduleMismatchBeforeWrite(t *testing.T) {
	tests := []struct {
		name     string
		adSet    string
		currency string
	}{
		{"budget", `{"id":"adset-1","account_id":"123","daily_budget":"3000","start_time":"2026-08-01","end_time":"2026-08-31"}`, "USD"},
		{"currency", `{"id":"adset-1","account_id":"123","daily_budget":"2500","start_time":"2026-08-01","end_time":"2026-08-31"}`, "EUR"},
		{"schedule", `{"id":"adset-1","account_id":"123","daily_budget":"2500","start_time":"2026-08-02","end_time":"2026-08-31"}`, "USD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := append(allReadScopes(), ScopeAdsManagement)
			requests := 0
			c := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) {
				requests++
				switch requests {
				case 1:
					return jsonResponse(req, 200, `{"id":"ad-1","account_id":"123","adset_id":"adset-1"}`), nil
				case 2:
					return jsonResponse(req, 200, tt.adSet), nil
				case 3:
					return jsonResponse(req, 200, `{"id":"act_123","account_id":"123","currency":"`+tt.currency+`"}`), nil
				default:
					t.Fatalf("mutation reached write: %s %s", req.Method, req.URL)
					return nil, nil
				}
			})
			if _, err := c.UpdateAdStatus(context.Background(), validMutationRequest()); !errors.Is(err, ErrConfirmationRequired) {
				t.Fatalf("actual target mismatch error=%v", err)
			}
			if requests != 3 {
				t.Fatalf("requests=%d", requests)
			}
		})
	}
}

func TestAdMutationUsesCampaignBudgetAndScheduleWhenAdSetInherits(t *testing.T) {
	scopes := append(allReadScopes(), ScopeAdsManagement)
	requests := 0
	c := testClient(t, scopes, true, func(req *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return jsonResponse(req, 200, `{"id":"ad-1","account_id":"123","adset_id":"adset-1"}`), nil
		case 2:
			return jsonResponse(req, 200, `{"id":"adset-1","account_id":"123","campaign_id":"campaign-1"}`), nil
		case 3:
			if req.URL.Path != "/v23.0/campaign-1" {
				t.Fatalf("campaign request=%s", req.URL)
			}
			return jsonResponse(req, 200, `{"id":"campaign-1","account_id":"123","lifetime_budget":"2500","start_time":"2026-08-01","stop_time":"2026-08-31"}`), nil
		case 4:
			return jsonResponse(req, 200, `{"id":"act_123","account_id":"123","currency":"USD"}`), nil
		case 5:
			return jsonResponse(req, 200, `{"success":true}`), nil
		default:
			t.Fatalf("unexpected request %d", requests)
		}
		return nil, nil
	})
	result, err := c.UpdateAdStatus(context.Background(), validMutationRequest())
	if err != nil || !result.Success || requests != 5 {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, requests)
	}
}
