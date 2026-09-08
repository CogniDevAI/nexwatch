import { BellOff, ArrowUpCircle } from "lucide-react";

/** Muted "this alert is in a maintenance window" flag — independent of the
 *  ok/warning/critical/offline <StatusIndicator> taxonomy, since a silenced
 *  alert can be any severity. See DESIGN.md §1/§7. */
export function SilencedBadge({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-xs font-medium text-[var(--color-ink-faint)] ${className}`}
    >
      <BellOff className="h-3 w-3" aria-hidden="true" />
      Silenced
    </span>
  );
}

/** "This alert kept firing past its escalation window" flag — amber like
 *  warning, but a distinct fact from severity (a warning-severity alert can
 *  still escalate). See DESIGN.md §1/§7. */
export function EscalatedBadge({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-[var(--radius-chip)] border border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 px-1.5 py-0.5 text-xs font-medium text-[var(--color-warn)] ${className}`}
    >
      <ArrowUpCircle className="h-3 w-3" aria-hidden="true" />
      Escalated
    </span>
  );
}
