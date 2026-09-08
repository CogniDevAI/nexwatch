import { create } from "zustand";
import type { Check } from "@/types";
import pb, { subscribeToCollection } from "@/lib/pocketbase";

interface ChecksState {
  checks: Check[];
  loading: boolean;
  error: string | null;
  fetchChecks: () => Promise<void>;
  subscribeToChecks: () => () => void;
}

/**
 * Single shared owner of the "checks" collection (raw config: name, type,
 * target, interval, thresholds, tags, ...), fetched and subscribed exactly
 * once by AppShell and read by the Checks page for its edit forms and to
 * react instantly to a create/delete. Live status/latency/uptime come from
 * a separate polled fetch of GET /api/custom/checks/summary (see
 * useChecksSummary) — mirrors how agentStore holds agent identity while
 * Dashboard polls /api/custom/dashboard for live metrics. See DESIGN.md
 * §10.
 */
export const useChecksStore = create<ChecksState>((set, get) => ({
  checks: [],
  loading: false,
  error: null,

  fetchChecks: async () => {
    set({ loading: true, error: null });
    try {
      const records = await pb.collection("checks").getFullList<Check>({ sort: "name" });
      set({ checks: records, loading: false });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : "Failed to load checks",
        loading: false,
      });
    }
  },

  subscribeToChecks: () => {
    return subscribeToCollection<Check>("checks", "*", (event) => {
      const { checks } = get();
      switch (event.action) {
        case "create":
          set({ checks: [...checks, event.record].sort((a, b) => a.name.localeCompare(b.name)) });
          break;
        case "update":
          set({
            checks: checks.map((c) => (c.id === event.record.id ? event.record : c)),
          });
          break;
        case "delete":
          set({ checks: checks.filter((c) => c.id !== event.record.id) });
          break;
      }
    });
  },
}));
