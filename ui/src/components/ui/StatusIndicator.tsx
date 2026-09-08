import type { ReactNode } from "react";
import { STATUS_META, type Status } from "./status";

export type { Status } from "./status";

function StatusGlyph({ status, pulse = false }: { status: Status; pulse?: boolean }) {
  const cls = `h-2.5 w-2.5 flex-shrink-0 ${pulse ? "signal-pulse" : ""}`;
  switch (status) {
    case "ok":
      return (
        <svg viewBox="0 0 10 10" className={cls} aria-hidden="true">
          <circle cx="5" cy="5" r="4.5" fill="currentColor" />
        </svg>
      );
    case "warning":
      return (
        <svg viewBox="0 0 10 10" className={cls} aria-hidden="true">
          <path d="M5 0.3 9.7 9.2H0.3Z" fill="currentColor" />
        </svg>
      );
    case "critical":
      return (
        <svg viewBox="0 0 10 10" className={cls} aria-hidden="true">
          <path d="M5 0 10 5 5 10 0 5Z" fill="currentColor" />
        </svg>
      );
    case "offline":
      return (
        <svg viewBox="0 0 10 10" className={cls} aria-hidden="true">
          <circle cx="5" cy="5" r="3.6" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <line x1="1.6" y1="8.4" x2="8.4" y2="1.6" stroke="currentColor" strokeWidth="1.6" />
        </svg>
      );
  }
}

export interface StatusIndicatorProps {
  status: Status;
  /** Overrides the default status label (e.g. a docker container's raw status string). */
  label?: ReactNode;
  /** Compact glyph-only rendering for dense tables — accessible name via role="img". */
  dotOnly?: boolean;
  /** Slow attention pulse — reserve for the Signal Rail's critical/offline ticks. */
  pulse?: boolean;
  className?: string;
  /** Overrides the default status-tinted color — use only when rendering on
   *  top of a same-color filled background (e.g. a FleetStrip segment),
   *  where the default color would make the glyph invisible against its own
   *  status color. */
  toneClassName?: string;
}

/** Status glyph + label. Maps status to a fixed color AND shape — see DESIGN.md §1. */
export function StatusIndicator({
  status,
  label,
  dotOnly = false,
  pulse = false,
  className = "",
  toneClassName,
}: StatusIndicatorProps) {
  const meta = STATUS_META[status];
  const text = label ?? meta.label;
  const colorClass = toneClassName ?? meta.textClass;

  if (dotOnly) {
    return (
      <span
        role="img"
        aria-label={typeof text === "string" ? text : meta.label}
        className={`inline-flex ${colorClass} ${className}`}
      >
        <StatusGlyph status={status} pulse={pulse} />
      </span>
    );
  }

  return (
    <span
      className={`inline-flex items-center gap-1.5 text-xs font-medium ${colorClass} ${className}`}
    >
      <StatusGlyph status={status} pulse={pulse} />
      {text}
    </span>
  );
}
