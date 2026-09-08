import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
}

/** Icon + heading + one-sentence body + optional action. See DESIGN.md §6. */
export function EmptyState({ icon: Icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="p-10 text-center">
      <Icon className="mx-auto mb-4 h-10 w-10 text-[var(--color-ink-faint)]" aria-hidden="true" />
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
