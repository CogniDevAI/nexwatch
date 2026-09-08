package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration applies the role-based access matrix to every collection
// that predates roles. It must run after both the collections that created
// them (collections.go, thread_dumps.go) and the migration that added the
// "users.role" field (users_roles.go) — the filename is deliberately sorted
// after "users_roles.go" ("." < "_" in ASCII) so a fresh install applies
// them in the correct order.
//
// Role matrix:
//   - agents, alert_rules, notification_channels, alerts, thread_dumps:
//     list/view any authenticated record; create/update/delete requires
//     role operator or admin (superusers bypass rules entirely).
//   - settings: list/view any authenticated record; create/update/delete
//     requires role admin.
//   - metrics, docker_containers: list/view any authenticated record;
//     create/update/delete is server-only (null rule) — the hub writes
//     these through the app-level API (metrics.Service, ws hub), which
//     saves records directly via core.App and therefore bypasses collection
//     API rules entirely. No UI code writes to these two collections
//     through the PocketBase SDK.
func init() {
	authOnly := "@request.auth.id != ''"
	operatorOrAbove := "@request.auth.id != '' && @request.auth.role != 'viewer'"
	adminOnly := "@request.auth.role = 'admin'"

	operatorManaged := []string{
		"agents",
		"alert_rules",
		"notification_channels",
		"alerts",
		"thread_dumps",
	}
	adminManaged := []string{"settings"}
	serverOnly := []string{"metrics", "docker_containers"}

	m.Register(func(app core.App) error {
		for _, name := range operatorManaged {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			col.ListRule = &authOnly
			col.ViewRule = &authOnly
			col.CreateRule = &operatorOrAbove
			col.UpdateRule = &operatorOrAbove
			col.DeleteRule = &operatorOrAbove
			if err := app.Save(col); err != nil {
				return err
			}
		}

		for _, name := range adminManaged {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			col.ListRule = &authOnly
			col.ViewRule = &authOnly
			col.CreateRule = &adminOnly
			col.UpdateRule = &adminOnly
			col.DeleteRule = &adminOnly
			if err := app.Save(col); err != nil {
				return err
			}
		}

		for _, name := range serverOnly {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			col.ListRule = &authOnly
			col.ViewRule = &authOnly
			col.CreateRule = nil
			col.UpdateRule = nil
			col.DeleteRule = nil
			if err := app.Save(col); err != nil {
				return err
			}
		}

		return nil
	}, func(app core.App) error {
		// Down: restore the pre-RBAC "any authenticated record" rules that
		// collections.go and thread_dumps.go originally set.
		restored := append(append([]string{}, operatorManaged...), adminManaged...)

		for _, name := range restored {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone
			}
			col.ListRule = &authOnly
			col.ViewRule = &authOnly
			col.CreateRule = &authOnly
			col.UpdateRule = &authOnly
			col.DeleteRule = &authOnly
			if err := app.Save(col); err != nil {
				return err
			}
		}

		// thread_dumps originally had UpdateRule = nil (no direct updates).
		if col, err := app.FindCollectionByNameOrId("thread_dumps"); err == nil {
			col.UpdateRule = nil
			if err := app.Save(col); err != nil {
				return err
			}
		}

		for _, name := range serverOnly {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			col.ListRule = &authOnly
			col.ViewRule = &authOnly
			col.CreateRule = nil
			col.UpdateRule = nil
			col.DeleteRule = nil
			if err := app.Save(col); err != nil {
				return err
			}
		}

		return nil
	})
}
