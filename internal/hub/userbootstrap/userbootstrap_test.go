package userbootstrap

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations (including the "users.role" field)
	// against core.AppMigrations so tests.NewTestApp applies them, matching
	// the schema Admin() runs against in production.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func TestAdmin_EmptyCredentialIsNoop(t *testing.T) {
	app := newTestApp(t)

	if err := Admin(app, ""); err != nil {
		t.Fatalf("Admin(\"\") returned error: %v", err)
	}

	existing, _ := app.FindAuthRecordByEmail("users", "")
	if existing != nil {
		t.Fatalf("expected no user to be created for an empty credential")
	}
}

func TestAdmin_MalformedCredentialErrors(t *testing.T) {
	app := newTestApp(t)

	cases := []string{
		"no-colon-here",
		":missing-email",
		"missing-password:",
	}

	for _, credential := range cases {
		t.Run(credential, func(t *testing.T) {
			if err := Admin(app, credential); err == nil {
				t.Fatalf("Admin(%q) expected an error, got nil", credential)
			}
		})
	}
}

func TestAdmin_CreatesAdminUser(t *testing.T) {
	app := newTestApp(t)

	if err := Admin(app, "admin@example.com:supersecret1"); err != nil {
		t.Fatalf("Admin() returned error: %v", err)
	}

	record, err := app.FindAuthRecordByEmail("users", "admin@example.com")
	if err != nil {
		t.Fatalf("expected admin user to exist: %v", err)
	}

	if got := record.GetString("role"); got != "admin" {
		t.Fatalf("created user role = %q, want %q", got, "admin")
	}

	if !record.ValidatePassword("supersecret1") {
		t.Fatalf("created user password does not match the bootstrap password")
	}
}

func TestAdmin_ExistingUserIsUntouched(t *testing.T) {
	app := newTestApp(t)

	if err := Admin(app, "admin@example.com:first-password"); err != nil {
		t.Fatalf("first Admin() call returned error: %v", err)
	}

	// A second call with a different password for the same email must be a
	// no-op — it should never overwrite an existing account.
	if err := Admin(app, "admin@example.com:second-password"); err != nil {
		t.Fatalf("second Admin() call returned error: %v", err)
	}

	record, err := app.FindAuthRecordByEmail("users", "admin@example.com")
	if err != nil {
		t.Fatalf("expected admin user to exist: %v", err)
	}

	if !record.ValidatePassword("first-password") {
		t.Fatalf("expected the original password to still be valid")
	}
	if record.ValidatePassword("second-password") {
		t.Fatalf("expected the second Admin() call to be a no-op")
	}
}
