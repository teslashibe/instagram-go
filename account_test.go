package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const accountFixtureID = "1234567890123456789"

func accountValue[T any](value T) *T { return &value }

type accountRoundTripFunc func(*http.Request) (*http.Response, error)

func (f accountRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func accountResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), Request: req,
	}
}

func newAccountTestClient(t *testing.T, fn accountRoundTripFunc, opts ...Option) *Client {
	t.Helper()
	base := []Option{
		WithHTTPClient(&http.Client{Transport: fn}), WithSkipSessionValidation(),
		WithMinRequestGap(0), WithMinWriteGap(0), WithRetry(3, time.Millisecond),
	}
	base = append(base, opts...)
	c, err := New(Cookies{SessionID: "session", CSRFToken: "csrf", DSUserID: accountFixtureID}, base...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func readAccountFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mutateAccountFixtureField(t *testing.T, fixture, field string, value any, remove bool) string {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixture), &envelope); err != nil {
		t.Fatal(err)
	}
	var user map[string]json.RawMessage
	if err := json.Unmarshal(envelope["user"], &user); err != nil {
		t.Fatal(err)
	}
	if remove {
		delete(user, field)
	} else {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		user[field] = raw
	}
	rawUser, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	envelope["user"] = rawUser
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(rawEnvelope)
}

func TestAccountReadContracts(t *testing.T) {
	fixture := readAccountFixture(t, "account_current_user_response.json")
	var requests int
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet || req.URL.Host != "i.instagram.com" || req.URL.Path != currentAccountPath {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		if req.URL.Query().Get("edit") != "true" {
			t.Fatalf("edit query = %q", req.URL.Query().Get("edit"))
		}
		return accountResponse(req, http.StatusOK, fixture), nil
	})

	current, err := c.GetCurrentAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings, err := c.GetAccountSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	professional, err := c.GetProfessionalAccountState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 || current.AccountID != accountFixtureID || settings.Profile.Biography == "" ||
		!professional.IsProfessional || professional.Settings.CategoryID != "1001" {
		t.Fatalf("unexpected projections: current=%#v settings=%#v professional=%#v requests=%d", current, settings, professional, requests)
	}
	encoded, _ := json.Marshal([]any{current, settings, professional})
	for _, forbidden := range []string{"email", "phone", "password", "two_factor", "security"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("safe projections leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestAccountReadRejectsMismatchAndChallenge(t *testing.T) {
	t.Run("mismatch", func(t *testing.T) {
		body := strings.Replace(readAccountFixture(t, "account_settings_response.json"), accountFixtureID, "999", 1)
		c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
			return accountResponse(req, http.StatusOK, body), nil
		})
		_, err := c.GetAccountSettings(context.Background())
		var mismatch *AccountMismatchError
		if !errors.Is(err, ErrAccountMismatch) || !errors.As(err, &mismatch) || mismatch.ActualAccountID != "999" {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("challenge", func(t *testing.T) {
		c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
			return accountResponse(req, http.StatusOK, `{"message":"challenge_required","status":"fail"}`), nil
		})
		_, err := c.GetCurrentAccount(context.Background())
		if !errors.Is(err, ErrChallengeRequired) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestAccountReadRejectsMissingNullAndInvalidContractFields(t *testing.T) {
	fixture := readAccountFixture(t, "account_current_user_response.json")
	fields := []string{
		"username", "full_name", "biography", "external_url", "is_private",
	}
	for _, field := range fields {
		field := field
		t.Run(field+" missing", func(t *testing.T) {
			body := mutateAccountFixtureField(t, fixture, field, nil, true)
			c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
				return accountResponse(req, http.StatusOK, body), nil
			})
			if _, err := c.GetCurrentAccount(context.Background()); !errors.Is(err, ErrUnexpectedResponse) {
				t.Fatalf("error = %v, want ErrUnexpectedResponse", err)
			}
		})
		t.Run(field+" null", func(t *testing.T) {
			body := mutateAccountFixtureField(t, fixture, field, nil, false)
			c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
				return accountResponse(req, http.StatusOK, body), nil
			})
			if _, err := c.GetCurrentAccount(context.Background()); !errors.Is(err, ErrUnexpectedResponse) {
				t.Fatalf("error = %v, want ErrUnexpectedResponse", err)
			}
		})
	}

	invalid := []struct {
		field string
		value any
	}{
		{field: "biography", value: false},
		{field: "is_private", value: "false"},
		{field: "account_type", value: 2.5},
		{field: "category_id", value: map[string]any{"id": "1001"}},
	}
	for _, tt := range invalid {
		t.Run(tt.field+" wrong type", func(t *testing.T) {
			body := mutateAccountFixtureField(t, fixture, tt.field, tt.value, false)
			c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
				return accountResponse(req, http.StatusOK, body), nil
			})
			if _, err := c.GetCurrentAccount(context.Background()); !errors.Is(err, ErrUnexpectedResponse) {
				t.Fatalf("error = %v, want ErrUnexpectedResponse", err)
			}
		})
	}
}

