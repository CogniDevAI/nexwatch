import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { Activity, Search, ArrowUpDown, X } from "lucide-react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import pb from "@/lib/pocketbase";
import type { ProcessEntry } from "@/types";

// ─── New interfaces for History/Audit API ───────────────────────────────────

interface ProcessHistoryEntry {
  name: string;
  cmd_fragment?: string;
  user: string;
  sample_count: number;
  avg_cpu: number;
  max_cpu: number;
  avg_mem: number;
  max_mem: number;
  max_rss: number;
}

interface ProcessHistoryResponse {
  range: string;
  snapshot_count: number;
  top_by_cpu: ProcessHistoryEntry[];
}

interface TimelinePoint {
  timestamp: number;
  cpu_percent: number;
  mem_percent: number;
  rss: number;
  pid: number;
}

interface ProcessTimelineResponse {
  name: string;
  range: string;
  points: TimelinePoint[];
}

// ─── Types ───────────────────────────────────────────────────────────────────

interface ServicesTabProps {
  agentId: string;
}

type SortKey = keyof Pick<
  ProcessEntry,
  "name" | "pid" | "cpu_percent" | "memory_percent" | "memory_rss" | "status" | "user"
>;

type SortDir = "asc" | "desc";

type ViewMode = "live" | "audit";

type AuditRange = "1h" | "6h" | "24h";

const REFRESH_INTERVAL = 15_000;
const AUDIT_RANGES: AuditRange[] = ["1h", "6h", "24h"];

// ─── Utilities ───────────────────────────────────────────────────────────────

function formatMB(bytes: number): string {
  const mb = bytes / (1024 * 1024);
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb.toFixed(1)} MB`;
}

// ─── Sub-components ──────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  let classes: string;

  if (normalized === "running" || normalized === "R") {
    classes = "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]";
  } else if (normalized === "stopped" || normalized === "T" || normalized === "zombie" || normalized === "Z") {
    classes = "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]";
  } else {
    classes = "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]";
  }

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${classes}`}>
      {status}
    </span>
  );
}

// ─── Timeline chart (uPlot) ──────────────────────────────────────────────────

function ProcessTimelineChart({ data, name }: { data: ProcessTimelineResponse; name: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);

  const uplotData: uPlot.AlignedData = useMemo(() => {
    if (!data.points.length) return [[], []];
    return [
      data.points.map((p) => p.timestamp),
      data.points.map((p) => p.cpu_percent),
    ];
  }, [data.points]);

  const buildOptions = useCallback(
    (width: number): uPlot.Options => ({
      width,
      height: 200,
      cursor: { drag: { x: false, y: false } },
      scales: {
        x: { time: true },
        y: { auto: true },
      },
      axes: [
        {
          stroke: "#484f58",
          grid: { stroke: "#1e1e2e", width: 1 },
          ticks: { stroke: "#1e1e2e", width: 1 },
          font: "11px Inter, sans-serif",
        },
        {
          stroke: "#484f58",
          grid: { stroke: "#1e1e2e", width: 1 },
          ticks: { stroke: "#1e1e2e", width: 1 },
          font: "11px Inter, sans-serif",
          values: (_self: uPlot, ticks: number[]) =>
            ticks.map((v) => `${v.toFixed(0)}`),
          label: "%",
          labelFont: "11px Inter, sans-serif",
          labelSize: 20,
        },
      ],
      series: [
        {},
        {
          label: "CPU %",
          stroke: "#06b6d4",
          width: 2,
          fill: "#06b6d410",
        },
      ],
    }),
    [],
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    if (chartRef.current) {
      chartRef.current.destroy();
      chartRef.current = null;
    }

    const opts = buildOptions(container.clientWidth);
    chartRef.current = new uPlot(opts, uplotData, container);

    return () => {
      if (chartRef.current) {
        chartRef.current.destroy();
        chartRef.current = null;
      }
    };
  }, [uplotData, buildOptions]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (chartRef.current && width > 0) {
          chartRef.current.setSize({ width, height: 200 });
        }
      }
    });

    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-5">
      <h3 className="text-sm font-semibold text-[var(--color-text-primary)] mb-4">
        {name} — CPU over time
        <span className="ml-2 text-xs font-normal text-[var(--color-text-muted)]">(%)</span>
      </h3>
      <div ref={containerRef} className="w-full" />
    </div>
  );
}

// ─── Audit view ───────────────────────────────────────────────────────────────

