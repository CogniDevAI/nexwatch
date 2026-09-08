package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func mustSaveUser(t *testing.T, app core.App, email, role string) *core.Record {
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

func mustSaveSubscription(t *testing.T, app core.App, userID, endpoint string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("push_subscriptions")
	if err != nil {
		t.Fatalf("find push_subscriptions collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("user_id", userID)
	rec.Set("endpoint", endpoint)
	// Valid-looking (if not cryptographically real) base64url values —
	// webpush-go only needs these to decode as base64 to build the
	// encryption context, it does not verify they came from a real browser.
	rec.Set("p256dh", "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM")
	rec.Set("auth", "tBHItJI5svbpez7KI4CCXg")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save push subscription: %v", err)
	}
	return rec
}

func TestSendToSubscription_SetsVAPIDAndEncryptionHeaders(t *testing.T) {
	app := newTestApp(t)
	user := mustSaveUser(t, app, "device-owner@example.com", "viewer")

	var gotAuth, gotEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotEncoding = r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sub := mustSaveSubscription(t, app, user.Id, server.URL+"/push/abc123")

	sender := &Sender{App: app, Client: server.Client()}
	err := sender.SendToSubscription(context.Background(), sub, Payload{Title: "t", Body: "b"})
	if err != nil {
		t.Fatalf("SendToSubscription() error: %v", err)
	}

	if gotAuth == "" || len(gotAuth) < 6 || gotAuth[:6] != "vapid " {
		t.Errorf("Authorization header = %q, want it to start with \"vapid \"", gotAuth)
	}
	if gotEncoding != "aes128gcm" {
		t.Errorf("Content-Encoding header = %q, want %q", gotEncoding, "aes128gcm")
	}
}

func TestSendToSubscription_PrunesOnGone(t *testing.T) {
	app := newTestApp(t)
	user := mustSaveUser(t, app, "gone-owner@example.com", "viewer")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	sub := mustSaveSubscription(t, app, user.Id, server.URL+"/push/expired")

	sender := &Sender{App: app, Client: server.Client()}
	if err := sender.SendToSubscription(context.Background(), sub, Payload{Title: "t", Body: "b"}); err == nil {
		t.Fatal("SendToSubscription() returned nil error for a 410 response, want an error")
	}

	if _, err := app.FindRecordById("push_subscriptions", sub.Id); err == nil {
		t.Error("subscription still exists after a 410 response, want it pruned")
	}
}

func TestSendToSubscription_RateLimitedDoesNotPrune(t *testing.T) {
	app := newTestApp(t)
	user := mustSaveUser(t, app, "rate-limited-owner@example.com", "viewer")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	sub := mustSaveSubscription(t, app, user.Id, server.URL+"/push/rate-limited")

	sender := &Sender{App: app, Client: server.Client()}
	if err := sender.SendToSubscription(context.Background(), sub, Payload{Title: "t", Body: "b"}); err == nil {
		t.Fatal("SendToSubscription() returned nil error for a 429 response, want an error")
	}

	if _, err := app.FindRecordById("push_subscriptions", sub.Id); err != nil {
		t.Error("subscription was pruned after a 429 response, want it kept (rate limiting is transient)")
	}
}

func TestSubscriptionsForAudience_FiltersByRole(t *testing.T) {
	app := newTestApp(t)
	admin := mustSaveUser(t, app, "admin-aud@example.com", "admin")
	operator := mustSaveUser(t, app, "operator-aud@example.com", "operator")
	viewer := mustSaveUser(t, app, "viewer-aud@example.com", "viewer")

	mustSaveSubscription(t, app, admin.Id, "https://push.example.com/admin")
	mustSaveSubscription(t, app, operator.Id, "https://push.example.com/operator")
	mustSaveSubscription(t, app, viewer.Id, "https://push.example.com/viewer")

	all, err := SubscriptionsForAudience(app, AudienceAll)
	if err != nil {
		t.Fatalf("SubscriptionsForAudience(all) error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len(all) = %d, want 3", len(all))
	}

	admins, err := SubscriptionsForAudience(app, AudienceAdmins)
	if err != nil {
		t.Fatalf("SubscriptionsForAudience(admins) error: %v", err)
	}
	if len(admins) != 1 || admins[0].GetString("endpoint") != "https://push.example.com/admin" {
		t.Errorf("SubscriptionsForAudience(admins) = %v, want exactly the admin's subscription", admins)
	}

	operators, err := SubscriptionsForAudience(app, AudienceOperators)
	if err != nil {
		t.Fatalf("SubscriptionsForAudience(operators) error: %v", err)
	}
	if len(operators) != 1 || operators[0].GetString("endpoint") != "https://push.example.com/operator" {
		t.Errorf("SubscriptionsForAudience(operators) = %v, want exactly the operator's subscription", operators)
	}
}
