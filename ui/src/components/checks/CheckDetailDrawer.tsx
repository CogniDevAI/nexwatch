import { useEffect, useState } from "react";
import type uPlot from "uplot";
import type { Check, CheckResultPoint } from "@/types";
import { apiFetch } from "@/lib/api";
import { formatLatency } from "@/lib/checks";
import { formatDateTime } from "@/lib/time";
import { MetricChart } from "@/components/charts/MetricChart";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { Skeleton } from "@/components/ui/Skeleton";
import { ErrorState } from "@/components/ui/ErrorState";

interface CheckDetailDrawerProps {
  check: Check;
}

type RangeOption = "24h" | "7d";

/** Expandable row content for one check: a latency chart and its most
 *  recent results, for the requested lookback window. */
export function CheckDetailDrawer({ check }: CheckDetailDrawerProps) {
  const [range, setRange] = useState<RangeOption>("24h");
  const [points, setPoints] = useState<CheckResultPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    const load = async () => {
      try {
        const res = await apiFetch(`/api/custom/checks/${check.id}/results?range=${range}`);
        if (!res.ok) throw new Error(`Request failed with status ${res.status}`);
        const data = (await res.json()) as { points: CheckResultPoint[] };
        if (!cancelled) setPoints(data.points ?? []);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load check history");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();

    return () => {
      cancelled = true;
    };
  }, [check.id, range]);

  const chartData: uPlot.AlignedData = [
    points.map((p) => p.timestamp),
    points.map((p) => p.latency_ms),
  ];

  return (
    <div className="space-y-4 border-t border-[var(--color-line)] bg-[var(--color-void)]/40 p-4">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-semibold text-[var(--color-ink)]">Latency history</h4>
        <div
          role="radiogroup"
          aria-label="History range"
          className="inline-flex gap-0.5 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] p-1"
        >
          {(["24h", "7d"] as const).map((r) => (
            <button
              key={r}
              type="button"
              role="radio"
              aria-checked={range === r}
              onClick={() => setRange(r)}
              className={`rounded-[var(--radius-chip)] px-3 py-1 text-xs font-medium transition-colors ${
                range === r
                  ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                  : "text-[var(--color-ink-muted)] hover:bg-[var(--color-panel-raised)] hover:text-[var(--color-ink)]"
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <Skeleton className="h-48 w-full" />
      ) : error ? (
        <ErrorState title="Couldn't load history" description={error} />
      ) : points.length === 0 ? (
        <p className="py-6 text-center text-sm text-[var(--color-ink-faint)]">
          No results recorded yet for this window.
        </p>
      ) : (
        <>
          <MetricChart
            title="Latency"
            data={chartData}
            unit="ms"
            seriesLabels={["Latency"]}
            height={180}
          />

          <div>
            <h4 className="mb-2 text-sm font-semibold text-[var(--color-ink)]">Recent results</h4>
            <div className="max-h-64 overflow-y-auto rounded-[var(--radius-control)] border border-[var(--color-line)]">
              <table className="w-full text-xs">
                <tbody className="divide-y divide-[var(--color-line-soft)]">
                  {points
                    .slice(-20)
                    .reverse()
                    .map((p, i) => (
                      <tr key={`${p.timestamp}-${i}`}>
                        <td className="px-3 py-1.5">
                          <StatusIndicator status={p.status === "up" ? "ok" : "critical"} dotOnly />
                        </td>
                        <td className="px-3 py-1.5 text-[var(--color-ink-muted)]">
                          {formatDateTime(new Date(p.timestamp * 1000).toISOString())}
                        </td>
                        <td className="px-3 py-1.5 text-right font-mono text-[var(--color-ink)] tabular-nums">
                          {formatLatency(p.latency_ms)}
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
