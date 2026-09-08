package api

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// TestRoleOrdering verifies the pure Role hierarchy comparison
// (viewer < operator < admin) that RequireRole relies on.
func TestRoleOrdering(t *testing.T) {
	cases := []struct {
		name string
		a    Role
		b    Role
		want bool // a < b
	}{
		{"viewer < operator", RoleViewer, RoleOperator, true},
		{"operator < admin", RoleOperator, RoleAdmin, true},
		{"viewer < admin", RoleViewer, RoleAdmin, true},
		{"operator !< viewer", RoleOperator, RoleViewer, false},
		{"admin !< admin", RoleAdmin, RoleAdmin, false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a < tt.b; got != tt.want {
				t.Fatalf("%v < %v = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestRole_String(t *testing.T) {
	cases := []struct {
		role Role
		want string
	}{
		{RoleViewer, "viewer"},
		{RoleOperator, "operator"},
		{RoleAdmin, "admin"},
		{Role(99), "viewer"}, // unknown falls back to viewer
	}

	for _, c := range cases {
		if got := c.role.String(); got != c.want {
			t.Fatalf("Role(%d).String() = %q, want %q", c.role, got, c.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Role
	}{
		{"viewer", "viewer", RoleViewer},
		{"operator", "operator", RoleOperator},
		{"admin", "admin", RoleAdmin},
		{"empty falls back to viewer", "", RoleViewer},
		{"unknown falls back to viewer", "superadmin", RoleViewer},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseRole(tt.in); got != tt.want {
				t.Fatalf("ParseRole(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestRoleOfRecord_NilRecord verifies the pure nil-safety branch without
// needing a real app/collection.
func TestRoleOfRecord_NilRecord(t *testing.T) {
	if got := RoleOfRecord(nil); got != RoleViewer {
		t.Fatalf("RoleOfRecord(nil) = %v, want %v", got, RoleViewer)
	}
}

// TestRoleOf_NoAuth verifies an unauthenticated request resolves to viewer.
func TestRoleOf_NoAuth(t *testing.T) {
	e := &core.RequestEvent{}
	if got := RoleOf(e); got != RoleViewer {
		t.Fatalf("RoleOf(no auth) = %v, want %v", got, RoleViewer)
	}
	if got := RoleOf(nil); got != RoleViewer {
		t.Fatalf("RoleOf(nil event) = %v, want %v", got, RoleViewer)
	}
}

// TestRoleOf_RealRecords exercises RoleOf/RoleOfRecord against actual
// PocketBase records built from a throwaway test app, so superuser
// detection and the "users" collection role field are checked against the
// real collection/field machinery instead of a hand-rolled fake.
func TestRoleOf_RealRecords(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	superusersCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find superusers collection: %v", err)
	}
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}

	// Add the "role" select field the way the users_roles migration does,
	// so GetString("role") behaves as it will in production.
	if usersCol.Fields.GetByName("role") == nil {
		usersCol.Fields.Add(&core.SelectField{
			Name:      "role",
			Required:  false,
			MaxSelect: 1,
			Values:    []string{"viewer", "operator", "admin"},
		})
		if err := app.Save(usersCol); err != nil {
			t.Fatalf("add role field: %v", err)
		}
	}

	superuser := core.NewRecord(superusersCol)
	superuser.SetEmail("root@example.com")
	superuser.SetPassword("password123")

	adminUser := core.NewRecord(usersCol)
	adminUser.SetEmail("admin@example.com")
	adminUser.SetPassword("password123")
	adminUser.Set("role", "admin")

	operatorUser := core.NewRecord(usersCol)
	operatorUser.SetEmail("operator@example.com")
	operatorUser.SetPassword("password123")
	operatorUser.Set("role", "operator")

	viewerUser := core.NewRecord(usersCol)
	viewerUser.SetEmail("viewer@example.com")
	viewerUser.SetPassword("password123")
	viewerUser.Set("role", "viewer")

	blankRoleUser := core.NewRecord(usersCol)
	blankRoleUser.SetEmail("blank@example.com")
	blankRoleUser.SetPassword("password123")
	// role left unset on purpose.

	cases := []struct {
		name   string
		record *core.Record
		want   Role
	}{
		{"superuser is always admin", superuser, RoleAdmin},
		{"users record with role=admin", adminUser, RoleAdmin},
		{"users record with role=operator", operatorUser, RoleOperator},
		{"users record with role=viewer", viewerUser, RoleViewer},
		{"users record with unset role defaults to viewer", blankRoleUser, RoleViewer},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := RoleOfRecord(tt.record); got != tt.want {
				t.Fatalf("RoleOfRecord(%s) = %v, want %v", tt.record.Email(), got, tt.want)
			}

			e := &core.RequestEvent{Auth: tt.record}
			if got := RoleOf(e); got != tt.want {
				t.Fatalf("RoleOf(%s) = %v, want %v", tt.record.Email(), got, tt.want)
			}
		})
	}
}

// TestRequireRole verifies the middleware allows requests whose role meets
// the minimum and rejects (403) requests that don't, without needing a real
// HTTP server — hook.Handler.Func can be invoked directly.
func TestRequireRole(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	if usersCol.Fields.GetByName("role") == nil {
		usersCol.Fields.Add(&core.SelectField{
			Name:      "role",
			MaxSelect: 1,
			Values:    []string{"viewer", "operator", "admin"},
		})
		if err := app.Save(usersCol); err != nil {
			t.Fatalf("add role field: %v", err)
		}
	}

	viewer := core.NewRecord(usersCol)
	viewer.Set("role", "viewer")

	operator := core.NewRecord(usersCol)
	operator.Set("role", "operator")

	admin := core.NewRecord(usersCol)
	admin.Set("role", "admin")

	cases := []struct {
		name       string
		auth       *core.Record
		min        Role
		wantForbid bool
	}{
		{"no auth against operator gate", nil, RoleOperator, true},
		{"viewer against operator gate", viewer, RoleOperator, true},
		{"operator against operator gate", operator, RoleOperator, false},
		{"admin against operator gate", admin, RoleOperator, false},
		{"operator against admin gate", operator, RoleAdmin, true},
		{"admin against admin gate", admin, RoleAdmin, false},
		{"viewer against viewer gate", viewer, RoleViewer, false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			handler := RequireRole(tt.min)
			e := &core.RequestEvent{Auth: tt.auth}

			err := handler.Func(e)

			if tt.wantForbid {
				if err == nil {
					t.Fatalf("expected a forbidden error, got nil")
				}
			} else if err != nil {
				t.Fatalf("expected no error (Next() should run), got: %v", err)
			}
		})
	}
}
