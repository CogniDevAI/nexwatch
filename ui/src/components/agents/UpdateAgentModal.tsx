import { useState } from "react";
import { apiFetch } from "@/lib/api";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input, Label } from "@/components/ui/Field";
import { useToast } from "@/components/ui/toastContext";
import type { Agent } from "@/types";

interface UpdateAgentModalProps {
  agent: Agent;
  /** Pre-filled target version: the "agent_target_version" setting when
   *  set, else the latest known release — the operator can still edit it. */
  defaultVersion: string;
  onClose: () => void;
}

/** Confirmation modal for POST /api/custom/agents/{id}/update — opened
 *  from the Agents table's per-row "Update" action and the host detail
 *  header's matching action (operator+). The agent's live update_status
 *  chip (UpdateStatusChip) picks up progress via the shared agents
 *  realtime subscription (DESIGN.md §10) — this modal never polls or
 *  patches local state itself, the same deferral AckControl already uses. */
export function UpdateAgentModal({ agent, defaultVersion, onClose }: UpdateAgentModalProps) {
  const { showToast } = useToast();
  const [version, setVersion] = useState(defaultVersion);
  const [submitting, setSubmitting] = useState(false);

  const handleConfirm = async () => {
    const target = version.trim();
    if (!target) return;
    setSubmitting(true);
    try {
      const res = await apiFetch(`/api/custom/agents/${agent.id}/update`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version: target }),
      });
      const data = (await res.json().catch(() => ({}))) as { ok?: boolean; error?: string };
      if (!res.ok || !data.ok) {
        throw new Error(data.error || `Request failed with status ${res.status}`);
      }
      showToast(`Update to ${target} requested for ${agent.hostname || agent.name}`, "success");
      onClose();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to request update", "error");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal title={`Update ${agent.hostname || agent.name}`} onClose={onClose}>
      <div className="space-y-4 p-6">
        <div>
          <Label htmlFor="update-agent-version">Target version</Label>
          <Input
            id="update-agent-version"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            placeholder="e.g. 0.9.1"
            onKeyDown={(e) => {
              if (e.key === "Enter") void handleConfirm();
            }}
          />
        </div>
        <p className="text-xs text-[var(--color-ink-faint)]">
          The agent downloads, verifies, and installs this version, then restarts itself — it will
          briefly disconnect during the restart. Signature verification is enforced by the agent's
          own local configuration.
        </p>
        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => void handleConfirm()}
            disabled={submitting || !version.trim()}
          >
            {submitting ? "Requesting…" : "Update agent"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
