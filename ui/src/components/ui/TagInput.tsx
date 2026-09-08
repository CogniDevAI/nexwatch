import { useMemo, useState, type KeyboardEvent } from "react";
import { X } from "lucide-react";

interface TagInputProps {
  id?: string;
  value: string[];
  onChange: (tags: string[]) => void;
  /** Existing tags to offer as a suggestion dropdown while typing — usually
   *  every tag already in use somewhere else, so a new tag reuses spelling
   *  instead of forking a near-duplicate ("web" vs "Web"). */
  suggestions?: string[];
  placeholder?: string;
  "aria-label"?: string;
}

function normalize(tag: string): string {
  return tag.trim();
}

/**
 * Chip-style multi-value tag editor: type text and press Enter or comma to
 * add it as a chip, Backspace on an empty field removes the last chip, and a
 * suggestion dropdown (filtered by the current input, existing chips
 * excluded) offers reuse of tags already used elsewhere. Dedupes
 * case-insensitively so "web" and "Web" never coexist as two tags. See
 * DESIGN.md §7.
 */
export function TagInput({
  id,
  value,
  onChange,
  suggestions = [],
  placeholder = "Add a tag…",
  "aria-label": ariaLabel,
}: TagInputProps) {
  const [draft, setDraft] = useState("");
  const [focused, setFocused] = useState(false);

  const addTag = (raw: string) => {
    const tag = normalize(raw);
    if (!tag) return;
    const exists = value.some((t) => t.toLowerCase() === tag.toLowerCase());
    if (!exists) onChange([...value, tag]);
    setDraft("");
  };

  const removeTag = (tag: string) => {
    onChange(value.filter((t) => t !== tag));
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      addTag(draft);
      return;
    }
    if (e.key === "Backspace" && draft === "" && value.length > 0) {
      removeTag(value[value.length - 1]!);
    }
  };

  const filteredSuggestions = useMemo(() => {
    const used = new Set(value.map((t) => t.toLowerCase()));
    const query = draft.trim().toLowerCase();
    return suggestions
      .filter((s) => !used.has(s.toLowerCase()))
      .filter((s) => query === "" || s.toLowerCase().includes(query))
      .slice(0, 8);
  }, [suggestions, value, draft]);

  const showSuggestions = focused && filteredSuggestions.length > 0;

  return (
    <div className="relative">
      <div className="flex flex-wrap items-center gap-1.5 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-2 py-1.5 focus-within:border-[var(--color-signal)]">
        {value.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] py-0.5 pr-1 pl-2 text-xs text-[var(--color-ink)]"
          >
            {tag}
            <button
              type="button"
              aria-label={`Remove tag ${tag}`}
              onClick={() => removeTag(tag)}
              className="rounded-full p-0.5 text-[var(--color-ink-faint)] hover:bg-[var(--color-line)] hover:text-[var(--color-ink)]"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          id={id}
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => {
            // Delay so a suggestion's onMouseDown still fires before the
            // dropdown unmounts on blur.
            setTimeout(() => setFocused(false), 100);
            addTag(draft);
          }}
          placeholder={value.length === 0 ? placeholder : ""}
          aria-label={ariaLabel}
          className="min-w-[100px] flex-1 bg-transparent py-0.5 text-sm text-[var(--color-ink)] placeholder:text-[var(--color-ink-faint)] focus:outline-none"
        />
      </div>

      {showSuggestions && (
        <div className="absolute z-10 mt-1 w-full overflow-hidden rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-raised)] shadow-lg">
          {filteredSuggestions.map((s) => (
            <button
              key={s}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault();
                addTag(s);
              }}
              className="block w-full px-3 py-1.5 text-left text-sm text-[var(--color-ink-muted)] hover:bg-[var(--color-panel)] hover:text-[var(--color-ink)]"
            >
              {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
