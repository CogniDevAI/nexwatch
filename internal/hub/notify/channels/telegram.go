package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// TelegramNotifier sends alert notifications via the Telegram Bot API.
type TelegramNotifier struct {
	client *http.Client
}

// NewTelegramNotifier creates a new Telegram notifier.
func NewTelegramNotifier() *TelegramNotifier {
	return &TelegramNotifier{
		client: &http.Client{},
	}
}

// Type returns the channel type identifier.
func (n *TelegramNotifier) Type() string {
	return "telegram"
}

// Send delivers an alert notification via the Telegram Bot API.
// Channel config expects: bot_token, chat_id
func (n *TelegramNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	botToken := notify.GetConfigString(config, "bot_token")
	chatID := notify.GetConfigString(config, "chat_id")

	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram config missing required fields (bot_token, chat_id)")
	}

	// Render the message text.
	text := notify.RenderMessage(alert)

	// Build Telegram sendMessage payload.
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create telegram request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Check Telegram's "ok" field in response.
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}

	return nil
}
