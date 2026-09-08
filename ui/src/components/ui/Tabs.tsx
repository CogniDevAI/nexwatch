import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

export interface TabItem<T extends string = string> {
  key: T;
  label: string;
  /** Optional leading icon, matching the host detail page's original tab
   *  bar (an icon per section) — the Add-agent modal's OS tabs use
   *  PlatformIcon the same way. */
  icon?: React.ComponentType<{ className?: string }>;
}

interface TabsProps<T extends string> {
  items: TabItem<T>[];
  activeKey: T;
  onChange: (key: T) => void;
  /** Accessible name for the tablist — every caller needs a distinct one
   *  (e.g. "Server detail sections", "Install command operating system"). */
  "aria-label": string;
  /** Applied to the outer wrapper (the scroll container and its overflow
   *  chevrons live inside it) — e.g. a plain bottom border for a short tab
   *  row (Add-agent modal's two OS tabs) or `-mb-px` to flush the row
   *  against a parent's border. Horizontal scrolling is always built in
   *  (see "Row and tab-bar overflow" in DESIGN.md §7), so a caller never
   *  needs to opt into it. */
  className?: string;
}

/**
 * Accessible tab bar shared by the host detail page's section tabs and the
 * Add-agent modal's Linux/Windows OS tabs (previously two near-identical
 * hand-rolled implementations — see DESIGN.md §7). Implements the WAI-ARIA
 * "tabs" pattern's keyboard behavior: ArrowRight/ArrowDown moves to the
 * next tab, ArrowLeft/ArrowUp to the previous one (wrapping at either end),
 * Home/End jump to the first/last tab — each selecting that tab
 * immediately (automatic activation) and moving focus to it, matching how
 * a sighted mouse user already switches tabs by clicking. Only the
 * currently active tab is in the natural tab order (roving tabindex), so
 * Tab/Shift+Tab skips over the inactive ones instead of stopping at each.
 *
 * Row overflow (R2 fix): the tab row always scrolls horizontally within
 * itself rather than growing past its container — a host detail page with
 * twelve tabs no longer pushes the last few off-screen with no way to reach
 * them. A small chevron affordance appears at whichever edge still has more
 * tabs to scroll to (tracked via scroll position + a ResizeObserver) and
 * scrolls one step on click; keyboard navigation calls `scrollIntoView` on
 * the newly focused tab so arrowing past the visible edge still works.
 */
export function Tabs<T extends string>({
  items,
  activeKey,
  onChange,
  "aria-label": ariaLabel,
  className = "",
}: TabsProps<T>) {
  const buttonRefs = useRef<Map<T, HTMLButtonElement>>(new Map());
  const scrollRef = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState({ left: false, right: false });

  const activeIndex = items.findIndex((item) => item.key === activeKey);

  const updateOverflow = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setOverflow({
      left: el.scrollLeft > 4,
      right: el.scrollLeft + el.clientWidth < el.scrollWidth - 4,
    });
  }, []);

  useEffect(() => {
    updateOverflow();
    const el = scrollRef.current;
    if (!el) return;

    el.addEventListener("scroll", updateOverflow, { passive: true });
    window.addEventListener("resize", updateOverflow);

    // jsdom (unit tests) has no ResizeObserver — guard rather than assume a
    // real browser, since a container-width change (sidebar collapse, a
    // responsive breakpoint) also needs to re-check overflow.
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(updateOverflow);
      observer.observe(el);
    }

    return () => {
      el.removeEventListener("scroll", updateOverflow);
      window.removeEventListener("resize", updateOverflow);
      observer?.disconnect();
    };
  }, [updateOverflow, items.length]);

  function focusAndSelect(index: number) {
    const item = items[index];
    if (!item) return;
    onChange(item.key);
    const button = buttonRefs.current.get(item.key);
    button?.focus();
    // Belt-and-braces: focus() already scrolls a focused element into view
    // in most browsers, but calling this explicitly keeps arrow-key
    // navigation reliable once the tab row scrolls past the visible edge.
    button?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }

  function handleKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    const count = items.length;
    if (count === 0) return;

    switch (e.key) {
      case "ArrowRight":
      case "ArrowDown":
        e.preventDefault();
        focusAndSelect((activeIndex + 1 + count) % count);
        break;
      case "ArrowLeft":
      case "ArrowUp":
        e.preventDefault();
        focusAndSelect((activeIndex - 1 + count) % count);
        break;
      case "Home":
        e.preventDefault();
        focusAndSelect(0);
        break;
      case "End":
        e.preventDefault();
        focusAndSelect(count - 1);
        break;
    }
  }

  function scrollByStep(direction: 1 | -1) {
    scrollRef.current?.scrollBy({ left: direction * 160, behavior: "smooth" });
  }

  return (
    <div className={`relative ${className}`}>
      <div
        ref={scrollRef}
        role="tablist"
        aria-label={ariaLabel}
        className="overflow-x-auto overscroll-x-contain"
      >
        <div className="inline-flex min-w-max gap-4">
          {items.map(({ key, label, icon: Icon }) => {
            const selected = key === activeKey;
            return (
              <button
                key={key}
                ref={(el) => {
                  if (el) buttonRefs.current.set(key, el);
                  else buttonRefs.current.delete(key);
                }}
                type="button"
                role="tab"
                aria-selected={selected}
                tabIndex={selected ? 0 : -1}
                onClick={() => onChange(key)}
                onKeyDown={handleKeyDown}
                className={`inline-flex items-center gap-1.5 border-b-2 pb-2.5 text-sm font-medium whitespace-nowrap transition-colors ${
                  selected
                    ? "border-[var(--color-signal)] text-[var(--color-signal)]"
                    : "border-transparent text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]"
                }`}
              >
                {Icon && <Icon className="h-3.5 w-3.5" aria-hidden="true" />}
                {label}
              </button>
            );
          })}
        </div>
      </div>

      {overflow.left && (
        <button
          type="button"
          tabIndex={-1}
          aria-hidden="true"
          onClick={() => scrollByStep(-1)}
          className="absolute top-0 -bottom-px left-0 flex items-center rounded-r-[var(--radius-control)] border border-l-0 border-[var(--color-line)] bg-[var(--color-panel-raised)] px-1 text-[var(--color-ink-muted)] shadow-sm transition-colors hover:text-[var(--color-ink)]"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
        </button>
      )}
      {overflow.right && (
        <button
          type="button"
          tabIndex={-1}
          aria-hidden="true"
          onClick={() => scrollByStep(1)}
          className="absolute top-0 right-0 -bottom-px flex items-center rounded-l-[var(--radius-control)] border border-r-0 border-[var(--color-line)] bg-[var(--color-panel-raised)] px-1 text-[var(--color-ink-muted)] shadow-sm transition-colors hover:text-[var(--color-ink)]"
        >
          <ChevronRight className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}
