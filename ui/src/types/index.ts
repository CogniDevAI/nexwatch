/** PocketBase record base fields */
export interface PBRecord {
  id: string;
  created: string;
  updated: string;
  collectionId: string;
  collectionName: string;
}

/** Agent registered in the hub */
/** Progress stage of a hub-initiated self-update (F9). Empty string and
 *  "idle" both mean "no update in progress" — an agent record that has
 *  never been targeted for an update simply has an empty update_status. */
export type AgentUpdateStatus =
  | ""
  | "idle"
  | "started"
  | "downloading"
  | "verifying"
  | "installing"
  | "restarting"
  | "failed"
  | "done"
  /** Windows only: the new binary was installed but its running .exe was
   *  locked, so the swap is scheduled for the next host restart instead of
   *  taking effect immediately (see Result.RestartRequired in
   *  internal/agent/update/update.go). Distinct from "restarting", which
   *  means an immediate re-exec is already underway. */
  | "restart_required";

export interface Agent extends PBRecord {
  name: string;
  hostname: string;
  os: string;
  ip: string;
  version: string;
  status: "online" | "offline";
  last_seen: string;
  /** Free-form labels used for alert-rule/silence targeting. The API can
   *  return `null` for an agent that has never had tags set — always read
   *  this via `agent.tags ?? []`, never assume a non-null array. */
  tags: string[];
  /** Runtime.GOARCH (e.g. "amd64", "arm64"), reported on REGISTER. Empty
   *  for an agent that has never connected with a build that sends it. */
  arch: string;
  /** Runtime.GOOS (e.g. "linux", "darwin"), reported on REGISTER —
   *  distinct from `os` (a human-readable display string), see
   *  internal/shared/protocol.RegisterPayload's doc comment. */
  platform: string;
  /** Self-update progress — see AgentUpdateStatus. */
  update_status: AgentUpdateStatus;
  /** The last update failure's message, if update_status is "failed". */
  update_error: string;
  /** The version most recently requested via a self-update. */
  update_target_version: string;
  update_requested_at: string;
  update_requested_by: string;
}

/** Raw metric data point from PocketBase */
export interface MetricRecord extends PBRecord {
  agent: string;
  type: "cpu" | "memory" | "disk" | "network" | "sysinfo";
  data: Record<string, unknown>;
  timestamp: string;
}

/** Docker container tracked per agent */
export interface DockerContainer extends PBRecord {
  agent_id: string;
  container_id: string;
  name: string;
  image: string;
  status: "running" | "stopped" | "paused" | "restarting" | "removing" | "exited" | "dead";
  // Field names below match the "docker_containers" collection schema
  // exactly (internal/hub/migrations/collections.go). The agent only
  // collects stats for a running container, so a stopped one simply keeps
  // whatever it last reported while running (0 if it never ran).
  cpu_percent: number;
  memory_usage: number;
  memory_limit: number;
  network_rx: number;
  network_tx: number;
  /** Image update-detection fields (internal/agent/collector/docker_update.go).
   *  Absent/empty for a container whose image has no registry reference
   *  (e.g. locally built), when the agent's docker_update_checks config is
   *  disabled, or before the agent has reported at all. */
  image_digest?: string;
  remote_digest?: string;
  update_available?: boolean;
}

/** Lifecycle action available on a running/stopped Docker container. */
export type DockerAction = "start" | "stop" | "restart";

/** Metric-threshold rule types — always use condition/threshold. */
export type ResourceMetricType = "cpu" | "memory" | "disk" | "network" | "docker" | "cve_count";

/** Boolean rule types — breach is yes/no, condition/threshold are ignored. */
export type AvailabilityMetricType =
  "process_down" | "service_failed" | "agent_offline" | "check_down" | "cert_expiry";

/** Log-based rule type — uses threshold/duration (count of matching log
 *  lines in the last `duration` seconds), but ignores `condition` (always
 *  "count >= threshold"). */
