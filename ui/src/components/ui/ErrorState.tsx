import { AlertTriangle } from "lucide-react";
import type { ReactNode } from "react";

interface ErrorStateProps {
  title: string;
  description?: string;
  action?: ReactNode;
}

/** Icon + heading + one-sentence body that says what to check next + optional
 *  retry action. See DESIGN.md §6. */
export function ErrorState({ title, description, action }: ErrorStateProps) {
  return (
    <div className="p-10 text-center">
      <AlertTriangle
        className="mx-auto mb-4 h-10 w-10 text-[var(--color-warn)]"
        aria-hidden="true"
      />
      <h3 className="text-base font-semibold text-[var(--color-ink)]">{title}</h3>
      {description && (
        <p className="mx-auto mt-1.5 max-w-prose text-sm text-[var(--color-ink-muted)]">
          {description}
        </p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
