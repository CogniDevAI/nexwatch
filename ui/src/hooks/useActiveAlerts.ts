import { useMemo } from "react";
import { useAlertsStore } from "@/stores/alertsStore";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";

export interface ActiveAlert {
  id: string;
  ruleId: string;
  ruleName: string;
  /** Empty for a check-based alert (check_down/cert_expiry) — see checkId. */
  agentId: string;
  /** Empty for a check-based alert. */
  agentName: string;
  /** Set instead of agentId for a check_down/cert_expiry alert — see
   *  internal/hub/alerts.isCheckMetricType. Empty for an agent-based alert. */
  checkId: string;
  /** Empty for an agent-based alert. */
  checkName: string;
  /** The check's target (URL/host:port/hostname) — empty for an
   *  agent-based alert or an unresolvable check id. */
  checkTarget: string;
  severity: "warning" | "critical";
  firedAt: string;
  /** Number of raw alert records collapsed into this row — the backend can
   *  currently produce duplicate firing alerts for the same rule+host, so
   *  the UI groups them into one row with a "×N" badge instead of showing
   *  five identical rows. */
  count: number;
  /** Ack/silence/escalation state, taken from the group's most-recently-fired
   *  record (see the `existing`-update branch below) — acknowledging one
   *  duplicate does not retroactively change older records collapsed into
   *  the same row, so this reflects only the representative one. */
  silenced: boolean;
  acknowledgedAt: string;
  acknowledgedBy: string;
  escalatedAt: string;
}

interface UseActiveAlertsResult {
  alerts: ActiveAlert[];
  loading: boolean;
  error: string | null;
  refetch: () => void;
}

/**
 * Firing alerts enriched with their rule (name, severity) and agent
 * (hostname), grouped by rule+host to collapse duplicates. Pure read —
 * fetching and the realtime subscription live in `alertsStore`/`agentStore`,
 * owned once by AppShell (see DESIGN.md §10), so calling this hook from
 * multiple components never triggers extra requests.
 */
export function useActiveAlerts(): UseActiveAlertsResult {
  const firingAlerts = useAlertsStore((s) => s.firingAlerts);
  const rules = useAlertsStore((s) => s.rules);
  const loading = useAlertsStore((s) => s.loading);
  const error = useAlertsStore((s) => s.error);
  const fetchAlerts = useAlertsStore((s) => s.fetchAlerts);
  const agents = useAgentStore((s) => s.agents);
  const checks = useChecksStore((s) => s.checks);

  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const checkById = useMemo(() => new Map(checks.map((c) => [c.id, c])), [checks]);

  const alerts = useMemo<ActiveAlert[]>(() => {
    const groups = new Map<string, ActiveAlert>();

    for (const a of firingAlerts) {
      // A check-based alert (check_down/cert_expiry) has no agent_id at
      // all — group by check_id instead, so distinct breaching checks
      // under the same rule don't collapse into one row (agent_id being
      // empty for every one of them would otherwise group them all under
      // the same "${ruleId}:" key).
      const key = a.agent_id
        ? `agent:${a.rule_id}:${a.agent_id}`
        : `check:${a.rule_id}:${a.check_id}`;
      const existing = groups.get(key);

      if (existing) {
        existing.count += 1;
        // Keep the most recently fired record as the representative
        // timestamp/id/ack-state.
        if (a.fired_at > existing.firedAt) {
          existing.firedAt = a.fired_at;
          existing.id = a.id;
          existing.silenced = a.silenced;
          existing.acknowledgedAt = a.acknowledged_at;
          existing.acknowledgedBy = a.acknowledged_by;
          existing.escalatedAt = a.escalated_at;
        }
        continue;
      }

      const rule = rules[a.rule_id];
      const agent = agentById.get(a.agent_id);
      const check = checkById.get(a.check_id);
      groups.set(key, {
        id: a.id,
        ruleId: a.rule_id,
        ruleName: rule?.name ?? a.rule_id,
        agentId: a.agent_id,
        agentName: a.agent_id ? agent?.hostname || agent?.name || a.agent_id : "",
        checkId: a.check_id,
        checkName: a.check_id ? check?.name || a.check_id : "",
        checkTarget: check?.target ?? "",
        severity: rule?.severity === "critical" ? "critical" : "warning",
        firedAt: a.fired_at,
        count: 1,
        silenced: a.silenced,
        acknowledgedAt: a.acknowledged_at,
        acknowledgedBy: a.acknowledged_by,
        escalatedAt: a.escalated_at,
      });
    }

    return Array.from(groups.values()).sort((a, b) => (a.firedAt < b.firedAt ? 1 : -1));
  }, [firingAlerts, rules, agentById, checkById]);

  return { alerts, loading, error, refetch: () => void fetchAlerts() };
}
