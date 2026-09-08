package migrations_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

// TestUpdateStatus_AcceptsRestartRequired pins the migration that adds
// "restart_required" to the agents.update_status select alongside idle/
// started/downloading/verifying/installing/restarting/failed/done — a
// record set to that value must save successfully rather than fail select
// validation.
func TestUpdateStatus_AcceptsRestartRequired(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}

	field := col.Fields.GetByName("update_status")
	if field == nil {
		t.Fatal("agents collection has no update_status field")
	}
	selectField, ok := field.(*core.SelectField)
	if !ok {
		t.Fatalf("update_status field is %T, want *core.SelectField", field)
	}
	found := false
	for _, v := range selectField.Values {
		if v == "restart_required" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("update_status select values = %v, want it to include restart_required", selectField.Values)
	}

	rec := core.NewRecord(col)
	rec.Set("hostname", "host-restart-required-migration")
	rec.Set("status", "online")
	rec.Set("update_status", "restart_required")
	if err := app.Save(rec); err != nil {
		t.Errorf("save agent with update_status=restart_required: %v", err)
	}
}
