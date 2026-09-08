import { create } from "zustand";
import type { Silence } from "@/types";
import pb, { subscribeToCollection } from "@/lib/pocketbase";

interface SilencesState {
  silences: Silence[];
  loading: boolean;
  error: string | null;
  fetchSilences: () => Promise<void>;
  subscribeToSilences: () => () => void;
}

/**
 * Single shared owner of the "silences" collection, fetched and subscribed
 * exactly once (by AppShell) and read by the Silences page via
 * `useSilences`. Mirrors `agentStore`/`alertsStore` — see DESIGN.md §10 —
 * even though only one page reads it today, so a second visit to
 * /alerts/silences never re-issues the initial getFullList, and any future
 * consumer (e.g. a "silences covering this host" count in ServerDetail)
 * gets the data for free instead of adding its own fetch.
 */
export const useSilencesStore = create<SilencesState>((set, get) => ({
  silences: [],
  loading: false,
  error: null,

  fetchSilences: async () => {
    set({ loading: true, error: null });
    try {
      const records = await pb.collection("silences").getFullList<Silence>({
        sort: "-starts_at",
      });
      set({ silences: records, loading: false });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : "Failed to load silences",
        loading: false,
      });
    }
  },

  subscribeToSilences: () => {
    return subscribeToCollection<Silence>("silences", "*", (event) => {
      const { silences } = get();
      switch (event.action) {
        case "create":
          set({ silences: [event.record, ...silences] });
          break;
        case "update":
          set({
            silences: silences.map((s) => (s.id === event.record.id ? event.record : s)),
          });
          break;
        case "delete":
          set({ silences: silences.filter((s) => s.id !== event.record.id) });
          break;
      }
    });
  },
}));
