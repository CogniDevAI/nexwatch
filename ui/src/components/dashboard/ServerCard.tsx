import { useNavigate } from "react-router-dom";
import { Monitor, Apple, Terminal } from "lucide-react";
import type { Agent } from "@/types";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import type { Status } from "@/components/ui/status";

export interface AgentMetricsSummary {
  cpu: number;
  memory: number;
  disk: number;
}

interface ServerCardProps {
  agent: Agent;
  /** Alert-derived status from the shared fleet-health layer — see
   *  DESIGN.md §3 ("one status source of truth"). Never derive connectivity
   *  status locally here. */
  status: Status;
  metrics?: AgentMetricsSummary;
}

const BORDER_CLASS: Record<Status, string> = {
  ok: "border-l-[var(--color-ok)]",
  warning: "border-l-[var(--color-warn)]",
  critical: "border-l-[var(--color-critical)]",
  offline: "border-l-[var(--color-offline)]",
};

function OsIcon({ os }: { os: string }) {
  const normalized = os.toLowerCase();
  if (normalized.includes("darwin") || normalized.includes("mac")) {
    return <Apple className="h-4 w-4" aria-hidden="true" />;
  }
  return <Terminal className="h-4 w-4" aria-hidden="true" />;
}

function usageColor(value: number): string {
  if (value >= 90) return "var(--color-critical)";
  if (value >= 75) return "var(--color-warn)";
  return "var(--color-signal)";
}

function UsageBar({ label, value }: { label: string; value: number }) {
  const clamped = Math.min(Math.max(value, 0), 100);
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs">
        <span className="text-[var(--color-ink-muted)]">{label}</span>
        <span className="font-mono font-medium text-[var(--color-ink)] tabular-nums">
          {clamped.toFixed(1)}%
        </span>
      </div>
      <div className="h-1.5 w-full rounded-full bg-[var(--color-void)]">
        <div
          className="h-full rounded-full transition-all duration-500 ease-out"
          style={{ width: `${clamped}%`, backgroundColor: usageColor(clamped) }}
        />
      </div>
    </div>
  );
}

export function ServerCard({ agent, status, metrics }: ServerCardProps) {
  const navigate = useNavigate();

  return (
    <button
      type="button"
      onClick={() => navigate(`/servers/${agent.id}`)}
      className={`group w-full cursor-pointer rounded-[var(--radius-panel)] border border-l-4 border-[var(--color-line)] ${BORDER_CLASS[status]} bg-[var(--color-panel)] p-5 text-left transition-colors hover:bg-[var(--color-panel-raised)]`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-ink-muted)] transition-colors group-hover:text-[var(--color-signal)]">
            {agent.os ? (
              <OsIcon os={agent.os} />
            ) : (
              <Monitor className="h-4 w-4" aria-hidden="true" />
            )}
          </div>
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold text-[var(--color-ink)]">
              {agent.hostname || agent.name}
            </h3>
            <p className="mt-0.5 truncate font-mono text-xs text-[var(--color-ink-faint)]">
              {agent.ip || "No IP"}
            </p>
          </div>
        </div>
        <StatusIndicator status={status} dotOnly />
      </div>

      {metrics && (
        <div className="mt-4 flex flex-col gap-2.5 border-t border-[var(--color-line-soft)] pt-3">
          <UsageBar label="CPU" value={metrics.cpu} />
          <UsageBar label="Memory" value={metrics.memory} />
          <UsageBar label="Disk" value={metrics.disk} />
        </div>
      )}

      {!metrics && (
        <div className="mt-4 flex items-center justify-between border-t border-[var(--color-line-soft)] pt-3">
          <span className="text-xs text-[var(--color-ink-muted)]">{agent.os || "Unknown OS"}</span>
          <StatusIndicator status={status} />
        </div>
      )}
    </button>
  );
}
