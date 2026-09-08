package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration prepares the "users" auth collection for role-based access
// control. PocketBase's own system migrations already create "users" as a
// default auth collection on every fresh install, so the "create if
// missing" branch below only matters as a defensive fallback (e.g. a
// deployment that manually deleted it); either way this migration ensures
// the "role" and "name" fields and the role-aware API rules are present.
//
// It must run after the base collections (collections.go) so a fresh
// install always has "users" available for the RBAC rule migration
// (users_roles_rbac.go) that follows it.
func init() {
	authOnly := "@request.auth.id != ''"
	adminOnly := "@request.auth.role = 'admin'"

	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			// Defensive fallback: PocketBase's system migrations normally
			// create "users" automatically, but re-create it if it is
			// somehow missing so this migration is self-sufficient.
			users = core.NewAuthCollection("users", "_pb_users_auth_")
		}

		// Keep classic email/password auth enabled.
		users.PasswordAuth.Enabled = true

		if users.Fields.GetByName("name") == nil {
			users.Fields.Add(&core.TextField{Name: "name", Max: 255})
		}

		if users.Fields.GetByName("role") == nil {
			users.Fields.Add(&core.SelectField{
				Name:      "role",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"viewer", "operator", "admin"},
			})
		}

		// Any authenticated record may list/view users; only admins
		// (superusers bypass rules entirely) may create/update/delete them.
		users.ListRule = &authOnly
		users.ViewRule = &authOnly
		users.CreateRule = &adminOnly
		users.UpdateRule = &adminOnly
		users.DeleteRule = &adminOnly

		return app.Save(users)
	}, func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return nil // already gone
		}

		users.Fields.RemoveByName("role")

		// Restore PocketBase's original default "users" rules (owner-only).
		ownerRule := "id = @request.auth.id"
		emptyRule := ""
		users.ListRule = &ownerRule
		users.ViewRule = &ownerRule
		users.CreateRule = &emptyRule
		users.UpdateRule = &ownerRule
		users.DeleteRule = &ownerRule

		return app.Save(users)
	})
}
