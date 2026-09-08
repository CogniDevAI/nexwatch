import type uPlot from "uplot";

/**
 * Shared uPlot chrome for every time-series chart on the server detail page
 * (Metrics tab, process CPU timeline): a y-scale that never collapses to a
 * single repeated tick label, and tick/legend value formatting that matches.
 * See DESIGN.md §3 ("Charts on server detail").
 */

export const CHART_AXIS_STROKE = "#57647a"; // --color-ink-faint
export const CHART_GRID_STROKE = "#1a2230"; // --color-line-soft
export const CHART_AXIS_FONT = "11px 'IBM Plex Mono', monospace";

/** Formats a single value the same way on the axis and the custom legend. */
export function formatChartValue(value: number | null | undefined, unit: string, span = 0): string {
  if (value === null || value === undefined || Number.isNaN(value)) return "—";
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}G`;
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  if (unit === "%") return span >= 5 ? value.toFixed(0) : value.toFixed(1);
  return value.toFixed(1);
}

/** uPlot y-axis tick formatter — reads the resolved scale span so a flat or
 *  near-flat series never renders the same rounded label five times over. */
export function makeYAxisFormatter(unit: string) {
  return (self: uPlot, ticks: number[]): string[] => {
    const scale = self.scales.y;
    const span = (scale?.max ?? 0) - (scale?.min ?? 0);
    return ticks.map((v) => formatChartValue(v, unit, span));
  };
}

/** uPlot y-scale range — enforces a minimum span so a flat series doesn't
 *  collapse the axis to a single repeated value, and clamps percentages to
 *  [0, 100]. */
export function makeYRange(unit: string): uPlot.Range.Function {
  return (_self, initMin, initMax) => {
    // uPlot can pass NaN for an empty series — fall back to a sensible default range.
    if (Number.isNaN(initMin) || Number.isNaN(initMax)) {
      return unit === "%" ? [0, 100] : [0, 1];
    }

    let lo = initMin;
    let hi = initMax;
    const minSpan = unit === "%" ? 5 : Math.max(Math.abs(hi) * 0.1, 1);

    if (hi - lo < minSpan) {
      const mid = (lo + hi) / 2;
      lo = mid - minSpan / 2;
      hi = mid + minSpan / 2;
    }

    if (unit === "%") {
      lo = Math.max(0, lo);
      hi = Math.min(100, Math.max(hi, lo + minSpan));
    } else {
      lo = Math.max(0, lo);
    }

    return [lo, hi];
  };
}
