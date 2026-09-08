import type { ReactNode } from "react";

type Tone = "default" | "ok" | "warning" | "critical";

const TONE_CLASS: Record<Tone, string> = {
  default: "text-[var(--color-ink)]",
  ok: "text-[var(--color-ok)]",
  warning: "text-[var(--color-warn)]",
  critical: "text-[var(--color-critical)]",
};

interface MetricTileProps {
  label: string;
  value: ReactNode;
  tone?: Tone;
  unit?: string;
}

/** Label + large monospace value, for score/count summaries. See DESIGN.md §7. */
export function MetricTile({ label, value, tone = "default", unit }: MetricTileProps) {
  return (
    <div className="text-center">
      <p className={`font-mono text-2xl font-semibold tabular-nums ${TONE_CLASS[tone]}`}>
        {value}
        {unit && (
          <span className="ml-1 text-sm font-normal text-[var(--color-ink-faint)]">{unit}</span>
        )}
      </p>
      <p className="mt-1 text-xs text-[var(--color-ink-muted)]">{label}</p>
    </div>
  );
}
