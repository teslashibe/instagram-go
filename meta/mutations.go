package meta

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AdMutationConfirmationPhrase must be repeated exactly in every ad mutation
// confirmation in addition to the account, budget, currency, and dates.
const AdMutationConfirmationPhrase = "CONFIRM META AD SPEND"

type AdMutationConfirmation struct {
	Approved    bool   `json:"approved"`
	Phrase      string `json:"phrase"`
	AdAccountID string `json:"ad_account_id"`
	BudgetMinor int64  `json:"budget_minor"`
	Currency    string `json:"currency"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

type UpdateAdStatusRequest struct {
	AdID         string                 `json:"ad_id"`
	AdAccountID  string                 `json:"ad_account_id"`
	Status       string                 `json:"status"`
	BudgetMinor  int64                  `json:"budget_minor"`
	Currency     string                 `json:"currency"`
	StartDate    string                 `json:"start_date"`
	EndDate      string                 `json:"end_date"`
	Confirmation AdMutationConfirmation `json:"confirmation"`
}

type MutationResult struct {
	Success bool `json:"success"`
}

// UpdateAdStatus is intentionally not exposed by the initial MCP provider.
// It demonstrates and enforces the mandatory mutation boundary for SDK users.
func (c *Client) UpdateAdStatus(ctx context.Context, request UpdateAdStatusRequest) (*MutationResult, error) {
	if err := c.validateMutationConfirmation(ctx, request); err != nil {
		return nil, err
	}
	account, err := c.GetAdAccount(ctx, request.AdAccountID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(account.Currency, request.Currency) {
		return nil, fmt.Errorf("%w: confirmed currency %s does not match ad account currency %s", ErrConfirmationRequired, request.Currency, account.Currency)
	}
	form := url.Values{"status": {request.Status}}
	var result MutationResult
	if err := c.doJSON(ctx, http.MethodPost, request.AdID, nil, form, []Scope{ScopeAdsManagement}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) validateMutationConfirmation(ctx context.Context, request UpdateAdStatusRequest) error {
	if !c.allowMutations {
		return ErrMutationNotAllowed
	}
	if err := c.requireScopes(ctx, ScopeAdsManagement); err != nil {
		return err
	}
	if strings.TrimSpace(request.AdID) == "" || normalizeAdAccountID(request.AdAccountID) == "" {
		return fmt.Errorf("%w: ad_id and ad_account_id are required", ErrInvalidInput)
	}
	if request.Status != "ACTIVE" && request.Status != "PAUSED" {
		return fmt.Errorf("%w: status must be ACTIVE or PAUSED", ErrInvalidInput)
	}
	if request.BudgetMinor <= 0 || len(request.Currency) != 3 {
		return fmt.Errorf("%w: positive budget_minor and ISO currency are required", ErrConfirmationRequired)
	}
	start, startErr := time.Parse("2006-01-02", request.StartDate)
	end, endErr := time.Parse("2006-01-02", request.EndDate)
	if startErr != nil || endErr != nil || end.Before(start) {
		return fmt.Errorf("%w: valid start_date and end_date are required", ErrConfirmationRequired)
	}
	confirmation := request.Confirmation
	if !confirmation.Approved || confirmation.Phrase != AdMutationConfirmationPhrase ||
		normalizeAdAccountID(confirmation.AdAccountID) != normalizeAdAccountID(request.AdAccountID) ||
		confirmation.BudgetMinor != request.BudgetMinor || !strings.EqualFold(confirmation.Currency, request.Currency) ||
		confirmation.StartDate != request.StartDate || confirmation.EndDate != request.EndDate {
		return fmt.Errorf("%w: confirmation must exactly match account, budget, currency, and dates and use phrase %q", ErrConfirmationRequired, AdMutationConfirmationPhrase)
	}
	return nil
}
