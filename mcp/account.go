package mcp

import (
	"context"
	"errors"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetCurrentAccountInput is the typed input for instagram_get_current_account.
type GetCurrentAccountInput struct{}

func getCurrentAccount(ctx context.Context, c *instagram.Client, _ GetCurrentAccountInput) (any, error) {
	result, err := c.GetCurrentAccount(ctx)
	return result, accountToolError(c, err)
}

// GetAccountSettingsInput is the typed input for instagram_get_account_settings.
type GetAccountSettingsInput struct{}

func getAccountSettings(ctx context.Context, c *instagram.Client, _ GetAccountSettingsInput) (any, error) {
	result, err := c.GetAccountSettings(ctx)
	return result, accountToolError(c, err)
}

// GetProfessionalAccountStateInput is the typed input for instagram_get_professional_account_state.
type GetProfessionalAccountStateInput struct{}

func getProfessionalAccountState(ctx context.Context, c *instagram.Client, _ GetProfessionalAccountStateInput) (any, error) {
	result, err := c.GetProfessionalAccountState(ctx)
	return result, accountToolError(c, err)
}

// UpdateProfileFieldsInput has no contact, credential, or security fields.
type UpdateProfileFieldsInput struct {
	ExpectedAccountID string                  `json:"expected_account_id" jsonschema:"description=numeric ID of the authenticated account expected to change,required"`
	Before            instagram.ProfileFields `json:"before" jsonschema:"description=complete profile state expected before the write,required"`
	After             instagram.ProfileFields `json:"after" jsonschema:"description=complete profile state required after the write,required"`
	Confirm           bool                    `json:"confirm" jsonschema:"description=explicit confirmation of this exact before-to-after mutation,required"`
}

func updateProfileFields(ctx context.Context, c *instagram.Client, in UpdateProfileFieldsInput) (any, error) {
	result, err := c.UpdateProfileFields(ctx, instagram.UpdateProfileFieldsParams{
		ExpectedAccountID: in.ExpectedAccountID, Before: in.Before, After: in.After, Confirm: in.Confirm,
	})
	return result, accountToolError(c, err)
}

// SetPrivacyInput guards one explicit public/private transition.
type SetPrivacyInput struct {
	ExpectedAccountID string `json:"expected_account_id" jsonschema:"description=numeric ID of the authenticated account expected to change,required"`
	Before            bool   `json:"before" jsonschema:"description=privacy state expected before the write,required"`
	After             bool   `json:"after" jsonschema:"description=privacy state required after the write,required"`
	Confirm           bool   `json:"confirm" jsonschema:"description=explicit confirmation of this exact before-to-after mutation,required"`
}

func setPrivacy(ctx context.Context, c *instagram.Client, in SetPrivacyInput) (any, error) {
	result, err := c.SetPrivacy(ctx, instagram.SetPrivacyParams{
		ExpectedAccountID: in.ExpectedAccountID, Before: in.Before, After: in.After, Confirm: in.Confirm,
	})
	return result, accountToolError(c, err)
}

// UpdateProfessionalSettingsInput permits display settings only; it cannot
// convert an account or alter public contact, ownership, or security data.
type UpdateProfessionalSettingsInput struct {
	ExpectedAccountID string                         `json:"expected_account_id" jsonschema:"description=numeric ID of the authenticated account expected to change,required"`
	Before            instagram.ProfessionalSettings `json:"before" jsonschema:"description=complete professional display state expected before the write,required"`
	After             instagram.ProfessionalSettings `json:"after" jsonschema:"description=complete professional display state required after the write,required"`
	Confirm           bool                           `json:"confirm" jsonschema:"description=explicit confirmation of this exact before-to-after mutation,required"`
}

func updateProfessionalSettings(ctx context.Context, c *instagram.Client, in UpdateProfessionalSettingsInput) (any, error) {
	result, err := c.UpdateProfessionalSettings(ctx, instagram.UpdateProfessionalSettingsParams{
		ExpectedAccountID: in.ExpectedAccountID, Before: in.Before, After: in.After, Confirm: in.Confirm,
	})
	return result, accountToolError(c, err)
}

func accountToolError(c *instagram.Client, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, instagram.ErrSessionExpired), errors.Is(err, instagram.ErrInvalidAuth):
		return &mcptool.Error{Code: "credential_expired", Message: "Instagram session expired; reconnect Instagram"}
	case errors.Is(err, instagram.ErrChallengeRequired):
		return &mcptool.Error{Code: "challenge_required", Message: "Instagram requires an account challenge before administration can continue"}
	case errors.Is(err, instagram.ErrAccountMismatch):
		data := map[string]any{}
		var mismatch *instagram.AccountMismatchError
		if errors.As(err, &mismatch) {
			data["expected_account_id"] = mismatch.ExpectedAccountID
			data["actual_account_id"] = mismatch.ActualAccountID
		}
		return &mcptool.Error{Code: "account_mismatch", Message: "authenticated account does not match the requested administration target", Data: data}
	case errors.Is(err, instagram.ErrMutationPrecondition):
		data := map[string]any{}
		var precondition *instagram.MutationPreconditionError
		if errors.As(err, &precondition) {
			data["field"] = precondition.Field
			data["reason"] = precondition.Reason
		}
		return &mcptool.Error{Code: "precondition_failed", Message: "account mutation was rejected before completion", Data: data}
	case errors.Is(err, instagram.ErrWriteSoftBlock), errors.Is(err, instagram.ErrRateLimited):
		state := c.RateLimit()
		return &mcptool.Error{
			Code: "rate_limited", Message: "Instagram account requests are cooling down", Retryable: true,
			Data: map[string]any{
				"cooldown_read_until": state.CooldownReadUntil, "cooldown_write_until": state.CooldownWriteUntil,
				"reason": state.BlockedReason,
			},
		}
	case errors.Is(err, instagram.ErrCSRF):
		return &mcptool.Error{Code: "credential_rejected", Message: "Instagram rejected the CSRF token for this write"}
	default:
		return err
	}
}

var accountTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetCurrentAccountInput](
		"instagram_get_current_account", "Read the authenticated account's safe identity and profile fields", "GetCurrentAccount", getCurrentAccount,
	),
	mcptool.Define[*instagram.Client, GetAccountSettingsInput](
		"instagram_get_account_settings", "Read reversible profile and privacy settings for the authenticated account", "GetAccountSettings", getAccountSettings,
	),
	mcptool.Define[*instagram.Client, GetProfessionalAccountStateInput](
		"instagram_get_professional_account_state", "Read professional type, category, and category visibility", "GetProfessionalAccountState", getProfessionalAccountState,
	),
	mcptool.Define[*instagram.Client, UpdateProfileFieldsInput](
		"instagram_update_profile_fields", "Confirm and verify a full-name, biography, or external-URL transition", "UpdateProfileFields", updateProfileFields,
	),
	mcptool.Define[*instagram.Client, SetPrivacyInput](
		"instagram_set_privacy", "Confirm and verify one explicit public/private account transition", "SetPrivacy", setPrivacy,
	),
	mcptool.Define[*instagram.Client, UpdateProfessionalSettingsInput](
		"instagram_update_professional_settings", "Confirm and verify professional category display settings", "UpdateProfessionalSettings", updateProfessionalSettings,
	),
}