func TestAccountMutationsRejectLocallyWithoutHTTP(t *testing.T) {
	requests := 0
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return accountResponse(req, http.StatusOK, `{}`), nil
	})
	profile := ProfileFields{FullName: "Name", Biography: "Bio", ExternalURL: "https://example.invalid"}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "missing confirmation", call: func() error {
			_, err := c.UpdateProfileFields(context.Background(), UpdateProfileFieldsParams{ExpectedAccountID: accountFixtureID, Before: accountValue(profile), After: accountValue(ProfileFields{FullName: "Other"})})
			return err
		}},
		{name: "missing account id", call: func() error {
			_, err := c.SetPrivacy(context.Background(), SetPrivacyParams{Before: accountValue(true), After: accountValue(false), Confirm: true})
			return err
		}},
		{name: "missing before", call: func() error {
			_, err := c.SetPrivacy(context.Background(), SetPrivacyParams{ExpectedAccountID: accountFixtureID, After: accountValue(false), Confirm: true})
			return err
		}},
		{name: "missing after", call: func() error {
			_, err := c.SetPrivacy(context.Background(), SetPrivacyParams{ExpectedAccountID: accountFixtureID, Before: accountValue(true), Confirm: true})
			return err
		}},
		{name: "profile no-op", call: func() error {
			_, err := c.UpdateProfileFields(context.Background(), UpdateProfileFieldsParams{ExpectedAccountID: accountFixtureID, Before: accountValue(profile), After: accountValue(profile), Confirm: true})
			return err
		}},
		{name: "privacy no-op", call: func() error {
			_, err := c.SetPrivacy(context.Background(), SetPrivacyParams{ExpectedAccountID: accountFixtureID, Before: accountValue(true), After: accountValue(true), Confirm: true})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrMutationPrecondition) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("local validation made %d requests", requests)
	}
}

func TestUpdateProfileFieldsUsesAllowlistAndVerifies(t *testing.T) {
	beforeBody := readAccountFixture(t, "account_settings_response.json")
	afterBody := strings.Replace(beforeBody, "reversible smoke-test profile", "changed biography", 1)
	var requests, writes int
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return accountResponse(req, http.StatusOK, beforeBody), nil
		case 2:
			writes++
			if req.Method != http.MethodPost || req.URL.Path != "/api/v1/accounts/edit_profile/" {
				t.Fatalf("write = %s %s", req.Method, req.URL.Path)
			}
			body, _ := io.ReadAll(req.Body)
			form := string(body)
			for _, want := range []string{"first_name=Burner+Account", "biography=changed+biography", "external_url="} {
				if !strings.Contains(form, want) {
					t.Errorf("form %q lacks %q", form, want)
				}
			}
			for _, forbidden := range []string{"email", "phone", "username", "password", "two_factor"} {
				if strings.Contains(form, forbidden) {
					t.Errorf("form leaked excluded field %q: %s", forbidden, form)
				}
			}
			return accountResponse(req, http.StatusOK, `{"status":"ok"}`), nil
		default:
			return accountResponse(req, http.StatusOK, afterBody), nil
		}
	})
	before := ProfileFields{FullName: "Burner Account", Biography: "reversible smoke-test profile", ExternalURL: "https://example.invalid/burner"}
	after := before
	after.Biography = "changed biography"
	result, err := c.UpdateProfileFields(context.Background(), UpdateProfileFieldsParams{
		ExpectedAccountID: accountFixtureID, Before: &before, After: &after, Confirm: true,
	})
	if err != nil || result == nil || !result.Verified || writes != 1 || requests != 3 {
		t.Fatalf("result=%#v err=%v requests=%d writes=%d", result, err, requests, writes)
	}
}

