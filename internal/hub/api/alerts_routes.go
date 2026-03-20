package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// RegisterAlertRoutes registers alert and notification API routes.
func RegisterAlertRoutes(se *core.ServeEvent, notifySvc *notify.Service) {
	router := se.Router

	// POST /api/custom/notifications/:id/test — send a test notification to a channel.
	router.POST("/api/custom/notifications/{id}/test", func(e *core.RequestEvent) error {
		return handleTestNotification(e, notifySvc)
	})
}

// handleTestNotification sends a test notification to the specified channel.
func handleTestNotification(e *core.RequestEvent, notifySvc *notify.Service) error {
	channelID := e.Request.PathValue("id")
	if channelID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "channel ID is required",
		})
	}

	channel, err := e.App.FindRecordById("notification_channels", channelID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "notification channel not found",
		})
	}

	if err := notifySvc.SendTestNotification(channel); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "test notification failed: " + err.Error(),
		})
	}

	return e.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Test notification sent successfully",
	})
}
