import { createContext, useContext } from "react";

export type ToastTone = "success" | "error";

export interface ToastContextValue {
  showToast: (text: string, tone?: ToastTone) => void;
}

export const ToastContext = createContext<ToastContextValue | null>(null);

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within a ToastProvider");
  return ctx;
}
