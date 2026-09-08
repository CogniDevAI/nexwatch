/** Read-only tag chip list — table rows, mobile cards, host detail header. */
export function TagChips({ tags, className = "" }: { tags: string[]; className?: string }) {
  if (tags.length === 0) return null;
  return (
    <div className={`flex flex-wrap items-center gap-1 ${className}`}>
      {tags.map((tag) => (
        <span
          key={tag}
          className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-xs text-[var(--color-ink-muted)]"
        >
          {tag}
        </span>
      ))}
    </div>
  );
}

interface TagFilterBarProps {
  tags: string[];
  selected: Set<string>;
  onToggle: (tag: string) => void;
}

/** Toggleable tag chips used to filter the Agents table and Dashboard grid —
 *  multiple selected tags mean ANY (not all), matching how `target_tags`
 *  targeting works on alert rules. Renders nothing when there are no tags in
 *  use yet, so it never occupies space on a fleet with no tagging set up. */
export function TagFilterBar({ tags, selected, onToggle }: TagFilterBarProps) {
  if (tags.length === 0) return null;
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <span className="text-xs text-[var(--color-ink-faint)]">Filter by tag:</span>
      {tags.map((tag) => {
        const active = selected.has(tag);
        return (
          <button
            key={tag}
            type="button"
            aria-pressed={active}
            onClick={() => onToggle(tag)}
            className={`rounded-[var(--radius-chip)] border px-2 py-1 text-xs font-medium transition-colors ${
              active
                ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                : "border-[var(--color-line)] bg-transparent text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
            }`}
          >
            {tag}
          </button>
        );
      })}
    </div>
  );
}
