import { useState } from "react";
import { apiFetch } from "@/lib/api";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input, Label } from "@/components/ui/Field";
import { useToast } from "@/components/ui/toastContext";
import type { Agent } from "@/types";

interface UpdateAllModalProps {
  /** Connected agents this call will target (already filtered to outdated
   *  ones by the caller) — shown so the admin knows exactly which hosts
   *  are affected before confirming. */
  affectedAgents: Agent[];
  defaultVersion: string;
  onClose: () => void;
}

/** Confirmation modal for POST /api/custom/agents/update-all (admin-only
 *  "Update all outdated" toolbar action on the Agents page). Always sends
 *  only_outdated: true — the affected list this modal shows IS the
 *  only_outdated filter applied client-side for display, so the two stay
 *  in sync by construction. */
export function UpdateAllModal({ affectedAgents, defaultVersion, onClose }: UpdateAllModalProps) {
  const { showToast } = useToast();
  const [version, setVersion] = useState(defaultVersion);
  const [submitting, setSubmitting] = useState(false);

  const handleConfirm = async () => {
    const target = version.trim();
    if (!target) return;
    setSubmitting(true);
    try {
      const res = await apiFetch("/api/custom/agents/update-all", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version: target, only_outdated: true }),
      });
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as { error?: string };
        throw new Error(data.error || `Request failed with status ${res.status}`);
      }
      const data = (await res.json()) as { results?: { ok: boolean }[] };
      const results = data.results ?? [];
      const failed = results.filter((r) => !r.ok).length;
      if (results.length === 0) {
        showToast("No connected agents matched — nothing to update", "success");
      } else if (failed > 0) {
        showToast(
          `Update requested for ${results.length} agent(s), ${failed} failed to acknowledge`,
          "error",
        );
      } else {
        showToast(`Update to ${target} requested for ${results.length} agent(s)`, "success");
      }
      onClose();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to request updates", "error");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal title="Update all outdated agents" onClose={onClose}>
      <div className="space-y-4 p-6">
        <div>
          <Label htmlFor="update-all-version">Target version</Label>
          <Input
            id="update-all-version"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            placeholder="e.g. 0.9.1"
          />
        </div>

        <div>
          <p className="mb-1.5 text-xs font-semibold text-[var(--color-ink-muted)]">
            {affectedAgents.length === 0
              ? "No connected agents are currently behind this version."
              : `This will update ${affectedAgents.length} connected host(s):`}
          </p>
          {affectedAgents.length > 0 && (
            <ul className="max-h-40 space-y-1 overflow-y-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-3 text-sm text-[var(--color-ink-muted)]">
              {affectedAgents.map((a) => (
                <li key={a.id}>
                  {a.hostname || a.name}{" "}
                  <span className="text-[var(--color-ink-faint)]">(v{a.version || "0.0.0"})</span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <p className="text-xs text-[var(--color-ink-faint)]">
          Each agent downloads, verifies, and installs this version, then restarts itself, briefly
          disconnecting in the process.
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => void handleConfirm()}
            disabled={submitting || !version.trim() || affectedAgents.length === 0}
          >
            {submitting ? "Requesting…" : `Update ${affectedAgents.length || ""} agent(s)`}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
