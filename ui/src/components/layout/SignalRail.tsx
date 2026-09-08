import { useLocation } from "react-router-dom";
import { useFleetHealth } from "@/hooks/useFleetHealth";
import { FleetStrip } from "@/components/ui/FleetStrip";

/**
 * The memorable thing (DESIGN.md §4): a persistent per-agent health strip,
 * visible from every page — not just the dashboard — so "is everything
 * healthy" can be answered in three seconds even three tabs deep into a
 * single host's detail view. Each tick reflects real health (connectivity
 * plus any firing alert), not just online/offline.
 */
export function SignalRail() {
  const { fleet } = useFleetHealth();
  const location = useLocation();

  if (fleet.length === 0) return null;

  return (
    <div className="border-t border-[var(--color-line)] px-4 py-3">
      <p className="text-2xs mb-2 font-medium tracking-wide text-[var(--color-ink-faint)]">
        Fleet signal
      </p>
      <FleetStrip agents={fleet} size="sm" currentPath={location.pathname} />
    </div>
  );
}
