package hubsecrets

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// "hub_secrets" collection this package reads/writes.
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

func TestGet_MissingKeyReturnsNotFound(t *testing.T) {
	app := newTestApp(t)

	if _, ok := Get(app, "does_not_exist"); ok {
		t.Error("Get() for a missing key returned ok=true, want false")
	}
}

func TestSetThenGet_RoundTrips(t *testing.T) {
	app := newTestApp(t)

	if err := Set(app, "vapid_private_key", "example-private-key-value"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got, ok := Get(app, "vapid_private_key")
	if !ok {
		t.Fatal("Get() after Set() returned ok=false, want true")
	}
	if got != "example-private-key-value" {
		t.Errorf("Get() = %q, want %q", got, "example-private-key-value")
	}
}

func TestSet_UpdatesExistingKeyInPlace(t *testing.T) {
	app := newTestApp(t)

	if err := Set(app, "k", "first"); err != nil {
		t.Fatalf("first Set() error: %v", err)
	}
	if err := Set(app, "k", "second"); err != nil {
		t.Fatalf("second Set() error: %v", err)
	}

	got, ok := Get(app, "k")
	if !ok || got != "second" {
		t.Errorf("Get() = (%q, %v), want (\"second\", true)", got, ok)
	}

	records, err := app.FindRecordsByFilter("hub_secrets", "key = {:key}", "", 0, 0, map[string]any{"key": "k"})
	if err != nil {
		t.Fatalf("FindRecordsByFilter() error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("found %d hub_secrets records for key %q, want 1 (upsert, not duplicate insert)", len(records), "k")
	}
}

func TestHubSecretsCollection_NotReadableByRegularUser(t *testing.T) {
	app := newTestApp(t)

	col, err := app.FindCollectionByNameOrId("hub_secrets")
	if err != nil {
		t.Fatalf("find hub_secrets collection: %v", err)
	}
	for _, rule := range []*string{col.ListRule, col.ViewRule, col.CreateRule, col.UpdateRule, col.DeleteRule} {
		if rule != nil {
			t.Errorf("hub_secrets rule = %q, want nil (superuser-only) for every API rule", *rule)
		}
	}
}
