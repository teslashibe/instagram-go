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

const (
	currentAccountPath = "/api/v1/accounts/current_user/"
	webFormDataPath    = "/api/v1/accounts/edit/web_form_data/"
)

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
	// Prefer the captured mobile current_user contract when it works (tests +
	// app-minted sessions). Browser-minted sessions fail that path and instead
	// succeed on www web_form_data + users/{id}/info/.
	state, err := c.readAccountContractFromCurrentUser(ctx)
	if err == nil {
		return state, nil
	}
	if !accountNeedsWebFormFallback(err) {
		return accountContract{}, err
	}
	return c.readAccountContractFromWebForm(ctx)
}

func (c *Client) readAccountContractFromCurrentUser(ctx context.Context) (accountContract, error) {
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
	return parseAccountContractFields(c, fields, true)
}

func (c *Client) readAccountContractFromWebForm(ctx context.Context) (accountContract, error) {
	var envelope struct {
		FormData json.RawMessage `json:"form_data"`
		Status   string          `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodGet, webFormDataPath, nil, &requestOptions{
		Referer: "https://www.instagram.com/accounts/edit/",
	}, &envelope); err != nil {
		return accountContract{}, err
	}
	if len(envelope.FormData) == 0 || string(envelope.FormData) == "null" {
		return accountContract{}, fmt.Errorf("%w: web form data response missing form_data", ErrUnexpectedResponse)
	}
	var form map[string]json.RawMessage
	if err := json.Unmarshal(envelope.FormData, &form); err != nil {
		return accountContract{}, fmt.Errorf("%w: parse web form data: %v", ErrUnexpectedResponse, err)
	}
	// web_form_data uses first_name instead of full_name and omits account id /
	// privacy. Normalize before the shared field parser.
	if _, ok := form["full_name"]; !ok {
		if first := form["first_name"]; len(first) > 0 {
			form["full_name"] = first
		}
	}
	for _, key := range []string{"biography", "external_url", "full_name", "username"} {
		if raw, ok := form[key]; !ok || len(raw) == 0 || string(raw) == "null" {
			form[key] = json.RawMessage(`""`)
		}
	}
	if _, ok := form["pk_id"]; !ok && c.cookies.DSUserID != "" {
		raw, _ := json.Marshal(c.cookies.DSUserID)
		form["pk_id"] = raw
	}
	me, err := c.GetProfileByID(ctx, c.cookies.DSUserID)
	if err != nil {
		return accountContract{}, fmt.Errorf("web form privacy projection: %w", err)
	}
	priv, _ := json.Marshal(me.IsPrivate)
	form["is_private"] = priv
	if raw, ok := form["business_account"]; ok && string(raw) != "null" {
		var business bool
		if json.Unmarshal(raw, &business) == nil {
			b, _ := json.Marshal(business)
			form["is_business"] = b
			form["is_professional_account"] = b
		}
	}
	return parseAccountContractFields(c, form, false)
}

func parseAccountContractFields(c *Client, fields map[string]json.RawMessage, requireProfessionalKeys bool) (accountContract, error) {
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
	isProfessional, err := optionalAccountBool(fields, "is_professional_account")
	if err != nil {
		return accountContract{}, err
	}
	isBusiness, err := optionalAccountBool(fields, "is_business")
	if err != nil {
		return accountContract{}, err
	}
	accountType, err := optionalAccountInt(fields, "account_type")
	if err != nil {
		return accountContract{}, err
	}
	categoryID, err := optionalAccountScalarString(fields, "category_id")
	if err != nil {
		return accountContract{}, err
	}
	categoryName, err := optionalAccountString(fields, "category_name")
	if err != nil {
		return accountContract{}, err
	}
	displayCategory, err := optionalAccountBool(fields, "should_show_category")
	if err != nil {
		return accountContract{}, err
	}
	_ = requireProfessionalKeys
	return accountContract{
		ID: id, Username: username, FullName: fullName, Biography: biography,
		ExternalURL: externalURL, IsPrivate: isPrivate,
		IsProfessional: isProfessional, IsBusiness: isBusiness,
		AccountType: accountType, CategoryID: categoryID,
		CategoryName: categoryName, DisplayCategory: displayCategory,
	}, nil
}

func accountNeedsWebFormFallback(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "something went wrong") ||
		strings.Contains(msg, "useragent mismatch") ||
		strings.Contains(msg, "login_required") ||
		strings.Contains(msg, "login required")
}

func requiredAccountRaw(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("%w: current account field %q is missing or null", ErrUnexpectedResponse, name)
	}
	return raw, nil
}

func optionalAccountRaw(fields map[string]json.RawMessage, name string) json.RawMessage {
	raw := fields[name]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func optionalAccountString(fields map[string]json.RawMessage, name string) (string, error) {
	raw := optionalAccountRaw(fields, name)
	if raw == nil {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: current account field %q must be a string", ErrUnexpectedResponse, name)
	}
	return value, nil
}

func optionalAccountBool(fields map[string]json.RawMessage, name string) (bool, error) {
	raw := optionalAccountRaw(fields, name)
	if raw == nil {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%w: current account field %q must be a boolean", ErrUnexpectedResponse, name)
	}
	return value, nil
}

func optionalAccountScalarString(fields map[string]json.RawMessage, name string) (string, error) {
	raw := optionalAccountRaw(fields, name)
	if raw == nil {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), nil
	}
	return "", fmt.Errorf("%w: current account field %q must be a string or number", ErrUnexpectedResponse, name)
}

func optionalAccountInt(fields map[string]json.RawMessage, name string) (int, error) {
	value, err := optionalAccountScalarString(fields, name)
	if err != nil || value == "" {
		return 0, err
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: current account field %q must contain an integer", ErrUnexpectedResponse, name)
	}
	return number, nil
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
		"_uuid":        {c.deviceUUID()},
		"_uid":         {c.cookies.DSUserID},
		"_csrftoken":   {c.cookies.CSRFToken},
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
		"_uuid":                {c.deviceUUID()},
		"_uid":                 {c.cookies.DSUserID},
		"_csrftoken":           {c.cookies.CSRFToken},
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
	if form == nil {
		form = url.Values{}
	}
	if form.Get("_uuid") == "" {
		form.Set("_uuid", c.deviceUUID())
	}
	if form.Get("_uid") == "" && c.cookies.DSUserID != "" {
		form.Set("_uid", c.cookies.DSUserID)
	}
	if form.Get("_csrftoken") == "" && c.cookies.CSRFToken != "" {
		form.Set("_csrftoken", c.cookies.CSRFToken)
	}
	// Prefer mobile private-API writes when available; browser sessions need the
	// www write profile (including X-Instagram-AJAX) for the same paths.
	err := c.doJSON(ctx, http.MethodPost, path, nil, &requestOptions{
		Host: requestHostAPI, IsWrite: true, NoRetry: true, FormBody: form,
	}, nil)
	if err == nil || !accountNeedsWebFormFallback(err) {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, nil, &requestOptions{
		IsWrite: true, NoRetry: true, FormBody: form,
		Referer: "https://www.instagram.com/accounts/edit/",
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
