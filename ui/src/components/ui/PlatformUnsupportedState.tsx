import { MonitorX } from "lucide-react";
import { EmptyState } from "./EmptyState";

interface PlatformUnsupportedStateProps {
  /** The tab/feature name, e.g. "Misconfigurations" or "Oracle DB monitoring". */
  feature: string;
  /** Human-readable platform display name, e.g. "Windows". */
  platform: string;
}

/**
 * Quiet placeholder for a host-detail tab backed by a collector that has no
 * implementation on the agent's OS (see internal/agent/platform.Supported
 * on the Go side) — e.g. Misconfigurations/Oracle DB on a Windows agent.
 * Rendered instead of mounting the real tab component, so it never
 * attempts a fetch that would otherwise sit in an empty/error state
 * indistinguishable from a real problem. Wrapped in the same bordered
 * panel shell each tab's own empty/error states already use (see
 * DESIGN.md §9) for visual consistency.
 */
export function PlatformUnsupportedState({ feature, platform }: PlatformUnsupportedStateProps) {
  return (
    <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
      <EmptyState
        icon={MonitorX}
        title={`Not available on ${platform}`}
        description={`${feature} is not supported on ${platform} agents.`}
      />
    </div>
  );
}
