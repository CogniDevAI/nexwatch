package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds the fields needed for F9 (hub-initiated agent
// self-update) to the "agents" collection:
//
//   - arch: the agent's runtime.GOARCH (e.g. "amd64", "arm64"), reported on
//     REGISTER (protocol.RegisterPayload.Arch — see cmd/agent/main.go's
//     buildRegisterMessage).
//   - platform: the agent's runtime.GOOS (e.g. "linux", "darwin"), also
//     reported on REGISTER (protocol.RegisterPayload.Platform).
//     Deliberately separate from the pre-existing "os" field: "os" is a
//     human-readable display string (e.g. "ubuntu 22.04", built from
//     gopsutil's host.Info()) that never matches the release asset naming
//     scheme's GOOS component
//     (nexwatch-agent_<version>_<goos>_<goarch>.tar.gz — see
//     .github/workflows/release.yml), so building a self-update download
//     URL from "os" would be wrong for every non-macOS host. "platform"
//     carries the exact string the release assets are actually built for.
//   - update_status: one of idle/started/downloading/verifying/installing/
//     restarting/failed/done, driven by the agent's COMMAND_RESPONSE
//     stream for an "update" command (internal/hub/update.Service) and by
//     REGISTER reconciliation after a successful restart (see
//     internal/hub/ws/handler.go's handleRegister).
//   - update_error: the last update failure's message, if any.
//   - update_target_version: the version the most recent update requested,
//     used both to detect "done" on the next REGISTER and by the UI to
//     show what an in-progress update is heading toward.
//   - update_requested_at / update_requested_by: audit-style bookkeeping
//     for the most recent update request, set by
//     internal/hub/api/update_routes.go alongside its own audit_log entry.
//
// It must run after the "agents" collection has been created
// (collections.go) — the filename is deliberately prefixed "collections_"
// (rather than "agent_...", which would byte-sort before "collections.go"
// and run too early) so a fresh install applies it in the correct order,
// the same convention collections_agent_token_hash.go already uses.
func init() {
	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		if agents.Fields.GetByName("arch") == nil {
			agents.Fields.Add(&core.TextField{Name: "arch", Max: 20})
		}
		if agents.Fields.GetByName("platform") == nil {
			agents.Fields.Add(&core.TextField{Name: "platform", Max: 20})
		}
		if agents.Fields.GetByName("update_status") == nil {
			agents.Fields.Add(&core.SelectField{
				Name:      "update_status",
				MaxSelect: 1,
				Values:    []string{"idle", "started", "downloading", "verifying", "installing", "restarting", "failed", "done"},
			})
		}
		if agents.Fields.GetByName("update_error") == nil {
			agents.Fields.Add(&core.TextField{Name: "update_error", Max: 2000})
		}
		if agents.Fields.GetByName("update_target_version") == nil {
			agents.Fields.Add(&core.TextField{Name: "update_target_version", Max: 50})
		}
		if agents.Fields.GetByName("update_requested_at") == nil {
			agents.Fields.Add(&core.DateField{Name: "update_requested_at"})
		}
		if agents.Fields.GetByName("update_requested_by") == nil {
			agents.Fields.Add(&core.TextField{Name: "update_requested_by", Max: 255})
		}

		return app.Save(agents)
	}, func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return nil // already gone
		}
		agents.Fields.RemoveByName("arch")
		agents.Fields.RemoveByName("platform")
		agents.Fields.RemoveByName("update_status")
		agents.Fields.RemoveByName("update_error")
		agents.Fields.RemoveByName("update_target_version")
		agents.Fields.RemoveByName("update_requested_at")
		agents.Fields.RemoveByName("update_requested_by")
		return app.Save(agents)
	})
}
