import { useEffect, useState, useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import { Radio, Server, ShieldCheck } from "lucide-react";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import { useAuthStore } from "@/stores/authStore";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import { checkStatus } from "@/lib/checks";
import type { AgentMetricsSummary } from "@/components/dashboard/ServerCard";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, Section, SectionHeader } from "@/components/ui/Panel";
import { FleetStrip } from "@/components/ui/FleetStrip";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import type { Status } from "@/components/ui/status";
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

const statusRank: Record<Status, number> = {
  critical: 0,
  warning: 1,
  offline: 2,
  ok: 3,
};

/** Left status rail on an operator row — the same status the row's glyph
 *  carries, repeated on the left edge so a vertical scan finds the bad row
 *  before any text is read. See DESIGN.md §3. */
const STATUS_RAIL: Record<Status, string> = {
  ok: "border-[var(--color-ok)]",
  warning: "border-[var(--color-warn)]",
  critical: "border-[var(--color-critical)]",
  offline: "border-[var(--color-offline)]",
};

const TONE_CLASS = {
  ok: "text-[var(--color-ok)]",
  warn: "text-[var(--color-warn)]",
  critical: "text-[var(--color-critical)]",
  muted: "text-[var(--color-ink)]",
} as const;

/** One compact contextual reading in the page header's meta line — deliberately
 *  not a KPI card: four equal-weight boxes would claim these numbers matter as
 *  much as the incident list they sit above. See DESIGN.md §3. */
function MetaStat({
  label,
  value,
  detail,
  tone = "muted",
}: {
  label: string;
  value: string;
  detail?: string;
  tone?: keyof typeof TONE_CLASS;
}) {
  return (
    <span className="flex items-baseline gap-2">
      <span className="text-[var(--color-ink-faint)]">{label}</span>
      <span className={`font-mono font-medium tabular-nums ${TONE_CLASS[tone]}`}>{value}</span>
      {detail && <span className="text-[var(--color-ink-faint)]">{detail}</span>}
    </span>
  );
}

/** Resource pressure as a labeled number plus a proportional bar — the bar is
 *  the part a 3 a.m. glance actually reads. Renders a plain "--" when the agent
 *  has not reported that metric; never a zeroed bar, which would read as idle. */
function MetricCell({ label, value }: { label: string; value?: number }) {
  if (value === undefined) {
    return (
      <span className="flex items-center gap-1.5 text-xs">
        <span className="w-8 text-[var(--color-ink-faint)]">{label}</span>
        <span className="font-mono text-[var(--color-ink-faint)]">--</span>
      </span>
    );
  }

  const clamped = Math.min(Math.max(value, 0), 100);
  const tone =
    clamped >= 90
      ? { text: "text-[var(--color-critical)]", bar: "bg-[var(--color-critical)]" }
      : clamped >= 75
        ? { text: "text-[var(--color-warn)]", bar: "bg-[var(--color-warn)]" }
        : { text: "text-[var(--color-ink)]", bar: "bg-[var(--color-signal-muted)]" };

  return (
    <span className="flex items-center gap-1.5 text-xs">
      <span className="w-8 text-[var(--color-ink-faint)]">{label}</span>
      <span className={`w-9 text-right font-mono tabular-nums ${tone.text}`}>
        {clamped.toFixed(0)}%
      </span>
      <span
        className="hidden h-1 min-w-8 flex-1 bg-[var(--color-line)] sm:block"
        aria-hidden="true"
      >
        <span className={`block h-full ${tone.bar}`} style={{ width: `${clamped}%` }} />
      </span>
    </span>
  );
}

