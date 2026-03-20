import { create } from "zustand";
import type { Agent } from "@/types";
import pb from "@/lib/pocketbase";

interface AgentState {
  agents: Agent[];
  loading: boolean;
  error: string | null;
  fetchAgents: () => Promise<void>;
  subscribeToAgents: () => () => void;
}

export const useAgentStore = create<AgentState>((set, get) => ({
  agents: [],
  loading: false,
  error: null,

  fetchAgents: async () => {
    set({ loading: true, error: null });
    try {
      const records = await pb.collection("agents").getFullList<Agent>({
        sort: "-last_seen",
      });
      set({ agents: records, loading: false });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : "Failed to fetch agents",
        loading: false,
      });
    }
  },

  subscribeToAgents: () => {
    const unsubscribePromise = pb
      .collection("agents")
      .subscribe<Agent>("*", (event) => {
        const { agents } = get();

        switch (event.action) {
          case "create":
            set({ agents: [event.record, ...agents] });
            break;
          case "update":
            set({
              agents: agents.map((a) =>
                a.id === event.record.id ? event.record : a,
              ),
            });
            break;
          case "delete":
            set({
              agents: agents.filter((a) => a.id !== event.record.id),
            });
            break;
        }
      });

    // Return cleanup function
    return () => {
      unsubscribePromise.then((unsub) => unsub());
    };
  },
}));
