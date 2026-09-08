import type { HTMLAttributes, ReactNode } from "react";

type PanelVariant = "panel" | "flush";

interface PanelProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  /** "flush" drops the card frame and keeps only horizontal rules — use it for
   *  a page section that is part of the page, not an object floating on it. */
  variant?: PanelVariant;
}

const PANEL_VARIANT: Record<PanelVariant, string> = {
  panel:
    "overflow-hidden rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]",
  flush: "border-y border-[var(--color-line)] bg-transparent",
};

/** Flat card container — 1px border, no shadow, small radius. See DESIGN.md §3/§7. */
export function Panel({ children, className = "", variant = "panel", ...rest }: PanelProps) {
  return (
    <div className={`${PANEL_VARIANT[variant]} ${className}`} {...rest}>
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

/**
 * A page section that is not a card: a titled band separated from its
 * neighbours by a rule and vertical rhythm rather than by a border on four
 * sides. See DESIGN.md §3 ("Sections, not cards").
 */
export function Section({
  children,
  className = "",
  ...rest
}: HTMLAttributes<HTMLElement> & { children: ReactNode }) {
  return (
    <section className={`mb-8 ${className}`} {...rest}>
      {children}
    </section>
  );
}

interface SectionHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  /** Compact contextual data shown at the end of the title rule (counts, ratios). */
  meta?: ReactNode;
  actions?: ReactNode;
  id?: string;
}

export function SectionHeader({ title, description, meta, actions, id }: SectionHeaderProps) {
  return (
    <div className="mb-3 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-[var(--color-line)] pb-2">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id={id} className="text-base font-semibold text-[var(--color-ink)]">
          {title}
        </h2>
        {description && (
          <p className="max-w-prose text-xs text-[var(--color-ink-faint)]">{description}</p>
        )}
      </div>
      <div className="flex flex-shrink-0 items-center gap-3">
        {meta && <span className="font-mono text-xs text-[var(--color-ink-muted)]">{meta}</span>}
        {actions}
      </div>
    </div>
  );
}
