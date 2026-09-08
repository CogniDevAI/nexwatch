import { useState, useEffect, useCallback, useRef } from "react";
import { FileCode2, Play, RefreshCw, Clock, Loader2, Copy, Check } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { useAuthStore } from "@/stores/authStore";
import { Panel, PanelHeader } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator, type Status } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { useToast } from "@/components/ui/toastContext";

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

function dumpStatus(status: DumpSummary["status"]): Status {
  if (status === "pending") return "warning";
  if (status === "success") return "ok";
  return "critical";
}

// ─── Dump Viewer ──────────────────────────────────────────────────────────────

function DumpViewer({ dump, onClose }: { dump: DumpDetail; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(dump.output).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <Modal
      title={
        <>
          Thread dump — PID {dump.pid}
          {dump.process_name && (
            <span className="ml-2 text-sm font-normal text-[var(--color-ink-faint)]">
              ({dump.process_name})
            </span>
          )}
        </>
      }
      onClose={onClose}
      maxWidth="max-w-5xl"
    >
      <div className="flex max-h-[85vh] flex-col">
        <div className="flex items-center justify-between border-b border-[var(--color-line)] px-6 py-2">
          <p className="text-xs text-[var(--color-ink-faint)]">{formatDate(dump.taken_at)}</p>
          <Button size="sm" onClick={handleCopy}>
            {copied ? (
              <Check className="h-3.5 w-3.5 text-[var(--color-ok)]" aria-hidden="true" />
            ) : (
              <Copy className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
        <div className="flex-1 overflow-auto p-4">
          {dump.status === "error" ? (
            <div className="rounded-[var(--radius-control)] border border-[var(--color-critical)]/25 bg-[var(--color-critical)]/10 p-4">
              <p className="font-mono text-sm text-[var(--color-critical)]">{dump.error}</p>
            </div>
          ) : (
            <pre className="font-mono text-xs leading-relaxed whitespace-pre text-[var(--color-ink-muted)]">
              {dump.output}
            </pre>
          )}
        </div>
      </div>
    </Modal>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export function ThreadDumpsTab({ agentId }: ThreadDumpsTabProps) {
  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const { showToast } = useToast();
  const [processes, setProcesses] = useState<Process[]>([]);
  const [dumps, setDumps] = useState<DumpSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [requesting, setRequesting] = useState<number | null>(null); // PID being dumped
  const [selectedDump, setSelectedDump] = useState<DumpDetail | null>(null);
  const [filterJava, setFilterJava] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Fetch processes ──
  const fetchProcesses = useCallback(async () => {
    try {
      const res = await apiFetch(`/api/custom/agents/${agentId}/processes`);
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
      const res = await apiFetch(`/api/custom/agents/${agentId}/thread-dumps`);
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
    void Promise.all([fetchProcesses(), fetchDumps()]).finally(() => setLoading(false));
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
      const res = await apiFetch(`/api/custom/agents/${agentId}/thread-dump`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pid: proc.pid, process_name: proc.name }),
      });
      if (!res.ok) {
        const err = await res.json();
        showToast(`Thread dump failed: ${err.error ?? "unknown error"}`, "error");
        setRequesting(null);
        return;
      }
      // Start polling for result.
      await fetchDumps();
    } catch (e) {
      showToast(`Thread dump failed: ${String(e)}`, "error");
      setRequesting(null);
    }
  };

  // ── Open dump detail ──
  const openDump = async (dump: DumpSummary) => {
    if (dump.status === "pending") return;
    try {
      const res = await apiFetch(`/api/custom/thread-dumps/${dump.id}`);
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
        <Loader2
          className="h-6 w-6 animate-spin text-[var(--color-ink-faint)]"
          aria-hidden="true"
        />
      </div>
    );
  }

  return (
    <>
      {selectedDump && <DumpViewer dump={selectedDump} onClose={() => setSelectedDump(null)} />}

      <div className="space-y-6">
        {/* Process list */}
        <Panel>
          <PanelHeader
            icon={<Play className="h-4 w-4" />}
            title="Running processes"
            badge={
              <span className="text-xs font-normal text-[var(--color-ink-faint)]">
                ({displayed.length})
              </span>
            }
            actions={
              <label className="flex cursor-pointer items-center gap-2 text-xs text-[var(--color-ink-muted)] select-none">
                <input
                  type="checkbox"
                  checked={filterJava}
                  onChange={(e) => setFilterJava(e.target.checked)}
                  className="rounded border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                />
                Java only
              </label>
            }
          />

          {displayed.length === 0 ? (
            <EmptyState
              icon={FileCode2}
              title="No processes to show"
              description={
                filterJava
                  ? "No Java processes found. Uncheck “Java only” to see all processes."
                  : "No processes available."
              }
            />
          ) : (
            <Table>
              <thead>
                <tr className="border-b border-[var(--color-line)]">
                  <Th>PID</Th>
                  <Th>Name</Th>
                  <Th>User</Th>
                  <Th>Command</Th>
                  <Th align="right">Action</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {displayed.map((proc, idx) => {
                  const isRunning = requesting === proc.pid;
                  return (
                    <tr key={`${proc.pid}-${idx}`} className={rowClass(idx)}>
                      <Td className="font-mono text-[var(--color-ink-faint)] tabular-nums">
                        {proc.pid}
                      </Td>
                      <Td className="font-medium">{proc.name}</Td>
                      <Td className="text-[var(--color-ink-muted)]">{proc.user}</Td>
                      <Td className="max-w-xs font-mono text-xs text-[var(--color-ink-faint)]">
                        <span className="block truncate" title={proc.cmdline}>
                          {proc.cmdline || "—"}
                        </span>
                      </Td>
                      <Td align="right">
                        <Button
                          size="sm"
                          variant="accent"
                          onClick={() => requestDump(proc)}
                          disabled={!canManage || isRunning || requesting !== null}
                          title={
                            canManage
                              ? undefined
                              : "You need the operator role to request a thread dump"
                          }
                        >
                          {isRunning ? (
                            <>
                              <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />{" "}
                              Dumping…
                            </>
                          ) : (
                            <>
                              <FileCode2 className="h-3 w-3" aria-hidden="true" /> Thread dump
                            </>
                          )}
                        </Button>
                      </Td>
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          )}
        </Panel>

        {/* Dump history */}
        <Panel>
          <PanelHeader
            icon={<Clock className="h-4 w-4" />}
            title="Dump history"
            badge={
              <span className="text-xs font-normal text-[var(--color-ink-faint)]">
                ({dumps.length})
              </span>
            }
            actions={
              <Button size="sm" onClick={fetchDumps}>
                <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
                Refresh
              </Button>
            }
          />

          {dumps.length === 0 ? (
            <EmptyState
              icon={FileCode2}
              title="No thread dumps yet"
              description="Select a process above and choose Thread dump to capture one."
            />
          ) : (
            <Table>
              <thead>
                <tr className="border-b border-[var(--color-line)]">
                  <Th>Status</Th>
                  <Th>PID</Th>
                  <Th>Process</Th>
                  <Th>Taken at</Th>
                  <Th align="right">Action</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {dumps.map((dump, idx) => (
                  <tr key={dump.id} className={rowClass(idx)}>
                    <Td>
                      <StatusIndicator status={dumpStatus(dump.status)} label={dump.status} />
                    </Td>
                    <Td className="font-mono text-[var(--color-ink-faint)] tabular-nums">
                      {dump.pid}
                    </Td>
                    <Td>{dump.process_name || "—"}</Td>
                    <Td className="text-xs text-[var(--color-ink-faint)]">
                      {formatDate(dump.taken_at)}
                    </Td>
                    <Td align="right">
                      <Button
                        size="sm"
                        onClick={() => openDump(dump)}
                        disabled={dump.status === "pending"}
                      >
                        View
                      </Button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Panel>
      </div>
    </>
  );
}
