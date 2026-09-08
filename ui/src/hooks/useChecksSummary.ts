import { useCallback, useEffect, useState } from "react";
import type { CheckSummary } from "@/types";
import { apiFetch } from "@/lib/api";

interface UseChecksSummaryResult {
  summary: CheckSummary[];
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

/**
 * Polls GET /api/custom/checks/summary — the live status/latency/uptime for
 * every check — every 10s (matching Dashboard's own metrics-summary poll
 * interval). This is a page-local fetch, not a shared store: unlike
 * agents/alerts, only the Checks page and the Dashboard's "Checks" panel
 * read it, and each polls independently the same way Dashboard already
 * polls /api/custom/dashboard on top of the shared agentStore. See
 * checksStore.ts and DESIGN.md §10.
 */
export function useChecksSummary(): UseChecksSummaryResult {
  const [summary, setSummary] = useState<CheckSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchSummary = useCallback(async () => {
    try {
      const response = await apiFetch("/api/custom/checks/summary");
      if (!response.ok) {
        throw new Error(`Request failed with status ${response.status}`);
      }
      const data = (await response.json()) as { checks: CheckSummary[] };
      setSummary(data.checks ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load check status");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchSummary();
    const interval = setInterval(fetchSummary, 10_000);
    return () => clearInterval(interval);
  }, [fetchSummary]);

  return { summary, loading, error, refetch: fetchSummary };
}
