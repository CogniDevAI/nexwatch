# NexWatch design system

**What changed:** NexWatch's dashboard moves from a generic dark SaaS look (near-black
background, cyan+purple accents, Inter, identical rounded cards) to a design built around
one job: tell a sysadmin whether their fleet is healthy in under three seconds, at 3 a.m.,
on a phone, then let them drill into one host and act. This file is the contract every
page follows. Palette, type, and component rules below are binding; deviate only by
editing this file first.

## Quick reference

| Decision | What we chose | Rejected default |
|---|---|---|
| Palette | Deep blue-charcoal void + one signal-blue accent, semantic status colors kept separate from brand accent | Near-black + cyan/purple dual accent |
| Type | IBM Plex Sans (UI) + IBM Plex Mono (data) | Inter |
| Cards | Flat panels, 1px border, no shadow, 8px radius | Identical rounded-xl cards with soft shadow |
| Status | Icon shape + color + text label (never color alone) | Colored pill with text only |
| Table headers | Sentence case, normal weight | Tracked-out ALL-CAPS eyebrows |
| The memorable thing | FleetStrip: one real-health tick per agent (connectivity + any firing alert), used as the hero bar on Dashboard/Agents and compacted into the sidebar Signal Rail on every page | — |
| Data states | Loading → error → empty → content, always in that order; a failed fetch never silently renders as "no data" | Empty state shown on both a real error and a genuinely empty list |
| Motion | One deliberate pulse on critical/offline signals; everything else is a fast, user-triggered 120ms transition | Scattered fade-ins and hover glows |

---

## 1. Palette

Five base tokens, plus a status set that is deliberately independent from the brand
accent so "this is clickable" (signal blue) is never confused with "this is broken"
(critical red).

| Token | Hex | Role |
|---|---|---|
| `--color-void` | `#0a0e16` | Page background. A deep blue-charcoal, not pure/near-black. |
| `--color-panel` | `#121926` | Card, table, and panel surfaces. |
| `--color-panel-raised` | `#1a2333` | Hover state, popovers, the active tab, modal surface. |
| `--color-line` | `#232d3d` | Borders and dividers. |
| `--color-signal` | `#5b9dff` | The ONE brand accent: links, focus ring, primary buttons, selected nav/tab, primary chart series. Never used for status. |

Text tints derived from the same family: `--color-ink` `#e7ecf3` (primary),
`--color-ink-muted` `#93a0b4` (secondary), `--color-ink-faint` `#57647a` (tertiary,
placeholders, disabled).

**Status colors** (semantic, independent of the brand accent, each paired with a fixed
icon shape so color-blind readers never depend on hue alone):

| Status | Color | Hex | Shape |
|---|---|---|---|
| Operational | green | `#34d399` | filled circle |
| Warning | amber | `#f5a524` | filled triangle |
| Critical | red | `#f5484f` | filled diamond with `!` |
| Offline / unknown | slate | `#5b6576` | hollow ring with slash |

### Why, and what we rejected

- **Rejected near-black `#0B0B0B`/`#111` + single acid accent.** It's the single most
  common "AI dashboard" tell. A deep *blue*-charcoal void reads as considered rather
  than default, and gives status colors (which are all warm-ish or green) more contrast
  to sit against.
- **Rejected the existing cyan+purple dual-accent scheme.** Two decorative accents with
  no semantic difference (the app used cyan for CPU and purple for memory, arbitrarily)
  is decoration, not information. One accent (signal blue) is reserved for
  interactivity; every other color on screen now means something.
- **Rejected color-only status.** The brief calls out that ok/warning/critical/offline
  must be distinguishable for color-blind readers. Pairing each with a distinct icon
  shape (circle/triangle/diamond/slashed-ring) means status reads correctly even in
  grayscale — which also happens to match how real rack/switch LEDs and systemd
  statuses are actually distinguished (steady vs. blinking vs. off), reinforcing the
  ops vernacular instead of borrowing generic SaaS conventions.

## 2. Typography

| Family | Role | Package |
|---|---|---|
| IBM Plex Sans | All UI text: headings, body, labels, buttons | `@fontsource/ibm-plex-sans` (400/500/600/700), self-hosted |
| IBM Plex Mono | All data: PIDs, ports, percentages, hex, byte counts, SQL text, thread dumps, hostnames/IPs where exactness matters | `@fontsource/ibm-plex-mono` (400/500/600), self-hosted |

Both ship as static-weight `@fontsource` packages bundled by Vite — no runtime request
to Google Fonts, which matters because the hub commonly runs with no internet access.

**Why not Inter:** Inter is the default typeface of every dashboard generated without a
specific typographic decision. Plex was designed by IBM specifically for dense
technical/enterprise interfaces, which is exactly NexWatch's brief, and its mono
sibling shares metrics with the sans family so mixing them (a hostname in Plex Sans
next to its port in Plex Mono) never looks like two unrelated fonts collided.

### Type scale (Elements of Typographic Style ratios, tuned for UI density)

| Token | Size | Line height | Use |
|---|---|---|---|
| `text-2xs` | 11px | 1.4 | Timestamps, micro meta |
| `text-xs` | 12px | 1.5 | Captions, status chip labels |
| `text-sm` | 13px | 1.55 | Secondary UI text, table cells |
| `text-base` | 15px | 1.6 | Body default |
| `text-lg` | 17px | 1.5 | Card/section titles |
| `text-xl` | 20px | 1.35 | Page section headings |
| `text-2xl` | 26px | 1.25 | Page titles |
| `text-3xl` | 36px | 1.15 | Hero numbers (hardening score, dashboard headline) |

Line length: prose (empty states, error copy, descriptions) is capped at `max-w-prose`
(~65ch). Tables are the exception by nature — they scroll horizontally in their own
container rather than wrap.

## 3. Layout concept

### Dashboard

```
┌────────────────────────────────────────────────────────────┐
│ NexWatch                                    [signal rail]  │  <- sidebar, see below
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ [●][●][▲][○] ...  one tick per agent, real width      │  │  <- FleetStrip (lg):
│  │ 12 of 15 operational, 2 warning, 1 offline            │  │     one segment IS one
│  └──────────────────────────────────────────────────────┘  │     agent, click-through
│                                                              │
│  Active alerts                        ▲ 2 warning ◆ 1 crit │  <- only rendered when
│  ┌──────────────────────────────────────────────────────┐  │     firing alerts exist;
│  │ ◆ High CPU   db-02      5m ago                        │  │     otherwise a single
│  │ ▲ Disk full  cache-3    12m ago                       │  │     "No active alerts"
│  └──────────────────────────────────────────────────────┘  │     line inside the strip
│                                                                │  panel above
│  Servers (15)                                                │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐                 │
│  │ ● web-01  │ │ ▲ db-02   │ │ ○ cache-3 │  ...             │  <- agent cards, left
│  │ 10.0.0.4  │ │ 10.0.0.9  │ │ offline   │                 │     accent bar = status
│  │ cpu 22%   │ │ cpu 91%   │ │ 4h ago    │                 │
│  │ mem 41%   │ │ mem 88%   │ │           │                 │
│  └───────────┘ └───────────┘ └───────────┘                 │
└────────────────────────────────────────────────────────────┘
```

The three identical "Total / Online / Offline" stat cards became a `FleetStrip`: one
tick per agent (not one segment per status bucket), each individually clickable and
colored/shaped by that agent's *real* health — connectivity plus any firing alert, not
just online/offline. This is what makes the strip answer "which one is unhealthy"
instead of only "how many are unhealthy." Below it, "Active alerts" surfaces the actual
incidents (rule, host, severity, since) so the dashboard shows *why* a tick is red, not
just that it is.

### Host detail

```
┌────────────────────────────────────────────────────────────┐
│ ← Dashboard                                                  │
│ web-01.internal ● operational [web][prod]  Silence this host │  <- tag chips next to
│ 10.0.0.4 · v0.3.1                                             │     status; quick action
│ Ubuntu 22.04 · 4 cores · 15.6 GB RAM · up 12d 4h             │     top-right (operator+)
│                                                                │
│ [Metrics] [Alerts] [Docker] [Ports] [System] [Hardening]… ›  │  <- tabs, underline
│                                                                │     style not pill;
│                                            1h 6h 24h 7d 30d  │     full row width,
│ ┌───────────────┐ ┌───────────────┐                          │     scrolls + a
│ │ CPU  ●CPU 22% │ │ Memory ●Mem 41%│  <- swatch+name+value in     chevron when it
│ │ [line chart]   │ │ [line chart]  │     the panel header (the    overflows (§7)
│ └───────────────┘ └───────────────┘     custom legend), not a
└────────────────────────────────────────────────────────────┘     rotated axis title
```

