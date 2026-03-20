import { useNavigate } from "react-router-dom";
import { Monitor, Apple, Terminal } from "lucide-react";
import type { Agent } from "@/types";

export interface AgentMetricsSummary {
  cpu: number;
  memory: number;
  disk: number;
}

interface ServerCardProps {
  agent: Agent;
  metrics?: AgentMetricsSummary;
}

function OsIcon({ os }: { os: string }) {
  const normalized = os.toLowerCase();
  if (normalized.includes("darwin") || normalized.includes("mac")) {
    return <Apple className="w-4 h-4" />;
  }
  return <Terminal className="w-4 h-4" />;
}

function StatusDot({ status }: { status: "online" | "offline" }) {
  return (
    <span className="relative flex h-2.5 w-2.5">
      {status === "online" && (
        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-[var(--color-accent-green)] opacity-75" />
      )}
      <span
        className={`relative inline-flex rounded-full h-2.5 w-2.5 ${
          status === "online"
            ? "bg-[var(--color-accent-green)]"
            : "bg-[var(--color-accent-red)]"
        }`}
      />
    </span>
  );
}

function UsageBar({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) {
  const clamped = Math.min(Math.max(value, 0), 100);
  return (
    <div>
      <div className="flex items-center justify-between text-xs mb-1">
        <span className="text-[var(--color-text-secondary)]">{label}</span>
        <span className="text-[var(--color-text-primary)] font-medium tabular-nums">
          {clamped.toFixed(1)}%
        </span>
      </div>
      <div className="h-1.5 w-full rounded-full bg-[var(--color-bg-primary)]">
        <div
          className="h-full rounded-full transition-all duration-500 ease-out"
          style={{ width: `${clamped}%`, backgroundColor: color }}
        />
      </div>
    </div>
  );
}

export function ServerCard({ agent, metrics }: ServerCardProps) {
  const navigate = useNavigate();

  const cpuUsage = metrics?.cpu ?? 0;
  const memUsage = metrics?.memory ?? 0;
  const diskUsage = metrics?.disk ?? 0;

  return (
    <button
      type="button"
      onClick={() => navigate(`/servers/${agent.id}`)}
      className="w-full text-left rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-5 hover:border-[var(--color-accent-cyan)]/40 hover:bg-[var(--color-bg-elevated)] transition-all duration-200 cursor-pointer group"
    >
      {/* Header */}
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex-shrink-0 w-9 h-9 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] flex items-center justify-center text-[var(--color-text-secondary)] group-hover:text-[var(--color-accent-cyan)] group-hover:border-[var(--color-accent-cyan)]/30 transition-colors">
            {agent.os ? (
              <OsIcon os={agent.os} />
            ) : (
              <Monitor className="w-4 h-4" />
            )}
          </div>
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)] truncate">
              {agent.hostname || agent.name}
            </h3>
            <p className="text-xs text-[var(--color-text-muted)] truncate">
              {agent.ip || "No IP"}
            </p>
          </div>
        </div>
        <StatusDot status={agent.status} />
      </div>

      {/* Usage bars */}
      <div className="space-y-3">
        <UsageBar
          label="CPU"
          value={cpuUsage}
          color="var(--color-accent-cyan)"
        />
        <UsageBar
          label="Memory"
          value={memUsage}
          color="var(--color-accent-purple)"
        />
        <UsageBar
          label="Disk"
          value={diskUsage}
          color="var(--color-accent-yellow)"
        />
      </div>

      {/* Footer */}
      <div className="mt-4 pt-3 border-t border-[var(--color-border-muted)] flex items-center justify-between">
        <span className="text-xs text-[var(--color-text-muted)]">
          {agent.os || "Unknown OS"}
        </span>
        <span
          className={`text-xs font-medium px-2 py-0.5 rounded-full ${
            agent.status === "online"
              ? "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]"
              : "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"
          }`}
        >
          {agent.status}
        </span>
      </div>
    </button>
  );
}
