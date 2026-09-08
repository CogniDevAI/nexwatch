package api

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Role represents a user's authorization level within the hub. Roles form a
// strict hierarchy: RoleViewer < RoleOperator < RoleAdmin.
type Role int

const (
	RoleViewer Role = iota
	RoleOperator
	RoleAdmin
)

// String returns the canonical lowercase name of the role, matching the
// values accepted by the "users.role" select field.
func (r Role) String() string {
	switch r {
	case RoleAdmin:
		return "admin"
	case RoleOperator:
		return "operator"
	default:
		return "viewer"
	}
}

// ParseRole converts a role string ("viewer", "operator", "admin") into a
// Role. Unknown or empty values fall back to RoleViewer so a missing or
// corrupt role never accidentally grants elevated access.
func ParseRole(s string) Role {
	switch s {
	case "admin":
		return RoleAdmin
	case "operator":
		return RoleOperator
	default:
		return RoleViewer
	}
}

// RoleOf returns the effective Role of the authenticated record attached to
// e. Superusers (records from the "_superusers" collection) are always
// treated as RoleAdmin regardless of any "role" field. Records from the
// "users" collection use their "role" field. A request with no
// authenticated record, or an authenticated record from any other
// collection, is treated as RoleViewer.
func RoleOf(e *core.RequestEvent) Role {
	if e == nil || e.Auth == nil {
		return RoleViewer
	}
	return RoleOfRecord(e.Auth)
}

// RoleOfRecord returns the effective Role of an auth record directly,
// without needing a full *core.RequestEvent. This is split out from RoleOf
// so the role-resolution logic can be unit tested against bare records.
func RoleOfRecord(record *core.Record) Role {
	if record == nil {
		return RoleViewer
	}
	if record.IsSuperuser() {
		return RoleAdmin
	}
	if record.Collection().Name == "users" {
		return ParseRole(record.GetString("role"))
	}
	return RoleViewer
}

// RequireRole returns a router middleware that rejects the request with a
// 403 JSON error when the authenticated user's effective role is below min.
//
// It must be chained after apis.RequireAuth() (or an equivalent middleware
// that populates e.Auth) since RequireRole itself does not check whether
// the request is authenticated at all — an unauthenticated request simply
// resolves to RoleViewer and is rejected the same way an under-privileged
// viewer would be.
func RequireRole(min Role) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Func: func(e *core.RequestEvent) error {
			if RoleOf(e) < min {
				return e.ForbiddenError("This action requires a higher role.", nil)
			}
			return e.Next()
		},
	}
}
