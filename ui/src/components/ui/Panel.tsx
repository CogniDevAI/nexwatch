import type { HTMLAttributes, ReactNode } from "react";

interface PanelProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
}

/** Flat card container — 1px border, no shadow, small radius. See DESIGN.md §3/§7. */
export function Panel({ children, className = "", ...rest }: PanelProps) {
  return (
    <div
      className={`overflow-hidden rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] ${className}`}
      {...rest}
    >
      {children}
    </div>
  );
}

interface PanelHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  icon?: ReactNode;
  badge?: ReactNode;
}

export function PanelHeader({ title, description, actions, icon, badge }: PanelHeaderProps) {
  return (
    <div className="flex flex-col gap-3 border-b border-[var(--color-line)] px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-center gap-2.5">
        {icon && <span className="flex-shrink-0 text-[var(--color-ink-faint)]">{icon}</span>}
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-base font-semibold text-[var(--color-ink)]">
            {title}
            {badge}
          </h3>
          {description && (
            <p className="mt-0.5 text-sm text-[var(--color-ink-muted)]">{description}</p>
          )}
        </div>
      </div>
      {actions && <div className="flex flex-shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

export function PanelBody({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={`p-5 ${className}`}>{children}</div>;
}
