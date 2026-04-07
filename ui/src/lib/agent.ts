import type { Agent } from "@/types";

/** Threshold in milliseconds — if last_seen is older than this, agent is considered offline */
const OFFLINE_THRESHOLD_MS = 90_000;

/**
 * Derive the real-time online status of an agent based on last_seen.
 * The DB field `status` can lag behind due to SSH tunnel reconnects or
 * race conditions between removeAgent and the next heartbeat write.
 * Using last_seen is the source of truth.
 */
export function isAgentOnline(agent: Agent): boolean {
  if (!agent.last_seen) return false;
  const lastSeen = new Date(agent.last_seen).getTime();
  return Date.now() - lastSeen < OFFLINE_THRESHOLD_MS;
}

export function agentStatus(agent: Agent): "online" | "offline" {
  return isAgentOnline(agent) ? "online" : "offline";
}
