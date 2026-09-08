import { useEffect, useState } from "react";
import { loadSettingsMap, asString } from "@/lib/settings";
import { fetchLatestVersion } from "@/lib/agentUpdates";

export interface AgentUpdateInfo {
  /** The admin-set "agent_target_version" setting, or "" when unset. */
  targetVersion: string;
  /** The cached latest published release version, or "" if unknown/offline. */
  latestVersion: string;
  latestPublishedAt: string;
  /** targetVersion when set, otherwise latestVersion — the version every
   *  "Update available" comparison in this feature actually compares
   *  against (see lib/agentUpdates.updateAvailable). */
  compareVersion: string;
  loading: boolean;
}

/**
 * Loads the two pieces of context the Agents page and a host detail page
 * both need to decide "is this agent behind": the admin-set target version
 * (a plain "settings" collection read) and the cached latest GitHub
 * release (GET /api/custom/agents/latest-version). Fetched on mount by
 * each caller rather than through a shared AppShell-owned store (DESIGN.md
 * §10) — unlike agents/alerts/silences/checks, this is a small, already
 * hub-side-cached (1h) read with at most two call sites, so the modest
 * duplicate fetch when navigating between the Agents page and a host
 * detail page was judged not worth a dedicated store for this feature.
 */
export function useAgentUpdateInfo(): AgentUpdateInfo {
  const [targetVersion, setTargetVersion] = useState("");
  const [latestVersion, setLatestVersion] = useState("");
  const [latestPublishedAt, setLatestPublishedAt] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      const [settings, latest] = await Promise.all([
        loadSettingsMap().catch((): Record<string, unknown> => ({})),
        fetchLatestVersion().catch(() => ({ version: "", publishedAt: "", cached: false })),
      ]);
      if (cancelled) return;
      setTargetVersion(asString(settings.agent_target_version, ""));
      setLatestVersion(latest.version);
      setLatestPublishedAt(latest.publishedAt);
      setLoading(false);
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  return {
    targetVersion,
    latestVersion,
    latestPublishedAt,
    compareVersion: targetVersion || latestVersion,
    loading,
  };
}
