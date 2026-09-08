import { RefreshCw, X } from "lucide-react";
import { usePwaStore } from "@/stores/pwaStore";
import { Button } from "@/components/ui/Button";

/**
 * F11 — installable PWA. Rendered once in AppShell; shows only once the
 * service worker registration (main.tsx's registerSW) reports a new,
 * waiting worker via pwaStore.needRefresh. Unlike the generic toast system
 * (components/ui/Toast.tsx, which auto-dismisses after 4s), this one
 * persists until the user acts — an available update can sit waiting for
 * an arbitrarily long time before anyone happens to be looking at the
 * screen, so auto-dismissing it would mean it is silently missed.
 */
export function UpdateAvailableToast() {
  const needRefresh = usePwaStore((s) => s.needRefresh);
  const updateServiceWorker = usePwaStore((s) => s.updateServiceWorker);
  const setNeedRefresh = usePwaStore((s) => s.setNeedRefresh);

  if (!needRefresh) return null;

  return (
    <div className="fixed inset-x-0 bottom-4 z-[100] flex justify-center px-4 sm:right-4 sm:left-auto sm:justify-end">
      <div
        role="status"
        className="flex items-center gap-3 rounded-[var(--radius-panel)] border border-[var(--color-signal)]/25 bg-[var(--color-panel-raised)] px-4 py-3 text-sm text-[var(--color-ink)] shadow-2xl"
      >
        <RefreshCw
          className="h-4 w-4 flex-shrink-0 text-[var(--color-signal)]"
          aria-hidden="true"
        />
        <span>A new version of NexWatch is available.</span>
        <Button size="sm" variant="primary" onClick={() => void updateServiceWorker?.(true)}>
          Reload
        </Button>
        <button
          type="button"
          aria-label="Dismiss update notice"
          onClick={() => setNeedRefresh(false)}
          className="text-[var(--color-ink-faint)] transition-colors hover:text-[var(--color-ink)]"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
