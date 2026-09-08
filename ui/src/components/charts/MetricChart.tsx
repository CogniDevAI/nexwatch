import { useEffect, useRef, useCallback, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { Panel } from "@/components/ui/Panel";
import {
  CHART_AXIS_FONT,
  CHART_AXIS_STROKE,
  CHART_GRID_STROKE,
  formatChartValue,
  makeYAxisFormatter,
  makeYRange,
} from "@/lib/uplotHelpers";

interface MetricChartProps {
  title: string;
  /** uPlot aligned data: [timestamps, ...series] */
  data: uPlot.AlignedData;
  /** Unit label for Y axis (e.g. "%", "GB", "MB/s") */
  unit: string;
  /** Series colors — defaults to the signal accent */
  colors?: string[];
  /** Series labels — defaults to ["Series 1", ...] */
  seriesLabels?: string[];
  /** Chart height in pixels — default 240 */
  height?: number;
}

const DEFAULT_COLORS = ["#5b9dff", "#34d399", "#f5a524", "#f5484f", "#5b6576"];

/** Reads the value at `idx` for every non-timestamp series, for the custom legend. */
function readSeriesValues(data: uPlot.AlignedData, idx: number): (number | null)[] {
  return data.slice(1).map((series) => {
    const v = series?.[idx];
    return typeof v === "number" ? v : null;
  });
}

export function MetricChart({
  title,
  data,
  unit,
  colors = DEFAULT_COLORS,
  seriesLabels,
  height = 240,
}: MetricChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);
  const [legendValues, setLegendValues] = useState<(number | null)[]>([]);

  const seriesCount = data.length - 1;
  const labels = Array.from(
    { length: seriesCount },
    (_, i) => seriesLabels?.[i] ?? `Series ${i + 1}`,
  );

  const buildOptions = useCallback(
    (width: number): uPlot.Options => {
      const series: uPlot.Series[] = [
        {}, // timestamp series (x-axis)
        ...Array.from({ length: seriesCount }, (_, i) => ({
          label: labels[i],
          stroke: colors[i % colors.length],
          width: 2,
          fill: `${colors[i % colors.length]}10`,
        })),
      ];

      return {
        width,
        height,
        cursor: {
          drag: { x: false, y: false },
        },
        legend: { show: false }, // replaced by the custom legend in the panel header
        scales: {
          x: { time: true },
          y: { auto: true, range: makeYRange(unit) },
        },
        axes: [
          {
            stroke: CHART_AXIS_STROKE,
            grid: { stroke: CHART_GRID_STROKE, width: 1 },
            ticks: { stroke: CHART_GRID_STROKE, width: 1 },
            font: CHART_AXIS_FONT,
          },
          {
            stroke: CHART_AXIS_STROKE,
            grid: { stroke: CHART_GRID_STROKE, width: 1 },
            ticks: { stroke: CHART_GRID_STROKE, width: 1 },
            font: CHART_AXIS_FONT,
            values: makeYAxisFormatter(unit),
            // No axis title: the unit is already in the panel header, and a
            // rotated single-character label here was too small to read.
          },
        ],
        series,
        hooks: {
          setCursor: [
            (u: uPlot) => {
              const lastIdx = u.data[0].length - 1;
              const idx = u.cursor.idx ?? (lastIdx >= 0 ? lastIdx : null);
              setLegendValues(idx === null ? [] : readSeriesValues(u.data, idx));
            },
          ],
        },
      };
    },
    [seriesCount, labels, colors, height, unit],
  );

  // Create/rebuild chart
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // Clean up previous chart
    if (chartRef.current) {
      chartRef.current.destroy();
      chartRef.current = null;
    }

    const opts = buildOptions(container.clientWidth);
    chartRef.current = new uPlot(opts, data, container);

    return () => {
      if (chartRef.current) {
        chartRef.current.destroy();
        chartRef.current = null;
      }
    };
  }, [data, buildOptions]);

  // Show the latest values by default (before any hover), and whenever fresh
  // data arrives from polling.
  useEffect(() => {
    const lastIdx = data[0]?.length - 1;
    setLegendValues(lastIdx !== undefined && lastIdx >= 0 ? readSeriesValues(data, lastIdx) : []);
  }, [data]);

  // Responsive resize
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (chartRef.current && width > 0) {
          chartRef.current.setSize({ width, height });
        }
      }
    });

    observer.observe(container);
    return () => observer.disconnect();
  }, [height]);

  return (
    <Panel className="p-5">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <h3 className="text-sm font-semibold text-[var(--color-ink)]">
          {title}
          <span className="ml-2 text-xs font-normal text-[var(--color-ink-faint)]">({unit})</span>
        </h3>
        {/* Custom legend: swatch + name + hovered-or-latest value, replacing
            uPlot's default "Time: -- CPU: --" legend row. */}
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          {labels.map((label, i) => (
            <span
              key={label}
              className="flex items-center gap-1.5 font-mono text-xs text-[var(--color-ink-muted)] tabular-nums"
            >
              <span
                className="h-2 w-2 flex-shrink-0 rounded-full"
                style={{ backgroundColor: colors[i % colors.length] }}
                aria-hidden="true"
              />
              {label}
              <span className="text-[var(--color-ink)]">
                {formatChartValue(legendValues[i], unit)}
              </span>
            </span>
          ))}
        </div>
      </div>
      <div ref={containerRef} className="w-full" />
    </Panel>
  );
}
