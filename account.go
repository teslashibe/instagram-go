package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const currentAccountPath = "/api/v1/accounts/current_user/"

type accountEnvelope struct {
	User json.RawMessage `json:"user"`
}

type accountContract struct {
	ID              string
	Username        string
	FullName        string
	Biography       string
	ExternalURL     string
	IsPrivate       bool
	IsProfessional  bool
	IsBusiness      bool
	AccountType     int
	CategoryID      string
	CategoryName    string
	DisplayCategory bool
}

// GetCurrentAccount fetches the authenticated account's safe identity and
// editable profile projection.
func (c *Client) GetCurrentAccount(ctx context.Context) (*CurrentAccount, error) {
	state, err := c.readAccountContract(ctx)
	if err != nil {
		return nil, err
	}
	return &CurrentAccount{
		AccountID: state.ID, Username: state.Username,
		Profile:   ProfileFields{FullName: state.FullName, Biography: state.Biography, ExternalURL: state.ExternalURL},
		IsPrivate: state.IsPrivate, IsProfessional: state.IsProfessional, AccountType: state.AccountType,
	}, nil
}

// GetAccountSettings fetches only the reversible profile and privacy fields.
func (c *Client) GetAccountSettings(ctx context.Context) (*AccountSettings, error) {
	state, err := c.readAccountContract(ctx)
	if err != nil {
		return nil, err
	}
	return &AccountSettings{
		AccountID: state.ID,
		Profile:   ProfileFields{FullName: state.FullName, Biography: state.Biography, ExternalURL: state.ExternalURL},
		IsPrivate: state.IsPrivate,
	}, nil
}

// GetProfessionalAccountState fetches professional type and display settings.
func (c *Client) GetProfessionalAccountState(ctx context.Context) (*ProfessionalAccountState, error) {
	state, err := c.readAccountContract(ctx)
	if err != nil {
		return nil, err
	}
	return &ProfessionalAccountState{
		AccountID: state.ID, IsProfessional: state.IsProfessional, IsBusiness: state.IsBusiness,
		AccountType: state.AccountType, CategoryName: state.CategoryName,
		Settings: ProfessionalSettings{CategoryID: state.CategoryID, DisplayCategory: state.DisplayCategory},
	}, nil
}

func (c *Client) readAccountContract(ctx context.Context) (accountContract, error) {
	q := url.Values{"edit": {"true"}}
	var envelope accountEnvelope
	if err := c.doJSON(ctx, http.MethodGet, currentAccountPath, q, &requestOptions{Host: requestHostAPI}, &envelope); err != nil {
		return accountContract{}, err
	}
	if len(envelope.User) == 0 || string(envelope.User) == "null" {
		return accountContract{}, fmt.Errorf("%w: current account response missing user", ErrUnexpectedResponse)
	}
	var raw struct {
		PK                    json.RawMessage `json:"pk"`
		PKID                  json.RawMessage `json:"pk_id"`
		ID                    json.RawMessage `json:"id"`
		Username              string          `json:"username"`
		FullName              string          `json:"full_name"`
		Biography             string          `json:"biography"`
		ExternalURL           string          `json:"external_url"`
		IsPrivate             bool            `json:"is_private"`
		IsProfessionalAccount bool            `json:"is_professional_account"`
		IsBusiness            bool            `json:"is_business"`
		AccountType           any             `json:"account_type"`
		CategoryID            json.RawMessage `json:"category_id"`
		CategoryName          string          `json:"category_name"`
		ShouldShowCategory    bool            `json:"should_show_category"`
	}
	if err := json.Unmarshal(envelope.User, &raw); err != nil {
		return accountContract{}, fmt.Errorf("%w: parse current account: %v", ErrUnexpectedResponse, err)
	}
	id := stringifyID(raw.PKID, raw.PK, raw.ID)
	if id == "" {
		return accountContract{}, fmt.Errorf("%w: current account response missing id", ErrUnexpectedResponse)
	}
	if err := c.requireAccountID(c.cookies.DSUserID, id); err != nil {
		return accountContract{}, err
	}
	return accountContract{
		ID: id, Username: raw.Username, FullName: raw.FullName, Biography: raw.Biography,
		ExternalURL: raw.ExternalURL, IsPrivate: raw.IsPrivate,
		IsProfessional: raw.IsProfessionalAccount, IsBusiness: raw.IsBusiness,
		AccountType: anyToInt(raw.AccountType), CategoryID: stringifyID(raw.CategoryID),
		CategoryName: raw.CategoryName, DisplayCategory: raw.ShouldShowCategory,
	}, nil
}

