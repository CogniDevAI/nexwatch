package push

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// "settings"/"hub_secrets" collections this package reads/writes.
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

func TestEnsureVAPIDKeys_GeneratesOnceAndPersists(t *testing.T) {
	app := newTestApp(t)

	pub1, priv1, subject1, err := EnsureVAPIDKeys(app)
	if err != nil {
		t.Fatalf("first EnsureVAPIDKeys() error: %v", err)
	}
	if pub1 == "" || priv1 == "" {
		t.Fatal("EnsureVAPIDKeys() returned an empty key")
	}
	if subject1 != DefaultSubject {
		t.Errorf("subject = %q, want default %q", subject1, DefaultSubject)
	}

	pub2, priv2, _, err := EnsureVAPIDKeys(app)
	if err != nil {
		t.Fatalf("second EnsureVAPIDKeys() error: %v", err)
	}
	if pub1 != pub2 || priv1 != priv2 {
		t.Error("EnsureVAPIDKeys() generated a new pair on the second call, want the same persisted pair")
	}
}

func TestEnsureVAPIDKeys_RespectsConfiguredSubject(t *testing.T) {
	app := newTestApp(t)

	if err := setSettingString(app, VAPIDSubjectSetting, "mailto:ops@example.com"); err != nil {
		t.Fatalf("setSettingString() error: %v", err)
	}

	_, _, subject, err := EnsureVAPIDKeys(app)
	if err != nil {
		t.Fatalf("EnsureVAPIDKeys() error: %v", err)
	}
	if subject != "mailto:ops@example.com" {
		t.Errorf("subject = %q, want %q", subject, "mailto:ops@example.com")
	}
}

func TestEnsureVAPIDKeys_PrivateKeyNeverStoredInSettings(t *testing.T) {
	app := newTestApp(t)

	if _, _, _, err := EnsureVAPIDKeys(app); err != nil {
		t.Fatalf("EnsureVAPIDKeys() error: %v", err)
	}

	// The readable "settings" collection must never carry a
	// "vapid_private_key" entry — the private key only ever lives in
	// hub_secrets. SettingString falls back to the sentinel default when
	// the key is absent, so seeing the sentinel back proves no such
	// setting exists.
	const sentinel = "no-such-setting"
	if got := SettingString(app, "vapid_private_key", sentinel); got != sentinel {
		t.Error("found a \"vapid_private_key\" entry in the readable settings collection, want it only in hub_secrets")
	}
}
