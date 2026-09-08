import { useEffect, useState, useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import { Server } from "lucide-react";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import { useAuthStore } from "@/stores/authStore";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import { checkStatus } from "@/lib/checks";
import { ServerCard, type AgentMetricsSummary } from "@/components/dashboard/ServerCard";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, PanelHeader } from "@/components/ui/Panel";
import { FleetStrip } from "@/components/ui/FleetStrip";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { TagFilterBar } from "@/components/ui/TagChips";
import { AckControl } from "@/components/alerts/AckControl";
import { SilencedBadge, EscalatedBadge } from "@/components/alerts/AlertBadges";
import { useFleetHealth } from "@/hooks/useFleetHealth";
import { useChecksSummary } from "@/hooks/useChecksSummary";
import { usePageTitle } from "@/hooks/usePageTitle";

interface DashboardSummary {
  agents: Record<string, AgentMetricsSummary>;
}

export function Dashboard() {
  usePageTitle("Dashboard");

  // Agents/checks are fetched and subscribed once by AppShell — this just
  // reads the shared stores. See DESIGN.md §10.
  const { agents, loading, error, fetchAgents } = useAgentStore();
  const { checks } = useChecksStore();
  const { summary: checksSummary } = useChecksSummary();
  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const [metricsSummary, setMetricsSummary] = useState<Record<string, AgentMetricsSummary>>({});
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set());
  const {
    fleet,
    statusByAgentId,
    activeAlerts,
    activeAlertsLoading,
    activeAlertsError,
    refetchActiveAlerts,
  } = useFleetHealth();

  // Checks needing attention: down, or a certificate expiring soon —
  // surfaced here so an incident doesn't require a separate trip to
  // /checks just to notice it exists.
  const problemChecks = useMemo(
    () => checksSummary.filter((c) => c.status === "down" || c.cert_expiring_soon),
    [checksSummary],
  );

  const allTags = useMemo(
    () => Array.from(new Set(agents.flatMap((a) => a.tags ?? []))).sort(),
    [agents],
  );

  const toggleTag = (tag: string) => {
    setSelectedTags((prev) => {
      const next = new Set(prev);
      if (next.has(tag)) next.delete(tag);
      else next.add(tag);
      return next;
    });
  };

  // Agents with ANY of the selected tags — an empty selection shows everyone.
  // The FleetStrip hero is scoped by the same filter so the health strip
  // answers "which one is unhealthy" for the tags currently in view, not the
  // whole fleet, once a filter is active.
  const filteredAgents = useMemo(
    () =>
      selectedTags.size === 0
        ? agents
        : agents.filter((a) => (a.tags ?? []).some((t) => selectedTags.has(t))),
    [agents, selectedTags],
  );
  const filteredAgentIds = useMemo(
    () => new Set(filteredAgents.map((a) => a.id)),
    [filteredAgents],
  );
  const filteredFleet = useMemo(
    () => fleet.filter((f) => filteredAgentIds.has(f.id)),
    [fleet, filteredAgentIds],
  );

  const fetchDashboardSummary = useCallback(async () => {
    try {
      const response = await apiFetch("/api/custom/dashboard");
      if (response.ok) {
        const data = (await response.json()) as DashboardSummary;
        setMetricsSummary(data.agents ?? {});
      }
    } catch {
      // Dashboard API might not be available yet
    }
  }, []);

  useEffect(() => {
    void fetchDashboardSummary();
    // Refresh metrics summary every 10 seconds (matches agent collection interval)
    const interval = setInterval(fetchDashboardSummary, 10_000);
    return () => clearInterval(interval);
  }, [fetchDashboardSummary]);

  const criticalCount = activeAlerts.filter((a) => a.severity === "critical").length;
  const warningCount = activeAlerts.filter((a) => a.severity === "warning").length;

  return (
    <div>
      <PageHeader title="Dashboard" description="Every connected server, at a glance." />

      {/* Fleet health — the hero: one tick per agent, real status, click-through.
          Skipped once we know there are zero agents, so that message isn't
          shown twice alongside the EmptyState below. */}
      {(loading || error || agents.length > 0) && (
        <Panel className="mb-6 p-5">
          {loading && agents.length === 0 ? (
            <Skeleton className="h-9 w-full" />
          ) : error ? (
            <ErrorState
              title="Couldn't load agents"
              description={error}
              action={
                <Button variant="primary" size="sm" onClick={() => void fetchAgents()}>
                  Try again
                </Button>
              }
            />
          ) : (
            <>
              <FleetStrip agents={filteredFleet} size="lg" />
              {!activeAlertsLoading && !activeAlertsError && activeAlerts.length === 0 && (
                <p className="mt-4 text-sm text-[var(--color-ink-faint)]">No active alerts</p>
              )}
            </>
          )}
        </Panel>
      )}

      {/* Active alerts — incident visibility, between the health strip and the grid.
          Only relevant once there's at least one agent to alert on. */}
      {agents.length === 0 ? null : activeAlertsError ? (
        <Panel className="mb-8">
          <ErrorState
            title="Couldn't load active alerts"
            description={activeAlertsError}
            action={
              <Button variant="primary" size="sm" onClick={refetchActiveAlerts}>
                Try again
              </Button>
            }
          />
        </Panel>
      ) : activeAlertsLoading && activeAlerts.length === 0 ? (
        <Panel className="mb-8 p-5">
          <Skeleton className="h-16 w-full" />
        </Panel>
      ) : activeAlerts.length > 0 ? (
        <Panel className="mb-8">
          <PanelHeader
            title="Active alerts"
            actions={
              <div className="flex items-center gap-2">
                {criticalCount > 0 && (
                  <StatusIndicator status="critical" label={`${criticalCount} critical`} />
                )}
                {warningCount > 0 && (
                  <StatusIndicator status="warning" label={`${warningCount} warning`} />
                )}
              </div>
            }
          />
          <div className="divide-y divide-[var(--color-line-soft)]">
            {activeAlerts.slice(0, 5).map((alert) => (
              <div
                key={alert.id}
                className="flex flex-wrap items-center gap-3 px-5 py-3 sm:flex-nowrap sm:gap-4"
              >
                <StatusIndicator status={alert.severity} dotOnly />
                <Link
                  to={alert.checkId ? "/checks" : `/servers/${alert.agentId}`}
                  className="min-w-0 flex-1 rounded-[var(--radius-control)] transition-colors hover:bg-[var(--color-panel-raised)]"
                >
                  <p className="flex items-center gap-1.5 truncate text-sm font-medium text-[var(--color-ink)]">
                    {alert.ruleName}
                    {alert.count > 1 && (
                      <span className="text-2xs rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 font-mono text-[var(--color-ink-faint)] tabular-nums">
                        ×{alert.count}
                      </span>
                    )}
                  </p>
                  <p className="truncate text-xs text-[var(--color-ink-faint)]">
                    {alert.checkId
                      ? `${alert.checkName}${alert.checkTarget ? ` (${alert.checkTarget})` : ""}`
                      : alert.agentName}
                  </p>
                </Link>
                {alert.silenced && <SilencedBadge />}
                {alert.escalatedAt && <EscalatedBadge />}
                <AckControl
                  alertId={alert.id}
                  acknowledgedAt={alert.acknowledgedAt}
                  acknowledgedBy={alert.acknowledgedBy}
                  canManage={canManage}
                />
                <span className="flex-shrink-0 text-xs text-[var(--color-ink-faint)]">
                  {timeSince(alert.firedAt)}
                </span>
              </div>
            ))}
          </div>
        </Panel>
      ) : null}

      {/* Checks — down checks and expiring certificates, so a black-box
          monitoring incident is visible here without a separate trip to
          /checks. Only rendered once at least one check exists. */}
      {checks.length > 0 && (
        <Panel className="mb-8">
          <PanelHeader
            title="Checks"
            actions={
              <Link
                to="/checks"
                className="text-sm font-medium text-[var(--color-signal)] hover:underline"
              >
                View all
              </Link>
            }
          />
          {problemChecks.length === 0 ? (
            <p className="px-5 py-4 text-sm text-[var(--color-ink-faint)]">
              All checks are healthy.
            </p>
          ) : (
            <div className="divide-y divide-[var(--color-line-soft)]">
              {problemChecks.slice(0, 5).map((c) => (
                <Link
                  key={c.id}
                  to="/checks"
                  className="flex items-center gap-3 px-5 py-3 transition-colors hover:bg-[var(--color-panel-raised)]"
                >
                  <StatusIndicator status={checkStatus(c)} dotOnly />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-[var(--color-ink)]">{c.name}</p>
                    <p className="truncate text-xs text-[var(--color-ink-faint)]">
                      {c.status === "down" ? "Down" : "Certificate expiring soon"} · {c.target}
                    </p>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </Panel>
      )}

      {/* Tag filter — scopes both the FleetStrip hero above and the grid
          below to agents carrying any of the selected tags. */}
      {!loading && !error && agents.length > 0 && (
        <TagFilterBar tags={allTags} selected={selectedTags} onToggle={toggleTag} />
      )}

      {/* Agent grid */}
      {!loading && !error && agents.length === 0 ? (
        <Panel>
          <EmptyState
            icon={Server}
            title="No agents connected"
            description="Add an agent to start monitoring your servers. Go to Agents to generate an install command."
          />
        </Panel>
      ) : !loading && !error && filteredAgents.length === 0 ? (
        <Panel>
          <EmptyState
            icon={Server}
            title="No agents match the selected tags"
            description="Clear a tag filter above to see more servers."
          />
        </Panel>
      ) : !loading && !error ? (
        <div>
          <h3 className="mb-3 text-sm font-medium text-[var(--color-ink-muted)]">
            Servers <span className="tabular-nums">({filteredAgents.length})</span>
          </h3>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {filteredAgents.map((agent) => (
              <ServerCard
                key={agent.id}
                agent={agent}
                status={statusByAgentId.get(agent.id) ?? "offline"}
                metrics={metricsSummary[agent.id]}
              />
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
