import { useState, useEffect, useCallback } from "react";
import type { Alert, Agent, AlertRule } from "@/types";
import pb from "@/lib/pocketbase";

type StatusFilter = "all" | "firing" | "resolved";
type SeverityFilter = "all" | "warning" | "critical";

export function AlertHistory() {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [agents, setAgents] = useState<Record<string, Agent>>({});
  const [rules, setRules] = useState<Record<string, AlertRule>>({});
  const [loading, setLoading] = useState(true);

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>("all");
  const [agentFilter, setAgentFilter] = useState("");

  const fetchAlerts = useCallback(async () => {
    setLoading(true);
    try {
      // Build filter.
      const filterParts: string[] = [];
      if (statusFilter !== "all") {
        filterParts.push(`status = '${statusFilter}'`);
      }
      if (agentFilter) {
        filterParts.push(`agent_id = '${agentFilter}'`);
      }

      const filter = filterParts.length > 0 ? filterParts.join(" && ") : "";

      const records = await pb.collection("alerts").getFullList<Alert>({
        sort: "-fired_at",
        filter: filter || undefined,
      });

      setAlerts(records);
    } catch {
      // Handle silently.
    } finally {
      setLoading(false);
    }
  }, [statusFilter, agentFilter]);

  // Load agents and rules for display names.
  useEffect(() => {
    const loadLookups = async () => {
      try {
        const [agentRecords, ruleRecords] = await Promise.all([
          pb.collection("agents").getFullList<Agent>({ sort: "hostname" }),
          pb.collection("alert_rules").getFullList<AlertRule>({ sort: "name" }),
        ]);

        const agentMap: Record<string, Agent> = {};
        for (const a of agentRecords) {
          agentMap[a.id] = a;
        }
        setAgents(agentMap);

        const ruleMap: Record<string, AlertRule> = {};
        for (const r of ruleRecords) {
          ruleMap[r.id] = r;
        }
        setRules(ruleMap);
      } catch {
        // Handle silently.
      }
    };
    loadLookups();
  }, []);

  useEffect(() => {
    fetchAlerts();
  }, [fetchAlerts]);

  // Real-time subscription for live updates.
  useEffect(() => {
    const unsubPromise = pb
      .collection("alerts")
      .subscribe<Alert>("*", (event) => {
        switch (event.action) {
          case "create":
            setAlerts((prev) => [event.record, ...prev]);
            break;
          case "update":
            setAlerts((prev) =>
              prev.map((a) =>
                a.id === event.record.id ? event.record : a,
              ),
            );
            break;
          case "delete":
            setAlerts((prev) =>
              prev.filter((a) => a.id !== event.record.id),
            );
            break;
        }
      });

    return () => {
      unsubPromise.then((unsub) => unsub());
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

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "—";
    try {
      return new Date(dateStr).toLocaleString();
    } catch {
      return dateStr;
    }
  };

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Alert History</h2>

      {/* Filters */}
      <div className="flex flex-wrap gap-3 mb-4">
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          className="px-3 py-2 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
        >
          <option value="all">All Statuses</option>
          <option value="firing">Firing</option>
          <option value="resolved">Resolved</option>
        </select>

        <select
          value={severityFilter}
          onChange={(e) =>
            setSeverityFilter(e.target.value as SeverityFilter)
          }
          className="px-3 py-2 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
        >
          <option value="all">All Severities</option>
          <option value="warning">Warning</option>
          <option value="critical">Critical</option>
        </select>

        <select
          value={agentFilter}
          onChange={(e) => setAgentFilter(e.target.value)}
          className="px-3 py-2 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
        >
          <option value="">All Agents</option>
          {agentList.map((a) => (
            <option key={a.id} value={a.id}>
              {a.hostname || a.id}
            </option>
          ))}
        </select>
      </div>

      {/* Alerts Table */}
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        {loading ? (
          <div className="p-6 text-center">
            <p className="text-sm text-[var(--color-text-muted)]">
              Loading alert history...
            </p>
          </div>
        ) : filteredAlerts.length === 0 ? (
          <div className="p-6">
            <p className="text-sm text-[var(--color-text-secondary)]">
              No alerts have been triggered yet.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-muted)]">
                  <th className="px-6 py-3 text-left font-medium">Status</th>
                  <th className="px-6 py-3 text-left font-medium">Agent</th>
                  <th className="px-6 py-3 text-left font-medium">Rule</th>
                  <th className="px-6 py-3 text-left font-medium">Value</th>
                  <th className="px-6 py-3 text-left font-medium">Message</th>
                  <th className="px-6 py-3 text-left font-medium">
                    Fired At
                  </th>
                  <th className="px-6 py-3 text-left font-medium">
                    Resolved At
                  </th>
                </tr>
              </thead>
              <tbody>
                {filteredAlerts.map((alert) => {
                  const agent = agents[alert.agent_id];
                  const rule = rules[alert.rule_id];

                  return (
                    <tr
                      key={alert.id}
                      className="border-b border-[var(--color-border-muted)] hover:bg-[var(--color-bg-elevated)]/50"
                    >
                      <td className="px-6 py-3">
                        <span
                          className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${
                            alert.status === "firing"
                              ? "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"
                              : "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]"
                          }`}
                        >
                          {alert.status}
                        </span>
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-primary)]">
                        {agent?.hostname ?? alert.agent_id}
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-secondary)]">
                        {rule?.name ?? alert.rule_id}
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-secondary)] font-mono">
                        {alert.value != null
                          ? Number(alert.value).toFixed(1)
                          : "—"}
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-secondary)] max-w-xs truncate">
                        {alert.message}
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-muted)] whitespace-nowrap">
                        {formatDate(alert.fired_at)}
                      </td>
                      <td className="px-6 py-3 text-[var(--color-text-muted)] whitespace-nowrap">
                        {formatDate(alert.resolved_at)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
