package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"text/template"
	"bytes"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Service dispatches alert notifications to configured channels.
type Service struct {
	app       core.App
	notifiers map[string]Notifier
	mu        sync.RWMutex
}

// NewService creates a new notification service.
func NewService(app core.App) *Service {
	return &Service{
		app:       app,
		notifiers: make(map[string]Notifier),
	}
}

// RegisterNotifier adds a notifier implementation for a given channel type.
func (s *Service) RegisterNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifiers[n.Type()] = n
	log.Printf("[notify] registered notifier: %s", n.Type())
}

// Dispatch sends notifications for an alert to all channels linked to the rule.
func (s *Service) Dispatch(app core.App, alert *core.Record, rule *core.Record) {
	channelIDs := rule.GetStringSlice("notification_channels")
	if len(channelIDs) == 0 {
		log.Printf("[notify] rule %s has no notification channels configured", rule.Id)
		return
	}

	for _, channelID := range channelIDs {
		channel, err := app.FindRecordById("notification_channels", channelID)
		if err != nil {
			log.Printf("[notify] channel %s not found: %v", channelID, err)
			continue
		}

		if !channel.GetBool("enabled") {
			log.Printf("[notify] channel %s (%s) is disabled, skipping", channel.GetString("name"), channelID)
			continue
		}

		channelType := channel.GetString("type")

		s.mu.RLock()
		notifier, exists := s.notifiers[channelType]
		s.mu.RUnlock()

		if !exists {
			log.Printf("[notify] no notifier registered for type: %s", channelType)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = notifier.Send(ctx, alert, channel)
		cancel()

		if err != nil {
			log.Printf("[notify] FAILED to send via %s (channel %s): %v", channelType, channel.GetString("name"), err)
		} else {
			log.Printf("[notify] sent via %s (channel %s) for alert %s", channelType, channel.GetString("name"), alert.Id)
		}
	}
}

// SendTestNotification sends a test message to a specific channel.
func (s *Service) SendTestNotification(channel *core.Record) error {
	channelType := channel.GetString("type")

	s.mu.RLock()
	notifier, exists := s.notifiers[channelType]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no notifier registered for type: %s", channelType)
	}

	// Create a fake alert record for testing.
	alertCollection, err := s.app.FindCollectionByNameOrId("alerts")
	if err != nil {
		return fmt.Errorf("alerts collection not found: %w", err)
	}

	fakeAlert := core.NewRecord(alertCollection)
	fakeAlert.Set("status", "firing")
	fakeAlert.Set("value", 95.5)
	fakeAlert.Set("message", "[TEST] This is a test notification from NexWatch. If you see this, your notification channel is working correctly!")
	fakeAlert.Set("fired_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return notifier.Send(ctx, fakeAlert, channel)
}

// AlertData holds template-friendly data for rendering notification messages.
type AlertData struct {
	AgentName  string
	MetricType string
	Value      float64
	Threshold  float64
	Condition  string
	Severity   string
	Status     string
	Message    string
	FiredAt    string
	ResolvedAt string
}

// defaultTemplate is the Go template used to format notification messages.
const defaultTemplate = `🚨 NexWatch Alert
Status: {{.Status}} | Severity: {{.Severity}}
{{.Message}}
Time: {{.FiredAt}}{{if .ResolvedAt}}
Resolved: {{.ResolvedAt}}{{end}}`

// RenderMessage formats an alert notification using the default template.
func RenderMessage(alert *core.Record) string {
	data := AlertData{
		Status:     alert.GetString("status"),
		Value:      alert.GetFloat("value"),
		Message:    alert.GetString("message"),
		FiredAt:    alert.GetString("fired_at"),
		ResolvedAt: alert.GetString("resolved_at"),
	}

	tmpl, err := template.New("alert").Parse(defaultTemplate)
	if err != nil {
		return data.Message
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return data.Message
	}

	return buf.String()
}

// ParseChannelConfig extracts the config JSON from a notification channel record.
func ParseChannelConfig(channel *core.Record) (map[string]any, error) {
	configStr := channel.GetString("config")
	if configStr == "" {
		return nil, fmt.Errorf("channel %s has empty config", channel.Id)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON for channel %s: %w", channel.Id, err)
	}

	return config, nil
}

// GetConfigString extracts a string value from channel config.
func GetConfigString(config map[string]any, key string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetConfigInt extracts an integer value from channel config.
func GetConfigInt(config map[string]any, key string) int {
	if v, ok := config[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return 0
}
