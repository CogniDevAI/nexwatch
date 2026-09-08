package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds agents.dropped_messages, which stores the agent
// transport's cumulative count of messages it has discarded because its
// outgoing send queue was full (see internal/agent/transport.WSTransport
// and the "dropped" field on protocol.HeartbeatPayload). The hub's
// heartbeat handler (internal/hub/ws/handler.go) writes the latest value
// here on every HEARTBEAT so operators can see queue overflow in the
// dashboard instead of it being silently invisible.
//
// It must run after the "agents" collection has been created
// (collections.go) — the filename is deliberately sorted after every
// existing migration file ("w" > "t" > "u" > "c" byte-wise) so a fresh
// install applies them in the correct order.
func init() {
	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		if agents.Fields.GetByName("dropped_messages") != nil {
			return nil // idempotent re-run safety
		}

		agents.Fields.Add(&core.NumberField{Name: "dropped_messages"})

		return app.Save(agents)
	}, func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		agents.Fields.RemoveByName("dropped_messages")
		return app.Save(agents)
	})
}