func TestAccountWritesAreSingleAttemptAndCooldownIsBounded(t *testing.T) {
	fixture := readAccountFixture(t, "account_settings_response.json")
	var writes int
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return accountResponse(req, http.StatusOK, fixture), nil
		}
		writes++
		resp := accountResponse(req, http.StatusTooManyRequests, `{"message":"rate_limit","status":"fail"}`)
		resp.Header.Set("Retry-After", "999999")
		return resp, nil
	})
	_, err := c.SetPrivacy(context.Background(), SetPrivacyParams{
		ExpectedAccountID: accountFixtureID, Before: accountValue(true), After: accountValue(false), Confirm: true,
	})
	if !errors.Is(err, ErrRateLimited) || writes != 1 {
		t.Fatalf("error=%v writes=%d", err, writes)
	}
	remaining := time.Until(c.RateLimit().CooldownWriteUntil)
	if remaining <= 0 || remaining > 30*time.Minute+time.Second {
		t.Fatalf("write cooldown = %v", remaining)
	}
}

func TestAccountWriteCooldownWaitIsContextBounded(t *testing.T) {
	requests := 0
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return accountResponse(req, http.StatusOK, `{"status":"ok"}`), nil
	})
	c.tripCooldown(true, time.Minute, "test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	err := c.accountWrite(ctx, "/api/v1/accounts/set_public/", nil)
	if !errors.Is(err, context.DeadlineExceeded) || requests != 0 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestProfessionalSettingsRequireExistingProfessionalAccount(t *testing.T) {
	body := strings.Replace(readAccountFixture(t, "professional_account_response.json"), `"is_professional_account": true`, `"is_professional_account": false`, 1)
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		return accountResponse(req, http.StatusOK, body), nil
	})
	_, err := c.UpdateProfessionalSettings(context.Background(), UpdateProfessionalSettingsParams{
		ExpectedAccountID: accountFixtureID,
		Before:            accountValue(ProfessionalSettings{CategoryID: "1001", DisplayCategory: true}),
		After:             accountValue(ProfessionalSettings{CategoryID: "1001", DisplayCategory: false}), Confirm: true,
	})
	if !errors.Is(err, ErrMutationPrecondition) {
		t.Fatalf("error = %v", err)
	}
}

