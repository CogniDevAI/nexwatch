/** Fleet/operational status. See DESIGN.md §1 — each state has a fixed color
 *  AND a fixed glyph shape, so status never depends on color alone. */
export type Status = "ok" | "warning" | "critical" | "offline";

interface StatusMeta {
  label: string;
  textClass: string;
}

export const STATUS_META: Record<Status, StatusMeta> = {
  ok: { label: "Operational", textClass: "text-[var(--color-ok)]" },
  warning: { label: "Warning", textClass: "text-[var(--color-warn)]" },
  critical: { label: "Critical", textClass: "text-[var(--color-critical)]" },
  offline: { label: "Offline", textClass: "text-[var(--color-offline)]" },
};

/** Hex values for consumers that can't use CSS classes (uPlot series colors). */
export const STATUS_HEX: Record<Status, string> = {
  ok: "#34d399",
  warning: "#f5a524",
  critical: "#f5484f",
  offline: "#5b6576",
};
