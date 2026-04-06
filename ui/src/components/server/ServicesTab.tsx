import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { Activity, Search, ArrowUpDown } from "lucide-react";
import pb from "@/lib/pocketbase";
import type { ProcessEntry } from "@/types";

interface ServicesTabProps {
  agentId: string;
}

type SortKey = keyof Pick<
  ProcessEntry,
  "name" | "pid" | "cpu_percent" | "memory_percent" | "memory_rss" | "status" | "user"
>;

type SortDir = "asc" | "desc";

const REFRESH_INTERVAL = 15_000;

function formatMB(bytes: number): string {
  const mb = bytes / (1024 * 1024);
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb.toFixed(1)} MB`;
}

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

export function ServicesTab({ agentId }: ServicesTabProps) {
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
      // API returns { processes: [...], total_count: N } with backend field names.
      // Map backend field names (mem_percent, rss, cmdline) to frontend ProcessEntry names.
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