// UpdateProfileFields changes only full name, biography, and external URL.
func (c *Client) UpdateProfileFields(ctx context.Context, p UpdateProfileFieldsParams) (*AccountMutationResult[ProfileFields], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm); err != nil {
		return nil, err
	}
	if p.Before == p.After {
		return nil, precondition("profile", "before and after values are identical")
	}
	current, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, current.AccountID); err != nil {
		return nil, err
	}
	if current.Profile != p.Before {
		return nil, precondition("profile", "before value does not match current state")
	}
	form := url.Values{
		"first_name":   {p.After.FullName},
		"biography":    {p.After.Biography},
		"external_url": {p.After.ExternalURL},
	}
	if err := c.accountWrite(ctx, "/api/v1/accounts/edit_profile/", form); err != nil {
		return nil, err
	}
	verified, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, verified.AccountID); err != nil {
		return nil, err
	}
	if verified.Profile != p.After {
		return nil, precondition("profile", "post-write state did not match after value")
	}
	return &AccountMutationResult[ProfileFields]{AccountID: verified.AccountID, Before: p.Before, After: p.After, Verified: true}, nil
}

// SetPrivacy changes only the account's public/private state.
func (c *Client) SetPrivacy(ctx context.Context, p SetPrivacyParams) (*AccountMutationResult[bool], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm); err != nil {
		return nil, err
	}
	if p.Before == p.After {
		return nil, precondition("privacy", "before and after values are identical")
	}
	current, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, current.AccountID); err != nil {
		return nil, err
	}
	if current.IsPrivate != p.Before {
		return nil, precondition("privacy", "before value does not match current state")
	}
	path := "/api/v1/accounts/set_public/"
	if p.After {
		path = "/api/v1/accounts/set_private/"
	}
	if err := c.accountWrite(ctx, path, url.Values{}); err != nil {
		return nil, err
	}
	verified, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, verified.AccountID); err != nil {
		return nil, err
	}
	if verified.IsPrivate != p.After {
		return nil, precondition("privacy", "post-write state did not match after value")
	}
	return &AccountMutationResult[bool]{AccountID: verified.AccountID, Before: p.Before, After: p.After, Verified: true}, nil
}

// UpdateProfessionalSettings changes only category ID and category visibility
// on an existing professional account. It cannot convert account type.
func (c *Client) UpdateProfessionalSettings(ctx context.Context, p UpdateProfessionalSettingsParams) (*AccountMutationResult[ProfessionalSettings], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm); err != nil {
		return nil, err
	}
	if p.Before == p.After {
		return nil, precondition("professional_settings", "before and after values are identical")
	}
	current, err := c.GetProfessionalAccountState(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, current.AccountID); err != nil {
		return nil, err
	}
	if !current.IsProfessional {
		return nil, precondition("professional_settings", "account is not already professional")
	}
	if current.Settings != p.Before {
		return nil, precondition("professional_settings", "before value does not match current state")
	}
	form := url.Values{
		"category_id":          {p.After.CategoryID},
		"should_show_category": {strconv.FormatBool(p.After.DisplayCategory)},
	}
	if err := c.accountWrite(ctx, "/api/v1/business/account/edit/", form); err != nil {
		return nil, err
	}
	verified, err := c.GetProfessionalAccountState(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, verified.AccountID); err != nil {
		return nil, err
	}
	if verified.Settings != p.After {
		return nil, precondition("professional_settings", "post-write state did not match after value")
	}
	return &AccountMutationResult[ProfessionalSettings]{AccountID: verified.AccountID, Before: p.Before, After: p.After, Verified: true}, nil
}

func (c *Client) accountWrite(ctx context.Context, path string, form url.Values) error {
	return c.doJSON(ctx, http.MethodPost, path, nil, &requestOptions{
		Host: requestHostAPI, IsWrite: true, NoRetry: true, FormBody: form,
	}, nil)
}

func validateMutationHeader(expectedID string, confirmed bool) error {
	if expectedID == "" {
		return precondition("expected_account_id", "is required")
	}
	if !confirmed {
		return precondition("confirm", "explicit confirmation is required")
	}
	return nil
}

func (c *Client) requireAccountID(expected, actual string) error {
	if expected == "" || actual == "" || expected != actual || c.cookies.DSUserID != actual {
		return &AccountMismatchError{ExpectedAccountID: expected, ActualAccountID: actual}
	}
	return nil
}

func precondition(field, reason string) error {
	return &MutationPreconditionError{Field: field, Reason: reason}
}
