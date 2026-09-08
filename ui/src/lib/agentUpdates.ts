import type { Agent, AgentUpdateStatus } from "@/types";
import { apiFetch } from "@/lib/api";

/** The stages update_status passes through while a hub-initiated
 *  self-update (F9) is actively running on an agent. */
const IN_PROGRESS_STATUSES: ReadonlySet<AgentUpdateStatus> = new Set([
  "started",
  "downloading",
  "verifying",
  "installing",
  "restarting",
]);

export function isUpdateInProgress(status: AgentUpdateStatus | undefined): boolean {
  return !!status && IN_PROGRESS_STATUSES.has(status);
}

/** Human label for the live status chip while an update is running or has
 *  just failed or needs a restart. Returns "" for every other status
 *  (idle/done/empty), which callers treat as "render nothing". */
export function updateStatusLabel(status: AgentUpdateStatus | undefined): string {
  switch (status) {
    case "started":
      return "Starting…";
    case "downloading":
      return "Downloading…";
    case "verifying":
      return "Verifying…";
    case "installing":
      return "Installing…";
    case "restarting":
      return "Restarting…";
    case "failed":
      return "Update failed";
    case "restart_required":
      return "Restart required";
    default:
      return "";
  }
}

/**
 * Compares two "X.Y.Z" (optionally "vX.Y.Z") version strings and reports
 * whether `current` is strictly older than `target`. Falls back to a plain
 * string inequality when either side doesn't parse as three numeric
 * parts, so an unparsable version still surfaces as "different" rather
 * than silently never flagging an update.
 */
export function isVersionOlder(current: string, target: string): boolean {
  if (!current || !target) return false;

  const parse = (v: string): number[] =>
    v
      .replace(/^v/, "")
      .split(".")
      .map((part) => parseInt(part, 10));

  const a = parse(current);
  const b = parse(target);
  const valid = (parts: number[]) => parts.length === 3 && parts.every((n) => !Number.isNaN(n));

  if (!valid(a) || !valid(b)) {
    return current !== target;
  }

  for (let i = 0; i < 3; i++) {
    const aPart = a[i] ?? 0;
    const bPart = b[i] ?? 0;
    if (aPart !== bPart) return aPart < bPart;
  }
  return false;
}

/**
 * Decides whether the "Update available" badge should render for an
 * agent, comparing its reported version against compareVersion — the
 * caller (useAgentUpdateInfo) already resolves that to the admin-set
 * "agent_target_version" setting when set, else the cached latest GitHub
 * release, since an operator's explicit target takes precedence over the
 * merely-latest upstream release.
 */
export function updateAvailable(agent: Pick<Agent, "version">, compareVersion: string): boolean {
  return isVersionOlder(agent.version, compareVersion);
}

export interface LatestVersionInfo {
  version: string;
  publishedAt: string;
  cached: boolean;
  error?: string;
}

/** Fetches GET /api/custom/agents/latest-version. Never throws on an
 *  upstream failure the hub itself already downgraded to a soft
 *  `{"error": "..."}` 200 — only a transport-level failure (network down,
 *  non-2xx from the hub itself) propagates as a rejected promise. */
export async function fetchLatestVersion(): Promise<LatestVersionInfo> {
  const res = await apiFetch("/api/custom/agents/latest-version");
  if (!res.ok) {
    throw new Error(`latest-version request failed with status ${res.status}`);
  }
  const data = (await res.json()) as {
    version?: string;
    published_at?: string;
    cached?: boolean;
    error?: string;
  };
  return {
    version: data.version ?? "",
    publishedAt: data.published_at ?? "",
    cached: data.cached ?? false,
    error: data.error,
  };
}
