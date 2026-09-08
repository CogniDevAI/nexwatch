import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { Activity, Search, X } from "lucide-react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { apiFetch } from "@/lib/api";
import type { ProcessEntry } from "@/types";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator, type Status } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Input } from "@/components/ui/Field";
import { IconButton } from "@/components/ui/Button";
import { Panel } from "@/components/ui/Panel";
import {
  CHART_AXIS_FONT,
  CHART_AXIS_STROKE,
  CHART_GRID_STROKE,
  formatChartValue,
  makeYAxisFormatter,
  makeYRange,
} from "@/lib/uplotHelpers";

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

function processStatus(status: string): Status {
  const normalized = status.toLowerCase();
  if (normalized === "running" || normalized === "r") return "ok";
  if (
    normalized === "stopped" ||
    normalized === "t" ||
    normalized === "zombie" ||
    normalized === "z"
  ) {
    return "critical";
  }
  return "offline";
}

function loadColor(value: number): string {
  if (value > 80) return "text-[var(--color-critical)]";
  if (value > 50) return "text-[var(--color-warn)]";
  return "text-[var(--color-ink)]";
}

// ─── Timeline chart (uPlot) ──────────────────────────────────────────────────

function ProcessTimelineChart({ data, name }: { data: ProcessTimelineResponse; name: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);
  const [legendValue, setLegendValue] = useState<number | null>(null);

  const uplotData: uPlot.AlignedData = useMemo(() => {
    if (!data.points.length) return [[], []];
    return [data.points.map((p) => p.timestamp), data.points.map((p) => p.cpu_percent)];
  }, [data.points]);

  const buildOptions = useCallback(
    (width: number): uPlot.Options => ({
      width,
      height: 200,
      cursor: { drag: { x: false, y: false } },
      legend: { show: false }, // replaced by the custom legend in the panel header
      scales: {
        x: { time: true },
        y: { auto: true, range: makeYRange("%") },
      },
      axes: [
        {
          stroke: CHART_AXIS_STROKE,
          grid: { stroke: CHART_GRID_STROKE, width: 1 },
          ticks: { stroke: CHART_GRID_STROKE, width: 1 },
          font: CHART_AXIS_FONT,
        },
        {
          stroke: CHART_AXIS_STROKE,
          grid: { stroke: CHART_GRID_STROKE, width: 1 },
          ticks: { stroke: CHART_GRID_STROKE, width: 1 },
          font: CHART_AXIS_FONT,
          values: makeYAxisFormatter("%"),
          // No axis title: the unit is already in the panel header.
        },
      ],
      series: [
        {},
        {
          label: "CPU",
          stroke: "#5b9dff",
          width: 2,
          fill: "#5b9dff10",
        },
      ],
      hooks: {
        setCursor: [
          (u: uPlot) => {
            const lastIdx = u.data[0].length - 1;
            const idx = u.cursor.idx ?? (lastIdx >= 0 ? lastIdx : null);
            const v = idx === null ? null : u.data[1]?.[idx];
            setLegendValue(typeof v === "number" ? v : null);
          },
        ],
      },
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

  // Show the latest value by default, before any hover.
  useEffect(() => {
    const lastIdx = uplotData[0]?.length - 1;
    const v = lastIdx !== undefined && lastIdx >= 0 ? uplotData[1]?.[lastIdx] : null;
    setLegendValue(typeof v === "number" ? v : null);
  }, [uplotData]);

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
    <Panel className="p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <h3 className="text-sm font-semibold text-[var(--color-ink)]">
          {name} — CPU over time
          <span className="ml-2 text-xs font-normal text-[var(--color-ink-faint)]">(%)</span>
        </h3>
        <span className="flex items-center gap-1.5 font-mono text-xs text-[var(--color-ink-muted)] tabular-nums">
          <span className="h-2 w-2 flex-shrink-0 rounded-full bg-[#5b9dff]" aria-hidden="true" />
          CPU
          <span className="text-[var(--color-ink)]">{formatChartValue(legendValue, "%")}</span>
        </span>
      </div>
      <div ref={containerRef} className="w-full" />
    </Panel>
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
        const res = await apiFetch(`/api/custom/agents/${agentId}/processes/history?range=${r}`);
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
        const res = await apiFetch(
          `/api/custom/agents/${agentId}/processes/timeline?name=${encodeURIComponent(name)}&range=${r}`,
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
    void fetchHistory(range);
    // Clear timeline when range changes so it refetches for current selection
    if (selectedProcess) {
      void fetchTimeline(selectedProcess, range);
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
    void fetchTimeline(name, range);
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
        <div className="mb-4 flex items-center gap-2" role="radiogroup" aria-label="Audit range">
          <span className="mr-1 text-sm text-[var(--color-ink-muted)]">Range</span>
          {AUDIT_RANGES.map((r) => (
            <button
              key={r}
              type="button"
              role="radio"
              aria-checked={range === r}
              onClick={() => setRange(r)}
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                range === r
                  ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                  : "border border-[var(--color-line)] text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]"
              }`}
            >
              {r}
            </button>
          ))}
          {history && (
            <span className="ml-auto text-xs text-[var(--color-ink-faint)]">
              {history.snapshot_count} snapshots
            </span>
          )}
        </div>

        {/* Table */}
        {historyLoading ? (
          <Skeleton className="h-48 w-full" />
        ) : historyError ? (
          <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
            <ErrorState
              title="No history data"
              description="No process history has been recorded for this agent yet."
            />
          </div>
        ) : top.length === 0 ? (
          <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
            <EmptyState icon={Activity} title="No top consumers for this range" />
          </div>
        ) : (
          <Table>
            <thead>
              <tr className="border-b border-[var(--color-line)]">
                <Th>Name</Th>
                <Th>User</Th>
                <Th align="right">Samples</Th>
                <Th align="right">Avg CPU</Th>
                <Th align="right">Max CPU</Th>
                <Th align="right">Avg mem</Th>
                <Th align="right">Max RSS</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {top.map((p, idx) => (
                <tr
                  key={p.name}
                  onClick={() => handleRowClick(p.name)}
                  className={`cursor-pointer ${
                    selectedProcess === p.name ? "bg-[var(--color-signal)]/5" : rowClass(idx)
                  }`}
                >
                  <Td className="font-medium">
                    <div className="flex flex-col gap-0.5">
                      <span className="max-w-[200px] truncate">
                        {p.cmd_fragment ? p.name.split(" (")[0] : p.name}
                      </span>
                      {p.cmd_fragment && (
                        <span className="max-w-[200px] truncate font-mono text-xs text-[var(--color-signal)]">
                          {p.cmd_fragment}
                        </span>
                      )}
                    </div>
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">{p.user}</Td>
                  <Td align="right">
                    <span className="inline-flex items-center justify-center rounded-full bg-[var(--color-panel-raised)] px-2 py-0.5 font-mono text-xs font-medium text-[var(--color-ink-muted)] tabular-nums">
                      {p.sample_count}
                    </span>
                  </Td>
                  <Td align="right" className={`font-mono tabular-nums ${loadColor(p.avg_cpu)}`}>
                    {p.avg_cpu.toFixed(1)}%
                  </Td>
                  <Td align="right" className={`font-mono tabular-nums ${loadColor(p.max_cpu)}`}>
                    {p.max_cpu.toFixed(1)}%
                  </Td>
                  <Td align="right" className="font-mono tabular-nums">
                    {p.avg_mem.toFixed(1)}%
                  </Td>
                  <Td align="right" className="font-mono tabular-nums">
                    {formatMB(p.max_rss)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </div>

      {/* Section 2: Timeline */}
      {selectedProcess && (
        <div>
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-[var(--color-ink)]">
              {selectedProcess} — CPU over time
            </h3>
            <IconButton aria-label="Deselect process" onClick={handleDeselect}>
              <X className="h-4 w-4" />
            </IconButton>
          </div>

          {timelineLoading ? (
            <Skeleton className="h-48 w-full" />
          ) : timeline && timeline.points.length > 0 ? (
            <ProcessTimelineChart data={timeline} name={selectedProcess} />
          ) : (
            <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-8 text-center">
              <p className="text-sm text-[var(--color-ink-muted)]">
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

  const fetchProcesses = useCallback(
    async (showLoading = false) => {
      if (showLoading) setLoading(true);
      try {
        const res = await apiFetch(`/api/custom/agents/${agentId}/processes`);
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
    },
    [agentId],
  );

  useEffect(() => {
    void fetchProcesses(true);
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
        typeof valA === "string" ? valA.localeCompare(valB as string) : valA - (valB as number);
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
    <Th align={align} sortable sortActive={sortKey === field} onClick={() => handleSort(field)}>
      {label}
    </Th>
  );

  if (loading) {
    return <Skeleton className="h-72 w-full" />;
  }

  if (error || processes.length === 0) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={Activity}
          title="No data yet"
          description="No process data has been reported by this agent. Make sure the process collector is enabled."
        />
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="relative">
          <Search
            className="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-[var(--color-ink-faint)]"
            aria-hidden="true"
          />
          <Input
            type="search"
            placeholder="Filter processes…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label="Filter processes"
            className="w-full py-2 pl-9 sm:w-72"
          />
        </div>
        <span className="text-sm text-[var(--color-ink-muted)]">
          <span className="font-mono font-medium text-[var(--color-ink)]">{filtered.length}</span>
          {filter ? ` of ${processes.length}` : ""}{" "}
          {processes.length === 1 ? "process" : "processes"}
        </span>
      </div>

      <Table>
        <thead>
          <tr className="border-b border-[var(--color-line)]">
            <SortableHeader label="Name" field="name" />
            <SortableHeader label="PID" field="pid" align="right" />
            <SortableHeader label="CPU" field="cpu_percent" align="right" />
            <SortableHeader label="Mem" field="memory_percent" align="right" />
            <SortableHeader label="RSS" field="memory_rss" align="right" />
            <SortableHeader label="Status" field="status" />
            <SortableHeader label="User" field="user" />
            <Th>Command</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-[var(--color-line-soft)]">
          {filtered.map((p, idx) => (
            <tr key={p.pid} className={rowClass(idx)}>
              <Td className="font-medium">
                <span className="block max-w-[180px] truncate">{p.name}</span>
              </Td>
              <Td align="right" className="font-mono text-[var(--color-ink-muted)] tabular-nums">
                {p.pid}
              </Td>
              <Td align="right" className={`font-mono tabular-nums ${loadColor(p.cpu_percent)}`}>
                {p.cpu_percent.toFixed(1)}%
              </Td>
              <Td align="right" className={`font-mono tabular-nums ${loadColor(p.memory_percent)}`}>
                {p.memory_percent.toFixed(1)}%
              </Td>
              <Td align="right" className="font-mono tabular-nums">
                {formatMB(p.memory_rss)}
              </Td>
              <Td>
                <StatusIndicator status={processStatus(p.status)} label={p.status} />
              </Td>
              <Td className="text-[var(--color-ink-muted)]">{p.user}</Td>
              <Td className="max-w-[300px] font-mono text-xs text-[var(--color-ink-faint)]">
                <span className="block cursor-help truncate" title={p.command}>
                  {p.command}
                </span>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}

// ─── Root component ───────────────────────────────────────────────────────────

export function ServicesTab({ agentId }: ServicesTabProps) {
  const [view, setView] = useState<ViewMode>("live");

  return (
    <div>
      {/* Live / Audit toggle */}
      <div
        className="mb-5 inline-flex gap-1 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] p-1"
        role="radiogroup"
        aria-label="View"
      >
        {(["live", "audit"] as ViewMode[]).map((v) => (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={view === v}
            onClick={() => setView(v)}
            className={`rounded-[var(--radius-chip)] px-4 py-1.5 text-sm font-medium capitalize transition-colors ${
              view === v
                ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                : "text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]"
            }`}
          >
            {v}
          </button>
        ))}
      </div>

      {view === "live" ? <LiveView agentId={agentId} /> : <AuditView agentId={agentId} />}
    </div>
  );
}
