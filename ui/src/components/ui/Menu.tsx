import { useEffect, useRef, useState } from "react";
import type { ComponentType } from "react";
import { MoreHorizontal } from "lucide-react";
import { IconButton } from "@/components/ui/Button";

export interface MenuItem {
  label: string;
  icon?: ComponentType<{ className?: string }>;
  onSelect: () => void;
  /** Styles the item like a destructive action (matches the danger hover
   *  treatment used elsewhere for delete-style row actions). */
  danger?: boolean;
  disabled?: boolean;
}

interface MenuProps {
  /** Accessible name for the trigger button, e.g. "More actions for web-01". */
  "aria-label": string;
  items: MenuItem[];
  className?: string;
}

/**
 * A small overflow menu for row actions that don't fit as labeled buttons —
 * see DESIGN.md §7/"Row action overflow". Trigger is a `⋯` `IconButton` with
 * `aria-haspopup="menu"`/`aria-expanded`; the panel is `role="menu"` with
 * `role="menuitem"` buttons, arrow-key roving focus, Escape closes and
 * returns focus to the trigger, and an outside click or item selection also
 * closes it. Used by the Agents table/mobile cards to keep "Update" and
 * "Edit tags" as direct labeled buttons while "Regenerate token" and
 * "Delete" move here once four labeled ghost buttons no longer fit a row.
 */
export function Menu({ "aria-label": ariaLabel, items, className = "" }: MenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

  useEffect(() => {
    if (!open) return;

    function handlePointerDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
        containerRef.current?.querySelector<HTMLButtonElement>("[data-menu-trigger]")?.focus();
        return;
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const enabled = itemRefs.current.filter((el): el is HTMLButtonElement => !!el);
        if (enabled.length === 0) return;
        const currentIndex = enabled.findIndex((el) => el === document.activeElement);
        const delta = e.key === "ArrowDown" ? 1 : -1;
        const nextIndex = (currentIndex + delta + enabled.length) % enabled.length;
        enabled[nextIndex]?.focus();
      }
    }

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  useEffect(() => {
    if (open) itemRefs.current[0]?.focus();
  }, [open]);

  return (
    <div className={`relative inline-block ${className}`} ref={containerRef}>
      <IconButton
        data-menu-trigger
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
      </IconButton>

      {open && (
        <div
          role="menu"
          aria-label={ariaLabel}
          className="absolute top-full right-0 z-20 mt-1 min-w-[10rem] overflow-hidden rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-raised)] py-1 shadow-lg"
        >
          {items.map((item, idx) => (
            <button
              key={item.label}
              ref={(el) => {
                itemRefs.current[idx] = el;
              }}
              role="menuitem"
              type="button"
              disabled={item.disabled}
              onClick={() => {
                setOpen(false);
                item.onSelect();
              }}
              className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                item.danger
                  ? "text-[var(--color-critical)] hover:bg-[var(--color-critical)]/10"
                  : "text-[var(--color-ink)] hover:bg-[var(--color-panel)]"
              }`}
            >
              {item.icon && <item.icon className="h-3.5 w-3.5 flex-shrink-0" aria-hidden="true" />}
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
