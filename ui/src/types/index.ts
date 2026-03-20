/** PocketBase record base fields */
export interface PBRecord {
  id: string;
  created: string;
  updated: string;
  collectionId: string;
  collectionName: string;
}

/** Agent registered in the hub */
export interface Agent extends PBRecord {
  name: string;
  hostname: string;
  os: string;
  ip: string;
  version: string;
  status: "online" | "offline";
  token: string;
  last_seen: string;
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
  agent: string;
  container_id: string;
  name: string;
  image: string;
  status: "running" | "stopped" | "paused" | "restarting" | "removing" | "exited" | "dead";
  cpu: number;
  memory_used: number;
  memory_limit: number;
  network_rx: number;
  network_tx: number;
}

/** Alert rule configuration */
export interface AlertRule extends PBRecord {
  name: string;
  metric_type: "cpu" | "memory" | "disk" | "network" | "docker";
  condition: "gt" | "lt" | "eq";
  threshold: number;
  duration: number;
  severity: "warning" | "critical";
  enabled: boolean;
  agent_id: string;
  notification_channels: string[];
}

/** Fired alert instance */
export interface Alert extends PBRecord {
  rule_id: string;
  agent_id: string;
  status: "firing" | "resolved";
  value: number;
  message: string;
  fired_at: string;
  resolved_at: string;
}

/** Notification channel */
export interface NotificationChannel extends PBRecord {
  name: string;
  type: "email" | "webhook" | "telegram" | "discord";
  config: Record<string, unknown>;
  enabled: boolean;
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
