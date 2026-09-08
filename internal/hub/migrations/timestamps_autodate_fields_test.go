package migrations_test

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Blank-imports the package under test so its init() functions register
	// every migration (including the one that adds "created"/"updated" to
	// every custom collection), matching how cmd/hub/main.go wires it up.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

// autodateCollections mirrors the list in timestamps_autodate_fields.go —
// every custom (non-auth) collection that needs "created"/"updated" fields
// so list queries can use the UI's default "sort=-created".
var autodateCollections = []string{
	"agents",
	"metrics",
	"docker_containers",
	"alert_rules",
	"alerts",
	"notification_channels",
	"settings",
	"thread_dumps",
}

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// TestAutodateFields_PresentOnEveryCustomCollection asserts that every
// collection created by this application's earlier migrations has both a
// "created" and an "updated" autodate field, which is what makes
// "sort=-created" (the UI's default list order) work instead of failing
// with a 400 "Something went wrong".
func TestAutodateFields_PresentOnEveryCustomCollection(t *testing.T) {
	app := newTestApp(t)

	for _, name := range autodateCollections {
		t.Run(name, func(t *testing.T) {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				t.Fatalf("find collection %s: %v", name, err)
			}
			if col.Fields.GetByName("created") == nil {
				t.Errorf("collection %s has no 'created' field", name)
			}
			if col.Fields.GetByName("updated") == nil {
				t.Errorf("collection %s has no 'updated' field", name)
			}
		})
	}
}

// TestAutodateFields_SortByCreatedWorks is an end-to-end regression test for
// the reported bug: before this migration, sort=-created on alert_rules or
// notification_channels returned an HTTP 400 ("Something went wrong")
// because the field did not exist, breaking the UI's rule/channel lists.
// apis.NewRouter (invoked internally by tests.ApiScenario) already wires up
// PocketBase's default "/api/collections/{collection}/records" route, so no
// BeforeTestFunc is needed here.
//
// Each collection gets its own fresh TestApp: tests.ApiScenario.Test()
// rebuilds the full router (and its app-level hooks) on every call, and
// invoking it more than once against the very same *TestApp within one test
// function trips over PocketBase's own duplicate-hook registration — an
// artifact of the test harness, unrelated to the migration under test.
func TestAutodateFields_SortByCreatedWorks(t *testing.T) {
	for _, collection := range []string{"alert_rules", "notification_channels"} {
		t.Run(collection, func(t *testing.T) {
			app := newTestApp(t)

			col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
			if err != nil {
				t.Fatalf("find superusers collection: %v", err)
			}
			superuser := core.NewRecord(col)
			superuser.SetEmail("root-" + collection + "@example.com")
			superuser.SetPassword("password123456")
			if err := app.Save(superuser); err != nil {
				t.Fatalf("save superuser: %v", err)
			}
			token, err := superuser.NewAuthToken()
			if err != nil {
				t.Fatalf("NewAuthToken: %v", err)
			}

			scenario := tests.ApiScenario{
				Name:            "sort=-created on " + collection,
				Method:          http.MethodGet,
				URL:             "/api/collections/" + collection + "/records?sort=-created",
				Headers:         map[string]string{"Authorization": token},
				ExpectedStatus:  http.StatusOK,
				ExpectedContent: []string{`"items"`},
				TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
			}
			scenario.Test(t)
		})
	}
}
