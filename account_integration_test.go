package instagram_test

import (
	"context"
	"os"
	"testing"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

const accountAdminConfirmation = "RESTORE_BURNER_SETTINGS"

func accountAdminBurner(t *testing.T) (*instagram.Client, string) {
	t.Helper()
	if os.Getenv("INSTAGRAM_ACCOUNT_ADMIN_LIVE_TEST") != "1" {
		t.Skip("set INSTAGRAM_ACCOUNT_ADMIN_LIVE_TEST=1 for reversible burner administration smoke tests")
	}
	if os.Getenv("INSTAGRAM_ACCOUNT_ADMIN_CONFIRM") != accountAdminConfirmation {
		t.Fatalf("INSTAGRAM_ACCOUNT_ADMIN_CONFIRM must equal %q", accountAdminConfirmation)
	}
	id := os.Getenv("INSTAGRAM_ACCOUNT_ADMIN_BURNER_ID")
	if id == "" {
		t.Fatal("INSTAGRAM_ACCOUNT_ADMIN_BURNER_ID is required")
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, err := c.GetCurrentAccount(ctx)
	if err != nil {
		t.Fatalf("GetCurrentAccount: %v", err)
	}
	if account.AccountID != id {
		t.Fatalf("configured burner ID %q does not match authenticated account %q", id, account.AccountID)
	}
	return c, id
}

func TestIntegration_AccountAdmin_ProfileRestoresBurner(t *testing.T) {
	c, id := accountAdminBurner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	original, err := c.GetAccountSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restoreProfileCleanup(t, c, id, original.Profile)

	changed := original.Profile
	changed.Biography += " [instagram-go reversible smoke]"
	if _, err := c.UpdateProfileFields(ctx, instagram.UpdateProfileFieldsParams{
		ExpectedAccountID: id, Before: original.Profile, After: changed, Confirm: true,
	}); err != nil {
		t.Fatalf("temporary profile mutation: %v", err)
	}
	if _, err := c.UpdateProfileFields(ctx, instagram.UpdateProfileFieldsParams{
		ExpectedAccountID: id, Before: changed, After: original.Profile, Confirm: true,
	}); err != nil {
		t.Fatalf("restore profile: %v", err)
	}
	assertProfileRestored(t, c, id, original.Profile)
}

func TestIntegration_AccountAdmin_PrivacyRestoresBurner(t *testing.T) {
	c, id := accountAdminBurner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	original, err := c.GetAccountSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restorePrivacyCleanup(t, c, id, original.IsPrivate)

	changed := !original.IsPrivate
	if _, err := c.SetPrivacy(ctx, instagram.SetPrivacyParams{
		ExpectedAccountID: id, Before: original.IsPrivate, After: changed, Confirm: true,
	}); err != nil {
		t.Fatalf("temporary privacy mutation: %v", err)
	}
	if _, err := c.SetPrivacy(ctx, instagram.SetPrivacyParams{
		ExpectedAccountID: id, Before: changed, After: original.IsPrivate, Confirm: true,
	}); err != nil {
		t.Fatalf("restore privacy: %v", err)
	}
	assertPrivacyRestored(t, c, id, original.IsPrivate)
}

func TestIntegration_AccountAdmin_ProfessionalDisplayRestoresBurner(t *testing.T) {
	c, id := accountAdminBurner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	original, err := c.GetProfessionalAccountState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !original.IsProfessional {
		t.Skip("configured burner is not a professional account")
	}
	restoreProfessionalCleanup(t, c, id, original.Settings)

	changed := original.Settings
	changed.DisplayCategory = !changed.DisplayCategory
	if _, err := c.UpdateProfessionalSettings(ctx, instagram.UpdateProfessionalSettingsParams{
		ExpectedAccountID: id, Before: original.Settings, After: changed, Confirm: true,
	}); err != nil {
		t.Fatalf("temporary professional mutation: %v", err)
	}
	if _, err := c.UpdateProfessionalSettings(ctx, instagram.UpdateProfessionalSettingsParams{
		ExpectedAccountID: id, Before: changed, After: original.Settings, Confirm: true,
	}); err != nil {
		t.Fatalf("restore professional settings: %v", err)
	}
	assertProfessionalRestored(t, c, id, original.Settings)
}

func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func restoreProfileCleanup(t *testing.T, c *instagram.Client, id string, original instagram.ProfileFields) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := cleanupContext()
		defer cancel()
		current, err := c.GetAccountSettings(ctx)
		if err != nil {
			t.Errorf("cleanup read profile: %v", err)
			return
		}
		if current.Profile != original {
			if _, err := c.UpdateProfileFields(ctx, instagram.UpdateProfileFieldsParams{ExpectedAccountID: id, Before: current.Profile, After: original, Confirm: true}); err != nil {
				t.Errorf("cleanup restore profile: %v", err)
			}
		}
	})
}

func restorePrivacyCleanup(t *testing.T, c *instagram.Client, id string, original bool) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := cleanupContext()
		defer cancel()
		current, err := c.GetAccountSettings(ctx)
		if err != nil {
			t.Errorf("cleanup read privacy: %v", err)
			return
		}
		if current.IsPrivate != original {
			if _, err := c.SetPrivacy(ctx, instagram.SetPrivacyParams{ExpectedAccountID: id, Before: current.IsPrivate, After: original, Confirm: true}); err != nil {
				t.Errorf("cleanup restore privacy: %v", err)
			}
		}
	})
}

func restoreProfessionalCleanup(t *testing.T, c *instagram.Client, id string, original instagram.ProfessionalSettings) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := cleanupContext()
		defer cancel()
		current, err := c.GetProfessionalAccountState(ctx)
		if err != nil {
			t.Errorf("cleanup read professional settings: %v", err)
			return
		}
		if current.Settings != original {
			if _, err := c.UpdateProfessionalSettings(ctx, instagram.UpdateProfessionalSettingsParams{ExpectedAccountID: id, Before: current.Settings, After: original, Confirm: true}); err != nil {
				t.Errorf("cleanup restore professional settings: %v", err)
			}
		}
	})
}

func assertProfileRestored(t *testing.T, c *instagram.Client, id string, want instagram.ProfileFields) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := c.GetAccountSettings(ctx)
	if err != nil || got.AccountID != id || got.Profile != want {
		t.Fatalf("profile restoration verification: got=%#v err=%v", got, err)
	}
}

func assertPrivacyRestored(t *testing.T, c *instagram.Client, id string, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := c.GetAccountSettings(ctx)
	if err != nil || got.AccountID != id || got.IsPrivate != want {
		t.Fatalf("privacy restoration verification: got=%#v err=%v", got, err)
	}
}

func assertProfessionalRestored(t *testing.T, c *instagram.Client, id string, want instagram.ProfessionalSettings) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := c.GetProfessionalAccountState(ctx)
	if err != nil || got.AccountID != id || got.Settings != want {
		t.Fatalf("professional restoration verification: got=%#v err=%v", got, err)
	}
}
