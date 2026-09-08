import type { ReactNode, ThHTMLAttributes, TdHTMLAttributes } from "react";
import { ArrowUpDown } from "lucide-react";

/** Table primitives shared by every data table in the app. Headers are
 *  sentence case (never tracked-out ALL-CAPS) — see DESIGN.md §6. */
export function Table({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">{children}</table>
      </div>
    </div>
  );
}

// Tailwind scans source text for literal class names, so alignment must be a
// lookup of complete strings rather than an interpolated `text-${align}`.
const ALIGN_TEXT: Record<"left" | "right", string> = { left: "text-left", right: "text-right" };

interface ThProps extends ThHTMLAttributes<HTMLTableCellElement> {
  align?: "left" | "right";
  sortable?: boolean;
  sortActive?: boolean;
}

export function Th({
  children,
  align = "left",
  sortable = false,
  sortActive = false,
  className = "",
  ...rest
}: ThProps) {
  return (
    <th
      className={`sticky top-0 bg-[var(--color-panel)] px-5 py-3 ${ALIGN_TEXT[align]} text-xs font-medium text-[var(--color-ink-muted)] ${
        sortable ? "cursor-pointer transition-colors select-none hover:text-[var(--color-ink)]" : ""
      } ${className}`}
      {...rest}
    >
      <span className={`inline-flex items-center gap-1 ${align === "right" ? "justify-end" : ""}`}>
        {children}
        {sortable && (
          <ArrowUpDown
            className={`h-3 w-3 ${sortActive ? "text-[var(--color-signal)]" : "text-[var(--color-ink-faint)]"}`}
          />
        )}
      </span>
    </th>
  );
}

interface TdProps extends TdHTMLAttributes<HTMLTableCellElement> {
  align?: "left" | "right";
}

export function Td({ children, align = "left", className = "", ...rest }: TdProps) {
  return (
    <td className={`px-5 py-3 ${ALIGN_TEXT[align]} text-[var(--color-ink)] ${className}`} {...rest}>
      {children}
    </td>
  );
}
