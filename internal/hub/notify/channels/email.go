package channels

import (
	"context"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// EmailNotifier sends alert notifications via SMTP email.
type EmailNotifier struct {
	// sendMail performs the actual SMTP delivery and defaults to
	// smtp.SendMail. It is a seam so tests can assert on the assembled
	// message (addr, auth, from, to, body) without a real SMTP server.
	sendMail func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error
}

// NewEmailNotifier creates a new email notifier.
func NewEmailNotifier() *EmailNotifier {
	return &EmailNotifier{sendMail: smtp.SendMail}
}

// Type returns the channel type identifier.
func (n *EmailNotifier) Type() string {
	return "email"
}

// Send delivers an alert notification via SMTP.
// Channel config expects: host, port, from, username, password, to
func (n *EmailNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	cfg, err := n.resolveConfig(channel)
	if err != nil {
		return err
	}

	body := notify.RenderMessage(alert, alertCtx.Severity)
	subject := emailSubject(alert, alertCtx)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.from, cfg.to, subject, body)

	return n.deliver(ctx, cfg, []byte(msg))
}

// emailSubject builds "[NexWatch][severity] rule on host" — naming the
// rule/check and the affected host right in the subject line, since that
// is often all an operator sees in an inbox list before opening the
// message. ruleOrMetric falls back to the alert's metric_type-derived
// message prefix (there is no readable rule name once a rule has been
// deleted after firing) and subject falls back to the check name for a
// check-based alert, then to "alerts" as a last resort when neither an
// agent nor a check is known.
func emailSubject(alert *core.Record, alertCtx notify.AlertContext) string {
	rule := alertCtx.RuleName
	if rule == "" {
		rule = "alert"
	}

	subject := alertCtx.Hostname
	if subject == "" {
		subject = alertCtx.CheckName
	}
	if subject == "" {
		subject = "alerts"
	}

	return fmt.Sprintf("[NexWatch][%s] %s on %s", alertCtx.Severity, rule, subject)
}

// SendRaw delivers an arbitrary HTML+plain-text email through this channel,
// as a multipart/alternative message so both a modern mail client (HTML)
// and a plain-text-only reader get a usable rendering. Unlike Send, it
// takes no "alert" record — it exists for the weekly report
// (internal/hub/report), which renders its own subject/html/text and has
// no single alert to notify about. It reuses the same channel config
// (host/port/from/username/password/to) and delivery seam as Send.
func (n *EmailNotifier) SendRaw(ctx context.Context, subject, html, text string, channel *core.Record) error {
	cfg, err := n.resolveConfig(channel)
	if err != nil {
		return err
	}

	msg := buildMultipartMessage(cfg.from, cfg.to, subject, text, html)
	return n.deliver(ctx, cfg, msg)
}

// emailConfig is the resolved, validated set of fields Send/SendRaw both
// need out of a notification_channels "config" JSON blob.
type emailConfig struct {
	host, from, username, password, to string
	port                               int
}

func (n *EmailNotifier) resolveConfig(channel *core.Record) (emailConfig, error) {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return emailConfig{}, err
	}

	cfg := emailConfig{
		host:     notify.GetConfigString(config, "host"),
		port:     notify.GetConfigInt(config, "port"),
		from:     notify.GetConfigString(config, "from"),
		username: notify.GetConfigString(config, "username"),
		password: notify.GetConfigString(config, "password"),
		to:       notify.GetConfigString(config, "to"),
	}

	if cfg.host == "" || cfg.from == "" || cfg.to == "" {
		return emailConfig{}, notify.NewConfigError("email config missing required fields (host, from, to)")
	}
	if cfg.port == 0 {
		cfg.port = 587
	}
	return cfg, nil
}

// deliver sends msg over SMTP using a goroutine with context cancellation
// for timeout support, matching the original Send's shape.
func (n *EmailNotifier) deliver(ctx context.Context, cfg emailConfig, msg []byte) error {
	addr := cfg.host + ":" + strconv.Itoa(cfg.port)

	var auth smtp.Auth
	if cfg.username != "" && cfg.password != "" {
		auth = smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
	}

	done := make(chan error, 1)
	go func() {
		done <- n.sendMail(addr, auth, cfg.from, []string{cfg.to}, msg)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("email send timed out: %w", ctx.Err())
	}
}

// buildMultipartMessage assembles a hand-formatted multipart/alternative
// RFC 5322 message with a plain-text and an HTML part — no external MIME
// library, matching this codebase's preference for small hand-rolled
// formatting over new dependencies (see e.g. the Prometheus exposition
// writer in internal/hub/api/prometheus.go).
func buildMultipartMessage(from, to, subject, text, html string) []byte {
	const boundary = "nexwatch-boundary-7f3a9c1e"

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(text)
	b.WriteString("\r\n\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(html)
	b.WriteString("\r\n\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	return []byte(b.String())
}
