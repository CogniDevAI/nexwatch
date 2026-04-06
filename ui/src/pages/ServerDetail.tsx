import { useState, useEffect, useCallback, useRef } from "react";
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
} from "lucide-react";
import pb from "@/lib/pocketbase";
import type { Agent, MetricsResponse } from "@/types";
import { MetricChart } from "@/components/charts/MetricChart";
import { TimeRangeSelector } from "@/components/charts/TimeRangeSelector";
import { DockerTab } from "@/components/server/DockerTab";
import { PortsTab } from "@/components/server/PortsTab";
import { ServicesTab } from "@/components/server/ServicesTab";
import { HardeningTab } from "@/components/server/HardeningTab";
import { VulnerabilitiesTab } from "@/components/server/VulnerabilitiesTab";

type Tab = "metrics" | "docker" | "ports" | "services" | "hardening" | "vulnerabilities";

const TABS: { key: Tab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { key: "metrics", label: "Metrics", icon: Activity },
  { key: "docker", label: "Docker", icon: Container },
  { key: "ports", label: "Ports", icon: Wifi },
  { key: "services", label: "Services", icon: ListTree },
  { key: "hardening", label: "Hardening", icon: Shield },
  { key: "vulnerabilities", label: "Vulns", icon: ShieldAlert },
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
          const hwResponse = await fetch(`/api/custom/agents/${id}/hardware`, {
            headers: { Authorization: pb.authStore.token },
          });
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

    fetchAgent();
  }, [id]);

  // Fetch metrics
  const fetchMetrics = useCallback(
    async (start: number, end: number, showLoading = true) => {
      if (!id) return;
      if (showLoading) setMetricsLoading(true);
      try {
        const response = await fetch(
          `/api/custom/metrics?agent_id=${id}&start=${start}&end=${end}`,
          { headers: { Authorization: pb.authStore.token } },
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
    fetchMetrics(start, end);

    // Set up polling interval
    metricsIntervalRef.current = setInterval(() => {
      const now = Math.floor(Date.now() / 1000);
      const duration = TIME_RANGE_DURATIONS[timeRangeRef.current] ?? 3600;
      fetchMetrics(now - duration, now, false);
    }, METRICS_REFRESH_INTERVAL);

    return () => {
      if (metricsIntervalRef.current) clearInterval(metricsIntervalRef.current);
    };
  }, [fetchMetrics]);

  function handleTimeRangeChange(range: {
    value: string;
    start: number;
    end: number;
  }) {
    setTimeRange(range.value);
    fetchMetrics(range.start, range.end);
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
      <div className="flex items-center justify-center py-20">
        <Activity className="w-6 h-6 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-[var(--color-text-secondary)]">
          Loading server...
        </span>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="text-center py-20">
        <MonitorSmartphone className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          Agent not found
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] mb-6">
          The agent with ID <code className="text-[var(--color-accent-cyan)]">{id}</code> does not exist.
        </p>
        <Link
          to="/"
          className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium hover:opacity-90 transition-opacity"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Dashboard
        </Link>
      </div>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <Link
        to="/"
        className="inline-flex items-center gap-1.5 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-accent-cyan)] transition-colors mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        Dashboard
      </Link>

      {/* Header */}
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-6 mb-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <h2 className="text-2xl font-bold text-[var(--color-text-primary)]">
                {agent.hostname || agent.name}
              </h2>
              <span
                className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${
                  agent.status === "online"
                    ? "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]"
                    : "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full ${
                    agent.status === "online"
                      ? "bg-[var(--color-accent-green)]"
                      : "bg-[var(--color-accent-red)]"
                  }`}
                />
                {agent.status}
              </span>
            </div>
            {/* Row 1: always shown — from agent record */}
            <div className="flex flex-wrap items-center gap-4 text-sm text-[var(--color-text-secondary)]">
              <span className="flex items-center gap-1.5">
                <Globe className="w-3.5 h-3.5" />
                {agent.ip || "No IP"}
              </span>
              <span className="flex items-center gap-1.5">
                <Cpu className="w-3.5 h-3.5" />
                {hardware?.platform
                  ? `${hardware.platform}${hardware.platform_version ? ` ${hardware.platform_version}` : ""}`
                  : agent.os || "Unknown OS"}
              </span>
              <span className="flex items-center gap-1.5">
                <HardDrive className="w-3.5 h-3.5" />
                v{agent.version || "0.0.0"}
              </span>
              <span className="flex items-center gap-1.5">
                <Clock className="w-3.5 h-3.5" />
                Last seen:{" "}
                {agent.last_seen
                  ? new Date(agent.last_seen).toLocaleString()
                  : "Never"}
              </span>
            </div>

            {/* Row 2: shown only when hardware data is available */}
            {hardware && (
              <div className="flex flex-wrap items-center gap-4 text-sm mt-1">
                {hardware.kernel && (
                  <span className="flex items-center gap-1.5">
                    <Server className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">Kernel:</span>
                    <span className="text-[var(--color-text-secondary)]">{hardware.kernel}</span>
                  </span>
                )}
                {(hardware.cpu_logical ?? 0) > 0 && (
                  <span className="flex items-center gap-1.5">
                    <Cpu className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">Cores:</span>
                    <span className="text-[var(--color-text-secondary)]">
                      {hardware.cpu_logical}
                      {hardware.cpu_physical && hardware.cpu_physical !== hardware.cpu_logical
                        ? ` (${hardware.cpu_physical} physical)`
                        : ""}
                    </span>
                  </span>
                )}
                {(hardware.total_ram ?? 0) > 0 && (
                  <span className="flex items-center gap-1.5">
                    <Database className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">RAM:</span>
                    <span className="text-[var(--color-text-secondary)]">{formatBytes(hardware.total_ram!)}</span>
                  </span>
                )}
                {(hardware.uptime ?? 0) > 0 && (
                  <span className="flex items-center gap-1.5">
                    <Activity className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">Uptime:</span>
                    <span className="text-[var(--color-text-secondary)]">{formatUptime(hardware.uptime!)}</span>
                  </span>
                )}
                {hardware.load1 !== undefined && (
                  <span className="flex items-center gap-1.5">
                    <BarChart2 className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">Load:</span>
                    <span className="text-[var(--color-text-secondary)]">
                      {hardware.load1.toFixed(2)} / {(hardware.load5 ?? 0).toFixed(2)} / {(hardware.load15 ?? 0).toFixed(2)}
                    </span>
                  </span>
                )}
                {(hardware.procs ?? 0) > 0 && (
                  <span className="flex items-center gap-1.5">
                    <Layers className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-secondary)]">{hardware.procs} processes</span>
                  </span>
                )}
                {hardware.arch && (
                  <span className="flex items-center gap-1.5">
                    <Box className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                    <span className="text-[var(--color-text-muted)]">Arch:</span>
                    <span className="text-[var(--color-text-secondary)]">{hardware.arch}</span>
                  </span>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-6">
        <div className="overflow-x-auto -mx-1 px-1 scrollbar-thin">
          <div className="inline-flex gap-1 rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-1 min-w-max">
            {TABS.map(({ key, label, icon: Icon }) => (
              <button
                key={key}
                onClick={() => setActiveTab(key)}
                className={`inline-flex items-center gap-1.5 px-4 py-2 rounded-md text-sm font-medium transition-all duration-150 whitespace-nowrap ${
                  activeTab === key
                    ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)]"
                    : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
                }`}
              >
                <Icon className="w-3.5 h-3.5" />
                {label}
              </button>
            ))}
          </div>
        </div>

        {activeTab === "metrics" && (
          <TimeRangeSelector
            selected={timeRange}
            onChange={handleTimeRangeChange}
          />
        )}
      </div>

      {/* Tab content */}
      {activeTab === "metrics" && (
        <div>
          {metricsLoading && (
            <div className="flex items-center gap-2 mb-4 text-sm text-[var(--color-text-secondary)]">
              <Activity className="w-4 h-4 animate-pulse text-[var(--color-accent-cyan)]" />
              Refreshing metrics...
            </div>
          )}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <MetricChart
              title="CPU Usage"
              data={toUPlotData(metrics?.cpu)}
              unit="%"
              colors={["#06b6d4"]}
              seriesLabels={["CPU"]}
            />
            <MetricChart
              title="Memory Usage"
              data={toUPlotData(metrics?.memory)}
              unit="%"
              colors={["#8b5cf6"]}
              seriesLabels={["Memory"]}
            />
            <MetricChart
              title="Disk Usage"
              data={toUPlotData(metrics?.disk)}
              unit="%"
              colors={["#f59e0b"]}
              seriesLabels={["Disk"]}
            />
            <MetricChart
              title="Network"
              data={
                metrics?.network_rx && metrics?.network_tx
                  ? [
                      metrics.network_rx.timestamps,
                      metrics.network_rx.values,
                      metrics.network_tx.values,
                    ]
                  : emptyTimeSeries()
              }
              unit="MB/s"
              colors={["#10b981", "#8b5cf6"]}
              seriesLabels={["RX", "TX"]}
            />
          </div>
        </div>
      )}

      {activeTab === "docker" && id && <DockerTab agentId={id} />}
      {activeTab === "ports" && id && <PortsTab agentId={id} />}
      {activeTab === "services" && id && <ServicesTab agentId={id} />}
      {activeTab === "hardening" && id && <HardeningTab agentId={id} />}
      {activeTab === "vulnerabilities" && id && <VulnerabilitiesTab agentId={id} />}
    </div>
  );
}
