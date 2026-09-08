import { useState, useEffect, useCallback, useMemo } from "react";
import { Plus, Pencil, Trash2, Bell } from "lucide-react";
import type { AlertRule } from "@/types";
import pb from "@/lib/pocketbase";
import { formatDuration } from "@/lib/time";
import { ruleSummary, ruleTargetingSummary, ruleEscalationSummary } from "@/lib/alertRules";
import { AlertRuleForm } from "@/components/alerts/AlertRuleForm";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, PanelHeader } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { Toggle } from "@/components/ui/Toggle";
import { Button, IconButton } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { usePageTitle } from "@/hooks/usePageTitle";

export function Alerts() {
  usePageTitle("Alert rules");

  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  const fetchRules = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const records = await pb.collection("alert_rules").getFullList<AlertRule>({
        sort: "-created",
      });
      setRules(records);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load alert rules");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchRules();
  }, [fetchRules]);

  // Hostnames/check names for the targeting-summary column — read from the
  // shared stores AppShell already fetches and subscribes once per session
  // (see DESIGN.md §10) rather than issuing a second full getFullList of
  // either collection just for this page's lookup maps.
  const agents = useAgentStore((s) => s.agents);
  const checks = useChecksStore((s) => s.checks);
  const agentsById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const checksById = useMemo(() => new Map(checks.map((c) => [c.id, c])), [checks]);

  const handleToggleEnabled = async (rule: AlertRule) => {
    try {
      await pb.collection("alert_rules").update(rule.id, { enabled: !rule.enabled });
      setRules((prev) => prev.map((r) => (r.id === rule.id ? { ...r, enabled: !r.enabled } : r)));
    } catch {
      // Handle silently.
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("alert_rules").delete(id);
      setRules((prev) => prev.filter((r) => r.id !== id));
      setDeleteConfirm(null);
    } catch {
      // Handle silently.
    }
  };

  const handleEdit = (rule: AlertRule) => {
    setEditingRule(rule);
    setShowForm(true);
  };

  const handleFormSave = () => {
    setShowForm(false);
    setEditingRule(null);
    void fetchRules();
  };

  const handleFormClose = () => {
    setShowForm(false);
    setEditingRule(null);
  };

  return (
    <div>
      <PageHeader
        title="Alert rules"
        actions={
          canManage && (
            <Button
              variant="primary"
              onClick={() => {
                setEditingRule(null);
                setShowForm(true);
              }}
            >
              <Plus className="h-4 w-4" aria-hidden="true" />
              Add rule
            </Button>
          )
        }
      />

      <Panel>
        <PanelHeader title="Rules" />

        {loading ? (
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load alert rules"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={() => void fetchRules()}>
                Try again
              </Button>
            }
          />
        ) : rules.length === 0 ? (
          <EmptyState
            icon={Bell}
            title="No alert rules configured yet"
            description="Create your first rule to get notified when something goes wrong."
          />
        ) : (
          <Table>
            <thead>
              <tr className="border-b border-[var(--color-line)]">
                <Th>Name</Th>
                <Th>Rule</Th>
                <Th>Duration</Th>
                <Th>Targeting</Th>
                <Th>Severity</Th>
                <Th>Escalation</Th>
                <Th>Enabled</Th>
                <Th align="right">Actions</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {rules.map((rule, idx) => (
                <tr key={rule.id} className={rowClass(idx)}>
                  <Td className="font-medium">{rule.name}</Td>
                  <Td>
                    <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-2 py-0.5 font-mono text-xs text-[var(--color-ink-muted)]">
                      {ruleSummary(rule)}
                    </span>
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">{formatDuration(rule.duration)}</Td>
                  <Td className="text-[var(--color-ink-muted)]">
                    {ruleTargetingSummary(rule, agentsById, checksById)}
                  </Td>
                  <Td>
                    <StatusIndicator
                      status={rule.severity === "critical" ? "critical" : "warning"}
                      label={rule.severity}
                    />
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">{ruleEscalationSummary(rule)}</Td>
                  <Td>
                    <Toggle
                      checked={rule.enabled}
                      disabled={!canManage}
                      onChange={() => handleToggleEnabled(rule)}
                      label={`${rule.enabled ? "Disable" : "Enable"} rule ${rule.name}`}
                    />
                  </Td>
                  <Td align="right">
                    {!canManage ? (
                      <span className="text-xs text-[var(--color-ink-faint)]">—</span>
                    ) : (
                      <div className="flex items-center justify-end gap-1">
                        <IconButton
                          aria-label={`Edit rule ${rule.name}`}
                          onClick={() => handleEdit(rule)}
                        >
                          <Pencil className="h-4 w-4" />
                        </IconButton>
                        {deleteConfirm === rule.id ? (
                          <div className="flex items-center gap-1">
                            <Button
                              size="sm"
                              variant="danger"
                              onClick={() => handleDelete(rule.id)}
                            >
                              Confirm
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => setDeleteConfirm(null)}
                            >
                              Cancel
                            </Button>
                          </div>
                        ) : (
                          <IconButton
                            aria-label={`Delete rule ${rule.name}`}
                            onClick={() => setDeleteConfirm(rule.id)}
                            className="hover:!bg-[var(--color-critical)]/10 hover:!text-[var(--color-critical)]"
                          >
                            <Trash2 className="h-4 w-4" />
                          </IconButton>
                        )}
                      </div>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      {/* Form Modal */}
      {showForm && (
        <AlertRuleForm rule={editingRule} onSave={handleFormSave} onClose={handleFormClose} />
      )}
    </div>
  );
}
