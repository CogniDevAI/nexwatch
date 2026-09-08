package report

import (
	"context"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify/channels"
)

// sendTimeout bounds a single channel's delivery attempt, matching
// internal/hub/notify.Service's 15s timeout for ordinary alert dispatch.
const sendTimeout = 15 * time.Second

// SendResult is the outcome of delivering rpt through one channel.
type SendResult struct {
	ChannelID   string
	ChannelName string
	Err         error
}

// rawSender is the subset of *channels.EmailNotifier that Send needs —
// declared narrowly so tests can inject a fake instead of dialing real SMTP.
type rawSender interface {
	SendRaw(ctx context.Context, subject, html, text string, channel *core.Record) error
}

// Send renders rpt's HTML and text bodies once and delivers them through
// every channel configured in the report_channel_ids setting, skipping
// (with a result entry noting why) any channel that is missing, disabled,
// or not of type "email" — the weekly report is email-only by design (see
// the mission brief: "email channels only"), unlike alert notifications
// which fan out to every configured channel type. It continues past an
// individual channel's failure, the same "best effort across channels"
// behavior notify.Service.dispatchToChannels already has for alerts.
func Send(app core.App, rpt Report) []SendResult {
	settings := LoadSettings(app)
	return send(app, rpt, settings.ChannelIDs, channels.NewEmailNotifier())
}

func send(app core.App, rpt Report, channelIDs []string, notifier rawSender) []SendResult {
	html, err := RenderHTML(rpt)
	if err != nil {
		return []SendResult{{Err: err}}
	}
	text, err := RenderText(rpt)
	if err != nil {
		return []SendResult{{Err: err}}
	}
	subject := Subject(rpt)

	results := make([]SendResult, 0, len(channelIDs))

	for _, id := range channelIDs {
		channel, err := app.FindRecordById("notification_channels", id)
		if err != nil {
			results = append(results, SendResult{ChannelID: id, Err: errNotFound(id)})
			continue
		}
		if channel.GetString("type") != "email" {
			results = append(results, SendResult{ChannelID: id, ChannelName: channel.GetString("name"), Err: errNotEmail(channel.GetString("type"))})
			continue
		}
		if !channel.GetBool("enabled") {
			results = append(results, SendResult{ChannelID: id, ChannelName: channel.GetString("name"), Err: errDisabled()})
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		sendErr := notifier.SendRaw(ctx, subject, html, text, channel)
		cancel()

		results = append(results, SendResult{ChannelID: id, ChannelName: channel.GetString("name"), Err: sendErr})
	}

	return results
}

// The three small sentinel-style error constructors below exist so Send's
// "why was this channel skipped" reasons read as plain English in
// logs/API responses without needing a shared error-type hierarchy for
// three call sites.

type reportSendError struct{ msg string }

func (e *reportSendError) Error() string { return e.msg }

func errNotFound(id string) error {
	return &reportSendError{msg: "notification channel " + id + " not found"}
}

func errNotEmail(actualType string) error {
	return &reportSendError{msg: "channel type " + actualType + " is not email; weekly reports only send through email channels"}
}

func errDisabled() error {
	return &reportSendError{msg: "channel is disabled"}
}