function AuditView({ agentId }: { agentId: string }) {
  const [range, setRange] = useState<AuditRange>("1h");
  const [history, setHistory] = useState<ProcessHistoryResponse | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState(false);
  const [selectedProcess, setSelectedProcess] = useState<string | null>(null);
  const [timeline, setTimeline] = useState<ProcessTimelineResponse | null>(null);
  const [timelineLoading, setTimelineLoading] = useState(false);

  const fetchHistory = useCallback(
    async (r: AuditRange) => {
      setHistoryLoading(true);
      setHistoryError(false);
      try {
        const res = await fetch(
          `/api/custom/agents/${agentId}/processes/history?range=${r}`,
          { headers: { Authorization: pb.authStore.token } },
        );
        if (!res.ok) {
          setHistoryError(true);
          setHistory(null);
          return;
        }
        const json = (await res.json()) as ProcessHistoryResponse;
        setHistory(json);
      } catch {
        setHistoryError(true);
        setHistory(null);
      } finally {
        setHistoryLoading(false);
      }
    },
    [agentId],
  );

  const fetchTimeline = useCallback(
    async (name: string, r: AuditRange) => {
      setTimelineLoading(true);
      try {
        const res = await fetch(
          `/api/custom/agents/${agentId}/processes/timeline?name=${encodeURIComponent(name)}&range=${r}`,
          { headers: { Authorization: pb.authStore.token } },
        );
        if (!res.ok) {
          setTimeline(null);
          return;
        }
        const json = (await res.json()) as ProcessTimelineResponse;
        setTimeline(json);
      } catch {
        setTimeline(null);
      } finally {
        setTimelineLoading(false);
      }
    },
    [agentId],
  );

  // Fetch when range changes
  useEffect(() => {
    fetchHistory(range);
    // Clear timeline when range changes so it refetches for current selection
    if (selectedProcess) {
      fetchTimeline(selectedProcess, range);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range, fetchHistory]);

  function handleRowClick(name: string) {
    if (selectedProcess === name) {
      setSelectedProcess(null);
      setTimeline(null);
      return;
    }
    setSelectedProcess(name);
    fetchTimeline(name, range);
  }

  function handleDeselect() {
    setSelectedProcess(null);
    setTimeline(null);
  }

  const top = history?.top_by_cpu ?? [];

  return (
    <div className="space-y-6">
      {/* Section 1: Top Consumers */}
      <div>
        {/* Range selector */}
        <div className="flex items-center gap-2 mb-4">
          <span className="text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider mr-1">
            Range
          </span>
          {AUDIT_RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className={`px-3 py-1 rounded-full text-xs font-medium transition-colors ${
                range === r
                  ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)]"
                  : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] border border-[var(--color-border-default)]"
              }`}
            >
              {r}
            </button>
          ))}
          {history && (
            <span className="ml-auto text-xs text-[var(--color-text-muted)]">
              {history.snapshot_count} snapshots
            </span>
          )}
        </div>

        {/* Table */}
        {historyLoading ? (
          <div className="flex items-center justify-center py-12">
            <Activity className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
            <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
              Loading history...
            </span>
          </div>
        ) : historyError ? (
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
            <Activity className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
            <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
              No history data
            </h3>
            <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
              No process history has been recorded for this agent yet.
            </p>
          </div>
        ) : top.length === 0 ? (
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
            <Activity className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
            <p className="text-sm text-[var(--color-text-secondary)]">
              No top consumers found for the selected range.
            </p>
          </div>
        ) : (
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[var(--color-border-default)]">
                    {(["NAME", "USER", "SAMPLES", "AVG CPU", "MAX CPU", "AVG MEM", "MAX RSS"] as const).map(
                      (col) => (
                        <th
                          key={col}
                          className={`px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)] ${
                            col === "NAME" || col === "USER" ? "text-left" : "text-right"
                          }`}
                        >
                          {col}
                        </th>
                      ),
                    )}
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--color-border-muted)]">
                  {top.map((p, idx) => (
                    <tr
                      key={p.name}
                      onClick={() => handleRowClick(p.name)}
                      className={`cursor-pointer transition-colors ${
                        selectedProcess === p.name
                          ? "bg-[var(--color-accent-cyan)]/5"
                          : idx % 2 === 0
                            ? "bg-transparent"
                            : "bg-[var(--color-bg-elevated)]/30"
                      } hover:bg-[var(--color-bg-elevated)]`}
                    >
                      <td className="px-5 py-3 font-medium text-[var(--color-text-primary)]">
                        <div className="flex flex-col gap-0.5">
                          <span className="truncate max-w-[200px]">
                            {p.cmd_fragment ? p.name.split(" (")[0] : p.name}
                          </span>
                          {p.cmd_fragment && (
                            <span className="text-xs text-[var(--color-accent-cyan)] font-mono truncate max-w-[200px]">
                              {p.cmd_fragment}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-5 py-3 text-[var(--color-text-secondary)]">{p.user}</td>
                      <td className="px-5 py-3 text-right">
                        <span className="inline-flex items-center justify-center px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-bg-elevated)] text-[var(--color-text-secondary)] tabular-nums">
                          {p.sample_count}
                        </span>
                      </td>
                      <td className="px-5 py-3 text-right tabular-nums">
                        <span
                          className={
                            p.avg_cpu > 80
                              ? "text-[var(--color-accent-red)]"
                              : p.avg_cpu > 50
                                ? "text-[var(--color-accent-yellow)]"
                                : "text-[var(--color-text-primary)]"
                          }
                        >
                          {p.avg_cpu.toFixed(1)}%
                        </span>
                      </td>
                      <td className="px-5 py-3 text-right tabular-nums">
                        <span
                          className={
                            p.max_cpu > 80
                              ? "text-[var(--color-accent-red)]"
                              : p.max_cpu > 50
                                ? "text-[var(--color-accent-yellow)]"
                                : "text-[var(--color-text-primary)]"
                          }
                        >
                          {p.max_cpu.toFixed(1)}%
                        </span>
                      </td>
                      <td className="px-5 py-3 text-right tabular-nums text-[var(--color-text-primary)]">
                        {p.avg_mem.toFixed(1)}%
                      </td>
                      <td className="px-5 py-3 text-right tabular-nums text-[var(--color-text-primary)]">
                        {formatMB(p.max_rss)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      {/* Section 2: Timeline */}
      {selectedProcess && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">
              {selectedProcess} — CPU over time
            </h3>
            <button
              onClick={handleDeselect}
              className="flex items-center justify-center w-6 h-6 rounded-md text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)] transition-colors"
              title="Deselect process"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          {timelineLoading ? (
            <div className="flex items-center justify-center py-12 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)]">
              <Activity className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
              <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
                Loading timeline...
              </span>
            </div>
          ) : timeline && timeline.points.length > 0 ? (
            <ProcessTimelineChart data={timeline} name={selectedProcess} />
          ) : (
            <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-8 text-center">
              <p className="text-sm text-[var(--color-text-secondary)]">
                No timeline data available for this process in the selected range.
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Live view ────────────────────────────────────────────────────────────────

function LiveView({ agentId }: { agentId: string }) {
  const [processes, setProcesses] = useState<ProcessEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [filter, setFilter] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("cpu_percent");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchProcesses = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/processes`, {
        headers: { Authorization: pb.authStore.token },
      });
      if (!res.ok) {
        setProcesses([]);
        setError(true);
        return;
      }
      type RawProcess = {
        pid: number;
        name: string;
        cpu_percent: number;
        mem_percent: number;
        rss: number;
        status: string;
        user: string;
        cmdline: string;
      };
      const json = (await res.json()) as { processes: RawProcess[]; total_count: number };
      const items: ProcessEntry[] = (json.processes ?? []).map((p) => ({
        pid: p.pid,
        name: p.name,
        cpu_percent: p.cpu_percent,
        memory_percent: p.mem_percent,
        memory_rss: p.rss,
        status: p.status,
        user: p.user,
        command: p.cmdline,
      }));
      setProcesses(items);
      setError(false);
    } catch {
      setProcesses([]);
      setError(true);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchProcesses(true);
    intervalRef.current = setInterval(() => fetchProcesses(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchProcesses]);

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "name" || key === "user" || key === "status" ? "asc" : "desc");
    }
  };

  const filtered = useMemo(() => {
    const q = filter.toLowerCase();
    let list = processes;
    if (q) {
      list = list.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          p.user.toLowerCase().includes(q) ||
          p.command.toLowerCase().includes(q) ||
          String(p.pid).includes(q),
      );
    }
    return [...list].sort((a, b) => {
      const valA = a[sortKey];
      const valB = b[sortKey];
      const cmp =
        typeof valA === "string"
          ? (valA as string).localeCompare(valB as string)
          : (valA as number) - (valB as number);
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [processes, filter, sortKey, sortDir]);

  const SortableHeader = ({
    label,
    field,
    align = "left",
  }: {
    label: string;
    field: SortKey;
    align?: "left" | "right";
  }) => (
    <th
      className={`text-${align} px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)] cursor-pointer select-none hover:text-[var(--color-accent-cyan)] transition-colors`}
      onClick={() => handleSort(field)}
    >
      <span className={`inline-flex items-center gap-1 ${align === "right" ? "justify-end" : ""}`}>
        {label}
        {sortKey === field && (
          <ArrowUpDown className="w-3 h-3 text-[var(--color-accent-cyan)]" />
        )}
      </span>
    </th>
  );

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Activity className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
          Loading processes...
        </span>
      </div>
    );
  }

  if (error || processes.length === 0) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <Activity className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          No data yet
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          No process data has been reported by this agent. Make sure the process
          collector is enabled.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--color-text-muted)]" />
          <input
            type="text"
            placeholder="Filter processes..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="pl-9 pr-4 py-2 rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] text-sm text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent-cyan)] transition-colors w-full sm:w-72"
          />
        </div>
        <span className="text-sm text-[var(--color-text-secondary)]">
          <span className="font-medium text-[var(--color-text-primary)]">{filtered.length}</span>
          {filter ? ` of ${processes.length}` : ""} {processes.length === 1 ? "process" : "processes"}
        </span>
      </div>

      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border-default)]">
                <SortableHeader label="Name" field="name" />
                <SortableHeader label="PID" field="pid" align="right" />
                <SortableHeader label="CPU %" field="cpu_percent" align="right" />
                <SortableHeader label="Mem %" field="memory_percent" align="right" />
                <SortableHeader label="RSS" field="memory_rss" align="right" />
                <SortableHeader label="Status" field="status" />
                <SortableHeader label="User" field="user" />
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Command
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {filtered.map((p, idx) => (
                <tr
                  key={p.pid}
                  className={`transition-colors ${
                    idx % 2 === 0
                      ? "bg-transparent"
                      : "bg-[var(--color-bg-elevated)]/30"
                  } hover:bg-[var(--color-bg-elevated)]`}
                >
                  <td className="px-5 py-3 font-medium text-[var(--color-text-primary)]">
                    <span className="truncate max-w-[180px] block">{p.name}</span>
                  </td>
                  <td className="px-5 py-3 text-right text-[var(--color-text-secondary)] tabular-nums">
                    {p.pid}
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums">
                    <span
                      className={
                        p.cpu_percent > 80
                          ? "text-[var(--color-accent-red)]"
                          : p.cpu_percent > 50
                            ? "text-[var(--color-accent-yellow)]"
                            : "text-[var(--color-text-primary)]"
                      }
                    >
                      {p.cpu_percent.toFixed(1)}%
                    </span>
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums">
                    <span
                      className={
                        p.memory_percent > 80
                          ? "text-[var(--color-accent-red)]"
                          : p.memory_percent > 50
                            ? "text-[var(--color-accent-yellow)]"
                            : "text-[var(--color-text-primary)]"
                      }
                    >
                      {p.memory_percent.toFixed(1)}%
                    </span>
                  </td>
                  <td className="px-5 py-3 text-right text-[var(--color-text-primary)] tabular-nums">
                    {formatMB(p.memory_rss)}
                  </td>
                  <td className="px-5 py-3">
                    <StatusBadge status={p.status} />
                  </td>
                  <td className="px-5 py-3 text-[var(--color-text-secondary)]">
                    {p.user}
                  </td>
                  <td className="px-5 py-3 text-[var(--color-text-muted)] font-mono text-xs max-w-[300px]">
                    <span
                      className="truncate block cursor-help"
                      title={p.command}
                    >
                      {p.command}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ─── Root component ───────────────────────────────────────────────────────────

export function ServicesTab({ agentId }: ServicesTabProps) {
  const [view, setView] = useState<ViewMode>("live");

  return (
    <div>
      {/* Live / Audit toggle */}
      <div className="inline-flex gap-1 rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-1 mb-5">
        {(["live", "audit"] as ViewMode[]).map((v) => (
          <button
            key={v}
            onClick={() => setView(v)}
            className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all duration-150 capitalize ${
              view === v
                ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)]"
                : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
            }`}
          >
            {v}
          </button>
        ))}
      </div>

      {view === "live" ? (
        <LiveView agentId={agentId} />
      ) : (
        <AuditView agentId={agentId} />
      )}
    </div>
  );
}
