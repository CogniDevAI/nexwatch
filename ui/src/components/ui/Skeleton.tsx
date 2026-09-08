/** Shaped loading placeholder — reduced-motion safe (see globals.css). */
export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <div
      className={`animate-pulse rounded-[var(--radius-control)] bg-[var(--color-panel-raised)] ${className}`}
    />
  );
}

/** A row of label/value skeletons matching <MetricTile>'s footprint. */
export function MetricTileSkeleton() {
  return (
    <div className="flex flex-col items-center gap-2">
      <Skeleton className="h-7 w-14" />
      <Skeleton className="h-3 w-16" />
    </div>
  );
}
