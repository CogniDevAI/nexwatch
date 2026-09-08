import { useMemo } from "react";
import { useSilencesStore } from "@/stores/silencesStore";
import type { Silence } from "@/types";

export type SilenceBucket = "active" | "scheduled" | "expired";

/** Which bucket a silence falls into right now — active covers
 *  `[starts_at, ends_at)`, scheduled is before it starts, expired is after
 *  it ends. See README "Silences (maintenance windows)". */
export function silenceBucket(silence: Silence, now: number = Date.now()): SilenceBucket {
  const start = new Date(silence.starts_at).getTime();
  const end = new Date(silence.ends_at).getTime();
  if (now < start) return "scheduled";
  if (now >= end) return "expired";
  return "active";
}

interface UseSilencesResult {
  silences: Silence[];
  loading: boolean;
  error: string | null;
  refetch: () => void;
}

/**
 * Pure read over the shared silences store — fetching and the realtime
 * subscription live in `silencesStore`, owned once by AppShell (see
 * DESIGN.md §10), so calling this hook never triggers extra requests.
 */
export function useSilences(): UseSilencesResult {
  const silences = useSilencesStore((s) => s.silences);
  const loading = useSilencesStore((s) => s.loading);
  const error = useSilencesStore((s) => s.error);
  const fetchSilences = useSilencesStore((s) => s.fetchSilences);

  const sorted = useMemo(
    () => [...silences].sort((a, b) => (a.starts_at < b.starts_at ? 1 : -1)),
    [silences],
  );

  return { silences: sorted, loading, error, refetch: () => void fetchSilences() };
}
