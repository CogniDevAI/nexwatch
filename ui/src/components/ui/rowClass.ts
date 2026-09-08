/** Row background for a zebra-striped, hoverable table body row. */
export function rowClass(idx: number): string {
  return `transition-colors ${idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-panel-raised)]/40"} hover:bg-[var(--color-panel-raised)]`;
}
