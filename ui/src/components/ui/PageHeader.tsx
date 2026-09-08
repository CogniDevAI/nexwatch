import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: ReactNode;
  /** Compact contextual data for the page (counts, posture, last update).
   *  Rendered as a meta line under the title rule — never as KPI cards. */
  meta?: ReactNode;
}

/**
 * Page-level heading rendered as a full-bleed bar rather than a floating block:
 * a rule under the title separates chrome from content, so the first box on a
 * page is real content instead of a header card. See DESIGN.md §3.
 */
export function PageHeader({ title, description, actions, meta }: PageHeaderProps) {
  return (
    <header className="bleed-x mb-6 border-b border-[var(--color-line)] pb-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold text-[var(--color-ink)]">{title}</h1>
          {description && (
            <p className="mt-0.5 max-w-prose text-sm text-[var(--color-ink-muted)]">
              {description}
            </p>
          )}
        </div>
        {actions && (
          <div className="flex flex-shrink-0 flex-wrap items-center gap-2">{actions}</div>
        )}
      </div>
      {meta && (
        <div className="mt-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-xs">{meta}</div>
      )}
    </header>
  );
}
