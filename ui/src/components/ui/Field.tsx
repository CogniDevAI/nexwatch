import type {
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import { ChevronDown } from "lucide-react";

const CONTROL_CLASS =
  "w-full rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-3 py-2 text-sm text-[var(--color-ink)] transition-colors placeholder:text-[var(--color-ink-faint)] focus:border-[var(--color-signal)] focus:outline-none disabled:opacity-60";

export function Input({ className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`${CONTROL_CLASS} ${className}`} {...rest} />;
}

export function Textarea({ className = "", ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${CONTROL_CLASS} ${className}`} {...rest} />;
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  /** Extra classes for the `<select>` control itself (e.g. compact padding
   *  in a table cell) — kept separate from `className` (below) so the two
   *  never collide over the same property. */
  selectClassName?: string;
}

/**
 * A custom-chevron select. `className` sizes the OUTER wrapper (defaults to
 * `w-full`) — the `<select>` itself always fills that wrapper, so a caller
 * asking for e.g. `w-auto` never has to fight the baked-in `w-full` (two
 * competing width classes on one element are cascade-order-dependent and
 * silently pick the wrong one; splitting wrapper vs. control avoids that).
 */
export function Select({ className = "", selectClassName = "", children, ...rest }: SelectProps) {
  return (
    <div className={`relative ${className || "w-full"}`}>
      <select
        className={`${CONTROL_CLASS} w-full appearance-none pr-9 ${selectClassName}`}
        {...rest}
      >
        {children}
      </select>
      <ChevronDown
        className="pointer-events-none absolute top-1/2 right-3 h-4 w-4 -translate-y-1/2 text-[var(--color-ink-faint)]"
        aria-hidden="true"
      />
    </div>
  );
}

interface LabelProps {
  htmlFor?: string;
  children: ReactNode;
  required?: boolean;
}

/** Field label — use a plain <FieldCaption> instead when the text describes a
 *  group of controls (a checkbox list, a button group) rather than one control. */
export function Label({ htmlFor, children, required = false }: LabelProps) {
  return (
    <label
      htmlFor={htmlFor}
      className="mb-1.5 block text-sm font-medium text-[var(--color-ink-muted)]"
    >
      {children}
      {required && <span className="ml-0.5 text-[var(--color-critical)]">*</span>}
    </label>
  );
}

/** Non-form-associated caption for a group of controls — avoids an orphaned
 *  <label for> that points at nothing. */
export function FieldCaption({ children }: { children: ReactNode }) {
  return (
    <span className="mb-1.5 block text-sm font-medium text-[var(--color-ink-muted)]">
      {children}
    </span>
  );
}
