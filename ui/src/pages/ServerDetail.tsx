import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import type uPlot from "uplot";
import {
  ArrowLeft,
  Activity,
  Clock,
  Globe,
  Cpu,
  HardDrive,
  MonitorSmartphone,
  Container,
  Wifi,
  ListTree,
  Shield,
  ShieldAlert,
  Server,
  Database,
  BarChart2,
  Layers,
  Box,
  Monitor,
  FileCode2,
  BellRing,
  BellOff,
  Bug,
  ScrollText,
  ArrowUpCircle,
} from "lucide-react";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import type { Agent, MetricsResponse } from "@/types";
import { useFleetHealth } from "@/hooks/useFleetHealth";
import { useAuthStore } from "@/stores/authStore";
import { useAgentUpdateInfo } from "@/hooks/useAgentUpdateInfo";
import { updateAvailable } from "@/lib/agentUpdates";
import { UpdateAvailableBadge, UpdateStatusChip } from "@/components/agents/UpdateBadges";
import { UpdateAgentModal } from "@/components/agents/UpdateAgentModal";
import { MetricChart } from "@/components/charts/MetricChart";
import { TimeRangeSelector } from "@/components/charts/TimeRangeSelector";
import { DockerTab } from "@/components/server/DockerTab";
import { PortsTab } from "@/components/server/PortsTab";
import { ServicesTab } from "@/components/server/ServicesTab";
import { HardeningTab } from "@/components/server/HardeningTab";
import { VulnerabilitiesTab } from "@/components/server/VulnerabilitiesTab";
import { CveScanTab } from "@/components/server/CveScanTab";
import { LogsTab } from "@/components/server/LogsTab";
import { SystemTab } from "@/components/server/SystemTab";
import { ThreadDumpsTab } from "@/components/server/ThreadDumpsTab";
import { OracleTab } from "@/components/server/OracleTab";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { PlatformUnsupportedState } from "@/components/ui/PlatformUnsupportedState";
import { Tabs } from "@/components/ui/Tabs";
import { PlatformIcon } from "@/components/ui/PlatformIcon";
import { Panel } from "@/components/ui/Panel";
import { Skeleton } from "@/components/ui/Skeleton";
import { Button } from "@/components/ui/Button";
import { TagChips } from "@/components/ui/TagChips";
import { AckControl } from "@/components/alerts/AckControl";
import { SilencedBadge, EscalatedBadge } from "@/components/alerts/AlertBadges";
import { SilenceForm } from "@/components/silences/SilenceForm";
import { usePageTitle } from "@/hooks/usePageTitle";

type Tab =
  | "metrics"
  | "alerts"
  | "logs"
  | "docker"
  | "ports"
  | "system"
  | "services"
  | "hardening"
  | "vulnerabilities"
  | "cve"
  | "threaddumps"
  | "oracle";

const TABS: { key: Tab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { key: "metrics", label: "Metrics", icon: Activity },
  { key: "alerts", label: "Alerts", icon: BellRing },
  { key: "logs", label: "Logs", icon: ScrollText },
  { key: "docker", label: "Docker", icon: Container },
  { key: "ports", label: "Ports", icon: Wifi },
  { key: "system", label: "System", icon: Monitor },
  { key: "services", label: "Services", icon: ListTree },
  { key: "hardening", label: "Hardening", icon: Shield },
  { key: "vulnerabilities", label: "Misconfigurations", icon: ShieldAlert },
  { key: "cve", label: "CVE scan", icon: Bug },
  { key: "threaddumps", label: "Thread dumps", icon: FileCode2 },
  { key: "oracle", label: "Oracle DB", icon: Database },
];

const METRICS_REFRESH_INTERVAL = 15_000;

/** Time range durations in seconds */
const TIME_RANGE_DURATIONS: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
  "30d": 2592000,
};

/** Generate mock empty chart data when no metrics are available */
function emptyTimeSeries(): uPlot.AlignedData {
  return [[], []];
}

/**
 * The hub reports network_rx/network_tx as a cumulative byte counter
 * (converted to MB) since the agent started, not a rate — so charting the
 * raw values directly produces an ever-climbing line mislabeled "MB/s".
 * This derives an actual MB/s rate from consecutive samples, clamping a
 * negative delta (the counter reset on an agent restart) to zero.
 */
