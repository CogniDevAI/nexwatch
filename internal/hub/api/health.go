package api

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// startTime records the process start time so /healthz can report an
// uptime that survives across requests without needing external state.
var startTime = time.Now()

// healthzResponse is the JSON body returned by GET /healthz.
type healthzResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	DB            string `json:"db"`
	AgentsOnline  int64  `json:"agents_online"`
}

// RegisterHealthRoute registers the unauthenticated GET /healthz endpoint
// directly on the top-level router, outside the "/api/custom" group (which
// requires an authenticated record) — so external load balancers, uptime
// monitors, and container orchestrators can probe hub health without a
// PocketBase auth token. version is the build version reported by the hub
// binary (cmd/hub's `version` variable).
func RegisterHealthRoute(se *core.ServeEvent, version string) {
	se.Router.GET("/healthz", func(e *core.RequestEvent) error {
		resp := healthzResponse{
			Status:        "ok",
			Version:       version,
			UptimeSeconds: int64(time.Since(startTime).Seconds()),
			DB:            "ok",
		}

		// A trivial query doubles as the DB health check: if the database is
		// unreachable or corrupted, this fails and status flips to "error".
		online, err := e.App.CountRecords("agents", dbx.HashExp{"status": "online"})
		if err != nil {
			resp.Status = "error"
			resp.DB = "error"
			return e.JSON(http.StatusServiceUnavailable, resp)
		}
		resp.AgentsOnline = online

		return e.JSON(http.StatusOK, resp)
	})
}
