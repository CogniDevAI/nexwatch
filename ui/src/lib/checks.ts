import type { CheckSummary } from "@/types";
import type { Status } from "@/components/ui/status";

/**
 * Maps a check's summary to the shared ok/warning/critical/offline status
 * taxonomy every other page uses (see DESIGN.md §1): "down" is critical, a
 * certificate expiring soon is warning even while the check itself is up,
 * and a check that has never completed a probe yet (no `status`) reads as
 * offline rather than a false "ok".
 */
export function checkStatus(check: CheckSummary): Status {
  if (!check.status) return "offline";
  if (check.status === "down") return "critical";
  if (check.cert_expiring_soon) return "warning";
  return "ok";
}

export interface ChecksSummaryCounts {
  up: number;
  down: number;
  expiringSoon: number;
}

/** Counts for the Checks page's summary strip and the Dashboard's "Checks"
 *  panel: "N up / M down / K with certificates expiring soon". A check
 *  that has never run yet counts as neither up nor down. */
export function summarizeChecks(checks: CheckSummary[]): ChecksSummaryCounts {
  let up = 0;
  let down = 0;
  let expiringSoon = 0;

  for (const check of checks) {
    if (check.status === "up") up++;
    else if (check.status === "down") down++;
    if (check.cert_expiring_soon) expiringSoon++;
  }

  return { up, down, expiringSoon };
}

/** Compact latency label: "42 ms" or "1.2 s" for anything at or above 1000ms. */
export function formatLatency(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)} s`;
  return `${Math.round(ms)} ms`;
}

/** Compact uptime percentage label: "99.95%". Callers should show an em
 *  dash instead of calling this when the check has never run (no
 *  `last_checked_at`) — 0% is otherwise indistinguishable from "no data
 *  yet" once rounded to a percentage. */
export function formatUptime(percent: number): string {
  return `${percent.toFixed(2)}%`;
}
