import { useEffect, useState, useRef, useCallback } from "react";
import { Wifi, AlertTriangle } from "lucide-react";
import { apiFetch } from "@/lib/api";
import type { PortEntry } from "@/types";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";

interface PortsTabProps {
  agentId: string;
}

const DANGEROUS_PORTS = new Set([
  21, 23, 25, 69, 111, 135, 139, 445, 514, 1433, 1434, 3389, 5900, 5985, 5986,
]);

const REFRESH_INTERVAL = 15_000;

export function PortsTab({ agentId }: PortsTabProps) {
  const [ports, setPorts] = useState<PortEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchPorts = useCallback(
    async (showLoading = false) => {
      if (showLoading) setLoading(true);
      try {
        const res = await apiFetch(`/api/custom/agents/${agentId}/ports`);
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
    },
    [agentId],
  );

  useEffect(() => {
    void fetchPorts(true);

    intervalRef.current = setInterval(() => fetchPorts(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchPorts]);

  if (loading) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (error) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <ErrorState
          title="Couldn't load open ports"
          description="Check that this agent is online and reporting to the hub, then try again."
        />
      </div>
    );
  }

  if (ports.length === 0) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={Wifi}
          title="No data yet"
          description="No open port data has been reported by this agent. Make sure the ports collector is enabled."
        />
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 flex items-center gap-3">
        <span className="text-sm text-[var(--color-ink-muted)]">
          <span className="font-mono font-medium text-[var(--color-ink)]">{ports.length}</span> open{" "}
          {ports.length === 1 ? "port" : "ports"}
        </span>
        {ports.some((p) => DANGEROUS_PORTS.has(p.port)) && (
          <span className="inline-flex items-center gap-1.5 text-xs text-[var(--color-warn)]">
            <AlertTriangle className="h-3.5 w-3.5" aria-hidden="true" />
            Potentially dangerous ports detected
          </span>
        )}
      </div>

      <Table>
        <thead>
          <tr className="border-b border-[var(--color-line)]">
            <Th>Port</Th>
            <Th>Protocol</Th>
            <Th>Process</Th>
            <Th align="right">PID</Th>
            <Th>Address</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-[var(--color-line-soft)]">
          {ports.map((p, idx) => {
            const isDangerous = DANGEROUS_PORTS.has(p.port);
            return (
              <tr key={`${p.port}-${p.protocol}-${p.pid}`} className={rowClass(idx)}>
                <Td className="font-mono tabular-nums">
                  <span
                    className={
                      isDangerous ? "font-medium text-[var(--color-critical)]" : "font-medium"
                    }
                  >
                    {p.port}
                  </span>
                  {isDangerous && (
                    <AlertTriangle
                      className="ml-1.5 inline h-3.5 w-3.5 text-[var(--color-warn)]"
                      aria-label="Potentially dangerous port"
                    />
                  )}
                </Td>
                <Td className="text-xs text-[var(--color-ink-muted)] uppercase">{p.protocol}</Td>
                <Td>{p.process}</Td>
                <Td align="right" className="font-mono text-[var(--color-ink-muted)] tabular-nums">
                  {p.pid}
                </Td>
                <Td className="font-mono text-xs text-[var(--color-ink-muted)]">{p.address}</Td>
              </tr>
            );
          })}
        </tbody>
      </Table>
    </div>
  );
}
