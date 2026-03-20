import { useEffect, useRef, useCallback } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

interface MetricChartProps {
  title: string;
  /** uPlot aligned data: [timestamps, ...series] */
  data: uPlot.AlignedData;
  /** Unit label for Y axis (e.g. "%", "GB", "MB/s") */
  unit: string;
  /** Series colors — defaults to cyan + purple */
  colors?: string[];
  /** Series labels — defaults to ["Series 1", ...] */
  seriesLabels?: string[];
  /** Chart height in pixels — default 240 */
  height?: number;
}

function formatYAxis(unit: string) {
  return (
    _self: uPlot,
    ticks: number[],
  ): string[] => {
    return ticks.map((v) => {
      if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(1)}G`;
      if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
      if (v >= 1_000) return `${(v / 1_000).toFixed(1)}K`;
      return `${v.toFixed(unit === "%" ? 0 : 1)}`;
    });
  };
}

const DEFAULT_COLORS = ["#06b6d4", "#8b5cf6", "#10b981", "#f59e0b", "#ef4444"];

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

  const buildOptions = useCallback(
    (width: number): uPlot.Options => {
      const seriesCount = data.length - 1; // first element is timestamps
      const series: uPlot.Series[] = [
        {}, // timestamp series (x-axis)
        ...Array.from({ length: seriesCount }, (_, i) => ({
          label: seriesLabels?.[i] ?? `Series ${i + 1}`,
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
        scales: {
          x: { time: true },
          y: { auto: true },
        },
        axes: [
          {
            stroke: "#484f58",
            grid: { stroke: "#1e1e2e", width: 1 },
            ticks: { stroke: "#1e1e2e", width: 1 },
            font: "11px Inter, sans-serif",
          },
          {
            stroke: "#484f58",
            grid: { stroke: "#1e1e2e", width: 1 },
            ticks: { stroke: "#1e1e2e", width: 1 },
            font: "11px Inter, sans-serif",
            values: formatYAxis(unit),
            label: unit,
            labelFont: "11px Inter, sans-serif",
            labelSize: 20,
          },
        ],
        series,
      };
    },
    [data.length, colors, seriesLabels, height, unit],
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
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-5">
      <h3 className="text-sm font-semibold text-[var(--color-text-primary)] mb-4">
        {title}
        <span className="ml-2 text-xs font-normal text-[var(--color-text-muted)]">
          ({unit})
        </span>
      </h3>
      <div ref={containerRef} className="w-full" />
    </div>
  );
}
