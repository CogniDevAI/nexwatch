import { useEffect, useState, useRef, useCallback } from "react";
import { Wifi, AlertTriangle } from "lucide-react";
import pb from "@/lib/pocketbase";
import type { PortEntry } from "@/types";

interface PortsTabProps {
  agentId: string;
}

const DANGEROUS_PORTS = new Set([21, 23, 25, 69, 111, 135, 139, 445, 514, 1433, 1434, 3389, 5900, 5985, 5986]);

const REFRESH_INTERVAL = 15_000;

export function PortsTab({ agentId }: PortsTabProps) {
  const [ports, setPorts] = useState<PortEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchPorts = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/ports`, {
        headers: { Authorization: pb.authStore.token },
      });
      if (!res.ok) {
        setPorts([]);
        setError(true);
        return;
      }
      const json = (await res.json()) as { listeners: PortEntry[]; count: number };
      const items = json.listeners ?? [];
      setPorts(items.sort((a, b) => a.port - b.port));
      setError(false);
    } catch {
      setPorts([]);
      setError(true);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchPorts(true);

    intervalRef.current = setInterval(() => fetchPorts(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchPorts]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Wifi className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
          Loading ports...
        </span>
      </div>
    );
  }

  if (error || ports.length === 0) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <Wifi className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          No data yet
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          No open port data has been reported by this agent. Make sure the ports
          collector is enabled.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <span className="text-sm text-[var(--color-text-secondary)]">
          <span className="font-medium text-[var(--color-text-primary)]">{ports.length}</span>{" "}
          open {ports.length === 1 ? "port" : "ports"}
        </span>
        {ports.some((p) => DANGEROUS_PORTS.has(p.port)) && (
          <span className="inline-flex items-center gap-1.5 text-xs text-[var(--color-accent-yellow)]">
            <AlertTriangle className="w-3.5 h-3.5" />
            Potentially dangerous ports detected
          </span>
        )}
      </div>

      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border-default)]">
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Port
                </th>
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Protocol
                </th>
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Process
                </th>
                <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  PID
                </th>
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Address
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {ports.map((p, idx) => {
                const isDangerous = DANGEROUS_PORTS.has(p.port);
                return (
                  <tr
                    key={`${p.port}-${p.protocol}-${p.pid}`}
                    className={`transition-colors ${
                      idx % 2 === 0
                        ? "bg-transparent"
                        : "bg-[var(--color-bg-elevated)]/30"
                    } hover:bg-[var(--color-bg-elevated)]`}
                  >
                    <td className="px-5 py-3 tabular-nums">
                      <span
                        className={`font-medium ${
                          isDangerous
                            ? "text-[var(--color-accent-red)]"
                            : "text-[var(--color-text-primary)]"
                        }`}
                      >
                        {p.port}
                      </span>
                      {isDangerous && (
                        <AlertTriangle className="inline w-3.5 h-3.5 ml-1.5 text-[var(--color-accent-yellow)]" />
                      )}
                    </td>
                    <td className="px-5 py-3 text-[var(--color-text-secondary)] uppercase text-xs">
                      {p.protocol}
                    </td>
                    <td className="px-5 py-3 text-[var(--color-text-primary)]">
                      {p.process}
                    </td>
                    <td className="px-5 py-3 text-right text-[var(--color-text-secondary)] tabular-nums">
                      {p.pid}
                    </td>
                    <td className="px-5 py-3 text-[var(--color-text-secondary)] font-mono text-xs">
                      {p.address}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
