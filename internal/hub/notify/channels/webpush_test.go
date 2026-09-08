package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

func mustSaveWebPushUser(t *testing.T, app core.App, email, role string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(email)
	rec.SetPassword("password123456")
	rec.Set("role", role)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save user: %v", err)
	}
	return rec
}

func mustSaveWebPushSubscription(t *testing.T, app core.App, userID, endpoint string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("push_subscriptions")
	if err != nil {
		t.Fatalf("find push_subscriptions collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("user_id", userID)
	rec.Set("endpoint", endpoint)
	rec.Set("p256dh", "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM")
	rec.Set("auth", "tBHItJI5svbpez7KI4CCXg")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save push subscription: %v", err)
	}
	return rec
}

func TestWebPushNotifier_Type(t *testing.T) {
	app := newTestApp(t)
	n := NewWebPushNotifier(app)
	if got := n.Type(); got != "webpush" {
		t.Errorf("Type() = %q, want %q", got, "webpush")
	}
}

func TestWebPushNotifier_Send_DeliversToMatchingAudience(t *testing.T) {
	app := newTestApp(t)
	admin := mustSaveWebPushUser(t, app, "wp-admin@example.com", "admin")
	viewer := mustSaveWebPushUser(t, app, "wp-viewer@example.com", "viewer")

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	// Both subscriptions point at the same fake push service; only the
	// admin's should receive a request for an "admins"-audience channel.
	mustSaveWebPushSubscription(t, app, admin.Id, server.URL+"/push/admin")
	mustSaveWebPushSubscription(t, app, viewer.Id, server.URL+"/push/viewer")

	n := NewWebPushNotifier(app)
	n.sender.Client = server.Client()

	channel := createChannel(t, app, "webpush", map[string]any{"audience": "admins"})
	alert, alertCtx := createAlert(t, app, "firing", 95, "[critical] cpu on web-01: cpu > 80.0 (current: 95.0)")

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("push service received %d requests, want exactly 1 (admins-only audience)", requestCount)
	}
}

func TestWebPushNotifier_Send_DefaultAudienceIsAll(t *testing.T) {
	app := newTestApp(t)
	admin := mustSaveWebPushUser(t, app, "wp-admin-2@example.com", "admin")
	viewer := mustSaveWebPushUser(t, app, "wp-viewer-2@example.com", "viewer")

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	mustSaveWebPushSubscription(t, app, admin.Id, server.URL+"/push/admin2")
	mustSaveWebPushSubscription(t, app, viewer.Id, server.URL+"/push/viewer2")

	n := NewWebPushNotifier(app)
	n.sender.Client = server.Client()

	// No "audience" key at all in config — should default to "all".
	channel := createChannel(t, app, "webpush", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 50, "[warning] memory on db-01: memory > 40.0 (current: 50.0)")

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if requestCount != 2 {
		t.Errorf("push service received %d requests, want exactly 2 (default \"all\" audience)", requestCount)
	}
}

func TestWebPushNotifier_Send_NoSubscriptionsIsNotAnError(t *testing.T) {
	app := newTestApp(t)
	n := NewWebPushNotifier(app)

	channel := createChannel(t, app, "webpush", map[string]any{"audience": "all"})
	alert, alertCtx := createAlert(t, app, "firing", 10, "[warning] disk on cache-1: disk > 5.0 (current: 10.0)")

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Errorf("Send() with no subscriptions returned an error, want nil: %v", err)
	}
}

func TestWebPushNotifier_Send_OneFailingSubscriptionDoesNotStopOthers(t *testing.T) {
	app := newTestApp(t)
	user1 := mustSaveWebPushUser(t, app, "wp-fail-1@example.com", "viewer")
	user2 := mustSaveWebPushUser(t, app, "wp-fail-2@example.com", "viewer")

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	var goodRequestCount int
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodRequestCount++
		w.WriteHeader(http.StatusCreated)
	}))
	defer good.Close()

	mustSaveWebPushSubscription(t, app, user1.Id, failing.URL+"/push/fail")
	mustSaveWebPushSubscription(t, app, user2.Id, good.URL+"/push/good")

	n := NewWebPushNotifier(app)
	// Both endpoints are httptest servers with independent clients, but
	// SendToSubscription always uses the sender's shared client — a
	// default *http.Client works against either httptest.Server URL, so
	// no per-server client swap is needed here.
	n.sender.Client = &http.Client{}

	channel := createChannel(t, app, "webpush", map[string]any{"audience": "all"})
	alert, alertCtx := createAlert(t, app, "firing", 90, "[critical] cpu on web-02: cpu > 80.0 (current: 90.0)")

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Errorf("Send() returned an error because one of two subscriptions failed, want nil (per-subscription failures are logged, not aggregated): %v", err)
	}
	if goodRequestCount != 1 {
		t.Errorf("the working subscription received %d requests, want 1 (one failing subscription must not stop delivery to the rest)", goodRequestCount)
	}
}

// --- buildAlertPayload -----------------------------------------------------

// TestBuildAlertPayload_HostPageURL covers both the relative (no
// public_base_url) and absolute (public_base_url configured) cases in one
// test (sharing a single test app) rather than two, since spinning up a
// full PocketBase test app is the dominant cost of this package's test
// suite under `go test -race`.
func TestBuildAlertPayload_HostPageURL(t *testing.T) {
	app := newTestApp(t)
	alert, alertCtx := createAlert(t, app, "firing", 95, "[critical] cpu on web-01: cpu > 80.0 (current: 95.0)")
	rule, err := app.FindRecordById("alert_rules", alert.GetString("rule_id"))
	if err != nil {
		t.Fatalf("find rule: %v", err)
	}

	t.Run("relative path when no public_base_url is configured", func(t *testing.T) {
		payload := buildAlertPayload(alert, alertCtx)

		want := "/servers/" + alert.GetString("agent_id")
		if payload.URL != want {
			t.Errorf("payload.URL = %q, want the relative host page %q", payload.URL, want)
		}
	})

	t.Run("absolute URL prefixed with public_base_url once configured", func(t *testing.T) {
		mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")

		// Re-resolve the context after the setting write so it picks up the
		// dashboard URL, the same way a real dispatch resolves it fresh.
		payload := buildAlertPayload(alert, notify.BuildAlertContext(app, alert, rule))

		want := "https://nexwatch.example.com/servers/" + alert.GetString("agent_id")
		if payload.URL != want {
			t.Errorf("payload.URL = %q, want the absolute host page %q", payload.URL, want)
		}
	})
}
