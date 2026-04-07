import { useState, useEffect, useCallback } from "react";
import {
  Database, Users, AlertTriangle, Zap, HardDrive,
  MemoryStick, Clock, Lock, RefreshCw, Loader2,
} from "lucide-react";
import pb from "@/lib/pocketbase";

// ─── Types ────────────────────────────────────────────────────────────────────

interface OracleTabProps { agentId: string }

interface OracleInstance {
  name: string; status: string; database_status: string;
  host: string; startup_time: string; version: string;
}
interface OracleSessions {
  total: number; active: number; inactive: number; waiting: number; blocked: number;
}
interface BlockedSession {
  sid: number; serial: number; username: string; status: string;
  blocking_sid: number; wait_class: string; event: string;
  seconds_in_wait: number; sql_text: string;
}
interface TopSQL {
  sql_id: string; executions: number; elapsed_secs: number;
  elapsed_per_exec: number; cpu_secs: number;
  buffer_gets: number; disk_reads: number; sql_text: string;
}
interface Tablespace {
  name: string; used_mb: number; total_mb: number;
  used_pct: number; status: string; contents: string;
}
interface WaitEvent {
  event: string; total_waits: number; time_waited_s: number; wait_class: string;
}
interface Lock {
  sid: number; username: string; lock_type: string;
  lock_mode: string; request: string; ctime: number; object_name: string;
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
  const color = pct >= 90 ? "var(--color-accent-red)" : pct >= 75 ? "var(--color-accent-yellow)" : "var(--color-accent-green)";
  return (
    <div className="flex items-center gap-3">
      <div className="w-32 text-xs text-[var(--color-text-secondary)] truncate">{label}</div>
      <div className="flex-1 h-2 bg-[var(--color-bg-elevated)] rounded-full overflow-hidden">
        <div className="h-full rounded-full transition-all" style={{ width: `${Math.min(pct, 100)}%`, background: color }} />
      </div>
      <div className="text-xs tabular-nums text-[var(--color-text-secondary)] w-12 text-right">{fmt(pct)}%</div>
    </div>
  );
}

function SectionCard({ icon: Icon, title, badge, badgeColor, children }: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  badge?: string | number;
  badgeColor?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
      <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex items-center gap-2">
        <Icon className="w-4 h-4 text-[var(--color-text-muted)]" />
        <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">{title}</h3>
        {badge !== undefined && (
          <span className={`ml-auto text-xs font-medium px-2 py-0.5 rounded-full ${badgeColor ?? "bg-[var(--color-bg-elevated)] text-[var(--color-text-secondary)]"}`}>
            {badge}
          </span>
        )}
      </div>
      <div className="p-5">{children}</div>
    </div>
  );
}

// ─── Main ─────────────────────────────────────────────────────────────────────

