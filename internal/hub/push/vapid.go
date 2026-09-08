// Package push implements Web Push delivery (F11 — PWA + Web Push
// notifications): VAPID key management and per-subscription delivery via
// github.com/SherClockHolmes/webpush-go, shared by the "webpush"
// notification channel (internal/hub/notify/channels/webpush.go) and the
// POST /api/custom/push/test route (internal/hub/api/push_routes.go) so
// both send through the exact same code path.
package push

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/hubsecrets"
)

const (
	// VAPIDPublicKeySetting and VAPIDSubjectSetting are "settings" keys —
	// readable by any authenticated user, matching every other setting in
	// this codebase (see internal/hub/api/settings.go). The public key is
	// not secret; it is handed to every browser that subscribes.
	VAPIDPublicKeySetting = "vapid_public_key"
	VAPIDSubjectSetting   = "vapid_subject"

	// vapidPrivateKeySecret is a "hub_secrets" key — superuser-only, never
	// exposed to a regular authenticated user (see internal/hub/hubsecrets).
	vapidPrivateKeySecret = "vapid_private_key"

	// DefaultSubject is used when no vapid_subject setting has been saved
	// yet — a bare "mailto:" is what push services expect as the VAPID
	// JWT "sub" claim identifying who to contact about this application.
	DefaultSubject = "mailto:admin@localhost"
)

// vapidMu serializes concurrent first-callers (e.g. several browsers
// requesting the public key at the same moment right after a fresh
// install) so exactly one key pair is ever generated for a given hub.
var vapidMu sync.Mutex

// EnsureVAPIDKeys returns the hub's VAPID key pair and subject, generating
// and persisting a new pair exactly once if none exists yet.
func EnsureVAPIDKeys(app core.App) (publicKey, privateKey, subject string, err error) {
	vapidMu.Lock()
	defer vapidMu.Unlock()

	subject = SettingString(app, VAPIDSubjectSetting, DefaultSubject)

	publicKey = SettingString(app, VAPIDPublicKeySetting, "")
	privateKey, hasPrivate := hubsecrets.Get(app, vapidPrivateKeySecret)

	if publicKey != "" && hasPrivate {
		return publicKey, privateKey, subject, nil
	}

	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", "", fmt.Errorf("generate VAPID keys: %w", err)
	}

	if err := hubsecrets.Set(app, vapidPrivateKeySecret, privateKey); err != nil {
		return "", "", "", fmt.Errorf("store VAPID private key: %w", err)
	}
	if err := setSettingString(app, VAPIDPublicKeySetting, publicKey); err != nil {
		return "", "", "", fmt.Errorf("store VAPID public key: %w", err)
	}

	slog.Info("generated new VAPID key pair for web push")
	return publicKey, privateKey, subject, nil
}

// SettingString reads a string "settings" value, falling back to def when
// the key is missing, unreadable, or not a JSON string. This duplicates the
// tiny "settings" key/value helper already duplicated in
// internal/hub/backup, internal/hub/report, and internal/hub/api/settings.go
// (see that file's own comment for why it is duplicated per-package rather
// than shared) — exported here since both this package and
// internal/hub/notify/channels/webpush.go (the "webpush" notifier, part of
// the same F11 feature) need to read the "public_base_url" setting.
func SettingString(app core.App, key, def string) string {
	record, err := app.FindFirstRecordByFilter("settings", "key = {:key}", map[string]any{"key": key})
	if err != nil {
		return def
	}
	var v any
	if err := json.Unmarshal([]byte(record.GetString("value")), &v); err != nil {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

func setSettingString(app core.App, key, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	record, err := app.FindFirstRecordByFilter("settings", "key = {:key}", map[string]any{"key": key})
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
