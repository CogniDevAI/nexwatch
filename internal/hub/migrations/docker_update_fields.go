package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds image update-detection fields to "docker_containers":
//   - image_digest: the container's image local RepoDigest (from
//     `docker image inspect`).
//   - remote_digest: the registry's current manifest digest for the same
//     image reference (from `DistributionInspect`).
//   - update_available: true when image_digest and remote_digest differ.
//
// All three are populated by metrics.Service.upsertDockerContainer only
// when the docker collector reports them (internal/agent/collector/
// docker.go, docker_update.go) — left empty/false for an image with no
// registry reference (e.g. a locally-built image), when
// "docker_update_checks" is disabled on the agent, or when the registry
// could not be reached.
//
// This migration only adds fields to an existing collection, so it has no
// ordering dependency beyond running after collections.go creates
// "docker_containers" in the first place ("d" > "c" byte-wise, so a fresh
// install already applies them in the right order).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("docker_containers")
		if err != nil {
			return err
		}
		col.Fields.Add(
			&core.TextField{Name: "image_digest", Max: 128},
			&core.TextField{Name: "remote_digest", Max: 128},
			&core.BoolField{Name: "update_available"},
		)
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("docker_containers")
		if err != nil {
			return nil // already gone
		}
		col.Fields.RemoveByName("image_digest")
		col.Fields.RemoveByName("remote_digest")
		col.Fields.RemoveByName("update_available")
		return app.Save(col)
	})
}
