// Package userbootstrap creates an initial admin account in the "users"
// collection on hub startup, so a fresh deployment always has a way to log
// into the dashboard without a separate manual provisioning step.
package userbootstrap

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Admin ensures a "users" record with role "admin" exists for the given
// "EMAIL:PASSWORD" credential. It is a no-op if credential is empty or if a
// user with that email already exists — an existing user's password and
// role are never modified by this function.
func Admin(app core.App, credential string) error {
	if credential == "" {
		return nil
	}

	email, password, ok := strings.Cut(credential, ":")
	if !ok || email == "" || password == "" {
		return fmt.Errorf("userbootstrap: expected EMAIL:PASSWORD, got %q", credential)
	}

	existing, _ := app.FindAuthRecordByEmail("users", email)
	if existing != nil {
		return nil
	}

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return fmt.Errorf("userbootstrap: users collection: %w", err)
	}

	record := core.NewRecord(col)
	record.SetEmail(email)
	record.SetPassword(password)
	record.SetVerified(true)
	record.Set("role", "admin")

	if err := app.Save(record); err != nil {
		return fmt.Errorf("userbootstrap: save admin user: %w", err)
	}
	return nil
}
