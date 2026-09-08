export type Severity = "critical" | "high" | "medium" | "low" | "info" | "unknown";

interface SeverityMeta {
  label: string;
  classes: string;
}

const SEVERITY_META: Record<Severity, SeverityMeta> = {
  critical: {
    label: "Critical",
    classes:
      "border-[var(--color-critical)]/30 bg-[var(--color-critical)]/10 text-[var(--color-critical)]",
  },
  high: {
    label: "High",
    classes:
      "border-[var(--color-severity-high)]/30 bg-[var(--color-severity-high)]/10 text-[var(--color-severity-high)]",
  },
  medium: {
    label: "Medium",
    classes: "border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 text-[var(--color-warn)]",
  },
  low: {
    label: "Low",
    classes:
      "border-[var(--color-signal)]/30 bg-[var(--color-signal)]/10 text-[var(--color-signal)]",
  },
  info: {
    label: "Info",
    classes:
      "border-[var(--color-ink-faint)]/30 bg-[var(--color-ink-faint)]/10 text-[var(--color-ink-muted)]",
  },
  unknown: {
    label: "Unknown",
    classes:
      "border-[var(--color-ink-faint)]/30 bg-[var(--color-ink-faint)]/10 text-[var(--color-ink-muted)]",
  },
};

/** Five-level vulnerability/finding severity chip — a distinct taxonomy from
 *  the four-state fleet <StatusIndicator>. See DESIGN.md §1. */
export function SeverityBadge({
  severity,
  className = "",
}: {
  severity: Severity;
  className?: string;
}) {
  const meta = SEVERITY_META[severity];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-[var(--radius-chip)] border px-2 py-0.5 text-xs font-medium ${meta.classes} ${className}`}
    >
      {meta.label}
    </span>
  );
}
