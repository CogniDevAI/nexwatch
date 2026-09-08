package api

import (
	"encoding/json"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

// getSettingValue reads a "settings" collection value by key and decodes it
// as JSON, mirroring the small get-setting helpers already duplicated in
// internal/hub/backup and internal/hub/checks/retention.go. It is shared
// across this package's own settings-backed handlers (public status page,
// Prometheus exposition) since they all live here, rather than duplicated
// per file the way those other packages do it.
func getSettingValue(app core.App, key string) (any, bool) {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return nil, false
	}

	var v any
	if err := json.Unmarshal([]byte(record.GetString("value")), &v); err != nil {
		return nil, false
	}
	return v, true
}

// settingBool reads a boolean setting, falling back to def when the key is
// missing, unreadable, or not a JSON boolean.
func settingBool(app core.App, key string, def bool) bool {
	v, ok := getSettingValue(app, key)
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

// settingString reads a string setting, falling back to def when the key is
// missing, unreadable, not a JSON string, or empty.
func settingString(app core.App, key, def string) string {
	v, ok := getSettingValue(app, key)
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

// settingInt reads a positive integer setting, accepting either a raw JSON
// number or a JSON string (the UI settings form convention used elsewhere
// in this codebase — see ui/src/pages/Settings.tsx — encodes numeric
// settings as a quoted string), falling back to def otherwise.
func settingInt(app core.App, key string, def int) int {
	v, ok := getSettingValue(app, key)
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			return parsed
		}
	}
	return def
}

// setSettingValue upserts a "settings" record for key, JSON-encoding value
// as its "value" field. It saves directly via core.App.Save, the same way
// every other server-side write in this codebase bypasses collection API
// rules (e.g. handleGenerateAgentToken saving an agent record directly) —
// callers are expected to already be behind their own authorization check
// (e.g. RequireRole(RoleAdmin) on the route), since this function performs
// none of its own.
func setSettingValue(app core.App, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		col, cErr := app.FindCollectionByNameOrId("settings")
		if cErr != nil {
			return cErr
		}
		record = core.NewRecord(col)
		record.Set("key", key)
	}
	record.Set("value", string(encoded))
	return app.Save(record)
}

// settingRaw returns the raw JSON-encoded string stored for key (the exact
// bytes of the "value" field), without decoding it — used for values that
// are themselves a JSON document (e.g. status_page_items) that the caller
// wants to unmarshal into a specific struct rather than `any`.
func settingRaw(app core.App, key string) (string, bool) {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return "", false
	}
	return record.GetString("value"), true
}
