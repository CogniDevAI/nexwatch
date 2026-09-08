import { useEffect, type ReactNode } from "react";
import { X } from "lucide-react";
import { IconButton } from "./Button";

interface ModalProps {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  maxWidth?: string;
}

/** Centered dialog used by every "New X" form and the thread dump viewer. */
export function Modal({ title, onClose, children, maxWidth = "max-w-lg" }: ModalProps) {
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button
        type="button"
        aria-label="Close dialog"
        className="fixed inset-0 bg-black/60"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === "string" ? title : undefined}
        className={`relative w-full ${maxWidth} rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel-raised)] shadow-2xl`}
      >
        <div className="flex items-center justify-between border-b border-[var(--color-line)] px-6 py-4">
          <h3 className="text-lg font-semibold text-[var(--color-ink)]">{title}</h3>
          <IconButton aria-label="Close dialog" onClick={onClose}>
            <X className="h-5 w-5" />
          </IconButton>
        </div>
        {children}
      </div>
    </div>
  );
}