export type LogMetricType = "log_match";

/** Alert rule configuration */
export interface AlertRule extends PBRecord {
  name: string;
  metric_type: ResourceMetricType | AvailabilityMetricType | LogMetricType;
  condition: "gt" | "lt" | "eq";
  threshold: number;
  duration: number;
  severity: "warning" | "critical";
  enabled: boolean;
  /** Targeting: `agent_id` set -> that agent only; else `target_tags`
   *  non-empty -> agents with ANY of those tags; else every agent. */
  agent_id: string;
  target_tags: string[];
  /** Process/service name to match — used by process_down/service_failed only. */
  target: string;
  notification_channels: string[];
  /** Second notification set, dispatched once after `escalation_after`
   *  seconds of unacknowledged, unsilenced firing (0 disables escalation). */
  escalation_channels: string[];
  escalation_after: number;
  /** check_down/cert_expiry only: the check this rule targets, or empty to
   *  apply to every check. For cert_expiry, `threshold` is reused as "warn
   *  within N days" rather than a metric comparison value. */
  check_id: string;
}

/** Fired alert instance */
export interface Alert extends PBRecord {
  rule_id: string;
  /** Empty for a check_down/cert_expiry-triggered alert — see check_id. */
  agent_id: string;
  /** Set instead of agent_id for a check_down/cert_expiry-triggered alert. */
  check_id: string;
  status: "firing" | "resolved";
  value: number;
  message: string;
  fired_at: string;
  resolved_at: string;
  silenced: boolean;
  /** Set by POST /api/custom/alerts/{id}/ack — already formatted by the hub
   *  as "<user id> (<email>)", ready to display as-is. */
  acknowledged_by: string;
  acknowledged_at: string;
  /** Set at most once per firing episode when escalation dispatches. */
  escalated_at: string;
}

/** Black-box monitored target (hub-side HTTP/TCP/ICMP probe). */
export interface Check extends PBRecord {
  name: string;
  type: "http" | "tcp" | "icmp";
  /** URL for http, "host:port" for tcp, a bare hostname/IP for icmp. */
  target: string;
  interval_seconds: number;
  timeout_seconds: number;
  /** http only. */
  method: "GET" | "HEAD";
  /** http only. */
  expected_status: number;
  /** http only, optional. */
  expected_body_contains: string;
  /** http only. */
  verify_tls: boolean;
  /** http only — days before certificate expiry to flag as "expiring soon". */
  tls_expiry_warn_days: number;
  failures_before_down: number;
  enabled: boolean;
  tags: string[];
}

/** One row of GET /api/custom/checks/summary. */
export interface CheckSummary {
  id: string;
  name: string;
  type: Check["type"];
  target: string;
  /** Absent if the check has never run yet. */
  status?: "up" | "down";
  latency_ms: number;
  last_checked_at?: string;
  /** Only present for http checks that recorded a TLS certificate. */
  tls_expires_at?: string;
  cert_expiring_soon: boolean;
  uptime_24h: number;
  uptime_7d: number;
  enabled: boolean;
}

/** Result of POST /api/custom/checks/{id}/run. */
export interface CheckRunResult {
  id: string;
  check_id: string;
  status: "up" | "down";
  latency_ms: number;
  status_code: number;
  error: string;
  tls_expires_at: string;
  checked_at: string;
}

/** One point of GET /api/custom/checks/{id}/results. */
export interface CheckResultPoint {
  timestamp: number;
  latency_ms: number;
  status: "up" | "down";
}

/** Maintenance-window silence (raw record). See README "Silences". */
export interface Silence extends PBRecord {
  name: string;
  reason: string;
  agent_id: string;
  tags: string[];
  /** Checks (internal/hub/checks) this silence targets directly, distinct
   *  from tags — see the Silences page's "covered checks" count. */
  check_ids: string[];
  starts_at: string;
  ends_at: string;
  created_by: string;
}

