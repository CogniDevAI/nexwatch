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

/** Label + large monospace value, for score/count summaries. Left-aligned like
 *  everything else — centered figures fight the top-to-bottom scan this tool is
 *  read with. See DESIGN.md §3 ("Alignment rule") and §7. */
export function MetricTile({ label, value, tone = "default", unit }: MetricTileProps) {
  return (
    <div className="text-left">
      <p className="text-xs text-[var(--color-ink-muted)]">{label}</p>
      <p className={`mt-0.5 font-mono text-2xl font-semibold tabular-nums ${TONE_CLASS[tone]}`}>
        {value}
        {unit && (
          <span className="ml-1 text-sm font-normal text-[var(--color-ink-faint)]">{unit}</span>
        )}
      </p>
    </div>
  );
}