**R2 fix — tab bar and time range selector no longer share a row.** With twelve
tabs (Metrics/Alerts/Logs/Docker/Ports/System/Services/Hardening/Misconfigurations/CVE
scan/Thread dumps/Oracle DB), the tab bar previously shared its row with
`TimeRangeSelector` (`justify-between`, each a flex item) — a flex item's default
`min-width: auto` meant the tab list never actually shrank to trigger its own
`overflow-x-auto`, so the row simply grew wider than the viewport and cut off the last
few tabs with no way to reach them. `TimeRangeSelector` only applies to the Metrics tab
in the first place, so it now renders inside that tab's own content (right-aligned,
above the charts) instead of the tab bar, and the tab bar gets the full row to itself.
See "Row and tab-bar overflow" under §7 for how `Tabs` itself handles a tab list wider
than its row.

The Alerts tab lists this host's currently-firing alerts (from the same `useFleetHealth`/
`useActiveAlerts` data every other page reads, filtered to this `agent_id` — no extra
fetch) with the same ack/silence/escalation controls as the Dashboard's Active alerts
list. "Silence this host" (operator+) opens `SilenceForm` prefilled to this agent with a
default 1h window, so the common "I'm about to restart this box, mute it for an hour"
action takes one click plus a confirm rather than a full trip to the Silences page.

The Logs tab (`src/components/server/LogsTab.tsx`, right after Alerts) renders the same
`LogsView` the standalone `/logs` page uses, locked to this host — see §13.

The Docker tab (`src/components/server/DockerTab.tsx`) lists this agent's reported
containers with per-row Start/Stop/Restart actions (operator+, `Button` ghost variants —
only the actions that make sense for the container's current status render: Start for
anything not running, Stop+Restart for a running one). Each action opens a `Modal`
confirmation naming the container and the action before it calls
`POST /api/custom/agents/{id}/docker/{containerId}/{action}`; the row's buttons disable
and read "Working…" while the request is in flight, and the result — success or the
agent's own error text — surfaces as a toast (`useToast`), never a silent failure. A
container whose image has a newer registry digest gets an amber "Update available" badge
next to its image name (hover for the short registry digest), and an "Updates available"
`Toggle` above the table filters the list to just those containers. This also fixed two
pre-existing bugs found while verifying this feature end-to-end against a real agent and
hub: the tab's PocketBase filter/subscription checked a `docker_containers.agent` field
that has never existed on that collection (the real field is `agent_id`), and the
`DockerContainer` type's `cpu`/`memory_used` fields never matched the collection's actual
`cpu_percent`/`memory_usage` columns — both silently produced an empty or broken tab in
production before this change (see the Docker actions README section for the matching
agent/hub-side field-name fixes in the ingestion pipeline itself).

Every chart on this page shares one uPlot chrome (`src/lib/uplotHelpers.ts`):

- **Y-scale never collapses.** A flat or near-flat series gets a minimum enforced span
  (5 points for a percentage, 10% of the peak value for a rate) so the axis never prints
  the same rounded tick five times over. A percentage axis always shows whole-number
  ticks once the span reaches that enforced minimum of 5 (i.e. always, in practice) —
  fractional ticks like "55.0, 56.0, …" only add noise once the range can't collapse.
- **No native uPlot legend, no rotated axis title.** Both read as chart-library default
  chrome ("Time: -- CPU: --", a tiny sideways "%"). Replaced with a compact legend in the
  panel header — a color swatch, the series name, and its hovered-or-latest value — since
  the unit is already in the panel title.
- **Network is a derived rate, not raw data.** The hub reports network bytes as a
  cumulative counter since the agent started (converted to MB), not a per-second rate —
  charting it directly produced an ever-climbing line mislabeled "MB/s" that could show
  something like "2.5K". The UI now computes MB/s client-side from consecutive samples
  (`toNetworkRateSeries` in `ServerDetail.tsx`), clamping a negative delta from an agent
  restart's counter reset to zero. This is a UI-side mitigation of a backend metric that
  needs a real fix (compute the rate at collection time, the way `diskio.go` already
  does) — out of scope here since it requires a Go change.

#### Misconfigurations vs. CVE scan

The tab formerly labeled **Vulnerabilities** is renamed **Misconfigurations**
(`src/components/server/VulnerabilitiesTab.tsx`, unchanged otherwise — same
`vulnerabilities` metric type, same file) because it only ever checked local
host misconfigurations (world-writable files, unusual SUID binaries,
services running as root, weak permissions on sensitive files), never
looked up a CVE against installed package versions, and the old label
actively misled an operator into thinking it did. A new **CVE scan** tab
(`src/components/server/CveScanTab.tsx`, `Bug` icon) reads
`GET /api/custom/agents/{id}/cve` and shows: severity summary tiles
(critical/high/medium/low, `MetricTile`, plus a "Fixable" count), a scanner
info line (name, version, DB build time, last-scanned time via `timeSince`,
an amber "Stale" chip once the cached scan is older than 2x the configured
interval), a filter row (severity `Select`, "Fixable only" `Toggle`, a
search `Input` matching CVE id/package/title), and one findings `Table` per
scanned target (the host filesystem, then each scanned container image) —
each row's CVE id links out to its NVD detail page. `SeverityBadge` gained
an `"unknown"` variant (styled like `"info"`) for a scanner-reported
severity this taxonomy doesn't otherwise recognize. Follows the same
loading → error → not-available → empty → content ordering as every other
list-fetching view (§9): "not available" (no `trivy`/`grype` on the agent
host) shows both scanners' exact one-line install commands in monospace
panels — mirroring the Agents page's install-command styling — rather than
a generic "no data" empty state, and a distinct "first scan is running"
message covers the pending-first-run case so an operator doesn't read
"install a scanner" when one is already installed and just hasn't finished
its first pass yet.

#### Unsupported-on-platform tabs (Windows)