/** One entry from GET /api/custom/silences/active — a currently-active
 *  silence annotated with the ids of every agent and check it actually
 *  covers. */
export interface ActiveSilence {
  id: string;
  name: string;
  reason: string;
  agent_id: string;
  tags: string[];
  check_ids: string[];
  starts_at: string;
  ends_at: string;
  covered_agent_ids: string[];
  covered_check_ids: string[];
}

/** Notification channel */
export interface NotificationChannel extends PBRecord {
  name: string;
  type:
    | "email"
    | "webhook"
    | "telegram"
    | "discord"
    | "slack"
    | "teams"
    | "pagerduty"
    | "ntfy"
    | "gotify"
    | "webpush";
  config: Record<string, unknown>;
  enabled: boolean;
}

/** One browser/device Web Push subscription (F11), owned by the
 *  authenticated user it belongs to — see the "push_subscriptions"
 *  migration and BrowserNotificationsSettings. */
export interface PushSubscriptionRecord extends PBRecord {
  user_id: string;
  endpoint: string;
  p256dh: string;
  auth: string;
  user_agent: string;
  last_seen: string;
}

/** Time series data for charts */
export interface TimeSeries {
  timestamps: number[];
  values: number[];
}

/** Structured metrics response from /api/custom/metrics */
export interface MetricsResponse {
  cpu: TimeSeries;
  memory: TimeSeries;
  disk: TimeSeries;
  network_rx: TimeSeries;
  network_tx: TimeSeries;
}

/** Time range option for chart queries */
export interface TimeRange {
  label: string;
  value: string;
  start: number;
  end: number;
}

/** Open port entry reported by agent */
export interface PortEntry {
  port: number;
  protocol: string;
  pid: number;
  process: string;
  address: string;
}

/** Running process entry reported by agent */
export interface ProcessEntry {
  pid: number;
  name: string;
  cpu_percent: number;
  memory_percent: number;
  memory_rss: number;
  status: string;
  user: string;
  command: string;
}

/** Individual hardening check result */
export interface HardeningCheck {
  name: string;
  status: "pass" | "fail" | "warn" | "skip";
  description: string;
  severity: "critical" | "high" | "medium" | "low";
}

/** Aggregated hardening scan data */
export interface HardeningData {
  checks: HardeningCheck[];
  score: number;
  total: number;
  passed: number;
  failed: number;
  warnings: number;
}

/** Individual vulnerability item */
export interface VulnerabilityItem {
  name: string;
  severity: "critical" | "high" | "medium" | "low" | "info";
  description: string;
  recommendation: string;
}

/** Aggregated vulnerability scan data */
export interface VulnerabilityData {
  items: VulnerabilityItem[];
  summary: {
    critical: number;
    high: number;
    medium: number;
    low: number;
    info: number;
    total: number;
  };
}

/** Severity counts shared by a single CVE scan target and the overall totals. */
export interface CveSeverityCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
}

/** One CVE finding (a single scanner-reported advisory match). */
export interface CveFinding {
  id: string;
  severity: "critical" | "high" | "medium" | "low" | "unknown";
  package: string;
  installed: string;
  fixed: string;
  title: string;
  /** The scanner's sub-target (e.g. an OS name, or a lockfile path). */
  target: string;
}

/** One scanned target — the host filesystem, or one running container's image. */
export interface CveScanTarget {
  kind: "host" | "image";
  ref: string;
  counts: CveSeverityCounts;
  /** Count of findings in this target that have a known fix. */
  fixable: number;
  /** Top 200 findings, sorted most-severe-first. */
  findings: CveFinding[];
}

/** CVE scan report from GET /api/custom/agents/{id}/cve
 *  (internal/agent/collector/cvescan.go). */
