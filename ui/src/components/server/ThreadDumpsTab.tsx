import { useState, useEffect, useCallback, useRef } from "react";
import { FileCode2, Play, RefreshCw, Clock, CheckCircle, XCircle, Loader2, Copy, Check } from "lucide-react";
import pb from "@/lib/pocketbase";

// ─── Types ────────────────────────────────────────────────────────────────────

interface ThreadDumpsTabProps {
  agentId: string;
}

interface Process {
  pid: number;
  name: string;
  cmdline: string;
  user: string;
}

interface DumpSummary {
  id: string;
  pid: number;
  process_name: string;
  request_id: string;
  status: "pending" | "success" | "error";
  error?: string;
  taken_at: string;
}

interface DumpDetail extends DumpSummary {
  output: string;
}

// ─── Utilities ────────────────────────────────────────────────────────────────

function formatDate(iso: string) {
  if (!iso) return "—";
  return new Date(iso).toLocaleString();
}

function isJavaProcess(p: Process) {
  return (
    p.name === "java" ||
    (p.cmdline ?? "").toLowerCase().includes("java") ||
    (p.cmdline ?? "").toLowerCase().includes(".jar")
  );
}

// ─── Status Badge ─────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: DumpSummary["status"] }) {
  if (status === "pending") {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]">
        <Loader2 className="w-3 h-3 animate-spin" />
        pending
      </span>
    );
  }
  if (status === "success") {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]">
        <CheckCircle className="w-3 h-3" />
        success
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]">
      <XCircle className="w-3 h-3" />
      error
    </span>
  );
}

// ─── Dump Viewer ──────────────────────────────────────────────────────────────

