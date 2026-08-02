package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.User, &fields); err != nil {
		return accountContract{}, fmt.Errorf("%w: parse current account: %v", ErrUnexpectedResponse, err)
	}
	id := stringifyID(fields["pk_id"], fields["pk"], fields["id"])
	if id == "" {
		return accountContract{}, fmt.Errorf("%w: current account response missing id", ErrUnexpectedResponse)
	}
	if err := c.requireAccountID(c.cookies.DSUserID, id); err != nil {
		return accountContract{}, err
	}
	username, err := requiredAccountString(fields, "username")
	if err != nil {
		return accountContract{}, err
	}
	fullName, err := requiredAccountString(fields, "full_name")
	if err != nil {
		return accountContract{}, err
	}
	biography, err := requiredAccountString(fields, "biography")
	if err != nil {
		return accountContract{}, err
	}
	externalURL, err := requiredAccountString(fields, "external_url")
	if err != nil {
		return accountContract{}, err
	}
	isPrivate, err := requiredAccountBool(fields, "is_private")
	if err != nil {
		return accountContract{}, err
	}
	isProfessional, err := requiredAccountBool(fields, "is_professional_account")
	if err != nil {
		return accountContract{}, err
	}
	isBusiness, err := requiredAccountBool(fields, "is_business")
	if err != nil {
		return accountContract{}, err
	}
	accountType, err := requiredAccountInt(fields, "account_type")
	if err != nil {
		return accountContract{}, err
	}
	categoryID, err := requiredAccountScalarString(fields, "category_id")
	if err != nil {
		return accountContract{}, err
	}
	categoryName, err := requiredAccountString(fields, "category_name")
	if err != nil {
		return accountContract{}, err
	}
	displayCategory, err := requiredAccountBool(fields, "should_show_category")
	if err != nil {
		return accountContract{}, err
	}
	return accountContract{
		ID: id, Username: username, FullName: fullName, Biography: biography,
		ExternalURL: externalURL, IsPrivate: isPrivate,
		IsProfessional: isProfessional, IsBusiness: isBusiness,
		AccountType: accountType, CategoryID: categoryID,
		CategoryName: categoryName, DisplayCategory: displayCategory,
	}, nil
}

func requiredAccountRaw(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("%w: current account field %q is missing or null", ErrUnexpectedResponse, name)
	}
	return raw, nil
}

func requiredAccountString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, err := requiredAccountRaw(fields, name)
	if err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: current account field %q must be a string", ErrUnexpectedResponse, name)
	}
	return value, nil
}

func requiredAccountBool(fields map[string]json.RawMessage, name string) (bool, error) {
	raw, err := requiredAccountRaw(fields, name)
	if err != nil {
		return false, err
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%w: current account field %q must be a boolean", ErrUnexpectedResponse, name)
	}
	return value, nil
}

func requiredAccountInt(fields map[string]json.RawMessage, name string) (int, error) {
	value, err := requiredAccountScalarString(fields, name)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: current account field %q must be an integer", ErrUnexpectedResponse, name)
	}
	return n, nil
}

// requiredAccountScalarString accepts the two shapes Instagram uses for
// identifier-like fields: a JSON string or an integer JSON number. It rejects
// floats, booleans, objects, arrays, missing fields, and null.
func requiredAccountScalarString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, err := requiredAccountRaw(fields, name)
	if err != nil {
		return "", err
	}
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", fmt.Errorf("%w: current account field %q must be a string or integer", ErrUnexpectedResponse, name)
		}
		return value, nil
	}
	value := strings.TrimSpace(string(raw))
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return "", fmt.Errorf("%w: current account field %q must be a string or integer", ErrUnexpectedResponse, name)
	}
	return value, nil
}

// UpdateProfileFields changes only full name, biography, and external URL.
func (c *Client) UpdateProfileFields(ctx context.Context, p UpdateProfileFieldsParams) (*AccountMutationResult[ProfileFields], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm, p.Before != nil, p.After != nil); err != nil {
		return nil, err
	}
	if *p.Before == *p.After {
		return nil, precondition("profile", "before and after values are identical")
	}
	c.accountMutationMu.Lock()
	defer c.accountMutationMu.Unlock()
	current, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, current.AccountID); err != nil {
		return nil, err
	}
	if current.Profile != *p.Before {
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
	if verified.Profile != *p.After {
		return nil, precondition("profile", "post-write state did not match after value")
	}
	return &AccountMutationResult[ProfileFields]{AccountID: verified.AccountID, Before: *p.Before, After: *p.After, Verified: true}, nil
}

// SetPrivacy changes only the account's public/private state.
func (c *Client) SetPrivacy(ctx context.Context, p SetPrivacyParams) (*AccountMutationResult[bool], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm, p.Before != nil, p.After != nil); err != nil {
		return nil, err
	}
	if *p.Before == *p.After {
		return nil, precondition("privacy", "before and after values are identical")
	}
	c.accountMutationMu.Lock()
	defer c.accountMutationMu.Unlock()
	current, err := c.GetAccountSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountID(p.ExpectedAccountID, current.AccountID); err != nil {
		return nil, err
	}
	if current.IsPrivate != *p.Before {
		return nil, precondition("privacy", "before value does not match current state")
	}
	path := "/api/v1/accounts/set_public/"
	if *p.After {
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
	if verified.IsPrivate != *p.After {
		return nil, precondition("privacy", "post-write state did not match after value")
	}
	return &AccountMutationResult[bool]{AccountID: verified.AccountID, Before: *p.Before, After: *p.After, Verified: true}, nil
}

// UpdateProfessionalSettings changes only category ID and category visibility
// on an existing professional account. It cannot convert account type.
func (c *Client) UpdateProfessionalSettings(ctx context.Context, p UpdateProfessionalSettingsParams) (*AccountMutationResult[ProfessionalSettings], error) {
	if err := validateMutationHeader(p.ExpectedAccountID, p.Confirm, p.Before != nil, p.After != nil); err != nil {
		return nil, err
	}
	if *p.Before == *p.After {
		return nil, precondition("professional_settings", "before and after values are identical")
	}
	c.accountMutationMu.Lock()
	defer c.accountMutationMu.Unlock()
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
	if current.Settings != *p.Before {
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
	if verified.Settings != *p.After {
		return nil, precondition("professional_settings", "post-write state did not match after value")
	}
	return &AccountMutationResult[ProfessionalSettings]{AccountID: verified.AccountID, Before: *p.Before, After: *p.After, Verified: true}, nil
}

func (c *Client) accountWrite(ctx context.Context, path string, form url.Values) error {
	return c.doJSON(ctx, http.MethodPost, path, nil, &requestOptions{
		Host: requestHostAPI, IsWrite: true, NoRetry: true, FormBody: form,
	}, nil)
}

func validateMutationHeader(expectedID string, confirmed, beforePresent, afterPresent bool) error {
	if expectedID == "" {
		return precondition("expected_account_id", "is required")
	}
	if !confirmed {
		return precondition("confirm", "explicit confirmation is required")
	}
	if !beforePresent {
		return precondition("before", "explicit before value is required")
	}
	if !afterPresent {
		return precondition("after", "explicit after value is required")
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