export interface CveScanData {
  scanner: "trivy" | "grype" | "";
  scanner_version: string;
  db_updated_at: string;
  scanned_at: string;
  duration_ms: number;
  /** True once the last completed scan is older than 2x the configured
   *  interval — the data shown is still the most recent available, just
   *  aging. */
  stale: boolean;
  /** False when neither trivy nor grype is installed on the agent host. */
  available: boolean;
  error: string;
  targets: CveScanTarget[];
  totals: CveSeverityCounts & { fixable: number };
}

/** Authorization role hierarchy: viewer < operator < admin */
export type Role = "viewer" | "operator" | "admin";

/** A "users" collection record, or a synthesized view of a _superusers record */
export interface User extends PBRecord {
  email: string;
  name: string;
  role: Role;
  verified: boolean;
}

/** One row of the append-only audit trail (internal/hub/audit). Read/write
 *  is server-only from the hub's side — the UI only ever lists these. */
export interface AuditLogEntry extends PBRecord {
  actor_id: string;
  actor_email: string;
  actor_role: string;
  /** Dotted action name, e.g. "docker.restart", "alert_rule.update". */
  action: string;
  target_type: string;
  target_id: string;
  /** Set only for agent-scoped actions (docker.*, threaddump.request, ...). */
  agent_id: string;
  /** JSON-encoded object — never contains secret values (see
   *  internal/hub/audit.redactedFieldNames). Parse before display. */
  details: string;
  result: "success" | "failure";
  ip: string;
  request_id: string;
}

/** One shipped log line — see GET /api/custom/logs
 *  (internal/hub/api/logs_routes.go), also the shape a "logs" realtime
 *  event's record carries. */
export interface LogEntry {
  id: string;
  agent_id: string;
  /** Unix milliseconds. */
  ts: number;
  /** "journald", or "file:<path>" for a tailed file. */
  source: string;
  /** systemd unit name (journald sources only). */
  unit?: string;
  level: "error" | "warning" | "info" | "debug";
  message: string;
  /** Small set of extra structured attributes (e.g. journald's
   *  SYSLOG_IDENTIFIER). */
  fields?: Record<string, string>;
}

/** Response shape of GET /api/custom/logs. */
export interface LogsQueryResponse {
  entries: LogEntry[];
  /** Cursor to pass as "before" for the next older page. Absent once
   *  there are no more results. */
  next_before?: string;
}

/** One entry of the status_page_items setting — an admin-curated
 *  allowlist of checks/agents to expose on the public status page, each
 *  with its own public-facing label. */
export interface StatusPageItemConfig {
  type: "check" | "agent";
  id: string;
  label: string;
}

/** One day of a status-page item's uptime history. */
export interface PublicStatusDaily {
  date: string;
  uptime: number;
  incidents: number;
}

/** One row of GET /api/public/status — never carries a raw id, hostname,
 *  IP, or check target; only the admin-typed label. */
export interface PublicStatusItem {
  type: "check" | "agent";
  label: string;
  status: "operational" | "degraded" | "down" | "unknown";
  uptime_24h: number;
  uptime_7d: number;
  uptime_30d: number;
  /** checks only. */
  latency_ms?: number;
  daily: PublicStatusDaily[];
  last_incident_at?: string;
}

/** Response shape of GET /api/public/status. */
export interface PublicStatusResponse {
  title: string;
  description?: string;
  overall: "operational" | "degraded" | "down" | "unknown";
  updated_at: string;
  items: PublicStatusItem[];
}

/** Response shape of GET /api/custom/reports/preview. */
export interface ReportPreview {
  subject: string;
  html: string;
  text: string;
}

/** One channel's outcome in POST /api/custom/reports/send's response. */
export interface ReportSendResult {
  channel_id: string;
  channel_name: string;
  success: boolean;
  error?: string;
}

/** Response shape of POST /api/custom/reports/send. */
export interface ReportSendResponse {
  subject: string;
  results: ReportSendResult[];
}