export function OracleTab({ agentId }: OracleTabProps) {
  const [data, setData] = useState<OracleData | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const headers = { Authorization: pb.authStore.token ?? "" };

  const fetchData = useCallback(async () => {
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/oracle`, { headers });
      if (!res.ok) return;
      const d = await res.json();
      setData(d);
      setLastUpdated(new Date());
    } catch { /* silent */ }
    finally { setLoading(false); }
  }, [agentId]);

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, 30_000);
    return () => clearInterval(interval);
  }, [fetchData]);

  if (loading) return (
    <div className="flex items-center justify-center py-20">
      <Loader2 className="w-6 h-6 animate-spin text-[var(--color-text-muted)]" />
    </div>
  );

  if (!data || !data.instance?.name) return (
    <div className="flex flex-col items-center justify-center py-20 gap-3">
      <Database className="w-10 h-10 text-[var(--color-text-muted)]" />
      <p className="text-sm text-[var(--color-text-secondary)]">No Oracle data yet. Make sure the oracle collector is enabled and the agent runs as oracle user.</p>
    </div>
  );

  const { instance, sessions, blocked_sessions, top_sql, tablespaces, sga, pga, waits, locks } = data;
  const hasBlocked = (blocked_sessions?.length ?? 0) > 0;
  const hasLocks = (locks?.length ?? 0) > 0;

  return (
    <div className="space-y-5">

      {/* ── Header: Instance info ── */}
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] px-5 py-4 flex flex-wrap items-center gap-6">
        <div className="flex items-center gap-2">
          <Database className="w-5 h-5 text-[var(--color-accent-cyan)]" />
          <div>
            <p className="text-sm font-bold text-[var(--color-text-primary)]">{instance.name}</p>
            <p className="text-xs text-[var(--color-text-muted)]">{instance.version}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-4 text-xs text-[var(--color-text-secondary)]">
          <span className={`px-2 py-1 rounded-full font-medium ${instance.status === "OPEN" ? "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]" : "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"}`}>
            {instance.database_status}
          </span>
          <span>Host: <strong>{instance.host}</strong></span>
          <span>Up since: <strong>{instance.startup_time}</strong></span>
          {data.redo_mb_last_hour !== undefined && (
            <span>Redo last hour: <strong>{fmt(data.redo_mb_last_hour)} MB</strong></span>
          )}
        </div>
        <button onClick={fetchData} className="ml-auto inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs border border-[var(--color-border-default)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-elevated)] transition-colors">
          <RefreshCw className="w-3.5 h-3.5" />
          {lastUpdated ? lastUpdated.toLocaleTimeString() : "Refresh"}
        </button>
      </div>

      {/* ── Sessions summary ── */}
      <SectionCard icon={Users} title="Sessions" badge={sessions?.total} badgeColor="bg-[var(--color-accent-cyan)]/10 text-[var(--color-accent-cyan)]">
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-4">
          {[
            { label: "Total", value: sessions?.total, color: "text-[var(--color-text-primary)]" },
            { label: "Active", value: sessions?.active, color: "text-[var(--color-accent-green)]" },
            { label: "Inactive", value: sessions?.inactive, color: "text-[var(--color-text-muted)]" },
            { label: "Waiting", value: sessions?.waiting, color: "text-[var(--color-accent-yellow)]" },
            { label: "Blocked", value: sessions?.blocked, color: sessions?.blocked > 0 ? "text-[var(--color-accent-red)]" : "text-[var(--color-text-muted)]" },
          ].map(({ label, value, color }) => (
            <div key={label} className="text-center">
              <p className={`text-2xl font-bold tabular-nums ${color}`}>{value ?? 0}</p>
              <p className="text-xs text-[var(--color-text-muted)] mt-1">{label}</p>
            </div>
          ))}
        </div>
      </SectionCard>

      {/* ── Blocked sessions — only if any ── */}
      {hasBlocked && (
        <SectionCard icon={AlertTriangle} title="Blocked Sessions" badge={blocked_sessions.length} badgeColor="bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]">
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-secondary)] uppercase tracking-wider">
                  {["SID", "User", "Status", "Blocked by", "Wait", "Seconds", "SQL"].map(h => (
                    <th key={h} className="text-left px-3 py-2 font-semibold">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-border-muted)]">
                {blocked_sessions.map((s, i) => (
                  <tr key={i} className="hover:bg-[var(--color-bg-elevated)]">
                    <td className="px-3 py-2 font-mono tabular-nums">{s.sid}</td>
                    <td className="px-3 py-2">{s.username}</td>
                    <td className="px-3 py-2">{s.status}</td>
                    <td className="px-3 py-2 tabular-nums text-[var(--color-accent-red)]">{s.blocking_sid}</td>
                    <td className="px-3 py-2">{s.event}</td>
                    <td className="px-3 py-2 tabular-nums">{s.seconds_in_wait}s</td>
                    <td className="px-3 py-2 font-mono max-w-xs truncate" title={s.sql_text}>{s.sql_text || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </SectionCard>
      )}

      {/* ── Top SQL ── */}
      <SectionCard icon={Zap} title="Top SQL by Elapsed Time" badge={`${top_sql?.length ?? 0} queries`}>
        {!top_sql?.length ? (
          <p className="text-sm text-[var(--color-text-muted)] text-center py-4">No SQL data available</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-secondary)] uppercase tracking-wider">
                  {["SQL ID", "Execs", "Elapsed (s)", "Per Exec (s)", "CPU (s)", "Buffer Gets", "Disk Reads", "SQL Text"].map(h => (
                    <th key={h} className="text-left px-3 py-2 font-semibold whitespace-nowrap">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-border-muted)]">
                {top_sql.map((s, i) => (
                  <tr key={i} className={`hover:bg-[var(--color-bg-elevated)] ${i === 0 ? "bg-[var(--color-accent-red)]/5" : ""}`}>
                    <td className="px-3 py-2 font-mono text-[var(--color-accent-cyan)]">{s.sql_id}</td>
                    <td className="px-3 py-2 tabular-nums">{fmt(s.executions, 0)}</td>
                    <td className="px-3 py-2 tabular-nums font-medium">{fmt(s.elapsed_secs)}</td>
                    <td className="px-3 py-2 tabular-nums">{fmt(s.elapsed_per_exec, 4)}</td>
                    <td className="px-3 py-2 tabular-nums">{fmt(s.cpu_secs)}</td>
                    <td className="px-3 py-2 tabular-nums">{fmt(s.buffer_gets, 0)}</td>
                    <td className="px-3 py-2 tabular-nums">{fmt(s.disk_reads, 0)}</td>
                    <td className="px-3 py-2 font-mono max-w-xs truncate" title={s.sql_text}>{s.sql_text}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </SectionCard>

      {/* ── Tablespaces ── */}
      <SectionCard icon={HardDrive} title="Tablespaces">
        <div className="space-y-3">
          {tablespaces?.map((ts) => (
            <div key={ts.name}>
              <div className="flex justify-between text-xs mb-1">
                <span className="font-medium text-[var(--color-text-primary)]">{ts.name}</span>
                <span className="text-[var(--color-text-muted)]">{fmt(ts.used_mb)} / {fmt(ts.total_mb)} MB</span>
              </div>
              <UsageBar pct={ts.used_pct} label={ts.contents} />
            </div>
          ))}
        </div>
      </SectionCard>

      {/* ── Memory: SGA + PGA ── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
        <SectionCard icon={MemoryStick} title="SGA">
          <div className="space-y-2">
            {Object.entries(sga ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between text-xs">
                <span className="text-[var(--color-text-secondary)] capitalize">{k.replace(/_/g, " ")}</span>
                <span className="tabular-nums font-medium text-[var(--color-text-primary)]">{fmt(v)} MB</span>
              </div>
            ))}
          </div>
        </SectionCard>
        <SectionCard icon={MemoryStick} title="PGA">
          <div className="space-y-2">
            {Object.entries(pga ?? {}).map(([k, v]) => (
              <div key={k} className="flex justify-between text-xs">
                <span className="text-[var(--color-text-secondary)] capitalize">{k.replace(/_/g, " ")}</span>
                <span className="tabular-nums font-medium text-[var(--color-text-primary)]">{fmt(v)} MB</span>
              </div>
            ))}
          </div>
        </SectionCard>
      </div>

      {/* ── Wait Events ── */}
      <SectionCard icon={Clock} title="Top Wait Events">
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-secondary)] uppercase tracking-wider">
                {["Event", "Class", "Total Waits", "Time Waited (s)"].map(h => (
                  <th key={h} className="text-left px-3 py-2 font-semibold">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {waits?.map((w, i) => (
                <tr key={i} className="hover:bg-[var(--color-bg-elevated)]">
                  <td className="px-3 py-2 text-[var(--color-text-primary)]">{w.event}</td>
                  <td className="px-3 py-2 text-[var(--color-text-muted)]">{w.wait_class}</td>
                  <td className="px-3 py-2 tabular-nums">{fmt(w.total_waits, 0)}</td>
                  <td className="px-3 py-2 tabular-nums">{fmt(w.time_waited_s)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </SectionCard>

      {/* ── Locks — only if any ── */}
      {hasLocks && (
        <SectionCard icon={Lock} title="Active Locks" badge={locks.length} badgeColor="bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]">
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-secondary)] uppercase tracking-wider">
                  {["SID", "User", "Type", "Mode", "Request", "Held (s)", "Object"].map(h => (
                    <th key={h} className="text-left px-3 py-2 font-semibold">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-border-muted)]">
                {locks.map((l, i) => (
                  <tr key={i} className="hover:bg-[var(--color-bg-elevated)]">
                    <td className="px-3 py-2 tabular-nums">{l.sid}</td>
                    <td className="px-3 py-2">{l.username}</td>
                    <td className="px-3 py-2 font-mono">{l.lock_type}</td>
                    <td className="px-3 py-2">{l.lock_mode}</td>
                    <td className="px-3 py-2">{l.request}</td>
                    <td className="px-3 py-2 tabular-nums">{l.ctime}s</td>
                    <td className="px-3 py-2">{l.object_name || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </SectionCard>
      )}

    </div>
  );
}
