import { useState } from "react";
import { Check } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import { Button } from "@/components/ui/Button";

interface AckControlProps {
  alertId: string;
  acknowledgedAt: string;
  /** Already formatted by the hub as "<user id> (<email>)" — display as-is. */
  acknowledgedBy: string;
  /** Operator role or higher — viewers see the acknowledged state but no
   *  Acknowledge/Undo controls. */
  canManage: boolean;
  className?: string;
}

/**
 * Acknowledge/undo control for one firing alert. Calls the hub's ack/unack
 * endpoints and relies on the caller's existing realtime subscription
 * (alertsStore, or AlertHistory's own "alerts" subscribe) to reflect the
 * resulting record change — it does not patch local state itself, matching
 * how every other mutation in this codebase (Agents delete/create) defers to
 * the shared subscription instead of hand-patching. See DESIGN.md §10.
 */
export function AckControl({
  alertId,
  acknowledgedAt,
  acknowledgedBy,
  canManage,
  className = "",
}: AckControlProps) {
  const [pending, setPending] = useState(false);

  const handleAck = async () => {
    setPending(true);
    try {
      await apiFetch(`/api/custom/alerts/${alertId}/ack`, { method: "POST" });
    } finally {
      setPending(false);
    }
  };

  const handleUnack = async () => {
    setPending(true);
    try {
      await apiFetch(`/api/custom/alerts/${alertId}/unack`, { method: "POST" });
    } finally {
      setPending(false);
    }
  };

  if (!acknowledgedAt) {
    if (!canManage) return null;
    return (
      <Button size="sm" variant="ghost" onClick={() => void handleAck()} disabled={pending}>
        {pending ? "Acknowledging…" : "Acknowledge"}
      </Button>
    );
  }

  return (
    <div className={`flex items-center gap-2 ${className}`}>
      <span
        className="inline-flex items-center gap-1.5 text-xs text-[var(--color-ink-faint)]"
        title={acknowledgedBy}
      >
        <Check className="h-3.5 w-3.5 flex-shrink-0 text-[var(--color-ok)]" aria-hidden="true" />
        Acknowledged by {acknowledgedBy || "someone"} · {timeSince(acknowledgedAt)}
      </span>
      {canManage && (
        <Button size="sm" variant="ghost" onClick={() => void handleUnack()} disabled={pending}>
          {pending ? "…" : "Undo"}
        </Button>
      )}
    </div>
  );
}