export function Dashboard() {
  usePageTitle("Dashboard");

  // Agents/checks are fetched and subscribed once by AppShell — this just
  // reads the shared stores. See DESIGN.md §10.
  const { agents, loading, error, fetchAgents } = useAgentStore();
  const { checks } = useChecksStore();
  const {
    summary: checksSummary,
    loading: checksSummaryLoading,
    error: checksSummaryError,
  } = useChecksSummary();
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

  const sortedFilteredAgents = useMemo(
    () =>
      [...filteredAgents].sort((a, b) => {
        const aStatus = statusByAgentId.get(a.id) ?? "offline";
        const bStatus = statusByAgentId.get(b.id) ?? "offline";
        return (
          statusRank[aStatus] - statusRank[bStatus] ||
          (a.hostname || a.name).localeCompare(b.hostname || b.name)
        );
      }),
    [filteredAgents, statusByAgentId],
  );

  const fetchDashboardSummary = useCallback(async () => {
    try {
      const response = await apiFetch("/api/custom/dashboard");
      if (response.ok) {
        const data = (await response.json()) as DashboardSummary;
        setMetricsSummary(data.agents ?? {});
      }
    } catch {
      // Dashboard API might not be available yet.
    }
  }, []);

  useEffect(() => {
    void fetchDashboardSummary();
    const interval = setInterval(fetchDashboardSummary, 10_000);
    return () => clearInterval(interval);
  }, [fetchDashboardSummary]);

  const onlineCount = fleet.filter((a) => a.status !== "offline").length;
  const offlineCount = fleet.filter((a) => a.status === "offline").length;
  const criticalCount = activeAlerts.filter((a) => a.severity === "critical").length;
  const warningCount = activeAlerts.filter((a) => a.severity === "warning").length;
  const downCheckCount = checksSummary.filter((c) => c.status === "down").length;
  const configuredCheckCount = checksSummary.length || checks.length;
  const incidentCount = activeAlerts.length + problemChecks.length;

  return (
    <div>
      <PageHeader
        title="Operations"
        description="Current fleet posture, alerts, and checks for the NexWatch Hub."
        meta={
          <>
            <MetaStat
              label="Agents"
              value={`${onlineCount}/${agents.length}`}
              detail={offlineCount > 0 ? `online, ${offlineCount} offline` : "online"}
              tone={offlineCount > 0 ? "critical" : "ok"}
            />
            <MetaStat
              label="Active alerts"
              value={`${activeAlerts.length}`}
              detail={
                activeAlerts.length > 0
                  ? `${criticalCount} critical, ${warningCount} warning`
                  : undefined
              }
              tone={criticalCount > 0 ? "critical" : warningCount > 0 ? "warn" : "muted"}
            />
            <MetaStat
              label="Checks"
              value={`${configuredCheckCount}`}
              detail={
                checksSummaryLoading
                  ? "loading status"
                  : downCheckCount > 0
                    ? `configured, ${downCheckCount} down`
                    : "configured, all up"
              }
              tone={downCheckCount > 0 ? "critical" : "muted"}
            />
          </>
        }
      />

      {/* Fleet posture band — full-bleed chrome directly under the page header,
          so the first thing on the page is the fleet itself, not a card. */}
      {(loading || error || agents.length > 0) && (
        <div className="bleed-x mb-8 border-b border-[var(--color-line)] bg-[var(--color-void-lift)] pb-4">
          {loading && agents.length === 0 ? (
            <Skeleton className="h-10 w-full" />
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
              {allTags.length > 0 && (
                <TagFilterBar tags={allTags} selected={selectedTags} onToggle={toggleTag} />
              )}
              <FleetStrip agents={filteredFleet} size="lg" />
            </>
          )}
        </div>
      )}

      <Section aria-labelledby="attention-heading">
        <SectionHeader
          id="attention-heading"
          title="Attention"
          description="Active alerts and failing checks, worst first."
          meta={incidentCount > 0 ? `${incidentCount} open` : "clear"}
        />

        {activeAlertsError ? (
          <ErrorState
            title="Couldn't load active alerts"
            description={activeAlertsError}
            action={
              <Button variant="primary" size="sm" onClick={refetchActiveAlerts}>
                Try again
              </Button>
            }
          />
        ) : activeAlertsLoading && activeAlerts.length === 0 ? (
          <Skeleton className="h-24 w-full" />
        ) : incidentCount === 0 ? (
          <div className="flex items-center gap-3 border-l-2 border-[var(--color-ok)] bg-[var(--color-panel)]/40 px-4 py-4 text-sm text-[var(--color-ink-muted)]">
            <ShieldCheck className="h-4 w-4 text-[var(--color-ok)]" aria-hidden="true" />
            No active alerts or failing checks.
          </div>
        ) : (
          <div className="divide-y divide-[var(--color-line-soft)]">
            {activeAlerts.slice(0, 6).map((alert) => (
              <div
                key={`alert-${alert.id}`}
                className={`grid gap-3 border-l-2 px-4 py-2.5 sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center ${STATUS_RAIL[alert.severity]}`}
              >
                <StatusIndicator status={alert.severity} dotOnly />
                <Link
                  to={alert.checkId ? "/checks" : `/servers/${alert.agentId}`}
                  className="min-w-0 transition-colors hover:text-[var(--color-signal)]"
                >
                  <p className="truncate text-sm font-medium text-[var(--color-ink)]">
                    {alert.ruleName}
                    {alert.count > 1 && (
                      <span className="text-2xs ml-2 rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 font-mono text-[var(--color-ink-faint)] tabular-nums">
                        ×{alert.count}
                      </span>
                    )}
                  </p>
                  <p className="truncate font-mono text-xs text-[var(--color-ink-faint)]">
                    {alert.checkId
                      ? `${alert.checkName}${alert.checkTarget ? ` (${alert.checkTarget})` : ""}`
                      : alert.agentName}
                  </p>
                </Link>
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {alert.silenced && <SilencedBadge />}
                  {alert.escalatedAt && <EscalatedBadge />}
                  <AckControl
                    alertId={alert.id}
                    acknowledgedAt={alert.acknowledgedAt}
                    acknowledgedBy={alert.acknowledgedBy}
                    canManage={canManage}
                  />
                  <span className="font-mono text-xs text-[var(--color-ink-faint)]">
                    {timeSince(alert.firedAt)}
                  </span>
                </div>
              </div>
            ))}

            {problemChecks.slice(0, 6).map((check) => (
              <Link
                key={`check-${check.id}`}
                to="/checks"
                className={`grid gap-3 border-l-2 px-4 py-2.5 transition-colors hover:bg-[var(--color-panel)] sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center ${STATUS_RAIL[checkStatus(check)]}`}
              >
                <StatusIndicator status={checkStatus(check)} dotOnly />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-[var(--color-ink)]">
                    {check.name}
                  </p>
                  <p className="truncate font-mono text-xs text-[var(--color-ink-faint)]">
                    {check.status === "down" ? "Down" : "Certificate expiring soon"} ·{" "}
                    {check.target}
                  </p>
                </div>
                <span className="font-mono text-xs text-[var(--color-ink-faint)] tabular-nums">
                  {check.latency_ms ? `${check.latency_ms.toFixed(0)} ms` : "--"}
                </span>
              </Link>
            ))}
          </div>
        )}
      </Section>

      <div className="grid gap-8 xl:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <Section aria-labelledby="matrix-heading" className="mb-0">
          <SectionHeader
            id="matrix-heading"
            title="Fleet"
            description="Hosts with live resource pressure, unhealthy first."
            meta={`${sortedFilteredAgents.length} hosts`}
          />

          {loading && agents.length === 0 ? (
            <Skeleton className="h-40 w-full" />
          ) : error ? null : agents.length === 0 ? (
            <Panel>
              <EmptyState
                icon={Server}
                title="No agents connected"
                description="Add an agent to start monitoring your servers. Go to Agents to generate an install command."
              />
            </Panel>
          ) : filteredAgents.length === 0 ? (
            <Panel>
              <EmptyState
                icon={Server}
                title="No agents match the selected tags"
                description="Clear a tag filter above to see more servers."
              />
            </Panel>
          ) : (
            <div className="divide-y divide-[var(--color-line-soft)]">
              {sortedFilteredAgents.map((agent) => {
                const metrics = metricsSummary[agent.id];
                const status = statusByAgentId.get(agent.id) ?? "offline";
                return (
                  <Link
                    key={agent.id}
                    to={`/servers/${agent.id}`}
                    className={`grid gap-x-4 gap-y-2 border-l-2 px-4 py-2.5 transition-colors hover:bg-[var(--color-panel)] lg:grid-cols-[auto_minmax(160px,1fr)_minmax(280px,1.4fr)_auto] lg:items-center ${STATUS_RAIL[status]}`}
                  >
                    <StatusIndicator
                      status={status}
                      dotOnly
                      pulse={status === "critical" || status === "offline"}
                    />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-[var(--color-ink)]">
                        {agent.hostname || agent.name || "Pending host"}
                      </p>
                      <p className="truncate font-mono text-xs text-[var(--color-ink-faint)]">
                        {agent.ip || "No IP"}
                      </p>
                    </div>
                    <div className="grid gap-x-4 gap-y-1 sm:grid-cols-3">
                      <MetricCell label="CPU" value={metrics?.cpu} />
                      <MetricCell label="MEM" value={metrics?.memory} />
                      <MetricCell label="DSK" value={metrics?.disk} />
                    </div>
                    <div className="flex items-center justify-between gap-4 lg:justify-end">
                      <span className="truncate text-xs text-[var(--color-ink-muted)]">
                        {agent.os || "Unknown OS"}
                      </span>
                      <span
                        className="font-mono text-xs whitespace-nowrap text-[var(--color-ink-faint)]"
                        title={agent.last_seen}
                      >
                        {timeSince(agent.last_seen)}
                      </span>
                    </div>
                  </Link>
                );
              })}
            </div>
          )}
        </Section>

        <Section aria-labelledby="checks-heading" className="mb-0">
          <SectionHeader
            id="checks-heading"
            title="Checks"
            description="Black-box probes."
            actions={
              <Link
                to="/checks"
                className="flex items-center gap-1.5 text-xs font-medium text-[var(--color-signal)] hover:underline"
              >
                <Radio className="h-3.5 w-3.5" aria-hidden="true" />
                View all
              </Link>
            }
          />
          {checksSummaryError ? (
            <ErrorState title="Couldn't load check status" description={checksSummaryError} />
          ) : checksSummaryLoading && checksSummary.length === 0 ? (
            <Skeleton className="h-28 w-full" />
          ) : configuredCheckCount === 0 ? (
            <p className="px-4 py-4 text-sm text-[var(--color-ink-faint)]">No checks configured.</p>
          ) : (
            <div className="divide-y divide-[var(--color-line-soft)]">
              {checksSummary.slice(0, 8).map((check) => (
                <Link
                  key={check.id}
                  to="/checks"
                  className={`grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-l-2 px-4 py-2.5 text-sm transition-colors hover:bg-[var(--color-panel)] ${STATUS_RAIL[checkStatus(check)]}`}
                >
                  <StatusIndicator status={checkStatus(check)} dotOnly />
                  <div className="min-w-0">
                    <p className="truncate font-medium text-[var(--color-ink)]">{check.name}</p>
                    <p className="truncate font-mono text-xs text-[var(--color-ink-faint)]">
                      {check.target}
                    </p>
                  </div>
                  <div className="text-right">
                    <p className="font-mono text-xs text-[var(--color-ink-muted)] tabular-nums">
                      {check.uptime_24h.toFixed(1)}%
                    </p>
                    <p className="text-2xs text-[var(--color-ink-faint)]">24h uptime</p>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </Section>
      </div>
    </div>
  );
}
