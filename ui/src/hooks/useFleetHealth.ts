import { useMemo } from "react";
import { useAgentStore } from "@/stores/agentStore";
import { agentStatus } from "@/lib/agent";
import { useActiveAlerts, type ActiveAlert } from "./useActiveAlerts";
import type { Status } from "@/components/ui/status";

export interface FleetAgent {
  id: string;
  name: string;
  status: Status;
}

interface UseFleetHealthResult {
  fleet: FleetAgent[];
  /** Status lookup by agent id — the "one status source of truth" every
   *  agent-status display (ServerCard, Agents table/cards, host detail
   *  header) reads from instead of deriving its own connectivity-only
   *  status. See DESIGN.md §3. */
  statusByAgentId: Map<string, Status>;
  activeAlerts: ActiveAlert[];
  activeAlertsLoading: boolean;
  activeAlertsError: string | null;
  refetchActiveAlerts: () => void;
}

/**
 * Per-agent fleet status for the Signal Rail / FleetStrip: an agent is
 * "critical" or "warning" when it has a matching firing alert, otherwise
 * "ok"/"offline" from its connectivity. This is what makes the strip answer
 * "which one is unhealthy" rather than just "which one is connected".
 *
 * Pure read — fetching and the realtime subscription live in
 * `agentStore`/`alertsStore`, owned once by AppShell (see DESIGN.md §10), so
 * calling this hook from multiple components never triggers extra requests.
 */
export function useFleetHealth(): UseFleetHealthResult {
  const agents = useAgentStore((s) => s.agents);
  const { alerts, loading, error, refetch } = useActiveAlerts();

  const severityByAgent = useMemo(() => {
    const sev = new Map<string, "critical" | "warning">();
    for (const a of alerts) {
      if (a.severity === "critical") {
        sev.set(a.agentId, "critical");
      } else if (!sev.has(a.agentId)) {
        sev.set(a.agentId, "warning");
      }
    }
    return sev;
  }, [alerts]);

  const fleet = useMemo<FleetAgent[]>(
    () =>
      agents.map((a) => {
        const name = a.hostname || a.name || "Unnamed agent";
        const alertSeverity = severityByAgent.get(a.id);
        const status: Status = alertSeverity ?? (agentStatus(a) === "online" ? "ok" : "offline");
        return { id: a.id, name, status };
      }),
    [agents, severityByAgent],
  );

  const statusByAgentId = useMemo(() => new Map(fleet.map((f) => [f.id, f.status])), [fleet]);

  return {
    fleet,
    statusByAgentId,
    activeAlerts: alerts,
    activeAlertsLoading: loading,
    activeAlertsError: error,
    refetchActiveAlerts: refetch,
  };
}
