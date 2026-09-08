package api

import (
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/push"
)

// RegisterPushRoutes registers Web Push API routes (F11 — PWA + Web Push
// notifications) on apiGroup, which the caller must already have bound
// with the desired auth middleware (e.g. apis.RequireAuth()) and mounted
// at the "/api/custom" prefix. It returns every route it registered, for
// openapi_test.go to cross-check against docs/openapi.yaml.
func RegisterPushRoutes(apiGroup *router.RouterGroup[*core.RequestEvent]) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// GET /api/custom/push/vapid-public-key — the hub's VAPID public key,
	// generating a new key pair lazily on first request if none exists yet.
	rec.GET("/push/vapid-public-key", handleVAPIDPublicKey)

	// POST /api/custom/push/test — send a test Web Push notification to
	// every subscription belonging to the caller.
	rec.POST("/push/test", handleSendTestPush)

	return rec.Registered
}

func handleVAPIDPublicKey(e *core.RequestEvent) error {
	publicKey, _, _, err := push.EnsureVAPIDKeys(e.App)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to generate VAPID keys",
		})
	}
	return e.JSON(http.StatusOK, map[string]string{"public_key": publicKey})
}

// handleSendTestPush sends a test notification to every "push_subscriptions"
// row owned by the caller, returning a per-endpoint success/failure result
// so the settings UI can show exactly which of the caller's devices
// received it.
func handleSendTestPush(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	subs, err := e.App.FindRecordsByFilter(
		"push_subscriptions",
		"user_id = {:uid}",
		"",
		0,
		0,
		map[string]any{"uid": e.Auth.Id},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to load push subscriptions",
		})
	}

	sender := push.NewSender(e.App)
	payload := push.Payload{
		Title:     "NexWatch test notification",
		Body:      "If you can see this, browser notifications are working.",
		URL:       "/settings/notifications",
		Severity:  "info",
		Tag:       "nexwatch-test",
		Timestamp: time.Now().UTC().UnixMilli(),
	}

	results := make([]push.SendResult, 0, len(subs))
	successCount := 0
	for _, sub := range subs {
		endpoint := sub.GetString("endpoint")
		if sendErr := sender.SendToSubscription(e.Request.Context(), sub, payload); sendErr != nil {
			results = append(results, push.SendResult{Endpoint: endpoint, Success: false, Error: sendErr.Error()})
			continue
		}
		results = append(results, push.SendResult{Endpoint: endpoint, Success: true})
		successCount++
	}

	auditResult := "success"
	if successCount == 0 && len(subs) > 0 {
		auditResult = "failure"
	}
	audit.Record(e.App, e, audit.Entry{
		Action:     "push.test",
		TargetType: "push_subscriptions",
		Details:    map[string]any{"subscriptions": len(subs), "succeeded": successCount},
		Result:     auditResult,
	})

	return e.JSON(http.StatusOK, map[string]any{"results": results})
}