func TestScopedPrivacyAndProfessionalWritesVerifyAllowlistedState(t *testing.T) {
	t.Run("privacy", func(t *testing.T) {
		beforeBody := readAccountFixture(t, "account_settings_response.json")
		afterBody := strings.Replace(beforeBody, `"is_private": true`, `"is_private": false`, 1)
		var request int
		c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
			request++
			switch request {
			case 1:
				return accountResponse(req, http.StatusOK, beforeBody), nil
			case 2:
				if req.Method != http.MethodPost || req.URL.Path != "/api/v1/accounts/set_public/" {
					t.Fatalf("write = %s %s", req.Method, req.URL.Path)
				}
				body, _ := io.ReadAll(req.Body)
				if len(body) != 0 {
					t.Fatalf("privacy write body = %q", body)
				}
				return accountResponse(req, http.StatusOK, `{"status":"ok"}`), nil
			default:
				return accountResponse(req, http.StatusOK, afterBody), nil
			}
		})
		result, err := c.SetPrivacy(context.Background(), SetPrivacyParams{
			ExpectedAccountID: accountFixtureID, Before: accountValue(true), After: accountValue(false), Confirm: true,
		})
		if err != nil || !result.Verified || request != 3 {
			t.Fatalf("result=%#v err=%v requests=%d", result, err, request)
		}
	})

	t.Run("professional display", func(t *testing.T) {
		beforeBody := readAccountFixture(t, "professional_account_response.json")
		afterBody := strings.Replace(beforeBody, `"should_show_category": true`, `"should_show_category": false`, 1)
		var request int
		c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
			request++
			switch request {
			case 1:
				return accountResponse(req, http.StatusOK, beforeBody), nil
			case 2:
				if req.Method != http.MethodPost || req.URL.Path != "/api/v1/business/account/edit/" {
					t.Fatalf("write = %s %s", req.Method, req.URL.Path)
				}
				body, _ := io.ReadAll(req.Body)
				form := string(body)
				if form != "category_id=1001&should_show_category=false" {
					t.Fatalf("professional form = %q", form)
				}
				return accountResponse(req, http.StatusOK, `{"status":"ok"}`), nil
			default:
				return accountResponse(req, http.StatusOK, afterBody), nil
			}
		})
		before := ProfessionalSettings{CategoryID: "1001", DisplayCategory: true}
		after := ProfessionalSettings{CategoryID: "1001", DisplayCategory: false}
		result, err := c.UpdateProfessionalSettings(context.Background(), UpdateProfessionalSettingsParams{
			ExpectedAccountID: accountFixtureID, Before: &before, After: &after, Confirm: true,
		})
		if err != nil || !result.Verified || request != 3 {
			t.Fatalf("result=%#v err=%v requests=%d", result, err, request)
		}
	})
}

func TestAccountMutationsSerializeCompleteTransactions(t *testing.T) {
	fixture := readAccountFixture(t, "account_settings_response.json")
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	requestDuringWrite := make(chan struct{}, 1)
	var mu sync.Mutex
	requestCount := 0
	private := true
	c := newAccountTestClient(t, func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requestCount++
		requestNumber := requestCount
		currentPrivate := private
		if req.Method == http.MethodPost {
			private = false
		}
		mu.Unlock()

		if requestNumber > 2 {
			select {
			case requestDuringWrite <- struct{}{}:
			default:
			}
		}
		if req.Method == http.MethodPost {
			close(writeStarted)
			<-releaseWrite
			return accountResponse(req, http.StatusOK, `{"status":"ok"}`), nil
		}
		body := fixture
		if !currentPrivate {
			body = strings.Replace(body, `"is_private": true`, `"is_private": false`, 1)
		}
		return accountResponse(req, http.StatusOK, body), nil
	})

	params := SetPrivacyParams{
		ExpectedAccountID: accountFixtureID, Before: accountValue(true), After: accountValue(false), Confirm: true,
	}
	results := make(chan error, 2)
	go func() {
		_, err := c.SetPrivacy(context.Background(), params)
		results <- err
	}()
	<-writeStarted
	go func() {
		_, err := c.SetPrivacy(context.Background(), params)
		results <- err
	}()

	select {
	case <-requestDuringWrite:
		t.Fatal("a second account mutation entered the transport before the first transaction completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseWrite)

	var succeeded, stale int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrMutationPrecondition):
			stale++
		default:
			t.Fatalf("unexpected mutation error: %v", err)
		}
	}
	mu.Lock()
	requests := requestCount
	mu.Unlock()
	if succeeded != 1 || stale != 1 || requests != 4 {
		t.Fatalf("succeeded=%d stale=%d requests=%d, want 1/1/4", succeeded, stale, requests)
	}
}