function toNetworkRateSeries(
  rx: { timestamps: number[]; values: number[] } | undefined,
  tx: { timestamps: number[]; values: number[] } | undefined,
): uPlot.AlignedData {
  if (!rx || !tx || rx.timestamps.length < 2) return emptyTimeSeries();

  const timestamps: number[] = [];
  const rxRates: number[] = [];
  const txRates: number[] = [];

  for (let i = 1; i < rx.timestamps.length; i++) {
    const dt = rx.timestamps[i]! - rx.timestamps[i - 1]!;
    if (dt <= 0) continue;
    const rxDelta = rx.values[i]! - rx.values[i - 1]!;
    const txDelta = tx.values[i]! - tx.values[i - 1]!;
    timestamps.push(rx.timestamps[i]!);
    rxRates.push(rxDelta > 0 ? rxDelta / dt : 0);
    txRates.push(txDelta > 0 ? txDelta / dt : 0);
  }

  return [timestamps, rxRates, txRates];
}

interface HardwareInfo {
  cpu_logical?: number;
  cpu_physical?: number;
  total_ram?: number;
  kernel?: string;
  arch?: string;
  uptime?: number;
  load1?: number;
  load5?: number;
  load15?: number;
  procs?: number;
  platform?: string;
  platform_version?: string;
}

/** Format seconds into a human-readable uptime string: "5d 3h", "2h 14m", "45m" */
function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days >= 1) return `${days}d ${hours}h`;
  if (hours >= 1) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

/** Format bytes into a human-readable string: "15.9 GB", "512 MB" */
function formatBytes(bytes: number): string {
  if (bytes >= 1_073_741_824) {
    return `${(bytes / 1_073_741_824).toFixed(1)} GB`;
  }
  if (bytes >= 1_048_576) {
    return `${(bytes / 1_048_576).toFixed(0)} MB`;
  }
  return `${(bytes / 1024).toFixed(0)} KB`;
}

/** One labeled fact in the host meta row — replaces middle-dot-joined strings
 *  with explicit label/value pairs. See DESIGN.md §6. */
function MetaField({
  icon: Icon,
  label,
  value,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label?: string;
  value: React.ReactNode;
}) {
  return (
    <span className="flex items-center gap-1.5 text-sm">
      <Icon className="h-3.5 w-3.5 text-[var(--color-ink-faint)]" aria-hidden="true" />
      {label && <span className="text-[var(--color-ink-faint)]">{label}</span>}
      <span className="text-[var(--color-ink-muted)]">{value}</span>
    </span>
  );
}

