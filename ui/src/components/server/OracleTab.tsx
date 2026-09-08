import { useState, useEffect, useCallback } from "react";
import {
  Database,
  Users,
  AlertTriangle,
  Zap,
  HardDrive,
  MemoryStick,
  Clock,
  Lock,
  RefreshCw,
  Loader2,
} from "lucide-react";
import { apiFetch } from "@/lib/api";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { MetricTile } from "@/components/ui/MetricTile";
import { EmptyState } from "@/components/ui/EmptyState";
import { Button } from "@/components/ui/Button";

// ─── Types ────────────────────────────────────────────────────────────────────

interface OracleTabProps {
  agentId: string;
}

interface OracleInstance {
  name: string;
  status: string;
  database_status: string;
  host: string;
  startup_time: string;
  version: string;
}
interface OracleSessions {
  total: number;
  active: number;
  inactive: number;
  waiting: number;
  blocked: number;
}
interface BlockedSession {
  sid: number;
  serial: number;
  username: string;
  status: string;
  blocking_sid: number;
  wait_class: string;
  event: string;
  seconds_in_wait: number;
  sql_text: string;
}
interface TopSQL {
  sql_id: string;
  executions: number;
  elapsed_secs: number;
  elapsed_per_exec: number;
  cpu_secs: number;
  buffer_gets: number;
  disk_reads: number;
  sql_text: string;
}
interface Tablespace {
  name: string;
  used_mb: number;
  total_mb: number;
  used_pct: number;
  status: string;
  contents: string;
}
interface WaitEvent {
  event: string;
  total_waits: number;
  time_waited_s: number;
  wait_class: string;
}
interface Lock {
  sid: number;
  username: string;
  lock_type: string;
  lock_mode: string;
  request: string;
  ctime: number;
  object_name: string;
}
interface OracleData {
  instance: OracleInstance;
  sessions: OracleSessions;
  blocked_sessions: BlockedSession[];
  top_sql: TopSQL[];
  tablespaces: Tablespace[];
  sga: Record<string, number>;
  pga: Record<string, number>;
  waits: WaitEvent[];
  locks: Lock[];
  redo_mb_last_hour?: number;
}

// ─── Utilities ────────────────────────────────────────────────────────────────

function fmt(n: number, dec = 1) {
  if (n === undefined || n === null) return "—";
  return n.toLocaleString(undefined, { maximumFractionDigits: dec });
}

function UsageBar({ pct, label }: { pct: number; label: string }) {
  const color =
    pct >= 90 ? "var(--color-critical)" : pct >= 75 ? "var(--color-warn)" : "var(--color-ok)";
  return (
    <div className="flex items-center gap-3">
      <div className="w-32 truncate text-xs text-[var(--color-ink-muted)]">{label}</div>
      <div className="h-2 flex-1 overflow-hidden rounded-full bg-[var(--color-panel-raised)]">
        <div
          className="h-full rounded-full transition-all"
          style={{ width: `${Math.min(pct, 100)}%`, background: color }}
        />
      </div>
      <div className="w-12 text-right font-mono text-xs text-[var(--color-ink-muted)] tabular-nums">
        {fmt(pct)}%
      </div>
    </div>
  );
}

// ─── Main ─────────────────────────────────────────────────────────────────────