Misconfigurations and Oracle DB are the two host-detail tabs backed by a
collector with no Windows implementation (`internal/agent/platform.Supported`
on the Go side never registers `vulnerabilities`/`oracle` there). Rather than
mounting `VulnerabilitiesTab`/`OracleTab` — which would sit in a permanent
"no data yet" empty state indistinguishable from a real problem, since a
Windows agent's data for these is not merely late but never coming —
`ServerDetail.tsx` checks `agent.platform === "windows"` before mounting
either tab and renders `PlatformUnsupportedState`
(`src/components/ui/PlatformUnsupportedState.tsx`) instead: the same
`EmptyState` shell every other quiet placeholder in this app uses (§9), a
`MonitorX` icon, and one sentence naming the feature and the platform ("Not
available on Windows"). The tab itself stays visible and clickable in the tab
bar — only its content differs — so a Windows host's tab list matches every
other host's, and an operator switching between hosts never sees tabs
appear/disappear based on OS.

### Alerts

```
┌────────────────────────────────────────────────────────────┐
│ Alert rules                                    + New rule   │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Name      Rule          Duration Targeting  Severity  │   │  <- sentence-case
│ │ High CPU  cpu > 90      5m       All agents ▲ warning │   │     headers; "Rule"
│ │ Nginx down Process down:nginx 1m Tags: web  ◆ critical│   │     folds metric+
│ └──────────────────────────────────────────────────────┘   │     condition+target
└────────────────────────────────────────────────────────────┘
```

`AlertRuleForm` groups `metric_type` into an optgroup'd select: "Resource metrics"
(cpu/memory/disk/network/docker, still condition+threshold), "Security"
(cve_count, also condition+threshold — a "Count" select scopes it to
"Critical + high severity" or "Critical only", reusing the `target` column as
a closed choice rather than `process_down`/`service_failed`'s free-text
field), and "Availability"
(process_down/service_failed/agent_offline/check_down/cert_expiry, boolean —
condition/threshold are hidden, `duration` stays). `process_down`/`service_failed`
additionally show a `target` field (process name or command-line substring / systemd
service name). `check_down`/`cert_expiry` target a black-box `checks` record (§11) instead
of an agent: the "Applies to" agent-targeting radio is replaced entirely by a
single `Check` select (populated from the `checks` collection, empty = every check, one
alert per breaching check) — a check isn't an agent, so `target_tags`/`agent_id` simply
don't apply to these two types. `cert_expiry` additionally repurposes the `threshold`
field as "Warn when certificate expires within (days)" with its own label, shown instead
of the generic Condition/Threshold pair (which stays hidden for every availability type,
check-based or not). A fourth optgroup, "Logs", holds `log_match` (§13) — it keeps the
generic Threshold field (labeled "Match count threshold": a count of matching log lines,
not a metric value) but hides Condition, since it's always semantically "count >=
threshold"; it shares `process_down`/`service_failed`'s free-text `target` Input,
relabeled "Log pattern" with its own caption explaining the substring-or-`/regex/`
syntax, and `duration`'s caption changes for this one type to explain it doubles as the
log-matching lookback window, not just the firing-persistence timer. Targeting for every
other type is a radio ("All agents" / "Agents
with tags" / "One agent") driving `target_tags` (a `TagInput`, suggestions from every tag
already used on an agent) or `agent_id`, mutually exclusive — switching modes clears the
other field so a rule can never carry stale data. Duration
has quick presets (30s/1m/5m/15m) plus the exact numeric field, since operators reach for
a common value far more often than an arbitrary one. Escalation is a collapsible section
(closed by default for a new rule, open when editing one that already escalates) with its
own channel multi-select and preset "escalate after" durations (off/10m/30m/1h) — kept
separate from the immediate `notification_channels` since escalation is a second,
optional dispatch. The rules list folds metric type, condition/threshold, and `target`
into one "Rule" column (`ruleSummary` in `src/lib/alertRules.ts`) rather than three
separate columns, since they're one fact ("what is being watched") and reads better as
"Process down: nginx" than as three cells that only make sense together; targeting and
escalation get their own summary columns (`ruleTargetingSummary`/`ruleEscalationSummary`,
same file).

Alert history's filters (status, severity, agent, acknowledged, silenced) sit in one
wrapping row using the shared `Select`, with a result count alongside them — not stacked
full-width native selects. Status reads "Firing"/"Resolved" (sentence case, not the raw
lowercase enum value); the "Resolved at" header is just "Resolved" so it doesn't wrap;
and the Message column carries a `title` attribute with the full text. Acknowledged/
Silenced are server-side filter clauses (`acknowledged_at != ''`, `silenced = true`),
unlike severity which stays a client-side filter since it isn't a direct `alerts`
column.

**R2 fix — Rule/Message column widths.** Giving Message `w-full` (so it absorbed
every pixel the browser's automatic table layout didn't claim elsewhere) starved the
Rule column of any minimum width, so a normal-length rule summary like "CPU above 1%
(test)" wrapped across four lines at common desktop widths. `Rule` now carries a
`min-w-[11rem]` on both its `Th` and `Td` (wraps to at most two lines instead of
four), and `Message` instead caps at `max-w-xs truncate` with a `title` tooltip for
the full text — Message is the least load-bearing fact once Status/Agent/Rule already
identify the row, so it's the column allowed to shrink toward its ellipsis first. The
table still scrolls inside its own container on narrower viewports (`Table`'s
`overflow-x-auto`, unchanged) rather than clipping. The desktop table also gained a
`md:hidden` mobile card layout (matching the Agents page's pattern) so Acknowledge
stays one tap away instead of requiring a horizontal scroll to reach the table's
Acknowledged column on a phone.

The Dashboard's "Active alerts" list groups by rule + host and shows a "×N" badge when
the backend has produced duplicate firing-alert rows for the same incident (it currently
can — a known backend defect for a later work unit) — the UI never shows five identical
rows for what is one incident.

#### Acknowledgement, silencing, and escalation

An unacknowledged firing alert (Dashboard's "Active alerts", Alert history, and a host's
own Alerts tab) shows an "Acknowledge" button for operator+ (`AckControl`,
`src/components/alerts/AckControl.tsx`); once acknowledged it shows a check mark plus
"Acknowledged by \<id (email)\> · \<time ago\>" — the hub already formats
`acknowledged_by` as `"<id> (<email>)"`, so the UI displays it verbatim rather than
joining against a users list — and an "Undo" button for operator+. `AckControl` only
calls the ack/unack endpoint; it never patches local state itself, the same way every
other mutation in this codebase (e.g. Agents delete) defers to the shared realtime
subscription to reflect the result (see §10). A `silenced` alert gets a muted "Silenced"
badge (`BellOff` icon) and an escalated one gets an amber "Escalated" badge
(`ArrowUpCircle`), both in `src/components/alerts/AlertBadges.tsx` — independent of the
ok/warning/critical/offline `StatusIndicator` taxonomy, since either can apply at any
severity.

**Decision: acknowledged alerts stay counted in fleet/severity summaries.** `useFleetHealth`
and the Dashboard's critical/warning counts do NOT exclude acknowledged alerts — an
acknowledged critical alert is still a critical condition that hasn't been fixed, only
one a human has seen. Excluding it would make the FleetStrip hero (§4) lie about "is
everything healthy." Acknowledgement is surfaced as a per-row fact (the check mark +
"Acknowledged by" line) everywhere the alert itself is listed, not as a change to the
aggregate counts.

### Silences (`/alerts/silences`)

A maintenance-window list, following the same list-page shape as Alert rules/history:
`PageHeader` + "New silence" (operator+) action, a status `Select` (All/Active/
Scheduled/Expired — `silenceBucket` in `src/hooks/useSilences.ts` derives the bucket from
`starts_at`/`ends_at` vs. now), and a `Table` with Name, Scope, Window, Created by,
Covered agents, Status, Actions. "Covered agents" is read only from
`GET /api/custom/silences/active`'s `covered_agent_ids` (page-local fetch, refreshed every
30s) rather than recomputed client-side from agent tags — the hub already owns that
agent_id/tag-overlap match, and duplicating it in the UI risks silently drifting from the
real rule. "End now" (active rows) patches `ends_at` to the current time; delete is
available on scheduled/expired rows only (an active silence is ended, not deleted, so its
history stays visible until it naturally expires). `SilenceForm`
(`src/components/silences/SilenceForm.tsx`) is create-only — there's no edit flow, matching
the brief's action set (create / end now / delete) — and is shared with the host detail
page's "Silence this host" action (prefilled `agent_id` + hostname, defaulting to a 1h
window). The hub does not stamp `silences.created_by` itself (verified empirically: a
record created without it comes back with `created_by: ""`), so the form sets it to the
creating user's email at submit time.

### Agents (fleet management)

