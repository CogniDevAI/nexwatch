import { useState, useEffect, useCallback } from "react";
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
} from "lucide-react";
import pb from "@/lib/pocketbase";
import type { Agent, MetricsResponse } from "@/types";
import { MetricChart } from "@/components/charts/MetricChart";
import { TimeRangeSelector } from "@/components/charts/TimeRangeSelector";
import { DockerTab } from "@/components/server/DockerTab";

type Tab = "metrics" | "docker";

/** Generate mock empty chart data when no metrics are available */
function emptyTimeSeries(): uPlot.AlignedData {
  return [[], []];
}

export function ServerDetail() {
  const { id } = useParams<{ id: string }>();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [metrics, setMetrics] = useState<MetricsResponse | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>("metrics");
  const [timeRange, setTimeRange] = useState("1h");
  const [loading, setLoading] = useState(true);
  const [metricsLoading, setMetricsLoading] = useState(false);

  // Fetch agent
  useEffect(() => {
    if (!id) return;

    async function fetchAgent() {
      try {
        const record = await pb.collection("agents").getOne<Agent>(id!);
        setAgent(record);
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
    async (start: number, end: number) => {
      if (!id) return;
      setMetricsLoading(true);
      try {
        const response = await fetch(
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

  // Initial metrics fetch
  useEffect(() => {
    const end = Math.floor(Date.now() / 1000);
    const start = end - 3600; // 1h default
    fetchMetrics(start, end);
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
            <div className="flex flex-wrap items-center gap-4 text-sm text-[var(--color-text-secondary)]">
              <span className="flex items-center gap-1.5">
                <Globe className="w-3.5 h-3.5" />
                {agent.ip || "No IP"}
              </span>
              <span className="flex items-center gap-1.5">
                <Cpu className="w-3.5 h-3.5" />
                {agent.os || "Unknown OS"}
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
          </div>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex gap-1 rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-1">
          <button
            onClick={() => setActiveTab("metrics")}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-all duration-150 ${
              activeTab === "metrics"
                ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)]"
                : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
            }`}
          >
            Metrics
          </button>
          <button
            onClick={() => setActiveTab("docker")}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-all duration-150 ${
              activeTab === "docker"
                ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)]"
                : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
            }`}
          >
            Docker
          </button>
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
    </div>
  );
}
