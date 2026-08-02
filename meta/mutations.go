package meta

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

type mutationAd struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	AdSetID   string `json:"adset_id"`
}

type mutationAdSet struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	CampaignID     string `json:"campaign_id"`
	DailyBudget    string `json:"daily_budget"`
	LifetimeBudget string `json:"lifetime_budget"`
	StartTime      string `json:"start_time"`
	EndTime        string `json:"end_time"`
}

type mutationCampaign struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	DailyBudget    string `json:"daily_budget"`
	LifetimeBudget string `json:"lifetime_budget"`
	StartTime      string `json:"start_time"`
	StopTime       string `json:"stop_time"`
}

type mutationTarget struct {
	BudgetMinor int64
	Currency    string
	StartDate   string
	EndDate     string
}

// UpdateAdStatus is intentionally not exposed by the initial MCP provider.
// It demonstrates and enforces the mandatory mutation boundary for SDK users.
func (c *Client) UpdateAdStatus(ctx context.Context, request UpdateAdStatusRequest) (*MutationResult, error) {
	if err := c.validateMutationConfirmation(ctx, request); err != nil {
		return nil, err
	}
	target, err := c.resolveMutationTarget(ctx, request.AdID, request.AdAccountID)
	if err != nil {
		return nil, err
	}
	if request.BudgetMinor != target.BudgetMinor || !strings.EqualFold(request.Currency, target.Currency) ||
		request.StartDate != target.StartDate || request.EndDate != target.EndDate {
		return nil, fmt.Errorf("%w: confirmation does not match the target ad's effective budget, currency, and schedule", ErrConfirmationRequired)
	}
	form := url.Values{"status": {request.Status}}
	var result MutationResult
	if err := c.doJSON(ctx, http.MethodPost, request.AdID, nil, form, []Scope{ScopeAdsManagement}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) resolveMutationTarget(ctx context.Context, adID, requestedAccountID string) (mutationTarget, error) {
	requestedAccountID = normalizeAdAccountID(requestedAccountID)

	var ad mutationAd
	if err := c.doJSON(ctx, http.MethodGet, adID,
		url.Values{"fields": {"id,account_id,adset_id"}}, nil, []Scope{ScopeAdsRead}, &ad); err != nil {
		return mutationTarget{}, err
	}
	if ad.ID != adID || normalizeAdAccountID(ad.AccountID) == "" || normalizeAdAccountID(ad.AccountID) != requestedAccountID {
		return mutationTarget{}, fmt.Errorf("%w: target ad does not belong to confirmed ad account", ErrConfirmationRequired)
	}
	if strings.TrimSpace(ad.AdSetID) == "" {
		return mutationTarget{}, fmt.Errorf("%w: target ad has no resolvable ad set", ErrConfirmationRequired)
	}

	var adSet mutationAdSet
	if err := c.doJSON(ctx, http.MethodGet, ad.AdSetID,
		url.Values{"fields": {"id,account_id,campaign_id,daily_budget,lifetime_budget,start_time,end_time"}},
		nil, []Scope{ScopeAdsRead}, &adSet); err != nil {
		return mutationTarget{}, err
	}
	if adSet.ID != ad.AdSetID || normalizeAdAccountID(adSet.AccountID) == "" || normalizeAdAccountID(adSet.AccountID) != requestedAccountID {
		return mutationTarget{}, fmt.Errorf("%w: target ad set does not belong to confirmed ad account", ErrConfirmationRequired)
	}

	budget, hasBudget, err := effectiveBudget(adSet.DailyBudget, adSet.LifetimeBudget)
	if err != nil {
		return mutationTarget{}, err
	}
	startDate, startOK := graphDate(adSet.StartTime)
	endDate, endOK := graphDate(adSet.EndTime)

	if !hasBudget || !startOK || !endOK {
		if strings.TrimSpace(adSet.CampaignID) == "" {
			return mutationTarget{}, fmt.Errorf("%w: target ad has no resolvable campaign budget or schedule", ErrConfirmationRequired)
		}
		var campaign mutationCampaign
		if err := c.doJSON(ctx, http.MethodGet, adSet.CampaignID,
			url.Values{"fields": {"id,account_id,daily_budget,lifetime_budget,start_time,stop_time"}},
			nil, []Scope{ScopeAdsRead}, &campaign); err != nil {
			return mutationTarget{}, err
		}
		if campaign.ID != adSet.CampaignID || normalizeAdAccountID(campaign.AccountID) == "" || normalizeAdAccountID(campaign.AccountID) != requestedAccountID {
			return mutationTarget{}, fmt.Errorf("%w: target campaign does not belong to confirmed ad account", ErrConfirmationRequired)
		}
		if !hasBudget {
			budget, hasBudget, err = effectiveBudget(campaign.DailyBudget, campaign.LifetimeBudget)
			if err != nil {
				return mutationTarget{}, err
			}
		}
		if !startOK {
			startDate, startOK = graphDate(campaign.StartTime)
		}
		if !endOK {
			endDate, endOK = graphDate(campaign.StopTime)
		}
	}
	if !hasBudget || !startOK || !endOK {
		return mutationTarget{}, fmt.Errorf("%w: target ad's effective budget and schedule must be available", ErrConfirmationRequired)
	}

	account, err := c.GetAdAccount(ctx, requestedAccountID)
	if err != nil {
		return mutationTarget{}, err
	}
	if normalizeAdAccountID(firstNonEmpty(account.AccountID, account.ID)) != requestedAccountID || len(account.Currency) != 3 {
		return mutationTarget{}, fmt.Errorf("%w: confirmed ad account currency could not be verified", ErrConfirmationRequired)
	}
	return mutationTarget{
		BudgetMinor: budget,
		Currency:    strings.ToUpper(account.Currency),
		StartDate:   startDate,
		EndDate:     endDate,
	}, nil
}

func effectiveBudget(daily, lifetime string) (int64, bool, error) {
	values := make([]int64, 0, 2)
	for _, raw := range []string{daily, lifetime} {
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "0" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return 0, false, fmt.Errorf("%w: target ad budget is invalid", ErrConfirmationRequired)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return 0, false, nil
	}
	if len(values) > 1 {
		return 0, false, fmt.Errorf("%w: target ad has ambiguous daily and lifetime budgets", ErrConfirmationRequired)
	}
	return values[0], true, nil
}

func graphDate(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < len("2006-01-02") {
		return "", false
	}
	date := value[:len("2006-01-02")]
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", false
	}
	return date, true
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
