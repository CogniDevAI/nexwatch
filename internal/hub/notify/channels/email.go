package channels

import (
	"context"
	"fmt"
	"net/smtp"
	"strconv"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// EmailNotifier sends alert notifications via SMTP email.
type EmailNotifier struct{}

// NewEmailNotifier creates a new email notifier.
func NewEmailNotifier() *EmailNotifier {
	return &EmailNotifier{}
}

// Type returns the channel type identifier.
func (n *EmailNotifier) Type() string {
	return "email"
}

// Send delivers an alert notification via SMTP.
// Channel config expects: host, port, from, username, password, to
func (n *EmailNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	host := notify.GetConfigString(config, "host")
	port := notify.GetConfigInt(config, "port")
	from := notify.GetConfigString(config, "from")
	username := notify.GetConfigString(config, "username")
	password := notify.GetConfigString(config, "password")
	to := notify.GetConfigString(config, "to")

	if host == "" || from == "" || to == "" {
		return fmt.Errorf("email config missing required fields (host, from, to)")
	}

	if port == 0 {
		port = 587
	}

	// Render message body.
	body := notify.RenderMessage(alert)
	subject := fmt.Sprintf("NexWatch Alert: %s", alert.GetString("status"))

	// Build the email message.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	addr := host + ":" + strconv.Itoa(port)

	// Set up authentication if credentials are provided.
	var auth smtp.Auth
	if username != "" && password != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}

	// Use a goroutine with context cancellation for timeout support.
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("email send timed out: %w", ctx.Err())
	}
}