function DumpViewer({ dump, onClose }: { dump: DumpDetail; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(dump.output).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="relative w-full max-w-5xl max-h-[85vh] mx-4 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] flex flex-col shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-[var(--color-border-default)]">
          <div className="flex items-center gap-3">
            <FileCode2 className="w-5 h-5 text-[var(--color-accent-cyan)]" />
            <div>
              <p className="text-sm font-semibold text-[var(--color-text-primary)]">
                Thread Dump — PID {dump.pid}
                {dump.process_name && (
                  <span className="ml-2 text-[var(--color-text-muted)] font-normal">({dump.process_name})</span>
                )}
              </p>
              <p className="text-xs text-[var(--color-text-muted)]">{formatDate(dump.taken_at)}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleCopy}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-[var(--color-border-default)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-elevated)] transition-colors"
            >
              {copied ? <Check className="w-3.5 h-3.5 text-[var(--color-accent-green)]" /> : <Copy className="w-3.5 h-3.5" />}
              {copied ? "Copied!" : "Copy"}
            </button>
            <button
              onClick={onClose}
              className="px-3 py-1.5 rounded-lg text-xs font-medium border border-[var(--color-border-default)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-elevated)] transition-colors"
            >
              Close
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-auto p-4">
          {dump.status === "error" ? (
            <div className="rounded-lg bg-[var(--color-accent-red)]/10 border border-[var(--color-accent-red)]/20 p-4">
              <p className="text-sm text-[var(--color-accent-red)] font-mono">{dump.error}</p>
            </div>
          ) : (
            <pre className="text-xs font-mono text-[var(--color-text-secondary)] whitespace-pre leading-relaxed">
              {dump.output}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export function ThreadDumpsTab({ agentId }: ThreadDumpsTabProps) {
  const [processes, setProcesses] = useState<Process[]>([]);
  const [dumps, setDumps] = useState<DumpSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [requesting, setRequesting] = useState<number | null>(null); // PID being dumped
  const [selectedDump, setSelectedDump] = useState<DumpDetail | null>(null);
  const [filterJava, setFilterJava] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const headers = { Authorization: pb.authStore.token ?? "" };

  // ── Fetch processes ──
  const fetchProcesses = useCallback(async () => {
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/processes`, { headers });
      if (!res.ok) return;
      const data = await res.json();
      setProcesses(data.processes ?? []);
    } catch {
      // silent
    }
  }, [agentId]);

  // ── Fetch dump history ──
  const fetchDumps = useCallback(async () => {
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/thread-dumps`, { headers });
      if (!res.ok) return;
      const data = await res.json();
      setDumps(data.dumps ?? []);
    } catch {
      // silent
    }
  }, [agentId]);

  // ── Initial load ──
  useEffect(() => {
    setLoading(true);
    Promise.all([fetchProcesses(), fetchDumps()]).finally(() => setLoading(false));
  }, [fetchProcesses, fetchDumps]);

  // ── Poll pending dumps ──
  useEffect(() => {
    const hasPending = dumps.some((d) => d.status === "pending");
    if (hasPending) {
      pollRef.current = setInterval(fetchDumps, 2000);
    } else {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
        // Clear requesting state when dump resolves.
        setRequesting(null);
      }
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [dumps, fetchDumps]);

  // ── Request a dump ──
  const requestDump = async (proc: Process) => {
    setRequesting(proc.pid);
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/thread-dump`, {
        method: "POST",
        headers: { ...headers, "Content-Type": "application/json" },
        body: JSON.stringify({ pid: proc.pid, process_name: proc.name }),
      });
      if (!res.ok) {
        const err = await res.json();
        alert(`Failed: ${err.error ?? "unknown error"}`);
        setRequesting(null);
        return;
      }
      // Start polling for result.
      await fetchDumps();
    } catch (e) {
      alert(`Error: ${e}`);
      setRequesting(null);
    }
  };

  // ── Open dump detail ──
  const openDump = async (dump: DumpSummary) => {
    if (dump.status === "pending") return;
    try {
      const res = await fetch(`/api/custom/thread-dumps/${dump.id}`, { headers });
      if (!res.ok) return;
      const detail = await res.json();
      setSelectedDump(detail);
    } catch {
      // silent
    }
  };

  const displayed = filterJava ? processes.filter(isJavaProcess) : processes;

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="w-6 h-6 animate-spin text-[var(--color-text-muted)]" />
      </div>
    );
  }

  return (
    <>
      {selectedDump && <DumpViewer dump={selectedDump} onClose={() => setSelectedDump(null)} />}

      <div className="space-y-6">
        {/* ── Process list ── */}
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
          <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Play className="w-4 h-4 text-[var(--color-text-muted)]" />
              <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">Running Processes</h3>
              <span className="text-xs text-[var(--color-text-muted)]">({displayed.length})</span>
            </div>
            <label className="flex items-center gap-2 text-xs text-[var(--color-text-secondary)] cursor-pointer select-none">
              <input
                type="checkbox"
                checked={filterJava}
                onChange={(e) => setFilterJava(e.target.checked)}
                className="rounded"
              />
              Java only
            </label>
          </div>

          {displayed.length === 0 ? (
            <div className="p-10 text-center text-sm text-[var(--color-text-muted)]">
              {filterJava ? "No Java processes found. Uncheck 'Java only' to see all processes." : "No processes available."}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[var(--color-border-default)]">
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">PID</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Name</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">User</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)] max-w-xs">Command</th>
                    <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--color-border-muted)]">
                  {displayed.map((proc, idx) => {
                    const isRunning = requesting === proc.pid;
                    return (
                      <tr
                        key={`${proc.pid}-${idx}`}
                        className={`transition-colors ${idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-bg-elevated)]/30"} hover:bg-[var(--color-bg-elevated)]`}
                      >
                        <td className="px-5 py-3 font-mono text-xs text-[var(--color-text-muted)] tabular-nums">{proc.pid}</td>
                        <td className="px-5 py-3 font-medium text-[var(--color-text-primary)]">{proc.name}</td>
                        <td className="px-5 py-3 text-[var(--color-text-secondary)] text-xs">{proc.user}</td>
                        <td className="px-5 py-3 text-[var(--color-text-muted)] font-mono text-xs max-w-xs">
                          <span className="truncate block" title={proc.cmdline}>{proc.cmdline || "—"}</span>
                        </td>
                        <td className="px-5 py-3 text-right">
                          <button
                            onClick={() => requestDump(proc)}
                            disabled={isRunning || requesting !== null}
                            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-[var(--color-accent-cyan)]/10 text-[var(--color-accent-cyan)] border border-[var(--color-accent-cyan)]/20 hover:bg-[var(--color-accent-cyan)]/20 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                          >
                            {isRunning ? (
                              <><Loader2 className="w-3 h-3 animate-spin" /> Dumping...</>
                            ) : (
                              <><FileCode2 className="w-3 h-3" /> Thread Dump</>
                            )}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* ── Dump history ── */}
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
          <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Clock className="w-4 h-4 text-[var(--color-text-muted)]" />
              <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">Dump History</h3>
              <span className="text-xs text-[var(--color-text-muted)]">({dumps.length})</span>
            </div>
            <button
              onClick={fetchDumps}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs text-[var(--color-text-secondary)] border border-[var(--color-border-default)] hover:bg-[var(--color-bg-elevated)] transition-colors"
            >
              <RefreshCw className="w-3.5 h-3.5" />
              Refresh
            </button>
          </div>

          {dumps.length === 0 ? (
            <div className="p-10 text-center">
              <FileCode2 className="w-10 h-10 text-[var(--color-text-muted)] mx-auto mb-3" />
              <p className="text-sm text-[var(--color-text-secondary)]">No thread dumps yet. Click "Thread Dump" on any process to capture one.</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[var(--color-border-default)]">
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Status</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">PID</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Process</th>
                    <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Taken At</th>
                    <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--color-border-muted)]">
                  {dumps.map((dump, idx) => (
                    <tr
                      key={dump.id}
                      className={`transition-colors ${idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-bg-elevated)]/30"} hover:bg-[var(--color-bg-elevated)]`}
                    >
                      <td className="px-5 py-3"><StatusBadge status={dump.status} /></td>
                      <td className="px-5 py-3 font-mono text-xs text-[var(--color-text-muted)] tabular-nums">{dump.pid}</td>
                      <td className="px-5 py-3 text-[var(--color-text-primary)]">{dump.process_name || "—"}</td>
                      <td className="px-5 py-3 text-xs text-[var(--color-text-muted)]">{formatDate(dump.taken_at)}</td>
                      <td className="px-5 py-3 text-right">
                        <button
                          onClick={() => openDump(dump)}
                          disabled={dump.status === "pending"}
                          className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-medium text-[var(--color-text-secondary)] border border-[var(--color-border-default)] hover:bg-[var(--color-bg-elevated)] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                        >
                          View
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
