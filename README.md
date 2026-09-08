# NexWatch

A unified, self-hosted monitoring platform built with Go and React. Monitor your servers, Docker containers, and infrastructure from a single real-time dashboard.

## Features

- **Real-time Dashboard** -- Live CPU, memory, disk, and network metrics with uPlot charts
- **Multi-Server Monitoring** -- Deploy lightweight agents on any Linux server
- **Docker Container Tracking** -- Monitor container status, CPU, memory, and network per container
- **Alerting Engine** -- Threshold-based alerts with configurable rules, severity levels, duration windows, and cooldowns
- **Black-box Checks** -- Hub-side HTTP/TCP/ICMP probes against any target, with TLS certificate expiry tracking and alert-engine integration
- **CVE Scanning** -- Real vulnerability scanning via Trivy or Grype, covering the host filesystem and running container images, with severity-threshold alerting
- **Multi-Channel Notifications** -- Email (SMTP), Webhook, Slack, Microsoft Teams, PagerDuty, Discord, Telegram, ntfy, and Gotify
- **Agent Management** -- Add/remove agents from the UI, generate install tokens with one click
- **Agent Self-Update** -- Push a checksum- and signature-verified update to one agent or the whole fleet from the UI, with live progress and automatic rollback on failure
- **Automatic Downsampling** -- Raw metrics aggregated to 1m, 5m, and 1h intervals; configurable retention policies
- **Public Status Page** -- An optional, unauthenticated `/status` page exposing only admin-curated checks/agents with custom labels
- **Weekly Email Report** -- A scheduled fleet health digest (alerts, resource peaks, checks, CVEs, log volume) sent through your email channels
- **Prometheus Exposition** -- A bearer-token-gated `/metrics` endpoint for scraping fleet, agent, container, check, and alert metrics into your own monitoring stack
- **Dark Theme Dashboard** -- Cyan and purple accent palette designed for monitoring workflows
- **Single Binary Deployment** -- Hub runs as a single binary with embedded UI; zero external dependencies
- **PocketBase Backend** -- SQLite-powered with real-time subscriptions, built-in auth, and admin panel

## Architecture

NexWatch is a Go monorepo with two binaries sharing protocol types:

- **Hub** -- A custom PocketBase application that handles WebSocket connections from agents, stores metrics in SQLite, evaluates alert rules, dispatches notifications, and serves the embedded React SPA. The hub is the single server component: no Redis, no Postgres, no external queue.

- **Agent** -- A lightweight Go binary (~10 MB) that collects system metrics (CPU, memory, disk, network, OS info) and optionally Docker container stats. It connects to the hub over WebSocket using MessagePack serialization and auto-reconnects with exponential backoff.

- **Frontend** -- A React + TypeScript SPA built with Vite. Uses Zustand for state management, PocketBase SDK for real-time subscriptions, uPlot for high-performance time-series charts, and Tailwind CSS for styling.

```
Agent (Go)                Hub (Go + PocketBase)        Frontend (React)
┌──────────┐  WebSocket   ┌──────────────┐  REST/SSE  ┌─────────┐
│Collectors ├─────────────>│ WS Handler   ├───────────>│Dashboard │
│ CPU, RAM  │  MessagePack │ Metrics Svc  │            │ Charts  │
│ Disk, Net │              │ Alert Engine │            │ Alerts  │
│ Docker    │              │ Notifier     │            │Settings │
└──────────┘              └──────────────┘            └─────────┘
```

Data flows: Collector -> msgpack encode -> WS push -> Hub decode -> batch insert (SQLite WAL) -> alert engine eval -> downsample cron (1m/5m/1h) -> purge per retention policy.

## Quick Start with Docker Compose

The fastest way to run NexWatch:

```bash
git clone https://github.com/CogniDevAI/nexwatch.git
cd nexwatch
docker-compose -f deploy/docker-compose.yml up -d
```

The hub (with embedded dashboard) will be available at `http://localhost:8090`.

On first launch, visit `http://localhost:8090/_/` to create an admin account via PocketBase's admin UI. Then log in to the NexWatch dashboard at `http://localhost:8090`.

## Agent Installation

### From the Dashboard

1. Go to the **Agents** page in the NexWatch dashboard.
2. Click **Add Agent**, enter a name, and click **Generate Token**.
3. Copy the install command and run it on the target server.

### Manual Install

