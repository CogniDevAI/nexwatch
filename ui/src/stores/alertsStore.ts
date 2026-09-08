import { create } from "zustand";
import type { Alert, AlertRule } from "@/types";
import pb, { subscribeToCollection } from "@/lib/pocketbase";

interface AlertsState {
  /** Raw firing alerts and their rules — joined against agents at read time
   *  by useActiveAlerts, so this store never needs its own agents fetch. */
  firingAlerts: Alert[];
  rules: Record<string, AlertRule>;
  loading: boolean;
  error: string | null;
  fetchAlerts: () => Promise<void>;
  subscribeToAlerts: () => () => void;
}

/**
 * Single shared owner of the "firing alerts" data — fetched and subscribed
 * exactly once (by AppShell), read by every consumer (SignalRail, Dashboard,
 * Agents) via useActiveAlerts/useFleetHealth. See DESIGN.md §10 (request
 * hoisting) for why this exists as its own store rather than a per-component
 * fetch.
 */
export const useAlertsStore = create<AlertsState>((set, get) => ({
  firingAlerts: [],
  rules: {},
  loading: false,
  error: null,

  fetchAlerts: async () => {
    set({ loading: true, error: null });
    try {
      const [firing, rules] = await Promise.all([
        pb.collection("alerts").getFullList<Alert>({
          filter: "status = 'firing'",
          sort: "-fired_at",
        }),
        pb.collection("alert_rules").getFullList<AlertRule>(),
      ]);

      const ruleMap: Record<string, AlertRule> = {};
      for (const r of rules) ruleMap[r.id] = r;

      set({ firingAlerts: firing, rules: ruleMap, loading: false });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : "Failed to load active alerts",
        loading: false,
      });
    }
  },

  subscribeToAlerts: () => {
    // Any alert create/update/delete can change the firing set — refetch
    // both alerts and rules together rather than hand-patching state, since
    // rule severity can also change independently.
    return subscribeToCollection<Alert>("alerts", "*", () => {
      void get().fetchAlerts();
    });
  },
}));