export function ServerDetail() {
  const { id } = useParams<{ id: string }>();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [hardware, setHardware] = useState<HardwareInfo | null>(null);
  const [metrics, setMetrics] = useState<MetricsResponse | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>("metrics");
  const [timeRange, setTimeRange] = useState("1h");
  const [loading, setLoading] = useState(true);
  const [metricsLoading, setMetricsLoading] = useState(false);
  const metricsIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const timeRangeRef = useRef(timeRange);
  // Same alert-derived status used everywhere else — see DESIGN.md §3 ("one
  // status source of truth"). Pure read; agents/alerts are fetched and
  // subscribed once by AppShell.
  const { statusByAgentId, activeAlerts, activeAlertsLoading, activeAlertsError } =
    useFleetHealth();
  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const [showSilenceForm, setShowSilenceForm] = useState(false);
  const { compareVersion } = useAgentUpdateInfo();
  const [showUpdateModal, setShowUpdateModal] = useState(false);

  const hostAlerts = useMemo(
    () => activeAlerts.filter((a) => a.agentId === id),
    [activeAlerts, id],
  );

  usePageTitle(agent?.hostname || agent?.name || "Server");

  // Keep ref in sync for interval callback
  useEffect(() => {
    timeRangeRef.current = timeRange;
  }, [timeRange]);

  // Fetch agent
  useEffect(() => {
    if (!id) return;

    async function fetchAgent() {
      try {
        const record = await pb.collection("agents").getOne<Agent>(id!);
        setAgent(record);

        // Fetch hardware info alongside agent data
        try {
          const hwResponse = await apiFetch(`/api/custom/agents/${id}/hardware`);
          if (hwResponse.ok) {
            const hwData = (await hwResponse.json()) as HardwareInfo;
            setHardware(hwData);
          }
        } catch {
          // Hardware data is optional — fail silently
        }
      } catch {
        setAgent(null);
      } finally {
        setLoading(false);
      }
    }

    void fetchAgent();

    // Live-update this one agent record — needed so the update_status/
    // update_error chip in the header reflects a self-update's progress
    // without a manual refresh. Scoped to this single record id (rather
    // than the "*" wildcard useAgentStore subscribes to for the
    // Agents/Dashboard pages) since this page only ever needs one agent
    // and already owns its own `agent` state independent of that store.
    const unsubscribePromise = pb.collection("agents").subscribe<Agent>(id, (event) => {
      if (event.action === "update") {
        setAgent(event.record);
      }
    });

    return () => {
      void unsubscribePromise.then((unsub) => unsub());
    };
  }, [id]);

  // Fetch metrics
  const fetchMetrics = useCallback(
    async (start: number, end: number, showLoading = true) => {
      if (!id) return;
      if (showLoading) setMetricsLoading(true);
      try {
        const response = await apiFetch(
          `/api/custom/metrics?agent_id=${id}&start=${start}&end=${end}`,
        );
        if (response.ok) {
          const data = (await response.json()) as MetricsResponse;
          setMetrics(data);
        }
      } catch {
        // API might not be available yet
        setMetrics(null);
      } finally {
        setMetricsLoading(false);
      }
    },
    [id],
  );

  // Auto-refresh metrics polling
  useEffect(() => {
    // Initial fetch
    const end = Math.floor(Date.now() / 1000);
    const start = end - 3600; // 1h default
    void fetchMetrics(start, end);

    // Set up polling interval
    metricsIntervalRef.current = setInterval(() => {
      const now = Math.floor(Date.now() / 1000);
      const duration = TIME_RANGE_DURATIONS[timeRangeRef.current] ?? 3600;
      void fetchMetrics(now - duration, now, false);
    }, METRICS_REFRESH_INTERVAL);

    return () => {
      if (metricsIntervalRef.current) clearInterval(metricsIntervalRef.current);
    };
  }, [fetchMetrics]);

  function handleTimeRangeChange(range: { value: string; start: number; end: number }) {
    setTimeRange(range.value);
    void fetchMetrics(range.start, range.end);
  }

  /** Convert TimeSeries to uPlot data format */
  function toUPlotData(
    ts: { timestamps: number[]; values: number[] } | undefined,
  ): uPlot.AlignedData {
    if (!ts || ts.timestamps.length === 0) return emptyTimeSeries();
    return [ts.timestamps, ts.values];
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-10 w-full max-w-2xl" />
      </div>
    );
  }

  if (!agent) {
    return (
      <Panel>
        <EmptyState
          icon={MonitorSmartphone}
          title="Agent not found"
          description={`The agent with ID "${id}" does not exist.`}
          action={
            <Link
              to="/"
              className="inline-flex items-center gap-2 rounded-[var(--radius-control)] bg-[var(--color-signal)] px-4 py-2 text-sm font-medium text-[var(--color-void)] transition-opacity hover:opacity-90"
            >
              <ArrowLeft className="h-4 w-4" />
              Back to dashboard
            </Link>
          }
        />
      </Panel>
    );
  }

  const status = statusByAgentId.get(agent.id) ?? "offline";

  return (
    <div>
      {/* Breadcrumb */}
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1.5 text-sm text-[var(--color-ink-muted)] transition-colors hover:text-[var(--color-signal)]"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Dashboard
      </Link>

      {/* Header */}
      <Panel className="mb-6 p-6">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-2xl font-bold text-[var(--color-ink)]">
              {agent.hostname || agent.name}
            </h2>
            <StatusIndicator status={status} />
            <TagChips tags={agent.tags ?? []} />
            {updateAvailable(agent, compareVersion) && (
              <UpdateAvailableBadge targetVersion={compareVersion} />
            )}
            <UpdateStatusChip
              status={agent.update_status}
              error={agent.update_error}
              onRetry={canManage ? () => setShowUpdateModal(true) : undefined}
            />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {canManage && updateAvailable(agent, compareVersion) && (
              <Button size="sm" variant="secondary" onClick={() => setShowUpdateModal(true)}>
                <ArrowUpCircle className="h-3.5 w-3.5" aria-hidden="true" />
                Update agent
              </Button>
            )}
            {canManage && (
              <Button size="sm" variant="secondary" onClick={() => setShowSilenceForm(true)}>
                <BellOff className="h-3.5 w-3.5" aria-hidden="true" />
                Silence this host
              </Button>
            )}
          </div>
        </div>

        {/* Row 1: always shown — from agent record */}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5">
          <MetaField icon={Globe} value={agent.ip || "No IP"} />
          {/* OS/platform: uses PlatformIcon (windows/linux/darwin glyph)
              instead of MetaField's generic icon slot, matching the same
              glyph shown next to this agent's OS in the Agents table. */}
          <span className="flex items-center gap-1.5 text-sm">
            <PlatformIcon platform={agent.platform} />
            <span className="text-[var(--color-ink-muted)]">
              {hardware?.platform
                ? `${hardware.platform}${hardware.platform_version ? ` ${hardware.platform_version}` : ""}`
                : agent.os || "Unknown OS"}
            </span>
          </span>
          <MetaField icon={HardDrive} value={`v${agent.version || "0.0.0"}`} />
          <MetaField
            icon={Clock}
            label="Last seen:"
            value={agent.last_seen ? new Date(agent.last_seen).toLocaleString() : "Never"}
          />
        </div>

        {/* Row 2: shown only when hardware data is available */}
        {hardware && (
          <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-1.5">
            {hardware.kernel && <MetaField icon={Server} label="Kernel:" value={hardware.kernel} />}
            {(hardware.cpu_logical ?? 0) > 0 && (
              <MetaField
                icon={Cpu}
                label="Cores:"
                value={
                  hardware.cpu_physical && hardware.cpu_physical !== hardware.cpu_logical
                    ? `${hardware.cpu_logical} (${hardware.cpu_physical} physical)`
                    : hardware.cpu_logical
                }
              />
            )}
            {(hardware.total_ram ?? 0) > 0 && (
              <MetaField icon={Database} label="RAM:" value={formatBytes(hardware.total_ram!)} />
            )}
            {(hardware.uptime ?? 0) > 0 && (
              <MetaField icon={Activity} label="Uptime:" value={formatUptime(hardware.uptime!)} />
            )}
            {hardware.load1 !== undefined && (
              <MetaField
                icon={BarChart2}
                label="Load:"
                value={`${hardware.load1.toFixed(2)} / ${(hardware.load5 ?? 0).toFixed(2)} / ${(hardware.load15 ?? 0).toFixed(2)}`}
              />
            )}
            {(hardware.procs ?? 0) > 0 && (
              <MetaField icon={Layers} value={`${hardware.procs} processes`} />
            )}
            {hardware.arch && <MetaField icon={Box} label="Arch:" value={hardware.arch} />}
          </div>
        )}
      </Panel>

      {/* Tab bar — full row width so a long tab list (twelve on this page)
          scrolls within itself rather than sharing the row with anything
          else and running out of space (R2 fix). The time range selector
          only applies to the Metrics tab, so it now lives in that tab's own
          content instead of competing with the tab bar for width. */}
      <div className="mb-6 border-b border-[var(--color-line)]">
        <Tabs
          items={TABS}
          activeKey={activeTab}
          onChange={setActiveTab}
          aria-label="Server detail sections"
          className="-mb-px"
        />
      </div>

      {/* Tab content */}
      {activeTab === "metrics" && (
        <div>
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div className="min-h-[1.25rem]">
              {metricsLoading && (
                <div className="flex items-center gap-2 text-sm text-[var(--color-ink-muted)]">
                  <Activity
                    className="h-4 w-4 animate-pulse text-[var(--color-signal)]"
                    aria-hidden="true"
                  />
                  Refreshing metrics…
                </div>
              )}
            </div>
            <TimeRangeSelector selected={timeRange} onChange={handleTimeRangeChange} />
          </div>

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <MetricChart
              title="CPU usage"
              data={toUPlotData(metrics?.cpu)}
              unit="%"
              colors={["#5b9dff"]}
              seriesLabels={["CPU"]}
            />
            <MetricChart
              title="Memory usage"
              data={toUPlotData(metrics?.memory)}
              unit="%"
              colors={["#34d399"]}
              seriesLabels={["Memory"]}
            />
            <MetricChart
              title="Disk usage"
              data={toUPlotData(metrics?.disk)}
              unit="%"
              colors={["#f5a524"]}
              seriesLabels={["Disk"]}
            />
            <MetricChart
              title="Network"
              data={toNetworkRateSeries(metrics?.network_rx, metrics?.network_tx)}
              unit="MB/s"
              colors={["#5b9dff", "#f5a524"]}
              seriesLabels={["RX", "TX"]}
            />
          </div>
        </div>
      )}

      {activeTab === "alerts" && (
        <Panel>
          {activeAlertsLoading && hostAlerts.length === 0 ? (
            <div className="p-5">
              <Skeleton className="h-16 w-full" />
            </div>
          ) : activeAlertsError ? (
            <EmptyState
              icon={BellRing}
              title="Couldn't load alerts"
              description={activeAlertsError}
            />
          ) : hostAlerts.length === 0 ? (
            <EmptyState icon={BellRing} title="No active alerts for this host" />
          ) : (
            <div className="divide-y divide-[var(--color-line-soft)]">
              {hostAlerts.map((alert) => (
                <div
                  key={alert.id}
                  className="flex flex-wrap items-center gap-3 px-5 py-3 sm:flex-nowrap sm:gap-4"
                >
                  <StatusIndicator status={alert.severity} dotOnly />
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-1.5 truncate text-sm font-medium text-[var(--color-ink)]">
                      {alert.ruleName}
                      {alert.count > 1 && (
                        <span className="text-2xs rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 font-mono text-[var(--color-ink-faint)] tabular-nums">
                          ×{alert.count}
                        </span>
                      )}
                    </p>
                    <p className="truncate text-xs text-[var(--color-ink-faint)]">
                      {timeSince(alert.firedAt)}
                    </p>
                  </div>
                  {alert.silenced && <SilencedBadge />}
                  {alert.escalatedAt && <EscalatedBadge />}
                  <AckControl
                    alertId={alert.id}
                    acknowledgedAt={alert.acknowledgedAt}
                    acknowledgedBy={alert.acknowledgedBy}
                    canManage={canManage}
                  />
                </div>
              ))}
            </div>
          )}
        </Panel>
      )}

      {activeTab === "logs" && id && <LogsTab agentId={id} />}
      {activeTab === "docker" && id && <DockerTab agentId={id} />}
      {activeTab === "ports" && id && <PortsTab agentId={id} />}
      {activeTab === "system" && id && <SystemTab agentId={id} />}
      {activeTab === "services" && id && <ServicesTab agentId={id} />}
      {activeTab === "hardening" && id && <HardeningTab agentId={id} />}
      {activeTab === "vulnerabilities" && id && (
        // vulnerabilities (Misconfigurations) has no Windows implementation
        // (internal/agent/platform.Supported) — a Windows agent never
        // registers this collector, so its data is permanently absent
        // rather than merely not-yet-reported. Show that plainly instead
        // of the tab's normal loading/error/empty fetch cycle.
        <>
          {agent?.platform === "windows" ? (
            <PlatformUnsupportedState feature="Misconfigurations" platform="Windows" />
          ) : (
            <VulnerabilitiesTab agentId={id} />
          )}
        </>
      )}
      {activeTab === "cve" && id && <CveScanTab agentId={id} />}
      {activeTab === "threaddumps" && id && <ThreadDumpsTab agentId={id} />}
      {activeTab === "oracle" && id && (
        // oracle has no Windows implementation — see the vulnerabilities
        // tab's identical comment above.
        <>
          {agent?.platform === "windows" ? (
            <PlatformUnsupportedState feature="Oracle DB monitoring" platform="Windows" />
          ) : (
            <OracleTab agentId={id} />
          )}
        </>
      )}

      {showSilenceForm && (
        <SilenceForm
          initialAgentId={agent.id}
          initialAgentHostname={agent.hostname || agent.name}
          onSave={() => setShowSilenceForm(false)}
          onClose={() => setShowSilenceForm(false)}
        />
      )}

      {showUpdateModal && (
        <UpdateAgentModal
          agent={agent}
          defaultVersion={compareVersion}
          onClose={() => setShowUpdateModal(false)}
        />
      )}
    </div>
  );
}
