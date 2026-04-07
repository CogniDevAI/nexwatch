import { useEffect, useState, useCallback } from "react";
import { Server, Wifi, WifiOff, Activity } from "lucide-react";
import { useAgentStore } from "@/stores/agentStore";
import { agentStatus } from "@/lib/agent";
import {
  ServerCard,
  type AgentMetricsSummary,
} from "@/components/dashboard/ServerCard";

interface DashboardSummary {
  agents: Record<string, AgentMetricsSummary>;
}

function StatCard({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: number;
  icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }>;
  color: string;
}) {
  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-5">
      <div className="flex items-center gap-3">
        <div
          className="w-10 h-10 rounded-lg flex items-center justify-center"
          style={{ backgroundColor: `${color}15` }}
        >
          <Icon className="w-5 h-5" style={{ color }} />
        </div>
        <div>
          <p className="text-2xl font-bold text-[var(--color-text-primary)]">
            {value}
          </p>
          <p className="text-xs text-[var(--color-text-secondary)]">{label}</p>
        </div>
      </div>
    </div>
  );
}

export function Dashboard() {
  const { agents, loading, error, fetchAgents, subscribeToAgents } =
    useAgentStore();
  const [metricsSummary, setMetricsSummary] =
    useState<Record<string, AgentMetricsSummary>>({});

  const fetchDashboardSummary = useCallback(async () => {
    try {
      const response = await fetch("/api/custom/dashboard");
      if (response.ok) {
        const data = (await response.json()) as DashboardSummary;
        setMetricsSummary(data.agents ?? {});
      }
    } catch {
      // Dashboard API might not be available yet
    }
  }, []);

  useEffect(() => {
    fetchAgents();
    fetchDashboardSummary();
    const unsubscribe = subscribeToAgents();

    // Refresh metrics summary every 10 seconds (matches agent collection interval)
    const interval = setInterval(fetchDashboardSummary, 10_000);

    return () => {
      unsubscribe();
      clearInterval(interval);
    };
  }, [fetchAgents, subscribeToAgents, fetchDashboardSummary]);

  const onlineCount = agents.filter((a) => agentStatus(a) === "online").length;
  const offlineCount = agents.filter((a) => agentStatus(a) === "offline").length;

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Dashboard</h2>

      {/* Stats row */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
        <StatCard
          label="Total Agents"
          value={agents.length}
          icon={Server}
          color="var(--color-accent-cyan)"
        />
        <StatCard
          label="Online"
          value={onlineCount}
          icon={Wifi}
          color="var(--color-accent-green)"
        />
        <StatCard
          label="Offline"
          value={offlineCount}
          icon={WifiOff}
          color="var(--color-accent-red)"
        />
      </div>

      {/* Error state */}
      {error && (
        <div className="mb-6 px-4 py-3 rounded-lg bg-[var(--color-accent-red)]/10 border border-[var(--color-accent-red)]/20 text-[var(--color-accent-red)] text-sm">
          {error}
        </div>
      )}

      {/* Loading state */}
      {loading && agents.length === 0 && (
        <div className="flex items-center justify-center py-20">
          <Activity className="w-6 h-6 text-[var(--color-accent-cyan)] animate-pulse" />
          <span className="ml-3 text-[var(--color-text-secondary)]">
            Loading agents...
          </span>
        </div>
      )}

      {/* Agent grid */}
      {!loading && agents.length === 0 ? (
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
          <Server className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
          <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
            No agents connected
          </h3>
          <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
            Add an agent to start monitoring your servers. Go to Settings to
            generate an install command.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {agents.map((agent) => (
            <ServerCard
              key={agent.id}
              agent={agent}
              metrics={metricsSummary[agent.id]}
            />
          ))}
        </div>
      )}
    </div>
  );
}
