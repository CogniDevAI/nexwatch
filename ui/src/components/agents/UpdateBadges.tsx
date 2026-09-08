import { ArrowUpCircle, AlertTriangle, Loader2, RotateCw } from "lucide-react";
import type { AgentUpdateStatus } from "@/types";
import { isUpdateInProgress, updateStatusLabel } from "@/lib/agentUpdates";

/** "A newer agent release is available" flag — amber like EscalatedBadge
 *  (src/components/alerts/AlertBadges.tsx), a distinct fact from an
 *  agent's connectivity status. See DESIGN.md §1/§7. */
export function UpdateAvailableBadge({
  targetVersion,
  className = "",
}: {
  targetVersion: string;
  className?: string;
}) {
  return (
    <span
      title={targetVersion ? `Update to ${targetVersion} available` : "Update available"}
      className={`inline-flex items-center gap-1 rounded-[var(--radius-chip)] border border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 px-1.5 py-0.5 text-xs font-medium text-[var(--color-warn)] ${className}`}
    >
      <ArrowUpCircle className="h-3 w-3" aria-hidden="true" />
      Update available
    </span>
  );
}

/** Live chip for an agent's update_status: a spinner + stage label while
 *  in progress, a critical "Update failed" chip (with an optional Retry
 *  action and the error as a tooltip) on failure, an amber "Restart
 *  required" chip (Windows only — see AgentUpdateStatus's doc comment) once
 *  the install succeeded but needs a host restart to actually take effect,
 *  and nothing for idle/done/empty — those states carry no ongoing
 *  information worth a persistent chip. */
export function UpdateStatusChip({
  status,
  error,
  onRetry,
  className = "",
}: {
  status: AgentUpdateStatus | undefined;
  error?: string;
  onRetry?: () => void;
  className?: string;
}) {
  if (status === "failed") {
    return (
      <span className={`inline-flex items-center gap-1.5 ${className}`}>
        <span
          title={error || "Update failed"}
          className="inline-flex items-center gap-1 rounded-[var(--radius-chip)] bg-[var(--color-critical)]/15 px-1.5 py-0.5 text-xs font-medium text-[var(--color-critical)]"
        >
          <AlertTriangle className="h-3 w-3" aria-hidden="true" />
          Update failed
        </span>
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="text-xs font-medium text-[var(--color-signal)] hover:underline"
          >
            Retry
          </button>
        )}
      </span>
    );
  }

  if (status === "restart_required") {
    return (
      <span
        title="The update was installed but its files were locked, so it will finish applying the next time this host restarts."
        className={`inline-flex items-center gap-1 rounded-[var(--radius-chip)] bg-[var(--color-warn)]/15 px-1.5 py-0.5 text-xs font-medium text-[var(--color-warn)] ${className}`}
      >
        <RotateCw className="h-3 w-3" aria-hidden="true" />
        Restart required
      </span>
    );
  }

  if (isUpdateInProgress(status)) {
    return (
      <span
        className={`inline-flex items-center gap-1 rounded-[var(--radius-chip)] bg-[var(--color-signal)]/15 px-1.5 py-0.5 text-xs font-medium text-[var(--color-signal)] ${className}`}
      >
        <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
        {updateStatusLabel(status)}
      </span>
    );
  }

  return null;
}