export function OracleTab({ agentId }: OracleTabProps) {
  const [data, setData] = useState<OracleData | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const res = await apiFetch(`/api/custom/agents/${agentId}/oracle`);
      if (!res.ok) return;
      const d = await res.json();
      setData(d);
      setLastUpdated(new Date());
    } catch {
      /* silent */
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    void fetchData();
    const interval = setInterval(fetchData, 30_000);
    return () => clearInterval(interval);
  }, [fetchData]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2
          className="h-6 w-6 animate-spin text-[var(--color-ink-faint)]"
          aria-hidden="true"
        />
      </div>
    );
  }

  if (!data || !data.instance?.name) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={Database}
          title="No Oracle data yet"
          description="Make sure the Oracle collector is enabled and the agent runs as the oracle user."
        />
      </div>
    );
  }

  const { instance, sessions, blocked_sessions, top_sql, tablespaces, sga, pga, waits, locks } =
    data;
  const hasBlocked = (blocked_sessions?.length ?? 0) > 0;
  const hasLocks = (locks?.length ?? 0) > 0;

  return (
    <div className="space-y-5">
      {/* Header: Instance info */}
      <div className="flex flex-wrap items-center gap-6 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] px-5 py-4">
        <div className="flex items-center gap-2">
          <Database className="h-5 w-5 text-[var(--color-signal)]" aria-hidden="true" />
          <div>
            <p className="text-sm font-bold text-[var(--color-ink)]">{instance.name}</p>
            <p className="font-mono text-xs text-[var(--color-ink-faint)]">{instance.version}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-4 text-xs text-[var(--color-ink-muted)]">
          <span
            className={`rounded-[var(--radius-chip)] px-2 py-1 font-medium ${
              instance.status === "OPEN"
                ? "bg-[var(--color-ok)]/10 text-[var(--color-ok)]"
                : "bg-[var(--color-critical)]/10 text-[var(--color-critical)]"
            }`}
          >
            {instance.database_status}
          </span>
          <span>
            Host: <strong className="text-[var(--color-ink)]">{instance.host}</strong>
          </span>
          <span>
            Up since: <strong className="text-[var(--color-ink)]">{instance.startup_time}</strong>
          </span>
          {data.redo_mb_last_hour !== undefined && (
            <span>
              Redo last hour:{" "}
              <strong className="text-[var(--color-ink)]">{fmt(data.redo_mb_last_hour)} MB</strong>
            </span>
          )}
        </div>
        <Button size="sm" className="ml-auto" onClick={fetchData}>
          <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
          {lastUpdated ? lastUpdated.toLocaleTimeString() : "Refresh"}
        </Button>
      </div>

      {/* Sessions summary */}
      <Panel>
        <PanelHeader
          icon={<Users className="h-4 w-4" />}
          title="Sessions"
          badge={
            <span className="rounded-[var(--radius-chip)] bg-[var(--color-signal)]/10 px-2 py-0.5 text-xs font-normal text-[var(--color-signal)]">
              {sessions?.total ?? 0}
            </span>
          }
        />
        <PanelBody>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
            <MetricTile label="Total" value={sessions?.total ?? 0} />
            <MetricTile label="Active" value={sessions?.active ?? 0} tone="ok" />
            <MetricTile label="Inactive" value={sessions?.inactive ?? 0} />
            <MetricTile label="Waiting" value={sessions?.waiting ?? 0} tone="warning" />
            <MetricTile
              label="Blocked"
              value={sessions?.blocked ?? 0}
              tone={sessions?.blocked > 0 ? "critical" : "default"}
            />
          </div>
        </PanelBody>
      </Panel>

      {/* Blocked sessions — only if any */}
      {hasBlocked && (
        <Panel>
          <PanelHeader
            icon={<AlertTriangle className="h-4 w-4" />}
            title="Blocked sessions"
            badge={
              <span className="rounded-[var(--radius-chip)] bg-[var(--color-critical)]/10 px-2 py-0.5 text-xs font-normal text-[var(--color-critical)]">
                {blocked_sessions.length}
              </span>
            }
          />
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-line)] text-[var(--color-ink-muted)]">
                  {["SID", "User", "Status", "Blocked by", "Wait", "Seconds", "SQL"].map((h) => (
                    <th key={h} className="px-3 py-2 text-left font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {blocked_sessions.map((s, i) => (
                  <tr key={i} className="hover:bg-[var(--color-panel-raised)]">
                    <td className="px-3 py-2 font-mono tabular-nums">{s.sid}</td>
                    <td className="px-3 py-2">{s.username}</td>
                    <td className="px-3 py-2">{s.status}</td>
                    <td className="px-3 py-2 font-mono text-[var(--color-critical)] tabular-nums">
                      {s.blocking_sid}
                    </td>
                    <td className="px-3 py-2">{s.event}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{s.seconds_in_wait}s</td>
                    <td className="max-w-xs truncate px-3 py-2 font-mono" title={s.sql_text}>
                      {s.sql_text || "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}

      {/* Top SQL */}
      <Panel>
        <PanelHeader
          icon={<Zap className="h-4 w-4" />}
          title="Top SQL by elapsed time"
          badge={
            <span className="text-xs font-normal text-[var(--color-ink-faint)]">
              {top_sql?.length ?? 0} queries
            </span>
          }
        />
        {!top_sql?.length ? (
          <PanelBody>
            <p className="text-center text-sm text-[var(--color-ink-faint)]">
              No SQL data available
            </p>
          </PanelBody>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-line)] text-[var(--color-ink-muted)]">
                  {[
                    "SQL ID",
                    "Execs",
                    "Elapsed (s)",
                    "Per exec (s)",
                    "CPU (s)",
                    "Buffer gets",
                    "Disk reads",
                    "SQL text",
                  ].map((h) => (
                    <th key={h} className="px-3 py-2 text-left font-medium whitespace-nowrap">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {top_sql.map((s, i) => (
                  <tr
                    key={i}
                    className={`hover:bg-[var(--color-panel-raised)] ${i === 0 ? "bg-[var(--color-critical)]/5" : ""}`}
                  >
                    <td className="px-3 py-2 font-mono text-[var(--color-signal)]">{s.sql_id}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(s.executions, 0)}</td>
                    <td className="px-3 py-2 font-mono font-medium tabular-nums">
                      {fmt(s.elapsed_secs)}
                    </td>
                    <td className="px-3 py-2 font-mono tabular-nums">
                      {fmt(s.elapsed_per_exec, 4)}
                    </td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(s.cpu_secs)}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(s.buffer_gets, 0)}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{fmt(s.disk_reads, 0)}</td>
                    <td className="max-w-xs truncate px-3 py-2 font-mono" title={s.sql_text}>
                      {s.sql_text}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      {/* Tablespaces */}
      <Panel>
        <PanelHeader icon={<HardDrive className="h-4 w-4" />} title="Tablespaces" />
        <PanelBody className="space-y-3">
          {tablespaces?.map((ts) => (
            <div key={ts.name}>
              <div className="mb-1 flex justify-between text-xs">
                <span className="font-medium text-[var(--color-ink)]">{ts.name}</span>
                <span className="font-mono text-[var(--color-ink-faint)]">
                  {fmt(ts.used_mb)} / {fmt(ts.total_mb)} MB
                </span>
              </div>
              <UsageBar pct={ts.used_pct} label={ts.contents} />
            </div>
          ))}
        </PanelBody>
      </Panel>

      {/* Memory: SGA + PGA */}
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
        <Panel>
          <PanelHeader icon={<MemoryStick className="h-4 w-4" />} title="SGA" />
          <PanelBody className="space-y-2">
            {Object.entries(sga ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between text-xs">
                <span className="text-[var(--color-ink-muted)] capitalize">
                  {k.replace(/_/g, " ")}
                </span>
                <span className="font-mono font-medium text-[var(--color-ink)] tabular-nums">
                  {fmt(v)} MB
                </span>
              </div>
            ))}
          </PanelBody>
        </Panel>
        <Panel>
          <PanelHeader icon={<MemoryStick className="h-4 w-4" />} title="PGA" />
          <PanelBody className="space-y-2">
            {Object.entries(pga ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between text-xs">
                <span className="text-[var(--color-ink-muted)] capitalize">
                  {k.replace(/_/g, " ")}
                </span>
                <span className="font-mono font-medium text-[var(--color-ink)] tabular-nums">
                  {fmt(v)} MB
                </span>
              </div>
            ))}
          </PanelBody>
        </Panel>
      </div>

      {/* Wait Events */}
      <Panel>
        <PanelHeader icon={<Clock className="h-4 w-4" />} title="Top wait events" />
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-[var(--color-line)] text-[var(--color-ink-muted)]">
                {["Event", "Class", "Total waits", "Time waited (s)"].map((h) => (
                  <th key={h} className="px-3 py-2 text-left font-medium">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {waits?.map((w, i) => (
                <tr key={i} className="hover:bg-[var(--color-panel-raised)]">
                  <td className="px-3 py-2 text-[var(--color-ink)]">{w.event}</td>
                  <td className="px-3 py-2 text-[var(--color-ink-faint)]">{w.wait_class}</td>
                  <td className="px-3 py-2 font-mono tabular-nums">{fmt(w.total_waits, 0)}</td>
                  <td className="px-3 py-2 font-mono tabular-nums">{fmt(w.time_waited_s)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>

      {/* Locks — only if any */}
      {hasLocks && (
        <Panel>
          <PanelHeader
            icon={<Lock className="h-4 w-4" />}
            title="Active locks"
            badge={
              <span className="rounded-[var(--radius-chip)] bg-[var(--color-warn)]/10 px-2 py-0.5 text-xs font-normal text-[var(--color-warn)]">
                {locks.length}
              </span>
            }
          />
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-line)] text-[var(--color-ink-muted)]">
                  {["SID", "User", "Type", "Mode", "Request", "Held (s)", "Object"].map((h) => (
                    <th key={h} className="px-3 py-2 text-left font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {locks.map((l, i) => (
                  <tr key={i} className="hover:bg-[var(--color-panel-raised)]">
                    <td className="px-3 py-2 font-mono tabular-nums">{l.sid}</td>
                    <td className="px-3 py-2">{l.username}</td>
                    <td className="px-3 py-2 font-mono">{l.lock_type}</td>
                    <td className="px-3 py-2">{l.lock_mode}</td>
                    <td className="px-3 py-2">{l.request}</td>
                    <td className="px-3 py-2 font-mono tabular-nums">{l.ctime}s</td>
                    <td className="px-3 py-2">{l.object_name || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}
    </div>
  );
}
