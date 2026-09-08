package report

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
	"time"
)

// Subject returns the email subject line for rpt.
func Subject(rpt Report) string {
	return fmt.Sprintf("NexWatch weekly report — %s to %s",
		rpt.PeriodStart.Format("Jan 2"), rpt.PeriodEnd.Format("Jan 2, 2006"))
}

// templateFuncs are shared between the HTML and text templates.
var templateFuncs = map[string]any{
	"pct":  func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
	"num":  func(v float64) string { return fmt.Sprintf("%.1f", v) },
	"dur":  formatDuration,
	"date": func(t time.Time) string { return t.Format("Jan 2, 2006") },
}

// formatDuration renders a duration as a compact "1h 30m" / "45m" / "12s"
// string — good enough resolution for a mean-time-to-resolve figure
// without pulling in a formatting dependency.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	d = d.Round(time.Minute)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return "<1m"
	}
}

// RenderHTML renders rpt as a self-contained HTML email — no external CSS
// (no <link>, no remote fonts: many mail clients strip both), inline
// styles on every element, and a small <style> block carrying only a
// prefers-color-scheme override so the report stays readable in a dark-mode
// mail client instead of rendering dark text on a dark background.
func RenderHTML(rpt Report) (string, error) {
	tmpl, err := htmltemplate.New("report.html").Funcs(templateFuncs).Parse(htmlTemplateSource)
	if err != nil {
		return "", fmt.Errorf("parse html template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, rpt); err != nil {
		return "", fmt.Errorf("execute html template: %w", err)
	}
	return buf.String(), nil
}

// RenderText renders rpt as the plain-text fallback part of the email.
func RenderText(rpt Report) (string, error) {
	tmpl, err := texttemplate.New("report.txt").Funcs(templateFuncs).Parse(textTemplateSource)
	if err != nil {
		return "", fmt.Errorf("parse text template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, rpt); err != nil {
		return "", fmt.Errorf("execute text template: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}

const htmlTemplateSource = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>NexWatch weekly report</title>
<style>
  @media (prefers-color-scheme: dark) {
    .nw-body { background-color: #0a0e16 !important; }
    .nw-panel { background-color: #121926 !important; border-color: #232d3d !important; }
    .nw-ink { color: #e7ecf3 !important; }
    .nw-muted { color: #93a0b4 !important; }
  }
</style>
</head>
<body class="nw-body" style="margin:0; padding:0; background-color:#f4f5f7; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="padding:24px 0;">
    <tr>
      <td align="center">
        <table role="presentation" width="600" cellpadding="0" cellspacing="0" class="nw-panel" style="max-width:600px; width:100%; background-color:#ffffff; border:1px solid #e2e5ea; border-radius:8px;">
          <tr>
            <td style="padding:24px 28px 8px 28px;">
              <h1 class="nw-ink" style="margin:0 0 4px 0; font-size:20px; color:#111827;">NexWatch weekly report</h1>
              <p class="nw-muted" style="margin:0; font-size:13px; color:#6b7280;">{{ date .PeriodStart }} – {{ date .PeriodEnd }} ({{ .PeriodDays }} days)</p>
            </td>
          </tr>

          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">Fleet</h2>
            <p class="nw-muted" style="margin:0; font-size:13px; color:#374151; line-height:1.6;">
              {{ .Fleet.AgentsOnline }} of {{ .Fleet.AgentsTotal }} agents online now &middot;
              average uptime {{ pct .Fleet.AverageUptimePercent }} over the period
            </p>
          </td></tr>

          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">Alerts</h2>
            <p class="nw-muted" style="margin:0 0 8px 0; font-size:13px; color:#374151;">
              {{ index .Alerts.CountBySeverity "critical" }} critical, {{ index .Alerts.CountBySeverity "warning" }} warning &middot;
              mean time to resolve {{ dur .Alerts.MeanTimeToResolve }}
            </p>
            {{ if .Alerts.TopRules }}
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:13px; color:#374151;">
              <tr><td style="padding:2px 0; font-weight:600; color:#6b7280;">Top firing rules</td></tr>
              {{ range .Alerts.TopRules }}
              <tr><td style="padding:2px 0;">{{ .RuleName }} — {{ .Firings }}</td></tr>
              {{ end }}
            </table>
            {{ else }}
            <p class="nw-muted" style="margin:0; font-size:13px; color:#6b7280;">No alerts fired this period.</p>
            {{ end }}
          </td></tr>

          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">Resource peaks</h2>
            {{ if .Peaks }}
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:13px; color:#374151; border-collapse:collapse;">
              <tr style="color:#6b7280; font-weight:600;">
                <td style="padding:4px 8px 4px 0;">Host</td><td style="padding:4px 8px;">CPU</td><td style="padding:4px 8px;">Memory</td><td style="padding:4px 0;">Disk</td>
              </tr>
              {{ range .Peaks }}
              <tr><td style="padding:4px 8px 4px 0;">{{ .Hostname }}</td><td style="padding:4px 8px;">{{ pct .MaxCPU }}</td><td style="padding:4px 8px;">{{ pct .MaxMemory }}</td><td style="padding:4px 0;">{{ pct .MaxDisk }}</td></tr>
              {{ end }}
            </table>
            {{ else }}
            <p class="nw-muted" style="margin:0; font-size:13px; color:#6b7280;">No agents reporting.</p>
            {{ end }}
          </td></tr>

          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">Checks</h2>
            <p class="nw-muted" style="margin:0; font-size:13px; color:#374151;">
              {{ pct .Checks.UptimePercent }} uptime across all probes
              {{ if .Checks.WorstLatencyName }}&middot; worst latency: {{ .Checks.WorstLatencyName }} ({{ num .Checks.WorstLatencyMillis }} ms){{ end }}
            </p>
          </td></tr>

          {{ if .CVE }}
          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">CVE totals</h2>
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:13px; color:#374151; border-collapse:collapse;">
              <tr style="color:#6b7280; font-weight:600;">
                <td style="padding:4px 8px 4px 0;">Host</td><td style="padding:4px 8px;">Critical</td><td style="padding:4px 8px;">High</td><td style="padding:4px 8px;">Medium</td><td style="padding:4px 0;">Low</td>
              </tr>
              {{ range .CVE }}
              <tr><td style="padding:4px 8px 4px 0;">{{ .Hostname }}</td><td style="padding:4px 8px;">{{ .Critical }}</td><td style="padding:4px 8px;">{{ .High }}</td><td style="padding:4px 8px;">{{ .Medium }}</td><td style="padding:4px 0;">{{ .Low }}</td></tr>
              {{ end }}
            </table>
          </td></tr>
          {{ end }}

          {{ if .Logs }}
          <tr><td style="padding:20px 28px 0 28px;">
            <h2 class="nw-ink" style="margin:0 0 8px 0; font-size:15px; color:#111827;">Log volume</h2>
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:13px; color:#374151; border-collapse:collapse;">
              {{ range .Logs }}
              <tr><td style="padding:2px 8px 2px 0;">{{ .Hostname }}</td><td style="padding:2px 0;">{{ .Rows }} rows</td></tr>
              {{ end }}
            </table>
          </td></tr>
          {{ end }}

          <tr><td style="padding:24px 28px 24px 28px;">
            <p class="nw-muted" style="margin:0; font-size:12px; color:#9ca3af; border-top:1px solid #e2e5ea; padding-top:16px;">
              Generated {{ date .GeneratedAt }} by NexWatch{{ if .HubURL }} &middot; <a href="{{ .HubURL }}" style="color:#5b9dff;">{{ .HubURL }}</a>{{ end }}
            </p>
          </td></tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>
`

const textTemplateSource = `NexWatch weekly report
{{ date .PeriodStart }} - {{ date .PeriodEnd }} ({{ .PeriodDays }} days)

FLEET
{{ .Fleet.AgentsOnline }} of {{ .Fleet.AgentsTotal }} agents online now
Average uptime: {{ pct .Fleet.AverageUptimePercent }} over the period

ALERTS
Critical: {{ index .Alerts.CountBySeverity "critical" }}  Warning: {{ index .Alerts.CountBySeverity "warning" }}
Mean time to resolve: {{ dur .Alerts.MeanTimeToResolve }}
{{ if .Alerts.TopRules }}Top firing rules:
{{ range .Alerts.TopRules }}  - {{ .RuleName }}: {{ .Firings }}
{{ end }}{{ else }}No alerts fired this period.
{{ end }}
RESOURCE PEAKS
{{ if .Peaks }}{{ range .Peaks }}  {{ .Hostname }}: cpu {{ pct .MaxCPU }}, memory {{ pct .MaxMemory }}, disk {{ pct .MaxDisk }}
{{ end }}{{ else }}No agents reporting.
{{ end }}
CHECKS
Uptime: {{ pct .Checks.UptimePercent }}{{ if .Checks.WorstLatencyName }}
Worst latency: {{ .Checks.WorstLatencyName }} ({{ num .Checks.WorstLatencyMillis }} ms){{ end }}
{{ if .CVE }}
CVE TOTALS
{{ range .CVE }}  {{ .Hostname }}: critical {{ .Critical }}, high {{ .High }}, medium {{ .Medium }}, low {{ .Low }}
{{ end }}{{ end }}{{ if .Logs }}
LOG VOLUME
{{ range .Logs }}  {{ .Hostname }}: {{ .Rows }} rows
{{ end }}{{ end }}
Generated {{ date .GeneratedAt }} by NexWatch{{ if .HubURL }} - {{ .HubURL }}{{ end }}
`