Same `FleetStrip` hero as the Dashboard, then the same table pattern as Alerts:
sentence-case headers, status as icon+label, install command shown in a monospace panel
with a single obvious copy action per command block. Row actions are ghost buttons with
a visible icon **and** label ("Regenerate token", "Delete") — never icon-only — and below
`md` the table becomes a stacked card per agent instead of a horizontally-scrolling
table, since a 6-column table has no good narrow layout. The OS column pairs
`agent.os` (a human-readable string like "Ubuntu 22.04" or "Microsoft Windows 11
Pro") with a small `PlatformIcon` (`src/components/ui/PlatformIcon.tsx`) derived
from `agent.platform` (the agent's own `runtime.GOOS`) — a generic device/terminal
glyph per platform family (window/terminal/laptop shapes, never an OS brand mark),
the same "shape carries information, not just decoration" principle behind
`StatusIndicator`'s status glyphs. The same icon repeats next to the OS/platform
line in the host detail header.

#### Add agent: OS tabs

The "Add agent" modal's step 2 (after token generation) splits its install
command by OS into two tabs, Linux and Windows — the same underline tab style
as the host detail page (§3), not pills, each labeled with its `PlatformIcon`.
The Linux tab keeps the existing Standard/Oracle DB command blocks unchanged;
the Windows tab shows one PowerShell block: a two-step
`Invoke-WebRequest`-then-run form rather than a one-line `irm | iex`, since an
agent install needs an elevated (Administrator) prompt and piping straight into
`iex` gives no chance to review the script first — `scripts/install-agent.ps1`'s
own header comment carries the identical reasoning, so the UI and the script
agree on why. Switching tabs is local `useState`, reset to Linux whenever the
modal closes (`handleCloseAddModal`), so reopening it for a different agent
never leaves a stale OS selection showing.

#### Tags

`agents.tags` (`string[]`, nullable — always read via `agent.tags ?? []`) is edited from a
per-row "Edit tags" ghost action (operator+) that opens a small modal around `TagInput`
rather than an always-editable inline field, so a table row keeps its fixed row height and
a mis-click can't silently start editing tags. Tags render as read-only chips
(`TagChips`, `src/components/ui/TagChips.tsx`) in the Agents table/mobile cards and the
host detail header. Both the Agents page and the Dashboard get a `TagFilterBar` (same
file) above their agent list/grid: toggleable chips built from every tag currently in use,
multiple selected = ANY (matching how `target_tags` targeting works on alert rules, so the
filter behaves the way an operator already expects from rule targeting). The filter scopes
the page's `FleetStrip` hero as well as its table/grid — filtering to a tag and still
seeing the *whole* fleet's health strip would defeat the point of narrowing the view.

#### Self-update (F9)

An agent behind the compare version (`agent_target_version` when the admin has set one,
else the cached latest GitHub release — `useAgentUpdateInfo`, `src/hooks/`) gets an amber
`UpdateAvailableBadge` (`src/components/agents/UpdateBadges.tsx`, styled like
`EscalatedBadge`) next to its version chip in the table, its mobile card, and the host
detail header. A connected, outdated agent also gets a per-row "Update" ghost action
(operator+) — icon **and** label, matching this page's own action-button rule — that opens
`UpdateAgentModal`: a target-version field pre-filled from the compare version, a note that
the agent restarts itself, and a Confirm that calls
`POST /api/custom/agents/{id}/update`. While an update is in flight, `UpdateStatusChip`
replaces the badge with a spinner and the current stage
(`downloading…`/`verifying…`/`installing…`/`restarting…`), sourced from
`agents.update_status` via a per-agent PocketBase realtime subscription (the host detail
page) or the shared `agentStore` subscription (the Agents page) — never polled. A `failed`
status shows a critical chip (the server's `update_error` as its tooltip) plus an inline
"Retry" link that reopens the same confirmation modal. An admin additionally gets an
"Update all outdated (N)" toolbar action (`PageHeader`'s action slot) that opens
`UpdateAllModal`, listing every connected, outdated host by name before confirming a
`POST /api/custom/agents/update-all` fan-out with `only_outdated: true` — the listed hosts
and the request's actual target set are the same filter applied once, so they can't drift
apart. Both modals defer to their respective realtime subscription to reflect the outcome,
the same way `AckControl` and the Docker actions tab already do — neither ever patches
local agent state itself.

### Settings

Stacked panels (Data retention, General), each with a one-line description under its
heading. The retention control is presets *or* one numeric input — not presets, a
slider, and a separate "X days" readout all showing the same value three times. "Save
settings" lives in the page header's action slot, the same place every other page puts
its primary action, so it's visible without scrolling on mobile. The Users and
Notifications links that used to sit on this page are gone — they're reachable from the
sidebar (see Navigation below), so a duplicate entry point isn't needed here. An
admin-only "Agent updates" panel (`AgentUpdateSettings`,
`src/components/settings/AgentUpdateSettings.tsx`) sits after Prometheus: a target-version
field and a release-base-URL field (for an offline/air-gapped mirror), a static explanation
that signature verification is each agent's own local policy rather than something this
panel configures, and a read-only "Latest published release" line with a manual refresh
action — following the same load → edit → "Save" shape as every other settings panel here,
not a live-validating form.

### Navigation

Notifications, Audit log, and Users are Settings sub-pages, not independent top-level
areas — they render as smaller, indented items nested under Settings in the sidebar,
connected with a thin left rule. The Settings link itself only highlights on an exact
match (`end`), so visiting a sub-page doesn't light up two nav items at once. Audit log
(`/settings/audit`) is visible to operator role or higher — matching the `audit_log`
collection's own read rule — unlike Users, which stays admin-only; a viewer hitting the
route directly is redirected back to `/settings`, the same pattern `Users.tsx` already
used for its admin-only gate. Silences (`/alerts/silences`)
is a top-level sidebar item next to Alert rules and Alert history, not nested under
either — it's a distinct record type (maintenance windows, not rules or incidents) with
its own create/manage flow, so folding it under one of the other two would misrepresent
what it is. Checks (`/checks`) sits between Agents and Alert rules — it's a monitoring
target list like Agents, not an alerting concept, so it belongs with the "what am I
watching" group rather than the "how do I get notified" group below it. Logs (`/logs`)
sits immediately after Checks, for the same reason — it's another "what is happening
right now" view, not an alerting concept — and before Alert rules so the "watching"
group (Dashboard implied, Agents, Checks, Logs) reads together ahead of the
"notification" group.

**Quiet count badges (R2).** Alert history and Silences each get a small neutral count
chip (`NavCountBadge`, `src/components/layout/AppShell.tsx`) next to their label —
Alert history shows the number of currently-firing alert groups (`useActiveAlerts`),
Silences shows the number of silences currently in the `active` bucket
(`useSilences`/`silenceBucket`). Both read pure selectors over stores `AppShell`
already fetches once per session, so the badges add zero extra requests (§10). The
chip is deliberately a single neutral tone (`--color-panel-raised` background,
`--color-ink-muted` text) regardless of count or page — a count is not itself a
severity signal, and coloring it would misuse the status palette (§1) as decoration.
It renders only when the count is greater than zero, so a quiet fleet leaves the nav
exactly as before.

### Alignment rule

Left-align everything. This is an operational tool read top-to-bottom, left-to-right,
scanned for the one row that's wrong — centered text or centered card content would
fight that scan pattern. Numbers in tables are right-aligned and tabular so magnitudes
compare vertically at a glance (already correct in the current code; kept).

## 4. The memorable thing: FleetStrip

One component, two sizes: `FleetStrip` renders one *segment* per agent — solidly filled
in its status color (not a bordered outline around a small icon), with the status glyph
and, at `size="lg"`, the hostname on top in whichever of void-dark or white gives better
contrast against that status color. Each agent's segment fills `1/N` of the row, so a
fleet of one renders as one full-width segment, not a small icon lost in an empty bar.
It is the hero bar on Dashboard and Agents (`size="lg"`, segments fill the row so the
bar itself answers "which one is unhealthy") and it compacts into the sidebar's Signal
Rail on every other page (`size="sm"`, small filled squares, wrapping, with a
plain-language summary line like "12 of 15 operational, 2 warning, 1 offline"). Hovering
a segment shows the hostname; clicking jumps straight to that host.

**Real health, not just connectivity.** A tick is "critical" or "warning" when that
agent has a matching firing alert (via `useFleetHealth`, which joins the agent list
against `useActiveAlerts`'s firing alerts and their rule severity), and falls back to
plain online/offline only when there's no active incident. This is deliberate: a fleet
where every agent is "online" but one has a firing disk-full alert should not look
uniformly green.

Why this and not a bigger dashboard-only hero: the brief's actual primary job is "is
everything healthy, and if not, where," answered in under three seconds, *repeatedly*,
all day, often while already investigating a different host. A hero that only exists on
the Dashboard only helps on the Dashboard. The same component surfacing on Agents (as
the literal fleet-management hero) and compacting into every other page's sidebar
answers the question from anywhere — which is what a 3 a.m. on-call workflow actually
needs, and the one piece of interface genuinely different from a generic admin panel
rather than a reskinned version of one.

## 5. Motion policy

- `prefers-reduced-motion: reduce` disables every non-essential transition and the
  pulse below; state still changes, it just doesn't animate.
- All hover/focus/press transitions: 120–150ms, opacity/color/background only (no
  movement, no scale).
- The one deliberate non-user-triggered motion: any `FleetStrip` tick (sidebar rail or
  the Dashboard/Agents hero) in **critical** or **offline** state pulses slowly (2s
  cycle, opacity 1→0.55→1). This is the single orchestrated moment the brief asks for —
  it exists purely to catch a peripheral glance at 3 a.m., which is the actual job of the
  interface. No other element animates on its own.

## 6. Copy rules

- Sentence case everywhere. No tracked-out ALL-CAPS labels (removes ~30 instances of
  `text-xs font-semibold tracking-wider uppercase` table headers).
- Active voice, outcome-named buttons: "Save changes", "Regenerate token", "Add agent" —
  already close to this in most of the app; a few were "Confirm"/"Submit"-style and are
  renamed.
- Toasts/inline confirmations echo the triggering verb: saving settings confirms "Settings
  saved", not "Success."
- Empty states: icon, one-sentence heading, one-sentence action-oriented body. Error
  states name what to check next, not just that something failed (mostly already true
  in this codebase — kept, restyled).
- No middle-dot meta strings news-ticker style; where multiple facts sit on one line
  (host meta row) they're separated by a plain vertical rule with visible spacing, each
  fact labeled in muted text so it reads as data fields, not a decorative string.

## 7. Component inventory

All under `src/components/ui/`. Existing feature components consume these instead of
repeating `rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)]`
inline (previously duplicated in ~25 places).

| Component | Purpose | Notes |
|---|---|---|
| `PageHeader` | Page-level title + description + actions | `src/components/ui/PageHeader.tsx`. Every page opens with this — title, an optional one-line description, and a right-aligned (wraps below on mobile) actions slot for the page's primary button, so "New X"/"Save" always lives in the same place. |
| `Panel` | Card/section container | 1px border, `--color-panel` bg, 8px radius, no shadow. Optional `PanelHeader` with title + actions slot. |
| `StatusIndicator` | Status glyph + label | Maps `ok/warning/critical/offline` → shape + color + text. Used for agents, docker containers, alerts, services, hardening checks. Has `dotOnly` mode for dense tables. |
| `SeverityBadge` | Finding-severity chip | `src/components/ui/SeverityBadge.tsx`. A distinct six-level taxonomy (`critical/high/medium/low/info/unknown`) for CVE/hardening findings — not the four-state `StatusIndicator` taxonomy, since a finding's severity and an agent/check's live status answer different questions. Never mixed with `StatusIndicator` on the same fact. |
| `rowClass` | Zebra/hover row background | `src/components/ui/rowClass.ts`. One helper every table body row calls (`rowClass(idx)`) so striping and hover state are pixel-identical across Agents, Alerts, Alert history, Silences, Checks, Users, Audit log. |
| `MetricTile` | Label + big mono value | Used for stat summaries (hardening score breakdown, Oracle sessions, vuln counts) instead of ad hoc `<div className="text-2xl font-bold">` blocks. |
| `FleetStrip` | One tick per agent, real health, click-through | The memorable thing, described in §4. `size="lg"` (Dashboard/Agents hero) or `size="sm"` (Signal Rail). |
| `Table`, `Th`, `Td` | Data table primitives | Sentence-case header, sortable header variant, sticky header, consistent zebra/hover. |
| `Button` | primary/secondary/accent/danger/ghost × sm/md | Replaces one-off button class strings. |
| `Input`, `Select`, `Textarea`, `Label`, `FieldCaption` | Form controls | Consistent border/focus ring, label association. `Select` renders a custom chevron and always fills its wrapper — the wrapper (not the `<select>`) takes the width class, so a caller asking for `w-auto` never has to fight a baked-in `w-full`. |
| `Toggle` | On/off control | A real `<button role="switch">`, not a checkbox-plus-peer-selector hack. |
| `Tabs` | Accessible underline tab bar | `src/components/ui/Tabs.tsx`. `role="tablist"`/`role="tab"`/`aria-selected` plus WAI-ARIA keyboard nav (arrow keys move and select, wrapping; Home/End jump to first/last; roving `tabIndex` so Tab skips inactive tabs) and an optional per-tab icon. Shared by the host detail page's section tabs and the Add-agent modal's Linux/Windows OS tabs — previously two separate hand-rolled, keyboard-inaccessible implementations. Always scrolls horizontally within itself rather than growing past its row — see "Row and tab-bar overflow" below. |
| `Menu` | Row-action overflow menu | `src/components/ui/Menu.tsx`. A `⋯` `IconButton` trigger (`aria-haspopup="menu"`/`aria-expanded`) opening a `role="menu"` panel of `role="menuitem"` buttons — arrow-key roving focus, Escape closes and returns focus to the trigger, an outside click or a selected item also closes it. See "Row action overflow" below. |
| `Modal` | Centered dialog | Used by all "New X" forms and the thread dump viewer. Escape closes it. |
| `Toast` | Transient confirmation | Used for settings save, test-notification result, thread-dump request errors. |
| `EmptyState` | Icon + heading + body + optional action | Rendered only after a successful, genuinely empty fetch — see §9. |
| `ErrorState` | Icon + heading + body + optional retry | Rendered on a failed fetch, with the server's message and a "Try again" action — see §9. |
| `PlatformIcon` | Small OS/platform glyph | `src/components/ui/PlatformIcon.tsx`. Maps `agent.platform` (windows/linux/darwin) to a generic device/terminal shape, never an OS brand mark. Used in the Agents table, host detail header, and the Add agent modal's OS tabs. |
| `PlatformUnsupportedState` | "Not available on \<platform\>" placeholder | `src/components/ui/PlatformUnsupportedState.tsx`. Wraps `EmptyState` for a host-detail tab whose collector has no implementation on the agent's OS — see "Unsupported-on-platform tabs (Windows)" above. |
| `Skeleton` | Loading placeholder block | Replaces spinning-icon-plus-"Loading…" text where a shaped placeholder reads faster. |
| `SignalRail` | Sidebar wrapper around `FleetStrip` | Reads `useFleetHealth` (a pure selector — see §10) and renders the compact strip; mounted once in `AppShell`, visible on every page. |
| `NavCountBadge` | Quiet nav-item count chip | `src/components/layout/AppShell.tsx`. A single neutral tone regardless of count — see "Quiet count badges (R2)" under Navigation. Used on Alert history (firing count) and Silences (active count); hidden at zero. |
| `TagInput` | Chip-style multi-value tag editor | Type + Enter/comma to add, Backspace on an empty field removes the last chip, case-insensitive dedupe, suggestion dropdown filtered by the current draft. Used by agent tag editing, `AlertRuleForm`'s tag targeting, and `SilenceForm`'s tag scope. |
| `TagChips`, `TagFilterBar` | Read-only tag display / toggleable tag filter | Both in `src/components/ui/TagChips.tsx`. `TagChips` renders a tag list as muted chips (Agents table/cards, host detail header). `TagFilterBar` renders every tag in use as a toggle chip, multiple selected = ANY — used above the Agents table and the Dashboard grid, scoping the `FleetStrip` hero too. |
| `AckControl` | Acknowledge/undo control for one firing alert | `src/components/alerts/AckControl.tsx`. Calls the ack/unack endpoints and relies on the caller's existing realtime subscription to reflect the result — never patches local state itself (see §10). Used by Dashboard's Active alerts, Alert history, and a host's Alerts tab. |
| `SilencedBadge`, `EscalatedBadge` | Alert flag chips | `src/components/alerts/AlertBadges.tsx`. Independent of `StatusIndicator`'s ok/warning/critical/offline taxonomy — either can apply at any severity. |
| `CheckForm` | Create/edit form for a black-box check | `src/components/checks/CheckForm.tsx`. Type radio (http/tcp/icmp) drives which fields show — method/expected status/body-contains/verify TLS/warn-days are http-only; interval and timeout each get quick presets plus an exact field, same pattern as `AlertRuleForm`'s duration. |
| `CheckDetailDrawer` | Expandable row content for one check | `src/components/checks/CheckDetailDrawer.tsx`. A 24h/7d toggle, a `MetricChart` latency series (reused as-is — see §11), and the most recent results list with a status dot per row. |
| `CveScanTab` | Host detail "CVE scan" tab | `src/components/server/CveScanTab.tsx`. Severity tiles, scanner info line, per-target findings tables with severity/fixable/search filters, and a not-available state with install commands for Trivy and Grype. See the host detail section above. |
| `LogsView` | Shared log search/live-tail view | `src/components/logs/LogsView.tsx`. Powers both the standalone `/logs` page and, via `LogsTab` (`src/components/server/LogsTab.tsx`, an `agentId`-locked wrapper), a host detail tab. See §13. |
| `UpdateAvailableBadge`, `UpdateStatusChip` | Self-update flag chips | `src/components/agents/UpdateBadges.tsx`. The badge is independent of `StatusIndicator`'s taxonomy, styled like `EscalatedBadge`; the chip renders a live in-progress stage or a failed state with an optional Retry action. See §3 "Self-update (F9)". |
| `UpdateAgentModal`, `UpdateAllModal` | Self-update confirmation modals | `src/components/agents/`. Single-agent and admin fan-out confirmations for `POST /api/custom/agents/{id}/update` and `.../update-all`. See §3 "Self-update (F9)". |

Two pure-selector hooks back this: `useActiveAlerts` (firing alerts grouped and enriched
with rule name/severity and agent hostname, plus each group's silenced/acknowledged/
escalated state) and `useFleetHealth` (joins that against the agent list to produce each
`FleetStrip` segment's status). Neither fetches or subscribes itself — see §10 for where
that actually happens. A third, `useSilences` (`src/hooks/useSilences.ts`), is a pure read
over `silencesStore` and exposes `silenceBucket` for deriving active/scheduled/expired. A
fourth, `useChecksSummary` (`src/hooks/useChecksSummary.ts`), is the one exception to
"pure selector, never fetches" — see §11 for why.

### Row action overflow (R2)

A table row (or mobile card) that needs more than two actions doesn't grow more labeled
`Button`s — past that point they stop fitting a desktop table row's width or a 390px
mobile card without wrapping onto an awkward second line or, worse, pushing a button
off-screen. The rule: keep the one or two actions reached most often as direct, visible,
labeled ghost buttons, and move the rest into a `Menu` (§7) opened from a single `⋯`
icon button labeled `"More actions for <name>"`. The Agents table/mobile cards are the
first case — Update (when available) and Edit tags stay direct; Regenerate token and
Delete move into the menu, selecting either behaves exactly as it did as a direct
button (Delete still opens the same inline Confirm/Cancel row, Regenerate token still
opens the same "Token regenerated" modal), so moving an action into the menu never
changes what it does, only how it's reached. Both the desktop table cell and the mobile
card footer render the same `AgentActions` component, so this fix (and any future one)
applies to both layouts at once instead of needing to be kept in sync by hand.

`AgentActions` is defined at module scope, not nested inside the `Agents` page
component as it originally was — a component recreated inline on every parent render
gets a fresh function-type identity each render, which React treats as a type change
and remounts rather than re-renders, silently discarding any state a descendant owns.
That was invisible before this fix (the row's only other transient state, the delete
confirmation, already lived in the parent page's state), but would have become a real,
visible defect — the "More actions" menu appearing to close itself on the next
unrelated re-render — once `Menu` owned its own local open/closed state. Any future
row-actions component should be defined at module scope for the same reason.

### Row and tab-bar overflow (R2)

Any row that can outgrow its container — a tab bar being the main case, since the host
detail page has twelve tabs — must scroll horizontally within itself rather than
letting the row (and the page body with it) grow wider than the viewport. `Tabs` (§7)
owns this internally: its tablist is `overflow-x-auto` on a dedicated scroll container,
not left to a caller to opt into, since a flex sibling (the old host-detail layout put
`TimeRangeSelector` next to the tab bar in a `justify-between` row) defaults to
`min-width: auto` and never actually shrinks enough to trigger overflow scrolling on
its own — the row just grows past the viewport instead. A small chevron button fades
into view at whichever edge still has more tabs to reach (tracked via scroll position
plus a `ResizeObserver`, `aria-hidden` and `tabIndex={-1}` since it's a pointer-only
convenience, not a new way to reach a tab) and scrolls one step per click. Keyboard
navigation (arrow keys, Home/End) calls `scrollIntoView` on the newly focused tab, so
arrowing past the currently visible edge still works without a mouse. The same
overflow handling covers any future long tab list — a caller never needs to add
`overflow-x-auto` itself.

## 8. Self-critique pass

Checked against the skill's generic-tell list before implementation:

1. Cream+terracotta / near-black+acid-green — neither applies; palette is blue-charcoal
   with a signal-blue accent and separate status colors, not decorative.
2. Broadsheet hairlines + zero radius + dense columns — rejected; this is a data tool,
   not an editorial layout, and needs breathing room for scanability, so panels keep a
   small radius and generous row padding.
3. Identical rounded SaaS cards with the same shadow — rejected. `Panel` has no shadow
   at all (flat), and cards are differentiated by content (a left accent bar for
   status-bearing items only, not decoration on every card).
4. Tracked-out ALL-CAPS eyebrows — removed everywhere; table headers are sentence case.
5. Middle-dot meta strings — removed; host meta row uses labeled fields with visible
   spacing instead.
6. Monospace for every small label — rejected; monospace is reserved for actual data
   (numbers, ids, hex, code/SQL/log text), never for ordinary UI labels.
7. Arrows appended to buttons — none added; buttons name the outcome directly.

## 9. Data states

Every list-fetching page follows the same order, and never skips a state:

**loading → error → empty → content**

A failed request must render `ErrorState` with the server's actual message and a "Try
again" action that re-runs the fetch — it must never fall through to `EmptyState`,
which claims a *successful* response came back with nothing in it. Those are different
facts and the interface says which one is true. `EmptyState` renders only once a fetch
has actually succeeded and the list is genuinely empty. This applies to Dashboard,
Agents, Alert rules, Alert history, Silences, Notifications, Users, Audit log, and Logs —
every page that lists records fetched from PocketBase (and the Docker tab on host
detail, one of several non-page components with the same list-fetching shape, alongside
`LogsView`).

## 10. Shared data layer

`agents` and firing `alerts` are fleet-wide facts that many components need at once
(the sidebar rail, the Dashboard hero and incident list, the Agents table, every host
detail header). Each of those used to fetch and subscribe to its collection
independently, which multiplied into a real request storm — one dashboard load fired
8 `agents` GETs and 6 `alerts`/`alert_rules` GETs, and every route change repeated it.

The fix mirrors the pattern `agentStore` already used correctly for a single consumer,
extended to a single *shared* owner for many:

- **`agentStore`** and **`alertsStore`** (`src/stores/`) are the only two places that
  ever call `pb.collection(...).getFullList()` or `.subscribe()` for these collections.
  `alertsStore` holds raw firing alerts and rules only — it does not re-fetch `agents`
  for name enrichment, since that would just be a second copy of the same data.
- **`silencesStore`** follows the same shape for the `silences` collection. Only the
  Silences page reads it today (via `useSilences`), unlike agents/alerts which the
  sidebar/Dashboard/every host page all need — but it's still owned by `AppShell` rather
  than fetched per-page-visit, so re-opening `/alerts/silences` never re-issues the
  initial `getFullList`, and any future consumer (e.g. a "silences covering this host"
  count on ServerDetail) gets the data for free instead of adding its own fetch.
- **`checksStore`** follows the same shape for the `checks` collection (check identity
  and config — name, type, target, thresholds, tags), read by the Checks page and the
  Dashboard's "Checks" panel (§11). It deliberately does *not* hold live status/latency/
  uptime — that comes from a separate polled fetch of `GET /api/custom/checks/summary`
  (`useChecksSummary`, `src/hooks/useChecksSummary.ts`), the same relationship
  `agentStore` (identity) has with Dashboard's own polled `/api/custom/dashboard`
  (live metrics). `useChecksSummary` is therefore the one hook in this section that does
  fetch itself (on mount, then every 10s) rather than only reading a store — it's a
  page-local live-metrics poll, not fleet-wide identity, so it doesn't belong in
  `AppShell`'s fetch-once lifecycle any more than the Dashboard metrics poll does.
- **`AppShell`** is the single owner of the fetch-once + subscribe-once lifecycle for
  `agentStore`/`alertsStore`/`silencesStore`/`checksStore`, triggered from one
  `useEffect` on mount (it renders exactly once for the whole authenticated session,
  wrapping every protected route).
- **`useActiveAlerts`**, **`useFleetHealth`**, and **`useSilences`** (`src/hooks/`) are
  pure selectors: they read the stores and derive (grouping duplicate alerts, joining
  agent names, computing per-agent status, bucketing a silence into
  active/scheduled/expired) with `useMemo`, but never fetch or subscribe themselves.
  Every consumer — `SignalRail`, `Dashboard`, `Agents`, `ServerDetail`, `Silences` —
  calls these hooks freely without adding a single extra request.

Verified empirically (headless Chrome, dev server, React StrictMode on): loading `/`
now fires exactly 2 GETs each for `agents`/`alerts`/`alert_rules` — StrictMode's
documented dev-only double-invoke of effects, not a storm — and navigating on to
`/agents` and `/servers/:id` fires **zero** further collection-list requests for either
collection. A production build has no StrictMode double-invoke, so the true count there
is 1 GET per collection for the whole session, not per page.

**R2 fix — Alerts and Alert history had regressed this.** Both pages had grown their
own page-local `pb.collection("agents").getFullList()` (and Alert history, `"checks"`
too) purely to build a display-name lookup map, duplicating data `agentStore`/
`checksStore` already held from `AppShell`'s fetch-once effect — exactly the request
storm this section exists to prevent. Both now read `useAgentStore`/`useChecksStore`
directly and derive their lookup maps with `useMemo`, adding zero extra requests. Each
page keeps its own `alert_rules` fetch, since there is no shared store for that
collection yet and each page's `rules` list serves a different purpose (Alerts owns
`alert_rules` CRUD; Alert history only reads it for name lookups) — introducing a
`rulesStore` was judged out of scope for a consistency pass and is a candidate for a
future work unit if a third consumer appears.

### One status source of truth

`useFleetHealth` exposes `statusByAgentId`, and every place that shows an agent's status
— `ServerCard`, the Agents table and mobile cards, and the host detail header — reads
from it instead of computing its own connectivity-only status. An agent with a firing
critical alert shows critical everywhere at once, not "online" in three places and
"critical" only in the fleet strip.

## 11. Checks (black-box monitoring, `/checks`)

```
┌────────────────────────────────────────────────────────────┐
│ Checks                                        + New check   │
│ ● 4 up   ◆ 1 down   ▲ 1 certificate expiring soon           │  <- summary strip
│ ┌──────────────────────────────────────────────────────┐   │
│ │ ▸ ● Billing API   http  billing.internal/health  42ms │   │
│ │ ▸ ◆ Postgres      tcp   10.0.0.9:5432       —    down │   │
│ │ ▾ ● CDN edge      http  cdn.example.com/…   38ms      │   │
│ │   [latency chart, 24h/7d toggle]                      │   │  <- expanded row
│ │   [recent results: status · time · latency]           │   │
│ └──────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────┘
```

Same list-page shape as Alerts/Silences: `PageHeader` + "New check" (operator+), a
summary strip, and a `Table`. The summary strip reuses `StatusIndicator` for its three
counts ("N up" / "M down" / "K certificates expiring soon" — `summarizeChecks` in
`src/lib/checks.ts`) rather than a bespoke stat-tile row, since it's the same
glyph-plus-label language as everywhere else, just counting checks instead of agents.
Certificate expiry is deliberately its own third count alongside up/down (not folded
into "down", and not merely a badge) — a check can be up and still have a certificate
about to expire, and that's a distinct thing to notice at a glance.

Each row expands in place (a chevron toggle, not a separate detail page or side panel)
into `CheckDetailDrawer`: a 24h/7d latency chart built from `MetricChart` — the exact
same component ServerDetail's metrics tab uses, just fed `[timestamps, latency_ms]` — and
a scrollable list of the most recent results (status dot, time, latency). Reusing
`MetricChart` here rather than a bespoke check-specific chart component keeps the y-scale
enforcement and legend behavior (§3's "Charts on server detail" notes) automatic instead
of re-solving the same "a flat series shouldn't collapse the axis" problem twice.

A check's status maps onto the same `ok`/`warning`/`critical`/`offline` taxonomy as
everywhere else (`checkStatus` in `src/lib/checks.ts`): `down` reads critical, `up` with
a certificate expiring soon reads warning (even though the check itself is healthy —
the certificate is the thing that needs attention), plain `up` reads ok, and a check
that has never completed a single probe yet reads offline rather than a false "ok" —
the same "loading isn't the same as empty isn't the same as failed" discipline as §9,
applied to one row instead of one page.

"Run now" (operator+, a `Play` icon action per row) calls
`POST /api/custom/checks/{id}/run` and refetches the summary immediately afterward,
rather than waiting for the next 10-second poll — the whole point of a manual run is to
see the result right away. Create/edit (`CheckForm`) and delete go straight through
`pb.collection("checks")`, the same direct-PocketBase-CRUD pattern `AlertRuleForm` and
`SilenceForm` already use; only the "run now"/summary/history reads go through the
custom `/api/custom/checks/*` API, since those need the scheduler's live in-memory state
and time-series aggregation that a plain collection query can't produce.

The Dashboard's "Checks" panel (only rendered once at least one check exists) lists
every check that is either down or has a certificate expiring soon, each linking to
`/checks` — mirroring "Active alerts"'s incident-visibility role, but for black-box
targets rather than agent-based alert rules. A healthy fleet of checks shows one
reassuring "All checks are healthy" line instead of an empty list, the same "say the
positive fact, don't just show nothing" choice the Dashboard already makes for zero
active alerts.

`AlertRuleForm`'s `check_down`/`cert_expiry` types (§3 above) share `checksStore`'s data
indirectly — they fetch their own `checks` list for the check selector rather than
reading the store, since the form can be opened from `/alerts` where `checksStore` may
not have been populated for a while and a stale dropdown would be worse than one extra
fetch. `ruleTargetingSummary` (`src/lib/alertRules.ts`) takes an optional `checksById`
map for the same reason the Alerts page already builds one for `agentsById` — resolving
a check-based rule's targeting summary to "All checks" or a check's name instead of a
raw id.

## 12. Audit log (`/settings/audit`)

```
┌────────────────────────────────────────────────────────────┐
│ Audit log                                                     │
│ Who did what, across the whole hub.                           │
│ [Filter by action____] [Filter by actor____]   42 entries     │
│ ┌──────────────────────────────────────────────────────┐   │
│ │▸ Time        Actor            Action          ...  ✓  │   │
│ │▸ 09:14:02    op@example.com   docker.restart   ...  ✓  │   │
│ │▾ 09:12:40    admin@x.com      alert_rule.update...  ✓  │   │
│ │   { "name": "High CPU", "metric_type": "cpu" }        │   │  <- expanded row
│ │▸ 09:10:15    op@example.com   docker.restart   ...  ✗  │   │
│ └──────────────────────────────────────────────────────┘   │
│                          [ Load more ]                        │
└────────────────────────────────────────────────────────────┘
```

An operator-or-higher-only Settings sub-page (§3's Navigation) — unlike every other
list page in this app, it reads a *paged* collection (`pb.collection("audit_log").
getList(page, 50, {sort: "-created", filter})`) rather than `getFullList`, since an
append-only 180-day audit trail can grow far larger than any other collection this app
lists. A "Load more" button (shown only while `page < totalPages`) appends the next page
rather than replacing the list, so scanning through history doesn't lose your place.
Both free-text filters (`Input`, debounced 300ms so a keystroke doesn't fire its own
request) rebuild the PocketBase filter expression and reset to page 1 — "Filter by
action" matches with PocketBase's `~` (contains) operator against `action`, so typing
"docker" surfaces every `docker.*` entry; "Filter by actor" matches the same way against
`actor_email` or `actor_id`. Each row expands in place (a chevron toggle, the same
pattern `CheckDetailDrawer` uses in §11) into a pretty-printed JSON block of `details` —
never a raw string dump, and never a value the hub itself would have redacted (channel
secrets are already stored as field names only server-side, see the audit log README
section). `Result` renders as a small "Success"/"Failure" pill rather than reusing
`StatusIndicator`'s ok/warning/critical/offline taxonomy, since an audit entry's result
is a binary outcome fact, not a live status.

## 13. Logs (`/logs`, and a host detail Logs tab)

```
┌────────────────────────────────────────────────────────────┐
│ Logs                                                          │
│ Search and live-tail journald and file logs shipped from      │
│ every agent.                                                  │
│ [web-01 ▾] [nginx.service ▾] [Search…    ] [15m|1h|24h|Custom]│
│                                                    Live ● ○   │
│ ● All levels  ◆ Error  ▲ Warning  Info  Debug                │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ 09:14:02  ERROR  nginx.service  connection refused   ⧉│   │
│ │ 09:14:01  INFO   app.service    request handled      ⧉│   │
│ │   { "syslog_identifier": "app" }                       │   │  <- expanded row
│ └──────────────────────────────────────────────────────┘   │
│                         [ Load older ]                        │
└────────────────────────────────────────────────────────────┘
```

`src/components/logs/LogsView.tsx` is the one component behind both the standalone Logs
page and a host detail's Logs tab (`src/components/server/LogsTab.tsx`), the same
share-one-component pattern `CheckDetailDrawer` uses for its chart (§11) — the page
renders it with no `agentId` prop (an agent `Select` is shown, defaulting to "All
agents"), the host tab renders it with `agentId` fixed (the selector is hidden entirely,
never shown-and-disabled, since a disabled control the user can't actually use is worse
than no control). Filters: an agent select (page only), level chips (single-select —
"All levels" plus one chip per `error`/`warning`/`info`/`debug`, colored via the same
tokens `StatusIndicator` uses for critical/warning, since a log's `error` level *is* a
critical-severity fact even though this isn't the four-state ok/warning/critical/offline
taxonomy), a unit `Select` (populated from `GET /api/custom/logs/units`, scoped to the
current agent selection), a debounced (300ms, matching Audit log's free-text filters —
§12) search box, and time range presets (15m/1h/24h/Custom, radio-button styled like
`CheckDetailDrawer`'s 24h/7d toggle) with a custom preset revealing two
`datetime-local` inputs. The log list itself is IBM Plex Mono (all data, per §2):
timestamp, level (colored, uppercase), unit (hidden below `sm`), the message (hovering
the row shows the full `source` — `"journald"` or `"file:<path>"` — as a native
tooltip), and a copy-line icon action. A row with non-empty `fields` is clickable to
expand a pretty-printed JSON block beneath it, the same expand-in-place pattern
`CheckDetailDrawer`/Audit log already use rather than a separate detail view. "Load
older" appends the next page using the API's opaque cursor rather than an offset, so
concurrent inserts during pagination can't shift or duplicate rows. The list is capped
at 2,000 rendered rows with a trailing notice ("narrow your filters to see fewer") —
true virtualization was judged unnecessary complexity for a monospace text list at that
scale, and this project has no virtualization dependency already in place to reach for.

## 14. Public status page (`/status`)

This is the one page in this codebase built for a different audience than the rest
of the app. Every other page in this file is read by an operator who is already
inside the tool, on a dark screen, at 3 a.m., cross-referencing six other panels.
`/status` is read by a customer who followed a link from an outage email or a
support ticket, wants one answer — "is it down, and since when" — and will likely
never see another page of NexWatch. That difference drives every choice below;
following DESIGN.md's existing dark-only palette and dense data-table conventions
here would optimize for the wrong reader.

### Plan

**Color** (light-first, since a status page is conventionally linked from public,
usually-light marketing/support surfaces, with a dark variant via
`prefers-color-scheme` for anyone whose system is set to dark):

| Token | Light | Dark | Role |
|---|---|---|---|
| `--sp-bg` | `#f7f8fa` | `#0b0f16` | Page background |
| `--sp-panel` | `#ffffff` | `#131a26` | The one content card |
| `--sp-border` | `#e4e7ec` | `#232d3d` | Card border, dividers |
| `--sp-ink` | `#101828` | `#e7ecf3` | Primary text |
| `--sp-ink-muted` | `#667085` | `#93a0b4` | Secondary text, captions |
| `--sp-ok` | `#1a9c6e` | `#34d399` | Operational |
| `--sp-warn` | `#b5720a` | `#f5a524` | Degraded |
| `--sp-down` | `#d1373f` | `#f5484f` | Down |
| `--sp-unknown` | `#6b7280` | `#5b6576` | Unknown |
| `--sp-accent` | `#3b6fd6` | `#5b9dff` | Links only |

Status hues echo the internal app's semantic palette (§1) for brand continuity —
this is still recognizably a NexWatch surface — but every value is re-picked for
contrast against a white or near-black page instead of the app's blue-charcoal
`--color-void`, since that background doesn't exist here.

**Type**: IBM Plex Sans for every word of prose (headings, descriptions, status
labels) — same family as the app, for the same density/technical-legibility
reasons (§2) — and IBM Plex Mono reserved for exactly two things: the uptime
percentages and the "Updated Ns ago" timestamp, mirroring the app's "mono means
exact data" rule (§2) rather than applying it to the whole page, which would read
as an ops dashboard leaking through.

**Layout**: single centered column, one panel, mobile-first, no sidebar, no nav —
there is nothing else on this site to navigate to.

```
┌──────────────────────────────────────┐
│  Acme status                          │  <- admin-set title
│  Real-time status of Acme's services  │  <- optional description
│                                        │
│  ┌──────────────────────────────────┐ │
│  │ ● All systems operational         │ │  <- one calm banner, not a
│  └──────────────────────────────────┘ │     wall of colored chips
│                                        │
│  ● Billing API            99.98%      │
│  ▁▁▁▁▁▁▂▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁ 30d   │  <- one bar per day, hover
│                                        │     for date/uptime/incidents
│  ● Web tier                100%       │
│  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁ 30d   │
│                                        │
│  Updated 12s ago                      │
└──────────────────────────────────────┘
```

**Principles**:
- Calm during an incident. One reduced status vocabulary (operational / degraded /
  down / unknown), one banner reporting the worst of them — never a grid of red
  that reads more alarming than the actual blast radius.
- Never show what the admin didn't choose to show. Every string on the page is
  either static UI copy or the admin's own `label` — never a hostname, IP, check
  target, or internal id (enforced server-side too; see the API contract).
  The page itself is a second layer of that same discipline: it renders only the
  fields the API returns, so there's no client-side field to accidentally add.
- A day bar is the industry-standard way to answer "when did this last break,"
  because it *is* a sequence (calendar days) — unlike the numbered-marker pattern
  the frontend-design skill warns against, which is a tell precisely when applied
  to non-sequential content.

### Self-critique against the generic-tell checklist

1. Cream+terracotta / near-black+acid-green — neither: light mode is a neutral
   near-white, dark mode reuses the app's own blue-charcoal, not a generic near-black.
2. SaaS-card kit (identical rounded cards, one shadow everywhere) — rejected; this
   page is intentionally *one* card, not a grid of them, because there is exactly
   one thing on it: the fleet's status.
3. Tracked-out ALL-CAPS eyebrows, middle-dot meta strings — none used; matches
   the main app's copy rules (§6).
4. Arrow-suffixed links — none; there are no links to elsewhere.
5. A day-by-day bar could read as the generic "01/02/03" numbered-step pattern —
   reviewed and kept: the content genuinely is a sequence (30 calendar days), which
   is exactly the exception the frontend-design skill calls out.

---

**Live tail** is a `Toggle` (§7) that subscribes to PocketBase realtime directly on the
`logs` collection (no custom endpoint — the collection's `ListRule`, "any authenticated
user," already gates realtime the same way it gates `list`/`view`) with a `filter`
expression built from the same agent/level/unit/search filters the search box uses
(`buildLogsRealtimeFilter` in `src/lib/logs.ts`); time range and cursor don't apply to a
live "from now on" stream. A new entry normally prepends immediately, but scrolling the
list away from the top (any `scrollTop > 4px`) latches a `paused` state that buffers
incoming entries instead of inserting them mid-read — the same "don't yank the content
out from under an actively-scrolling user" principle behind chat apps' "new messages"
bars — surfaced as a pill ("N new lines — click to show") above the list; clicking it
flushes the buffer, prepends everything at once, and scrolls back to the top. Turning
Live off clears both the pause state and any buffered lines, since they're only
meaningful while a live stream is actually open. Disabling and re-enabling Live, or
changing a filter while Live is on, always re-subscribes rather than reusing a stale
subscription against a now-wrong filter expression.

Empty/error states follow §9 exactly (loading → error → empty → content, `EmptyState`/
`ErrorState`, never silently falling through). The alert rule form's `log_match` type
(§3 above, §Alerting's "New rule types" in the README) is documented there rather than
duplicated here.

## 15. PWA and browser notifications (F11)

NexWatch is installable and can deliver alerts as native browser
notifications, even when no tab is open. This is infrastructure the
existing design language mostly already covers — the additions below are
small and deliberately quiet, matching §5's "one deliberate motion, no
scattered chrome" rule rather than introducing a new visual system.

**Offline banner.** A thin, full-width bar (`OfflineBanner`,
`src/components/layout/OfflineBanner.tsx`) appears at the very top of
`AppShell`, above the sidebar/main-content row, only while
`navigator.onLine` is false. It uses the existing `--color-warn` token
(the same amber as a warning `StatusIndicator`) rather than a new color,
since "the app is showing stale data" is the same severity class as any
other warning-level fact in this app. It disappears the instant an
`online` event fires — no fade, matching §5's "everything else is a fast,
user-triggered transition" (here, the network coming back is the trigger).

**Update-available prompt.** `UpdateAvailableToast`
(`src/components/layout/UpdateAvailableToast.tsx`) is deliberately NOT the
generic auto-dismissing toast system (`components/ui/Toast.tsx`, §7): a
waiting service worker update can sit unnoticed for a long time, and
auto-dismissing after 4 seconds would mean it's silently missed. It uses
the same bottom-right placement and panel-raised surface as the real toast
system for visual consistency, but persists until the user clicks
"Reload" or dismisses it, backed by `pwaStore` (`src/stores/pwaStore.ts`)
— the one bridge between the service worker registration lifecycle
(registered in `main.tsx`, outside the React tree) and React state.

**Browser notifications panel.** `BrowserNotificationsSettings`
(`src/components/settings/BrowserNotificationsSettings.tsx`), mounted at
the top of Settings › Notifications, above the channel list — it's
personal to the signed-in user (their own devices), not an
operator-managed fleet setting, so unlike "Add channel" it renders for
every role. It follows the same Panel/PanelHeader/PanelBody shape as every
other settings panel (§7) and the same support-detection pattern as a
`PlatformUnsupportedState`: a browser with no Push API support (or one not
served over HTTPS) sees one explanatory sentence instead of a broken form.
The device list reuses the same "state that actually happened" discipline
as §9's data-states rule: loading, "no devices enabled yet", or the list —
never a silent empty render if the fetch itself failed.

**`webpush` notification channel.** Added to `NotificationChannels.tsx`'s
existing channel-type list (§ "Notification channels" in the README) with
one config field — an audience `Select` (all/admins/operators) — rather
than the free-text `Input` fields every other channel type uses, since
there's nothing here for an operator to type. The `ConfigField` type
gained a `"select"` variant for exactly this case; a select field's
submitted value defaults to its first option when untouched, matching
what the control visibly shows as selected (see `fieldValue` in that
file) rather than silently submitting an empty string.

**Service worker.** `src/sw.ts` (an `injectManifest`-strategy worker, not
`vite-plugin-pwa`'s auto-generated one) precaches the built app shell with
`workbox-precaching` — JS, CSS, HTML, the self-hosted `@fontsource` fonts,
and the icon set — and nothing else. It never adds a runtime-caching route
for `/api/*`, `/ws/*`, or `/_/` (PocketBase's own routes): there is no
fetch handler for them at all, so every live request reaches the network
exactly as if the service worker didn't exist. This matters more here than
in a typical PWA, since a stale cached `/api/custom/dashboard` response
would silently misreport fleet health — the one thing this app exists to
get right.

**Icons.** `public/pwa-64x64.png`, `pwa-192x192.png`, `pwa-512x512.png`,
`maskable-icon-512x512.png`, `apple-touch-icon-180x180.png`, and
`favicon.ico` are generated from the existing `favicon.svg` mark (§1's
void/signal palette, unchanged) via `@vite-pwa/assets-generator`'s
`minimal2023` preset (`pwa-assets.config.ts`) and committed — the
production build never depends on the generator or its native `sharp`
dependency at build or runtime. `theme_color`/`background_color` in both
`index.html`'s meta tags and the web manifest use `--color-void`
(`#0a0e16`), matching the app's existing dark-only chrome rather than
introducing the signal-blue accent as a second, unrelated brand color for
browser tinting.