Run the install script on any Linux server (amd64 or arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- \
  --hub ws://YOUR_HUB:8090/ws/agent \
  --token YOUR_TOKEN
```

The script will:
- Download the latest agent binary from GitHub Releases
- Verify the release's GPG signature against the signed `SHA256SUMS` when the
  release publishes one and `gpg` is available, falling back to the per-file
  SHA256 checksum otherwise (see [docs/RELEASE_SIGNING.md](docs/RELEASE_SIGNING.md))
- Create a config file at `/etc/nexwatch/agent.yaml`
- Set up and start a systemd service

**Signature verification flags** (all optional):

| Flag | Env var | Description |
|------|---------|-------------|
| `--require-signature` | `NEXWATCH_REQUIRE_SIGNATURE` | Abort the install if the release signature can't be verified, instead of warning and falling back to the per-file checksum. |
| `--signing-key-url URL` | `NEXWATCH_SIGNING_KEY_URL` | Fetch the release signing public key from a different URL than the default (`scripts/release-signing-key.asc` on `main`). |
| `--signing-key-file PATH` | `NEXWATCH_SIGNING_KEY_FILE` | Use a local public key file instead of fetching one over the network. |

### Agent Configuration

The agent reads configuration from a YAML file, environment variables, and CLI flags (in that priority order):

```yaml
# /etc/nexwatch/agent.yaml
hub_url: "ws://hub-server:8090/ws/agent"
token: "your-agent-token"
interval: 10s
docker_socket: /var/run/docker.sock
docker_update_checks: true
auto_update_enabled: true
update_require_signature: false
cve_scan_enabled: true
cve_scan_interval: 12h
cve_scan_max_images: 10
cve_scan_cache_dir: /var/lib/nexwatch/scanner-cache
collectors_enabled:
  - cpu
  - memory
  - disk
  - network
  - sysinfo
  - docker
  - cve_scan
```

**Environment variables** and their corresponding CLI flags:

| Variable | Flag | Default | Description |
|----------|------|---------|-------------|
| `NEXWATCH_HUB_URL` | `--hub` | `ws://localhost:8090/ws/agent` | Hub WebSocket URL |
| `NEXWATCH_TOKEN` | `--token` | (required) | Agent authentication token |
| `NEXWATCH_INTERVAL` | `--interval` | `10` | Collection interval in seconds |
| `NEXWATCH_DOCKER_SOCKET` | `--docker-socket` | `/var/run/docker.sock` | Docker socket path |
| `NEXWATCH_DOCKER_UPDATE_CHECKS` | `--docker-update-checks` | `true` | Check container images for available registry updates |
| `NEXWATCH_AUTO_UPDATE_ENABLED` | `--auto-update-enabled` | `true` | Accept hub-initiated self-update commands (see [Agent updates](#agent-updates)) |
| `NEXWATCH_UPDATE_REQUIRE_SIGNATURE` | `--update-require-signature` | `false` | Refuse a self-update unless its release signature verifies |
| `NEXWATCH_UPDATE_SIGNING_KEY_URL` | `--update-signing-key-url` | `scripts/release-signing-key.asc` on GitHub | URL to fetch the release signing public key from |
| `NEXWATCH_UPDATE_SIGNING_KEY_FILE` | `--update-signing-key-file` | (none) | Local file with the release signing public key, instead of fetching one |
| `NEXWATCH_CVE_SCAN_ENABLED` | (YAML/env only) | `true` | Enable the CVE scanner (see [CVE scanning](#cve-scanning)) |
| `NEXWATCH_CVE_SCAN_INTERVAL` | (YAML/env only) | `12h` | How often the background scan re-runs (floor: `1h`) |
| `NEXWATCH_CVE_SCAN_MAX_IMAGES` | (YAML/env only) | `10` | Max unique running-container images scanned per run |
| `NEXWATCH_CVE_SCAN_CACHE_DIR` | (YAML/env only) | `/var/lib/nexwatch/scanner-cache` | Scanner vulnerability-DB cache directory |
| `NEXWATCH_ORACLE_HOME` | `--oracle-home` | (none) | `ORACLE_HOME` path for the `oracle` collector (Linux/Unix only) |
| `NEXWATCH_ORACLE_SID` | `--oracle-sid` | (none) | Oracle SID for the `oracle` collector (Linux/Unix only) |

**CLI flags**: `--hub`, `--token`, `--interval`, `--config`, `--docker-socket`, `--docker-update-checks`,
`--auto-update-enabled`, `--update-require-signature`, `--update-signing-key-url`, `--update-signing-key-file`,
`--oracle-home`, `--oracle-sid`.
The `cve_scan_*` keys are YAML-file/env-var only (no dedicated CLI flags) — set
them in `agent.yaml` or via their `NEXWATCH_*` environment variables.

### Uninstalling the Agent

```bash
sudo systemctl stop nexwatch-agent
sudo systemctl disable nexwatch-agent
sudo rm /etc/systemd/system/nexwatch-agent.service
sudo systemctl daemon-reload
sudo rm /usr/local/bin/nexwatch-agent
sudo rm -rf /etc/nexwatch
sudo userdel nexwatch
```

### Windows agent

**Requirements**: Windows 10/11 or Windows Server 2016+, amd64. An elevated
(Administrator) PowerShell prompt for install/uninstall — the agent runs as
a Windows service under LocalSystem afterward, not interactively.

**Install** — download-then-run rather than a one-line `irm | iex`, so the
script can be reviewed before it runs with the privileges an agent install
requires:

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.ps1 -OutFile install-agent.ps1
.\install-agent.ps1 -Hub wss://YOUR_HUB:8090/ws/agent -Token YOUR_TOKEN
```

The Agents page's **Add agent** modal generates this exact command (Windows
tab, alongside the existing Linux one) with your hub URL and token already
filled in. The script downloads the release, verifies its checksum (and
signature when `-RequireSignature` is passed and `gpg.exe` is on `PATH`),
extracts `nexwatch-agent.exe` to `C:\Program Files\NexWatch`, writes
`%ProgramData%\NexWatch\agent.yaml`, registers and starts the
`NexWatchAgent` Windows service. `scripts/uninstall-agent.ps1` reverses
this (pass `-RemoveData` to also delete `%ProgramData%\NexWatch`).

**Service management** — the agent registers itself as a normal Windows
service (`cmd/agent/service_windows.go`), so it responds to the usual
tools:

```powershell
Get-Service NexWatchAgent
Restart-Service NexWatchAgent
Stop-Service NexWatchAgent
```

It can also be managed directly via its own `--service` subcommand
(what the installer itself calls): `nexwatch-agent.exe --service
install|uninstall|start|stop|status`, with `install` accepting `--config
<path>` (defaults to `%ProgramData%\NexWatch\agent.yaml`). The service is
configured to restart itself 2 seconds after an unexpected failure (the
Windows equivalent of the Linux systemd unit's `Restart=always`), and
Start/Stop events are best-effort written to the Windows Event Log under
the `NexWatchAgent` source.

**What is and is not collected on Windows** — every collector runs except
`vulnerabilities` (POSIX file permissions and SUID bits, meaningless on
NTFS/ACLs) and `oracle` (assumes a Linux/Unix Oracle client install); the
agent logs one line at startup naming anything disabled and why
(`internal/agent/platform.Supported`). Two collectors have Windows-specific
implementations rather than simply being unavailable:

- **`services`** lists Windows services via the Service Control Manager
  (`golang.org/x/sys/windows/svc/mgr`) instead of systemd units. A service
  that is `Stopped` while configured to start automatically maps to the
  same "failed" state a Linux systemd unit reports, so a `service_failed`
  alert rule targeting it fires the same way on either OS; a manually
  stopped or disabled service does not.
- **`hardening`** reports a smaller, honest set of checks: Windows
  Firewall's per-profile state (`netsh advfirewall`), Windows Defender
  real-time protection (`Get-MpComputerStatus`, skipped gracefully if
  PowerShell or the Defender module isn't present), and whether Remote
  Desktop is enabled (read directly from the registry). The
  POSIX-specific checks (SSH root login, `/etc/passwd`/`/etc/shadow`
  permissions, extra UID-0 accounts) report `skip` on Windows, the same
  way they already do on macOS.

**Docker**: `docker_socket` defaults to Docker Desktop's named pipe,
`npipe:////./pipe/docker_engine`, instead of a Unix socket path — the
`docker` and `cve_scan` collectors and the Docker container actions
(start/stop/restart) all work against it unchanged.

**Self-update**: a Windows agent updates from a `.zip` release asset
(`nexwatch-agent_<version>_windows_amd64.zip`) instead of `.tar.gz`, and
handles one Windows-specific edge case: if the running executable's file
name is transiently locked (e.g. by antivirus scanning) when the update
tries to swap in the new binary, it falls back to scheduling the
replacement via `MOVEFILE_DELAY_UNTIL_REBOOT` and keeps running the old
version until the host is next restarted, rather than failing the update
outright.

**Logs**: written to a rotating `%ProgramData%\NexWatch\agent.log` (capped
at 10 MiB, with one `agent.log.1` backup) rather than a console — the
Service Control Manager gives a service process no console to write to.
Tail it the same way you'd tail journald output on Linux:

```powershell
Get-Content "$env:ProgramData\NexWatch\agent.log" -Tail 50 -Wait
```

> **Note**: `install-agent.ps1`/`uninstall-agent.ps1` and the Go-side
> Windows service integration were authored and cross-compiled
> (`GOOS=windows go build`/`go vet`) on a machine with no Windows or
> PowerShell available, and have not been executed against a real Windows
> host. Run `scripts/test-install-agent.ps1` on an actual Windows machine
> to self-check the installer's pure helper functions before relying on it
> in production.

## Agent updates

The hub can push a self-update to any connected agent instead of requiring a
manual re-run of `install-agent.sh` on every host.

### How it works

1. An operator (Agents page, per-row "Update" action, or a host's own detail
   page) or an admin ("Update all outdated") requests a version.
2. The hub sends an `update` command over the agent's WebSocket connection
   and waits for its immediate acknowledgement (`agents.update_status`
   becomes `started`).
3. The agent downloads `nexwatch-agent_<version>_<os>_<arch>.tar.gz`
   (`.zip` on Windows — see [Windows agent](#windows-agent)) from
   `<agent_release_base_url>/v<version>/`, verifies its SHA-256 checksum
   against the release's `SHA256SUMS` (falling back to the per-file
   `<asset>.sha256` when `SHA256SUMS` itself isn't published), and verifies
   the GPG signature over `SHA256SUMS` whenever a `SHA256SUMS.asc` is
   published — using the same signing key
   `scripts/install-agent.sh --require-signature` trusts by default. A
   published signature that fails to verify always aborts the update,
   regardless of `update_require_signature`; that setting only controls
   whether a *missing* signature is tolerated.
4. It extracts the `nexwatch-agent` binary, sanity-runs `<new> --version` to
   confirm it reports the requested version, then atomically replaces the
   running executable: the current binary is renamed to `<path>.previous`
   before the new one takes its place, and any failure past that point
   rolls the rename back so the agent is never left without a working
   binary.
5. On success the agent re-execs itself in place (`syscall.Exec`) so the
   update takes effect immediately; if re-exec isn't possible it exits 0
   instead, which is why the shipped systemd unit uses `Restart=always`
   (not `on-failure` — that flag does not restart on a clean exit).
   Throughout, the agent streams `downloading` → `verifying` → `installing`
   → `restarting` → `done`/`failed` back to the hub, visible as a live chip
   on the Agents page and the host's own detail page; on the next REGISTER
   after restarting, the hub also reconciles `update_status` to `done` if
   the reported version now matches what was requested.

**Rollback**: a failed install restores `<path>.previous` automatically. If
an update completes but the new version misbehaves, stop the agent, restore
`sudo mv /usr/local/bin/nexwatch-agent.previous /usr/local/bin/nexwatch-agent`,
and restart it.

**Offline / air-gapped fleets**: point the `agent_release_base_url` setting
at an internal mirror that serves the same layout
(`<mirror>/v<version>/nexwatch-agent_<version>_<os>_<arch>.tar.gz` plus
`SHA256SUMS`, and optionally `SHA256SUMS.asc`) — agents never need to reach
GitHub directly for a self-update.

### Config keys (agent)

| Key | Default | Description |
|-----|---------|-------------|
| `auto_update_enabled` | `true` | When `false`, the agent refuses every `update` command with a clear error instead of downloading anything. |
| `update_require_signature` | `false` | Fail an update unless a valid GPG signature over `SHA256SUMS` was verified. |
| `update_signing_key_url` | the repo's `scripts/release-signing-key.asc` on GitHub | Where to fetch the release signing public key from. |
| `update_signing_key_file` | (none) | Read the signing key from a local file instead of fetching it. |

### Settings (hub, admin)

| Key | Default | Description |
|-----|---------|-------------|
| `agent_release_base_url` | `https://github.com/CogniDevAI/nexwatch/releases/download` | Base URL agents download release assets from. |
| `agent_target_version` | (none) | When set, agents older than this version show an "Update available" badge; otherwise the badge compares against the latest published GitHub release. |

### API

- `POST /api/custom/agents/{id}/update` (operator+) — `{"version": "0.9.1"}`.
- `POST /api/custom/agents/update-all` (admin) — `{"version": "0.9.1", "only_outdated": true}`, fanned out to every connected agent.
- `GET /api/custom/agents/latest-version` — the latest published release, cached for one hour.

See `docs/openapi.yaml` for full request/response schemas.

## Configuration

### Hub

The hub is configured via CLI flags:

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--http` | — | `0.0.0.0:8090` | HTTP listen address |
| `--retention` | — | `30` | Metric data retention in days |
| `--bootstrap-agent-token` | `NEXWATCH_BOOTSTRAP_AGENT_TOKEN` | _(none)_ | If set, ensure a `bootstrap` agent exists with this token's hash on startup (dev/docker-compose convenience). |
| `--bootstrap-admin` | `NEXWATCH_BOOTSTRAP_ADMIN` | _(none)_ | If set to `EMAIL:PASSWORD`, ensure a `users` record with role `admin` exists for that email on startup (no-op if it already exists). |
| `--log-format` | `NEXWATCH_LOG_FORMAT` | `text` | Structured log output format: `text` (human-readable) or `json` (one JSON object per line, for log aggregators). The flag takes precedence over the env var. |

PocketBase subcommands (e.g. `./nexwatch-hub superuser upsert EMAIL PASSWORD`) also work — the hub only forces the `serve` subcommand when no other subcommand is given.

### Health check

`GET /healthz` is an unauthenticated liveness/readiness endpoint (outside
the `/api/custom` auth group), intended for load balancers, uptime
monitors, and container orchestrators:

```json
{
  "status": "ok",
  "version": "1.2.3",
  "uptime_seconds": 4213,
  "db": "ok",
  "agents_online": 3
}
```

`db` performs a trivial database query as its health check; if it fails,
`status`/`db` become `"error"` and the endpoint returns HTTP 503 instead of
200.

### Logging

Every hub request gets an `X-Request-ID` (propagated from an incoming
`X-Request-ID` header, or generated if absent) echoed back on the response,
and one structured access-log line (`method`, `path`, `status`,
`duration_ms`, `request_id`). Internal hub packages (`ws`, `alerts`,
`metrics`, `notify`, `threaddump`, `api`) log through
[`log/slog`](https://pkg.go.dev/log/slog) with structured key/value fields
(`agent_id`, `rule_id`, `alert_id`, etc.) rather than free-form strings —
set `--log-format=json` (or `NEXWATCH_LOG_FORMAT=json`) to emit them as
JSON for a log aggregator.

### API documentation

The hub serves its own OpenAPI 3.1 specification (authenticated, like the
rest of `/api/custom`) at `GET /api/custom/openapi.yaml`, describing every
custom route's parameters, auth, and request/response schemas. The source
lives at [`docs/openapi.yaml`](docs/openapi.yaml).

### Scheduled backups

The hub backs up its `pb_data` directory automatically via a cron job
(`app.Cron()`/`app.CreateBackup`), using `zip` archives under
`pb_data/backups`. Configuration lives in the `settings` collection
(editable from the dashboard or via the PocketBase API), not a CLI flag:

| Settings key | Default | Description |
|--------------|---------|--------------|
| `backups_enabled` | `true` | Whether the scheduled backup job runs at all. |
| `backup_cron` | `0 3 * * *` | Standard 5-field cron expression for when backups run. Falls back to the default if invalid. |
| `backup_keep` | `7` | How many of the most recent backups to retain; older ones are deleted after each run. `0` or less disables pruning. |

### Network metrics

The `network` metric type reports both cumulative counters
(`bytes_recv`/`bytes_sent`, kept for backwards compatibility) and current
throughput (`bytes_recv_per_sec`/`bytes_sent_per_sec`, added by the agent's
network collector). The hub's `/api/custom/metrics` endpoint exposes the
throughput series as `network_rx_rate`/`network_tx_rate` (bytes per
second) alongside the existing `network_rx`/`network_tx` cumulative-MB
series — prefer the `*_rate` series for anything labeled as a rate.

### Settings (via Dashboard)

From the **Settings** page in the dashboard you can configure:

- **Data Retention** -- How many days of raw metric data to retain (7-90 days)
- **Default Collection Interval** -- Recommended interval for new agents
- **Scheduled Backups** -- Enable/disable, cron schedule, and retention count (see above)

## Alerting

The alert engine (`internal/hub/alerts`) evaluates enabled `alert_rules`
every 30 seconds and manages an idempotent firing/resolved state machine per
(rule, agent) pair, so a persistently-breaching rule keeps updating a single
`alerts` record instead of spamming duplicates.

### Targeting

An `alert_rules` record targets agents via `agent_id` and/or `target_tags`:

- `agent_id` set -- the rule applies to that agent only.
- `agent_id` empty and `target_tags` non-empty -- the rule applies to every
  agent that has ANY of those tags (`agents.tags`).
- Both empty -- the rule applies to every agent.

`agent_id`-scoped rules are evaluated regardless of the agent's status.
Tag-scoped and global rules are evaluated against online agents only,
except `agent_offline` rules (see below), which must also see offline
agents.

### New rule types

`alert_rules.metric_type` additionally accepts:

- **`process_down`** -- breaches when no process in the agent's latest
  process list has a name or command line containing `alert_rules.target`
  (case-insensitive substring).
- **`service_failed`** -- breaches when the agent's latest systemd service
  list shows `alert_rules.target` in a failed/inactive/dead state, or does
  not list it at all (a stopped service simply disappears from the list).
- **`agent_offline`** -- breaches when the agent's status is `offline`, or
  its `last_seen` is older than the 90-second heartbeat timeout. This rule
  type ignores `condition`/`threshold`.

All three still honor `duration` (how long the condition must hold before
firing) the same way ordinary metric-threshold rules do.

A fourth, **`log_match`**, targets shipped log entries instead of metrics
or process/service state -- see [Logs](#logs) for its full contract.

### Silences (maintenance windows)

A `silences` record (`name`, `reason`, optional `agent_id`, optional
`tags`, `starts_at`, `ends_at`) suppresses notifications for the agents it
covers during `[starts_at, ends_at)`, without losing track of the incident:
the engine still tracks state and keeps the `alerts` record current, it
just sets `alerts.silenced = true` and skips dispatch. A silence matches an
agent when its `agent_id` equals the agent, ANY of its `tags` overlap the
agent's tags, or both are empty (a global silence). Once the window ends,
the next evaluation notifies immediately (subject to the normal cooldown
afterwards) and clears `silenced`. `GET /api/custom/silences/active` lists
currently-active silences together with the ids of every agent they cover.

### Acknowledgement

`POST /api/custom/alerts/{id}/ack` (operator role or higher) records
`acknowledged_at`/`acknowledged_by` on a firing alert; `.../unack` clears
them. An acknowledged alert is never re-notified after its cooldown and is
never escalated, even while it keeps breaching. Resolving the alert later
keeps both fields for history.

### Escalation

An `alert_rules` record can set `escalation_channels` (a second set of
notification channels) and `escalation_after` (seconds, `0` disables it).
Once an unacknowledged, unsilenced alert has been firing for at least
`escalation_after` seconds, the engine dispatches once to
`escalation_channels` with an "Escalated:" message prefix and records
`alerts.escalated_at` -- at most once per firing episode.

### Notification content

Rendered notifications (email, Telegram, Discord, webhook, and the rest of
the channels below) include the agent's tags and the rule's target (when
set) in the message body, plus an acknowledgement/escalation line when
either has happened, so an operator reading any channel sees the same
context as the dashboard. A resolved alert dispatches its own notification
(distinct from the firing one), which channels like PagerDuty rely on to
close out an incident.

### Notification channels

Each `notification_channels` record's `config` (JSON) holds the fields
below. Fields not listed are optional.

| Channel | Type value | Required config | Notes |
|---|---|---|---|
| Email (SMTP) | `email` | `host`, `from`, `to` | `port` defaults to 587; `username`/`password` enable SMTP auth |
| Webhook | `webhook` | `url` | `method` defaults to POST; `headers` (JSON object) are added to the request |
| Telegram | `telegram` | `bot_token`, `chat_id` | Uses the Telegram Bot API's `sendMessage` |
| Discord | `discord` | `webhook_url` | Sends a Discord embed, colored red (firing) or green (resolved) |
| Slack | `slack` | `webhook_url` | Sends a Slack Incoming Webhook message with severity-colored blocks |
| Microsoft Teams | `teams` | `webhook_url` | Sends an Adaptive Card via an Incoming Webhook connector |
| PagerDuty | `pagerduty` | `routing_key` | Events API v2; fires `trigger`/`resolve` using the alert's ID as `dedup_key` |
| ntfy | `ntfy` | `topic` | `server_url` defaults to `https://ntfy.sh`; `token` for a protected topic; `priority` (1-5) overrides the severity-based default |
| Gotify | `gotify` | `server_url`, `app_token` | `priority` overrides the severity-based default (0-10 scale) |
| Browser push | `webpush` | none required | `audience` (`all`/`admins`/`operators`, default `all`) selects which subscribed users' devices receive it -- see "Browser notifications and PWA" below |

A channel's severity (`warning`/`critical`) is not a column on the `alerts`
record itself -- it is recovered from the leading `[severity]` tag that the
alerts engine prefixes onto every rendered message, since severity only
lives on the `alert_rules` record and channels only receive the fired
alert. `POST /api/custom/notifications/{id}/test` sends a synthetic test
alert through a channel; a channel whose config is missing a required
field (e.g. PagerDuty without `routing_key`) returns an HTTP 400 with a
message naming the missing field, instead of a generic 500.

## Checks (black-box monitoring)

The checks scheduler (`internal/hub/checks`) runs periodic HTTP/TCP/ICMP
probes directly from the hub against arbitrary targets -- a URL, a
`host:port` pair, or a bare hostname/IP -- independent of the agent fleet.
Each enabled `checks` record gets its own goroutine on a jittered
(&plusmn;10%) interval; creating, editing, or deleting a check takes effect
immediately (no hub restart) via PocketBase record hooks.

### Check types and fields

| Field | Applies to | Default | Notes |
|---|---|---|---|
| `type` | all | -- | `http`, `tcp`, or `icmp` |
| `target` | all | -- | URL (http), `host:port` (tcp), hostname/IP (icmp) |
| `interval_seconds` | all | 60 | Minimum 10 |
| `timeout_seconds` | all | 5 | |
| `failures_before_down` | all | 2 | Consecutive failed attempts required before the check flips to `down`; a single success flips it back to `up` immediately |
| `method` | http | `GET` | `GET` or `HEAD` |
| `expected_status` | http | 200 | |
| `expected_body_contains` | http | -- | Optional substring match against the response body (reads at most 1 MiB) |
| `verify_tls` | http | `true` | `false` skips certificate verification but still records its expiry |
| `tls_expiry_warn_days` | http | 14 | Threshold behind the UI's "certificate expiring soon" badge |

Every probe attempt is persisted as a `check_results` row (`status`,
`latency_ms`, `status_code`, `error`, `tls_expires_at`, `checked_at`).
`status` on that row is the *debounced* state, not the raw attempt outcome
-- see `failures_before_down` above. `check_results_retention_days`
(default 7, a `settings` key like the metrics retention setting) controls
how long history is kept; a background sweep purges older rows hourly.

ICMP checks shell out to the system `ping` binary (`-c 1`, with `-t` on
macOS/BSD or `-W` on Linux for the timeout) -- the hub host needs `ping`
installed and permitted to send ICMP (typically already true, but some
minimal containers omit it).

### API

- `GET /api/custom/checks/summary` -- every check's current status,
  latency, TLS expiry, and 24h/7d uptime.
- `POST /api/custom/checks/{id}/run` (operator role or higher) -- runs a
  check immediately, on demand.
- `GET /api/custom/checks/{id}/results?range=24h|7d` -- a latency/status
  time series for charting.

### Alerting integration

Two additional `alert_rules.metric_type` values target `checks` records
instead of agents, reusing the same firing/resolved state machine as every
other rule type (the check's id simply stands in for an agent id in the
engine's internal state key):

- **`check_down`** -- breaches when the check's latest debounced status is
  `down`.
- **`cert_expiry`** -- breaches when the latest recorded `tls_expires_at`
  is within `threshold` days of now (`threshold` is reused as "warn within
  N days" rather than a metric comparison for this rule type).

`alert_rules.check_id` set targets that one check; left empty, the rule
applies to every check (one alert per breaching check). A check-triggered
alert has no agent at all, so `alerts.agent_id` is optional and
`alerts.check_id` is set instead -- existing agent-based silences do not
apply to check-based rules.

## Logs

The agent ships log lines from journald and/or plain files to the hub,
where they're stored, searchable, retained on a schedule, and available as
a live tail. This is separate from metrics: logs are free-text events
(errors, warnings, application output), not numeric time series.

### Agent configuration

| Key | Default | Notes |
|---|---|---|
| `logs_enabled` | `true` | Disables log shipping entirely when `false` |
| `logs_max_lines_per_sec` | 200 | Per-agent rate limit; excess lines are dropped and counted, never queued |
| `log_sources` | one journald source, if `journalctl` exists | See below |

`log_sources` is a list of:

```yaml
log_sources:
  - type: journald          # or "file"
    units: []                # journald only; empty = every unit
    priority_max: warning    # journald only; empty = every priority
  - type: file
    path: /var/log/myapp.log # file only
```

When `log_sources` is left empty (the common case), the agent defaults to
one journald source covering every unit at `warning` priority or more
severe -- but only when the `journalctl` binary is present on `PATH`. A
host with no systemd journal (e.g. this project's own macOS dev machines)
ships nothing by default rather than spamming its own log with
"journalctl: command not found" every cycle. Both env vars
(`NEXWATCH_LOGS_ENABLED`, `NEXWATCH_LOGS_MAX_LINES_PER_SEC`) and the YAML
config file work the usual way (flags/env override the file); `log_sources`
itself is only settable via the YAML file.

**Permissions:** reading journald and `/var/log` requires the agent's
service user to belong to the `systemd-journal` and `adm` groups
respectively -- `scripts/install-agent.sh` and
`deploy/nexwatch-agent.service` add both automatically (in every mode,
including Oracle mode, additive to its existing `dba`/`docker` group
handling) when those groups exist on the host.

Journald lines are parsed from `journalctl -f -o json --since now [-u
unit...] [-p priority]`, extracting the message, unit
(`_SYSTEMD_UNIT`, falling back to `SYSLOG_IDENTIFIER`), syslog priority
(mapped to `error`/`warning`/`info`/`debug`), and timestamp
(`__REALTIME_TIMESTAMP`). File sources are tailed by polling every 500ms,
starting at the current end of the file (existing content is never
replayed), with no third-party dependencies; truncation and rotation
(logrotate's rename-then-recreate) are both detected and handled by
reopening the file from the start. A file-tailed line's level is guessed
from an `error|warn(ing)|info|notice|debug|trace` token in the text
(defaulting to `info`), since a plain file carries no structured level of
its own.

Entries are batched (flushed every 2s or 200 entries, whichever comes
first) before being sent to the hub over the same WebSocket connection as
metrics, as a new `LOGS` message type.

### Storage and retention

The `logs` collection stores `agent_id`, `ts`, `source`
(`"journald"` or `"file:<path>"`), `unit`, `level`, `message` (capped at
8 KiB), and `fields` (a small JSON map of extra attributes, e.g.
journald's `SYSLOG_IDENTIFIER`). Writes are server-only; reads (list/view,
including realtime subscriptions for live tail) are open to any
authenticated user.

Two independent settings (both `settings` collection keys, purged/enforced
by the same hourly sweep the metrics downsampler already runs):

- `logs_retention_days` (default 3) -- rows older than this are purged.
- `logs_max_rows` (default 2,000,000) -- a hard cap trimmed back down by
  deleting the oldest rows once exceeded, independent of age.

### Search API

- `GET /api/custom/logs?agent_id=&level=&unit=&q=&since=&until=&limit=&before=`
  -- newest first, `limit` capped at 500 (default 100). `q` is a
  case-insensitive substring match against the message. `since`/`until`
  are unix milliseconds. `before` is an opaque cursor (the previous
  response's `next_before`) for "load older" pagination.
- `GET /api/custom/logs/units?agent_id=` -- distinct units seen in the
  last 24h, for populating a unit filter dropdown.

Live tail doesn't use a custom endpoint -- the UI subscribes directly to
PocketBase realtime on the `logs` collection with a filter matching the
current search filters.

### `log_match` alert rule

Breaches when the count of an agent's log entries matching
`alert_rules.target` in the last `duration` seconds is &ge; `threshold`.
`target` is either a case-insensitive substring or a `/regex/` pattern
(delimited by slashes); `condition` is ignored (always "count >=
threshold" -- the UI hides the Condition field for this rule type). The
substring case is matched via the `logs` collection's indexed
case-insensitive `LIKE` filter; a regex is evaluated in Go against a
candidate set fetched by agent + time range alone, capped at 5,000 rows
per evaluation cycle.

### UI

The **Logs** page (`/logs`, in the sidebar after Checks) and a host
detail's **Logs** tab share one component: an agent selector (page only;
the host tab is locked to that host), level chips, a unit filter, a search
box, time range presets (15m/1h/24h/custom), a monospace log list (newest
first, level colored, hover to see the source, click a line with
attributes to expand its `fields`), "Load older" pagination, a copy-line
action, and a "Live" toggle that subscribes to realtime for the current
filters and prepends new entries -- auto-pausing when you scroll away from
the top, with a "N new lines" pill to resume. The list is capped at 2,000
rendered rows with a notice, so a very active filter stays fast to scroll.

## Docker container actions

Operators can start, stop, or restart a Docker container reported by an
agent directly from the Docker tab on a host's detail page.

- `POST /api/custom/agents/{id}/docker/{containerId}/{action}` (operator
  role or higher) validates `action` (`start`, `stop`, or `restart`,
  strictly allowlisted -- both hub and agent enforce this independently)
  and `containerId` (12-64 hex characters), requires the target agent to
  be connected, and dispatches a `docker_action` command over the agent's
  WebSocket connection. The hub waits synchronously (up to 35s, via
  `internal/hub/commands.Broker`) for the agent's result and returns
  `{ok, state, error}`: `200` on success, `502` when the agent reports a
  failure or is not connected, `504` on timeout.
- The agent (`internal/agent/command/docker_action.go`) re-validates the
  same allowlist and container id shape before touching the Docker
  daemon, runs the action with a 30s timeout, and reports the container's
  resulting state (`running`, `exited`, ...) back to the hub.
- Every call -- success or failure -- is written to the [audit
  log](#audit-log) as `docker.start` / `docker.stop` / `docker.restart`.

### Image update detection

The docker collector (`internal/agent/collector/docker.go`) additionally
compares each container's locally cached image digest against its
registry's current manifest digest:

- `image_digest` -- the image's local `RepoDigest` (from `docker image
  inspect`).
- `remote_digest` -- the registry's current manifest digest for the same
  reference (`DistributionInspect`).
- `update_available` -- `true` when the two differ.

These are skipped entirely for an image with no registry reference (e.g.
one built locally with no `RepoDigest`), so a locally-built image never
shows a false "update available." A registry lookup is cached per image
reference for 1 hour to avoid hammering registries on every collection
cycle, and a registry that can't be reached never fails the collection --
it's logged at debug (`update_check_error`) and the container simply
reports no update-availability fields for that cycle. Disable the feature
entirely with `docker_update_checks: false` (see [Agent
Configuration](#agent-configuration)); it defaults to `true`.

The Docker tab shows an "Update available" badge (hover for the short
registry digest) per affected container, plus an "Updates available"
filter toggle above the table.

**Bug fixes found while verifying this feature end-to-end** against a real
agent, hub, and Docker daemon: the `docker_containers` collection had never
actually been populated in production. `metrics.Service.IngestMetrics` was
passing the docker collector's whole per-cycle payload
(`{available, container_count, containers: [...]}`) to
`upsertDockerContainer` instead of one entry from its `containers` array,
the collector's container-id key (`id`) never matched what
`upsertDockerContainer` read (`container_id`), and the collector's
CPU/memory/network stat keys (`mem_usage`/`mem_limit`/`net_rx`/`net_tx`)
never matched the collection's actual column names
(`memory_usage`/`memory_limit`/`network_rx`/`network_tx`). All three are
fixed as part of this change; the UI's `DockerContainer` type had its own
matching mismatches (`agent` vs. `agent_id`, `cpu`/`memory_used` vs.
`cpu_percent`/`memory_usage`), also fixed here.

## CVE scanning

The `cve_scan` collector (`internal/agent/collector/cvescan.go`) runs a real
CVE vulnerability scan of the agent host — this is a distinct feature from
the `vulnerabilities` collector (the host detail page's **Misconfigurations**
tab), which only checks for local misconfigurations (world-writable files,
unusual SUID binaries, services running as root, weak file permissions) and
never contacts a CVE database. **CVE scan** looks up known CVEs against the
packages actually installed on the host and, when Docker is reachable, the
images of its running containers.

### Supported scanners

The agent detects [Trivy](https://github.com/aquasecurity/trivy) first, then
falls back to [Grype](https://github.com/anchore/grype), by checking `PATH`.
Install one of them on every host you want scanned — the agent never installs
or updates a scanner itself:

| Distro | Install |
|---|---|
| Debian / Ubuntu | `curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh \| sh -s -- -b /usr/local/bin` (Trivy), or `curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh \| sh -s -- -b /usr/local/bin` (Grype) |
| RHEL / CentOS / Fedora | Same install-script one-liners as above, or `rpm`-based install per the [Trivy](https://aquasecurity.github.io/trivy/latest/getting-started/installation/) / [Grype](https://github.com/anchore/grype#installation) docs |
| Alpine | `apk add trivy` (Trivy ships an official Alpine package); Grype via the same install script (statically linked, no glibc dependency) |

When neither binary is found, the collector still reports a `cve_scan`
metric with `available: false` and a clear `error` naming both install
options — the **CVE scan** tab shows the same install commands.

### What gets scanned

- **Host filesystem**: `trivy rootfs --scanners vuln --format json --quiet
  --timeout 10m /` or `grype dir:/ -o json`.
- **Running container images** (best effort — skipped entirely when Docker
  is unreachable, never counted as an error): `trivy image --format json
  --quiet <ref>` or `grype <ref> -o json`, for up to `cve_scan_max_images`
  (default 10) unique image references among currently running containers.

### Scheduling and caching

Scanning a whole filesystem is comparatively expensive, so it never runs on
the agent's normal collection cadence. Instead it runs on its own
background schedule: a first scan 2 minutes after the agent starts, then
every `cve_scan_interval` (default `12h`, floor `1h`) thereafter, each run
bounded by a 15-minute hard timeout. Every regular collection cycle returns
the most recently completed scan immediately (never blocking on a new scan)
— once that cached result is older than 2x the interval, the payload is
marked `stale: true` so the UI can flag it without hiding the last-known
data. The scanner's own vulnerability database is cached in
`cve_scan_cache_dir` (default `/var/lib/nexwatch/scanner-cache`, exported to
the scanner subprocess as `TRIVY_CACHE_DIR`/`GRYPE_DB_CACHE_DIR`) so it
persists across runs instead of re-downloading every time. The systemd unit
declares `StateDirectory=nexwatch`, which creates and owns
`/var/lib/nexwatch` for the service user even under `ProtectSystem=strict`.

### Payload and API

`GET /api/custom/agents/{id}/cve` (any authenticated role) returns the
latest `cve_scan` metric payload verbatim:

```json
{
  "scanner": "trivy",
  "scanner_version": "0.50.1",
  "db_updated_at": "2026-09-01T00:12:00Z",
  "scanned_at": "2026-09-05T08:00:00Z",
  "duration_ms": 42300,
  "stale": false,
  "available": true,
  "error": "",
  "targets": [
    {
      "kind": "host",
      "ref": "/",
      "counts": {"critical": 1, "high": 3, "medium": 5, "low": 2, "unknown": 0},
      "fixable": 6,
      "findings": [
        {
          "id": "CVE-2024-0001",
          "severity": "critical",
          "package": "openssl",
          "installed": "3.0.2-0ubuntu1.10",
          "fixed": "3.0.2-0ubuntu1.12",
          "title": "openssl: heap buffer overflow",
          "target": "Ubuntu 22.04 (ubuntu)"
        }
      ]
    }
  ],
  "totals": {"critical": 1, "high": 3, "medium": 5, "low": 2, "unknown": 0, "fixable": 6}
}
```

Findings are deduplicated by `(id, package, target)`, sorted most-severe
first, and capped at the top 200 per target. The dashboard summary
(`GET /api/custom/dashboard`) also includes a per-agent `cve_totals` field
(the same severity counts) when a scan is available, at no extra query cost.

### Alerting

A `cve_count` alert rule (grouped under "Security" in the rule form)
breaches when the agent's latest `cve_scan` totals satisfy
`condition`/`threshold` against critical+high severity findings combined,
or critical-only when the rule's scope is set to "Critical only"
(`internal/hub/alerts/engine.go`'s `evalCveCount`). Like every other
resource-metric rule type it uses `condition`/`threshold`/`duration`
normally; it never evaluates while no scan is available yet (scanning
disabled, no scanner installed, or the first run hasn't completed).

### UI

The host detail page's **CVE scan** tab shows severity summary tiles
(critical/high/medium/low, plus a fixable count), the scanner name/version/
DB build time/last-scanned time (with a "Stale" badge when applicable), and
one findings table per scanned target (host, then each image) with
severity/fixable/search filters. A CVE id links out to its NVD detail page.
The existing **Vulnerabilities** tab is renamed **Misconfigurations** in
this UI (no data or API change — same `vulnerabilities` metric type) to stop
implying it does CVE scanning, which it never has.

## Audit log

Every operator/admin-triggered mutation across the hub is recorded to the
`audit_log` collection (`internal/hub/audit`): `actor_id`/`actor_email`/
`actor_role`, `action` (a dotted name like `docker.restart`,
`alert_rule.update`, `agent.token.regenerate`, `threaddump.request`,
`alert.ack`/`alert.unack`, `check.run`, `notification_channel.*`,
`silence.*`, `check.*`, `user.*`, `agent.delete`), `target_type`/
`target_id`, `agent_id` (when applicable), `details` (JSON, action-specific
context), `result` (`success`/`failure`), `ip`, and `request_id`.

- Custom `/api/custom/*` mutating routes (Docker actions, agent token
  regeneration, thread dump requests, alert acknowledgement, check
  run-now) call `audit.Record` directly from their handlers.
- Collections mutated straight through PocketBase's default REST CRUD API
  (`alert_rules`, `notification_channels`, `silences`, `checks`, `users`,
  and agent *deletion* only) are audited via request-scoped hooks
  (`OnRecordCreateRequest`/`OnRecordUpdateRequest`/`OnRecordDeleteRequest`)
  bound in `audit.RegisterHooks` -- these only ever fire for a genuine
  incoming HTTP request, so an internal engine write (the alert engine
  flipping `alerts.status`, the checks scheduler saving `check_results`,
  the WebSocket hub updating `agents.last_seen`) is never logged as if a
  person did it.
- **Secrets are never stored.** `notification_channels.config` (webhook
  URLs, bot tokens, API keys, ...) is redacted to just its field *names*
  (`config_fields: ["url"]`, never the value); an agent's `token`/
  `token_hash` is never included in its audit details either.
- Reads: `list`/`view` require the operator role or higher (both operator
  and admin see everything); writes are server-only. The UI reads this
  directly via the SDK (`pb.collection("audit_log").getList(...)`, paged)
  rather than a custom endpoint -- the collection's own rule is
  sufficient, matching how Alert history/Checks/Silences already read
  their collections. `/settings/audit` (a Settings sub-page, visible to
  operator role or higher) lists entries with time/actor/action/target/
  agent/result/request id columns, filters by action and actor, and an
  expandable row for the details JSON.
- Retention: entries older than `audit_retention_days` (default 180, a
  `settings` key like the metrics/checks retention settings) are purged
  hourly by the same background sweep that purges old metrics
  (`metrics.Downsampler`).

## Status page

An optional public, unauthenticated status page for customers — no login,
no internal hostnames, ids, or check targets, only what an admin explicitly
chooses to show. Disabled by default.

### Settings

| Key | Default | Meaning |
|---|---|---|
| `status_page_enabled` | `false` | Serves `GET /api/public/status` and `/status` when `true`; both 404 while `false`. |
| `status_page_title` | `"Service status"` | Page heading. |
| `status_page_description` | `""` | Optional one-line description under the title. |
| `status_page_items` | `[]` | JSON array of `{type: "check"\|"agent", id, label}` — an allowlist. Only these items, under the label you type, ever appear publicly. |
| `status_page_show_uptime_days` | `30` | Length of the daily uptime bar (1-90 days). |

Configure it from **Settings → Status page** (admin only): toggle it on, set
a title/description, add checks and/or agents with a custom public label,
and pick the uptime-bar length. The panel links straight to `/status` once
enabled.

### API

`GET /api/public/status` — unauthenticated, `Cache-Control: max-age=30`,
rate-limited to 60 requests/minute per client IP. Returns 404 while
disabled. Never includes a check's target/URL, an agent's hostname/IP, or
either's raw record id — only the `label` an admin typed into
`status_page_items`. Each item reports `status` (`operational` / `degraded`
/ `down` / `unknown`), `uptime_24h`/`uptime_7d`/`uptime_30d`, a `daily`
history array (`date`/`uptime`/`incidents`), and — for checks — `latency_ms`.
An overall page-level `status` is the worst of every item's status.

A check's status comes from the same debounced scheduler snapshot the
Checks page reads, with `degraded` added when its TLS certificate is
expiring within `tls_expiry_warn_days`. An agent's status is its plain
`online`/`offline` field. **Known limitation:** an agent's uptime history is
derived from its alert history (every alert fired against it counts as
downtime, merged when several overlap) rather than a direct connectivity
log — an agent that drops offline with no alert rule configured to notice
it will read as 100% uptime. Configure an `agent_offline` alert rule for
every agent you put on the status page if you want accurate history.

### UI

`/status` is a fully self-contained page — no login, no app shell, no shared
components with the authenticated dashboard — so it renders correctly even
if you've never logged in. It follows the visitor's system light/dark
preference, auto-refreshes every 60 seconds, and shows "This status page is
not enabled" rather than a broken page when disabled. See `ui/DESIGN.md`
§ Public status page for the full design rationale.

## Weekly report

An optional scheduled email digest of fleet health, sent through your
configured email notification channels. Disabled by default.

### Settings

| Key | Default | Meaning |
|---|---|---|
| `report_enabled` | `false` | Registers the cron job when `true`. Takes effect immediately on change — no hub restart. |
| `report_cron` | `"0 8 * * 1"` (Monday 08:00) | Standard 5-field cron expression, evaluated in the hub's local timezone. An invalid expression falls back to the default and is logged. |
| `report_channel_ids` | `[]` | Notification channel ids to send through — **email channels only**; a non-email or disabled channel is skipped with a per-channel error. |
| `report_period_days` | `7` | Lookback window for every section below. |

Configure it from **Settings → Weekly report** (admin only): enable it, pick
a schedule preset (Monday 08:00 / daily 08:00 / a custom cron expression),
select which email channels receive it, and set the period. **Preview**
renders the report without sending it (opens in an iframe). **Send now**
builds and delivers it immediately through the configured channels,
regardless of the schedule.

### Contents

- **Fleet**: agents online/total, and average uptime over the period
  (derived from alert history the same way the status page does — see its
  known limitation above).
- **Alerts**: counts by severity, the top 5 rules by firing count, and mean
  time to resolve (fired-to-resolved) across alerts resolved in the period.
- **Resource peaks**: per-host max CPU/memory/disk over the period, from
  `1h`-downsampled metrics (falling back to raw when no downsampled rows
  exist yet, e.g. a period shorter than the downsampler's window).
- **Checks**: overall uptime percentage across every probe run in the
  period, and the single worst latency observed (with which check).
- **CVE totals**: each host's *latest* `cve_scan` severity totals — a live
  snapshot, not period-scoped, since there's no CVE history to average.
- **Log volume**: rows shipped per host during the period (hosts with zero
  rows are omitted rather than shown as a zero).

### API

- `GET /api/custom/reports/preview?period_days=N` (admin) — `{subject,
  html, text}`, built but not sent.
- `POST /api/custom/reports/send` (admin) — builds the report for the
  configured period and sends it now, returning
  `{subject, results: [{channel_id, channel_name, success, error?}]}`.

### Delivery

Both the HTML and plain-text bodies are rendered from the same data
(`internal/hub/report`), with no external CSS or fonts (many mail clients
strip both) and a `prefers-color-scheme` override so it stays readable in a
dark-mode mail client. Delivery uses a `SendRaw` method on the email
notifier (`internal/hub/notify/channels/email.go`) that assembles a
hand-formatted `multipart/alternative` message — the same `sendMail` SMTP
seam alert notifications already use, just with an HTML+text body instead
of one plain-text alert message.

## Prometheus

An optional Prometheus text-exposition endpoint for scraping fleet, agent,
container, check, and alert metrics into your own monitoring stack.
Disabled until a token is generated.

### Settings

| Key | Default | Meaning |
|---|---|---|
| `prometheus_token` | unset | The **SHA-256 hash** of the bearer token `/metrics` requires — never the plaintext. Empty/unset disables the endpoint (404). |

The `settings` collection's `list`/`view` rules allow any authenticated
user, so a plaintext secret stored there would leak to a viewer-role
account — the same reason agent tokens are hashed (`agents.token_hash`).
Only the one-time generation response ever carries the plaintext.

Configure it from **Settings → Prometheus** (admin only): **Generate
token** calls `POST /api/custom/prometheus/token`, which returns the
plaintext exactly once (copy it immediately — it cannot be recovered) and
stores only its hash. **Regenerate token** rotates it, invalidating the
previous one. **Disable** clears the setting. The panel also shows a
ready-to-copy scrape config snippet using `public_base_url` (or your
browser's current origin when that setting is unset).

### Endpoint

`GET /metrics` — outside `/api/custom`, at the root, like `/healthz`.
Requires `Authorization: Bearer <token>`; `401` for a missing/incorrect
token, `404` when no token has been generated. Response is
`Content-Type: text/plain; version=0.0.4`, hand-formatted (no new
dependency), with `# HELP`/`# TYPE` lines per metric family and label
values escaped per the exposition format.

### Metric catalogue

| Metric | Labels | Meaning |
|---|---|---|
| `nexwatch_hub_uptime_seconds` | — | Seconds since the hub process started. |
| `nexwatch_agents_online` | — | Agents currently online. |
| `nexwatch_agents_total` | — | Registered agents. |
| `nexwatch_agent_up` | `agent_id`, `hostname` | 1 online, 0 otherwise. |
| `nexwatch_cpu_percent` | `agent_id`, `hostname` | Latest total CPU percent. |
| `nexwatch_memory_percent` | `agent_id`, `hostname` | Latest memory used percent. |
| `nexwatch_disk_percent` | `agent_id`, `hostname` | Latest root filesystem used percent. |
| `nexwatch_network_rx_bytes_per_second` / `_tx_bytes_per_second` | `agent_id`, `hostname` | Latest throughput, summed across non-loopback interfaces. |
| `nexwatch_load1` | `agent_id`, `hostname` | 1-minute load average. |
| `nexwatch_agent_dropped_messages_total` | `agent_id`, `hostname` | Cumulative transport queue-overflow drops. |
| `nexwatch_cve_findings` | `agent_id`, `hostname`, `severity` | Latest CVE scan finding count, when a scan is available. |
| `nexwatch_container_cpu_percent`, `_memory_bytes`, `_up` | `agent_id`, `container`, `image` | Per-container stats; `_up` is 1 for a `running` container. |
| `nexwatch_check_up`, `_latency_milliseconds`, `_tls_expiry_seconds` | `check_id`, `name`, `type` | Only emitted for a check that has been probed at least once; TLS expiry only when the check recorded a certificate. |
| `nexwatch_alerts_firing` | `severity` | Currently-firing alerts, fleet-wide, by rule severity. |

## Browser notifications and PWA

NexWatch's dashboard is an installable Progressive Web App (PWA) with Web
Push notifications, so an operator can get alerted without a browser tab
open or the app installed as a window.

### How it works

- **Installable app.** The hub serves a `manifest.webmanifest` and a
  service worker (`sw.js`) alongside the SPA. A supporting browser (Chrome,
  Edge, and most Chromium-based browsers) offers to install NexWatch as a
  standalone app; the Settings &rsaquo; Notifications page also surfaces an
  explicit "Add to home screen" button when the browser fires its
  `beforeinstallprompt` event.
- **HTTPS is required.** Both installability and Web Push are gated by the
  browser on a secure context -- `https://` in production, or `localhost`
  in local development. A deployment served over plain HTTP will report
  push notifications as unsupported and never offer to install.
- **VAPID keys.** The hub generates a VAPID key pair the first time
  `GET /api/custom/push/vapid-public-key` is called (any authenticated
  user), and reuses it afterward. The private key is stored in the
  superuser-only `hub_secrets` collection -- never in `settings`, which any
  authenticated user can read -- while the public key lives in the
  `vapid_public_key` setting. An admin can additionally set a
  `vapid_subject` setting (default `mailto:admin@localhost`), the VAPID
  JWT's contact address, shown to a push service if it needs to reach the
  operator of a misbehaving sender.
- **Subscribing.** Settings &rsaquo; Notifications' "Browser notifications"
  panel lets a signed-in user enable push on their current device
  ("Enable on this device"): it requests browser permission, subscribes
  the active service worker registration to the hub's VAPID key, and saves
  the resulting subscription (endpoint + encryption keys) as a
  `push_subscriptions` record owned by that user. The same panel lists
  every device the user has enabled and can remove one, and offers "Send
  test" to push a one-off test notification to all of the user's own
  devices.
- **Audiences.** The `webpush` notification channel type (see "Notification
  channels" above) sends an alert to every subscribed device matching its
  `audience`: `all` (every subscribed user, any role), `admins` (`admin`
  role only), or `operators` (`operator` role only -- this is an exact
  role-bucket match, not an "operator or above" permission floor). A
  superuser has no subscriptions unless it also has a matching `users`
  record, since `push_subscriptions.user_id` only relates to `users`.
- **Delivery.** A notification's payload carries a title, body, target URL
  (the hub's `public_base_url` setting when set, otherwise a path resolved
  against whichever origin the browser is currently on), severity, and a
  tag derived from the alert rule -- so repeated firings of the same rule
  replace the previous browser notification instead of stacking. Clicking
  a notification focuses an already-open NexWatch tab on that URL, or
  opens a new one. If a push service reports a subscription as gone (HTTP
  404/410 -- the user revoked permission, uninstalled the app, or cleared
  site data), the hub deletes that `push_subscriptions` record; a 429
  (rate limited) is logged and skipped without touching the subscription.
- **Service worker scope.** `sw.js` precaches the built app shell (JS, CSS,
  HTML, self-hosted fonts, and icons) with `workbox-precaching` and nothing
  else -- it never intercepts or caches `/api/*`, `/ws/*`, or `/_/`
  (PocketBase's own routes), so every live request always reaches the
  network. A new service worker version installs and waits; the app shows
  an "Update available" prompt (bottom-right) rather than reloading
  unannounced, since a background reload mid-investigation would be
  disruptive.
- **Offline.** A thin banner appears across the top of the app whenever the
  browser reports itself offline, so a stale live view doesn't read as
  "everything is fine" during a real connectivity gap.

### API

| Method & path | Auth | Description |
|---|---|---|
| `GET /api/custom/push/vapid-public-key` | Any authenticated user | Returns `{"public_key": "..."}`, generating a VAPID key pair on first call. |
| `POST /api/custom/push/test` | Any authenticated user | Sends a test notification to every `push_subscriptions` row owned by the caller; returns a per-endpoint `{endpoint, success, error?}` result. Recorded in the audit log as `push.test`. |

`push_subscriptions` itself is managed directly through the PocketBase SDK
from the UI (create on "Enable on this device", delete on "Remove"), the
same direct-CRUD pattern `alert_rules` and `checks` already use -- list,
view, create, and delete are scoped to the record's own `user_id`; there is
no update, since a subscription is replaced rather than edited.

## Development

### Prerequisites

- Go 1.24+
- Node.js 22+ with pnpm
- Docker (optional, for container monitoring)

### Build

```bash
# Install Go dependencies
go mod tidy

# Install frontend dependencies
cd ui && pnpm install && cd ..

# Start the hub (in one terminal)
make dev-hub

# Start the frontend dev server (in another terminal)
make dev-ui

# Start an agent (in another terminal)
make dev-agent
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build-all` | Build UI, hub, and agent |
| `make build-ui` | Build React frontend |
| `make build-hub` | Build hub binary (includes UI build) |
| `make build-agent` | Build agent binary |
| `make build-agent-windows` | Cross-compile the agent for windows/amd64 (`bin/nexwatch-agent.exe`) |
| `make dev-hub` | Run hub in development mode |
| `make dev-ui` | Run Vite dev server with HMR |
| `make dev-agent` | Run agent against local hub |
| `make test-go` | Run Go tests with race detector and coverage |
| `make lint-go` | Run golangci-lint against the Go codebase |
| `make fmt-go` | Format Go code with go fmt |
| `make test-ui` | Run the UI test suite with Vitest |
| `make lint-ui` | Lint the UI codebase with ESLint |
| `make fmt-ui` | Format UI code with Prettier |
| `make test` | Run Go and UI tests |
| `make lint` | Lint Go and UI code |
| `make clean` | Remove build artifacts |
| `make tidy` | Run go mod tidy |
| `make fmt` | Format Go and TypeScript code |
| `make release-agent` | Cross-compile agent for linux/amd64, linux/arm64, and windows/amd64 |
| `make checksums` | Generate SHA256 checksums for release artifacts |

### Docker

```bash
# Build and run hub + example agent
cd deploy && docker compose up -d
```

The Docker Compose setup includes a hub and a sample agent that monitors the host (with Docker socket mounted). The hub's `NEXWATCH_BOOTSTRAP_AGENT_TOKEN` env var (or the equivalent `--bootstrap-agent-token` flag) upserts a `bootstrap` agent with that token's hash on startup, so the sample agent's hardcoded `--token=changeme` keeps authenticating without a manual token-generation step.

## Project Structure

```
nexwatch/
├── cmd/
│   ├── hub/               # Hub entry point (PocketBase app)
│   └── agent/             # Agent entry point
├── internal/
│   ├── hub/
│   │   ├── api/           # Custom API routes and PocketBase hooks
│   │   ├── ws/            # WebSocket handler and agent registry
│   │   ├── metrics/       # Metric ingestion, queries, and downsampling
│   │   ├── alerts/        # Alert evaluation engine
│   │   ├── checks/        # Black-box HTTP/TCP/ICMP monitoring scheduler
│   │   ├── notify/        # Notification service and channels
│   │   ├── backup/        # Scheduled pb_data backups and retention pruning
│   │   ├── logging/       # slog setup and the request-id/access-log middleware
│   │   └── migrations/    # PocketBase collection migrations
│   ├── agent/
│   │   ├── collector/     # Metric collectors (CPU, RAM, disk, net, Docker)
│   │   ├── transport/     # WebSocket client with auto-reconnect
│   │   └── config/        # YAML config + CLI flag parsing
│   └── shared/
│       ├── protocol/      # MessagePack message types
│       └── models/        # Shared data models
├── docs/
│   └── openapi.yaml       # OpenAPI 3.1 spec for /api/custom/*, embedded and served at GET /api/custom/openapi.yaml
├── ui/                    # React frontend (Vite + TypeScript + Tailwind)
│   └── src/
│       ├── components/    # Reusable UI components
│       ├── pages/         # Route pages
│       ├── stores/        # Zustand state stores
│       ├── lib/           # PocketBase SDK instance
│       └── types/         # TypeScript type definitions
├── scripts/               # Install script for agent
├── deploy/                # Dockerfiles and docker-compose
├── .github/workflows/     # CI/CD pipelines
└── Makefile
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| Hub | Go + [PocketBase](https://pocketbase.io) |
| Agent | Go (single binary, ~10 MB) |
| Frontend | React + TypeScript + Vite + Tailwind CSS |
| Charts | [uPlot](https://github.com/leeoniya/uPlot) |
| State Management | Zustand + PocketBase SDK real-time |
| Wire Protocol | WebSocket + MessagePack |
| Database | SQLite (via PocketBase, WAL mode) |
| CI/CD | GitHub Actions |
| Containers | Docker (GHCR) |

## CI/CD

- **CI** (`.github/workflows/ci.yml`): Runs on every push to `main` and on pull requests. Lints Go code with golangci-lint, runs tests with race detector, builds Go binaries for linux/amd64 and linux/arm64, and builds the React frontend.

- **Release** (`.github/workflows/release.yml`): Triggered by version tags (`v*`). Cross-compiles hub and agent for linux/{amd64,arm64} and darwin/{amd64,arm64}, creates a GitHub Release with attached binaries and SHA256 checksums, and builds + pushes multi-arch Docker images to `ghcr.io/cognidevai/nexwatch-hub` and `ghcr.io/cognidevai/nexwatch-agent`.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes and ensure tests pass (`make test`)
4. Commit using [conventional commits](https://www.conventionalcommits.org/) format
5. Push to your branch and open a Pull Request

Please ensure:
- `make lint` passes (Go via `golangci-lint`, UI via ESLint)
- TypeScript compiles without errors (`pnpm exec tsc --noEmit`)
- New features include appropriate tests

## License

[MIT](LICENSE) - CogniDevAI
