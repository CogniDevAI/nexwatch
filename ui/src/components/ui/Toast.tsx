import { useCallback, useMemo, useState, type ReactNode } from "react";
import { Check, AlertTriangle } from "lucide-react";
import { ToastContext, type ToastTone } from "./toastContext";

interface ToastMessage {
  id: number;
  text: string;
  tone: ToastTone;
}

let nextId = 0;

/** Mounted once at the app root. Provides `useToast()` for transient,
 *  verb-echoing confirmations (e.g. "Settings saved"). See DESIGN.md §6. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const showToast = useCallback((text: string, tone: ToastTone = "success") => {
    const id = nextId++;
    setToasts((prev) => [...prev, { id, text, tone }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 4000);
  }, []);

  const value = useMemo(() => ({ showToast }), [showToast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="fixed inset-x-0 bottom-4 z-[100] flex flex-col items-center gap-2 px-4 sm:right-4 sm:left-auto sm:items-end"
        role="region"
        aria-label="Notifications"
      >
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`flex items-center gap-2 rounded-[var(--radius-panel)] border px-4 py-3 text-sm shadow-2xl ${
              t.tone === "success"
                ? "border-[var(--color-ok)]/25 bg-[var(--color-panel-raised)] text-[var(--color-ink)]"
                : "border-[var(--color-critical)]/25 bg-[var(--color-panel-raised)] text-[var(--color-ink)]"
            }`}
          >
            {t.tone === "success" ? (
              <Check className="h-4 w-4 flex-shrink-0 text-[var(--color-ok)]" aria-hidden="true" />
            ) : (
              <AlertTriangle
                className="h-4 w-4 flex-shrink-0 text-[var(--color-critical)]"
                aria-hidden="true"
              />
            )}
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
