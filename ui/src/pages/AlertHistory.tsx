import { useState, useEffect, useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import { BellRing } from "lucide-react";
import type { Alert, AlertRule } from "@/types";
import pb from "@/lib/pocketbase";
import { formatDateTime } from "@/lib/time";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Select } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { AckControl } from "@/components/alerts/AckControl";
import { SilencedBadge, EscalatedBadge } from "@/components/alerts/AlertBadges";
import { usePageTitle } from "@/hooks/usePageTitle";

type StatusFilter = "all" | "firing" | "resolved";
type SeverityFilter = "all" | "warning" | "critical";
type TriStateFilter = "all" | "yes" | "no";

export function AlertHistory() {
  usePageTitle("Alert history");

  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [rules, setRules] = useState<Record<string, AlertRule>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>("all");
  const [agentFilter, setAgentFilter] = useState("");
  const [ackFilter, setAckFilter] = useState<TriStateFilter>("all");
  const [silencedFilter, setSilencedFilter] = useState<TriStateFilter>("all");
  const canManage = useAuthStore((s) => s.hasRole("operator"));

  const fetchAlerts = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // Build filter.
      const filterParts: string[] = [];
      if (statusFilter !== "all") {
        filterParts.push(`status = '${statusFilter}'`);
      }
      if (agentFilter) {
        filterParts.push(`agent_id = '${agentFilter}'`);
      }
      if (ackFilter !== "all") {
        filterParts.push(ackFilter === "yes" ? "acknowledged_at != ''" : "acknowledged_at = ''");
      }
      if (silencedFilter !== "all") {
        filterParts.push(`silenced = ${silencedFilter === "yes" ? "true" : "false"}`);
      }

      const filter = filterParts.length > 0 ? filterParts.join(" && ") : "";

      const records = await pb.collection("alerts").getFullList<Alert>({
        sort: "-fired_at",
        filter: filter || undefined,
      });

      setAlerts(records);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load alert history");
    } finally {
      setLoading(false);
    }
  }, [statusFilter, agentFilter, ackFilter, silencedFilter]);

  // Agents/checks are read from the shared, realtime-synced stores AppShell
  // already fetches once per session (see DESIGN.md §10) instead of a
  // page-local getFullList of either collection just for name lookups.
  // alert_rules has no equivalent shared store yet, so it stays a page-local
  // fetch here (matching Alerts.tsx, which owns alert_rules CRUD).
  const storeAgents = useAgentStore((s) => s.agents);
  const storeChecks = useChecksStore((s) => s.checks);
  const agents = useMemo(
    () => Object.fromEntries(storeAgents.map((a) => [a.id, a])),
    [storeAgents],
  );
  const checks = useMemo(
    () => Object.fromEntries(storeChecks.map((c) => [c.id, c])),
    [storeChecks],
  );

  useEffect(() => {
    const loadRules = async () => {
      try {
        const ruleRecords = await pb.collection("alert_rules").getFullList<AlertRule>({
          sort: "name",
        });
        const ruleMap: Record<string, AlertRule> = {};
        for (const r of ruleRecords) {
          ruleMap[r.id] = r;
        }
        setRules(ruleMap);
      } catch {
        // Handle silently — rule names fall back to raw ids.
      }
    };
    void loadRules();
  }, []);

  useEffect(() => {
    void fetchAlerts();
  }, [fetchAlerts]);

  // Real-time subscription for live updates.
  useEffect(() => {
    const unsubPromise = pb.collection("alerts").subscribe<Alert>("*", (event) => {
      switch (event.action) {
        case "create":
          setAlerts((prev) => [event.record, ...prev]);
          break;
        case "update":
          setAlerts((prev) => prev.map((a) => (a.id === event.record.id ? event.record : a)));
          break;
        case "delete":
          setAlerts((prev) => prev.filter((a) => a.id !== event.record.id));
          break;
      }
    });

    return () => {
      void unsubPromise.then((unsub) => unsub());
    };
  }, []);

  // Client-side severity filter (requires rule lookup).
  const filteredAlerts = alerts.filter((alert) => {
    if (severityFilter !== "all") {
      const rule = rules[alert.rule_id];
      if (rule && rule.severity !== severityFilter) {
        return false;
      }
    }
    return true;
  });

  const agentList = Object.values(agents);

  return (
    <div>
      <PageHeader title="Alert history" />

      {/* Filters — one row, wraps on mobile, with a result count alongside */}
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          aria-label="Filter by status"
          className="w-auto"
        >
          <option value="all">All statuses</option>
          <option value="firing">Firing</option>
          <option value="resolved">Resolved</option>
        </Select>

        <Select
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value as SeverityFilter)}
          aria-label="Filter by severity"
          className="w-auto"
        >
          <option value="all">All severities</option>
          <option value="warning">Warning</option>
          <option value="critical">Critical</option>
        </Select>

        <Select
          value={agentFilter}
          onChange={(e) => setAgentFilter(e.target.value)}
          aria-label="Filter by agent"
          className="w-auto"
        >
          <option value="">All agents</option>
          {agentList.map((a) => (
            <option key={a.id} value={a.id}>
              {a.hostname || a.id}
            </option>
          ))}
        </Select>

        <Select
          value={ackFilter}
          onChange={(e) => setAckFilter(e.target.value as TriStateFilter)}
          aria-label="Filter by acknowledged"
          className="w-auto"
        >
          <option value="all">Acknowledged: any</option>
          <option value="yes">Acknowledged: yes</option>
          <option value="no">Acknowledged: no</option>
        </Select>

        <Select
          value={silencedFilter}
          onChange={(e) => setSilencedFilter(e.target.value as TriStateFilter)}
          aria-label="Filter by silenced"
          className="w-auto"
        >
          <option value="all">Silenced: any</option>
          <option value="yes">Silenced: yes</option>
          <option value="no">Silenced: no</option>
        </Select>

        {!loading && !error && (
          <span className="text-sm text-[var(--color-ink-faint)]">
            {filteredAlerts.length} {filteredAlerts.length === 1 ? "result" : "results"}
          </span>
        )}
      </div>

      {/* Alerts Table */}
      <Panel>
        {loading ? (
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load alert history"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={() => void fetchAlerts()}>
                Try again
              </Button>
            }
          />
        ) : filteredAlerts.length === 0 ? (
          <EmptyState icon={BellRing} title="No alerts have been triggered yet" />
        ) : (
          <>
            {/* Desktop table. Rule gets a minimum width so a normal-length
                summary ("CPU above 90% (5m)") wraps to at most two lines
                instead of collapsing to a sliver and wrapping to four;
                Message is the one column allowed to shrink toward its
                truncated ellipsis first, since it's the least load-bearing
                fact once Rule + Agent + Status already identify the row. */}
            <div className="hidden md:block">
              <Table>
                <thead>
                  <tr className="border-b border-[var(--color-line)]">
                    <Th>Status</Th>
                    <Th>Agent</Th>
                    <Th className="min-w-[11rem]">Rule</Th>
                    <Th align="right">Value</Th>
                    <Th>Message</Th>
                    <Th>Flags</Th>
                    <Th>Acknowledged</Th>
                    <Th>Fired at</Th>
                    <Th>Resolved</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--color-line-soft)]">
                  {filteredAlerts.map((alert, idx) => {
                    const agent = agents[alert.agent_id];
                    const check = checks[alert.check_id];
                    const rule = rules[alert.rule_id];

                    return (
                      <tr key={alert.id} className={rowClass(idx)}>
                        <Td>
                          <StatusIndicator
                            status={alert.status === "firing" ? "critical" : "ok"}
                            label={alert.status === "firing" ? "Firing" : "Resolved"}
                          />
                        </Td>
                        <Td className="font-medium whitespace-nowrap">
                          {alert.check_id ? (
                            <Link
                              to="/checks"
                              className="text-[var(--color-signal)] hover:underline"
                              title={check?.target}
                            >
                              {check?.name ?? alert.check_id}
                            </Link>
                          ) : (
                            (agent?.hostname ?? alert.agent_id)
                          )}
                        </Td>
                        <Td className="min-w-[11rem] text-[var(--color-ink-muted)]">
                          {rule?.name ?? alert.rule_id}
                        </Td>
                        <Td
                          align="right"
                          className="font-mono text-[var(--color-ink-muted)] tabular-nums"
                        >
                          {alert.value != null ? Number(alert.value).toFixed(1) : "—"}
                        </Td>
                        <Td
                          className="max-w-xs truncate text-[var(--color-ink-muted)]"
                          title={alert.message}
                        >
                          {alert.message}
                        </Td>
                        <Td>
                          <div className="flex items-center gap-1.5">
                            {alert.silenced && <SilencedBadge />}
                            {alert.escalated_at && <EscalatedBadge />}
                            {!alert.silenced && !alert.escalated_at && (
                              <span className="text-[var(--color-ink-faint)]">—</span>
                            )}
                          </div>
                        </Td>
                        <Td className="whitespace-nowrap">
                          {alert.status === "firing" ? (
                            <AckControl
                              alertId={alert.id}
                              acknowledgedAt={alert.acknowledged_at}
                              acknowledgedBy={alert.acknowledged_by}
                              canManage={canManage}
                            />
                          ) : alert.acknowledged_at ? (
                            <span
                              className="text-xs text-[var(--color-ink-faint)]"
                              title={alert.acknowledged_by}
                            >
                              Yes — {alert.acknowledged_by || "someone"}
                            </span>
                          ) : (
                            <span className="text-xs text-[var(--color-ink-faint)]">—</span>
                          )}
                        </Td>
                        <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                          {formatDateTime(alert.fired_at)}
                        </Td>
                        <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                          {formatDateTime(alert.resolved_at)}
                        </Td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            </div>

            {/* Mobile cards — same data, stacked, so Acknowledge stays one
                tap away instead of requiring a horizontal scroll to reach
                the table's Acknowledged column. */}
            <div className="space-y-3 p-3 md:hidden">
              {filteredAlerts.map((alert) => {
                const agent = agents[alert.agent_id];
                const check = checks[alert.check_id];
                const rule = rules[alert.rule_id];

                return (
                  <div
                    key={alert.id}
                    className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-raised)]/40 p-4"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <StatusIndicator
                        status={alert.status === "firing" ? "critical" : "ok"}
                        label={alert.status === "firing" ? "Firing" : "Resolved"}
                      />
                      <div className="flex items-center gap-1.5">
                        {alert.silenced && <SilencedBadge />}
                        {alert.escalated_at && <EscalatedBadge />}
                      </div>
                    </div>
                    <p className="mt-2 font-medium text-[var(--color-ink)]">
                      {alert.check_id ? (
                        <Link
                          to="/checks"
                          className="text-[var(--color-signal)] hover:underline"
                          title={check?.target}
                        >
                          {check?.name ?? alert.check_id}
                        </Link>
                      ) : (
                        (agent?.hostname ?? alert.agent_id)
                      )}
                    </p>
                    <p className="text-sm text-[var(--color-ink-muted)]">
                      {rule?.name ?? alert.rule_id}
                    </p>
                    <p className="mt-1 text-sm text-[var(--color-ink-muted)]">{alert.message}</p>
                    <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm">
                      <dt className="text-[var(--color-ink-faint)]">Value</dt>
                      <dd className="text-right font-mono text-[var(--color-ink-muted)] tabular-nums">
                        {alert.value != null ? Number(alert.value).toFixed(1) : "—"}
                      </dd>
                      <dt className="text-[var(--color-ink-faint)]">Fired at</dt>
                      <dd className="text-right text-xs text-[var(--color-ink-muted)]">
                        {formatDateTime(alert.fired_at)}
                      </dd>
                      {alert.status === "resolved" && (
                        <>
                          <dt className="text-[var(--color-ink-faint)]">Resolved</dt>
                          <dd className="text-right text-xs text-[var(--color-ink-muted)]">
                            {formatDateTime(alert.resolved_at)}
                          </dd>
                        </>
                      )}
                    </dl>
                    <div className="mt-3 border-t border-[var(--color-line-soft)] pt-3">
                      {alert.status === "firing" ? (
                        <AckControl
                          alertId={alert.id}
                          acknowledgedAt={alert.acknowledged_at}
                          acknowledgedBy={alert.acknowledged_by}
                          canManage={canManage}
                        />
                      ) : alert.acknowledged_at ? (
                        <span
                          className="text-xs text-[var(--color-ink-faint)]"
                          title={alert.acknowledged_by}
                        >
                          Acknowledged by {alert.acknowledged_by || "someone"}
                        </span>
                      ) : (
                        <span className="text-xs text-[var(--color-ink-faint)]">
                          Not acknowledged
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </Panel>
    </div>
  );
}
