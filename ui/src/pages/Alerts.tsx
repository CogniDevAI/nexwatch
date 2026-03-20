import { useState, useEffect, useCallback } from "react";
import { Plus, Pencil, Trash2 } from "lucide-react";
import type { AlertRule } from "@/types";
import pb from "@/lib/pocketbase";
import { AlertRuleForm } from "@/components/alerts/AlertRuleForm";

const conditionLabels: Record<string, string> = {
  gt: ">",
  lt: "<",
  eq: "=",
};

export function Alerts() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  const fetchRules = useCallback(async () => {
    setLoading(true);
    try {
      const records = await pb
        .collection("alert_rules")
        .getFullList<AlertRule>({
          sort: "-created",
        });
      setRules(records);
    } catch {
      // Handle silently.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRules();
  }, [fetchRules]);

  const handleToggleEnabled = async (rule: AlertRule) => {
    try {
      await pb
        .collection("alert_rules")
        .update(rule.id, { enabled: !rule.enabled });
      setRules((prev) =>
        prev.map((r) =>
          r.id === rule.id ? { ...r, enabled: !r.enabled } : r,
        ),
      );
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
    fetchRules();
  };

  const handleFormClose = () => {
    setShowForm(false);
    setEditingRule(null);
  };

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Alert Rules</h2>

      {/* Alert rules list */}
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        <div className="px-6 py-4 border-b border-[var(--color-border-default)] flex items-center justify-between">
          <h3 className="text-lg font-medium">Rules</h3>
          <button
            onClick={() => {
              setEditingRule(null);
              setShowForm(true);
            }}
            className="flex items-center gap-2 px-4 py-2 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity"
          >
            <Plus className="w-4 h-4" />
            Add Rule
          </button>
        </div>

        {loading ? (
          <div className="p-6 text-center">
            <p className="text-sm text-[var(--color-text-muted)]">
              Loading rules...
            </p>
          </div>
        ) : rules.length === 0 ? (
          <div className="p-6">
            <p className="text-sm text-[var(--color-text-secondary)]">
              No alert rules configured yet. Create your first rule to get
              notified when something goes wrong.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-muted)]">
                  <th className="px-6 py-3 text-left font-medium">Name</th>
                  <th className="px-6 py-3 text-left font-medium">Metric</th>
                  <th className="px-6 py-3 text-left font-medium">
                    Condition
                  </th>
                  <th className="px-6 py-3 text-left font-medium">
                    Duration
                  </th>
                  <th className="px-6 py-3 text-left font-medium">
                    Severity
                  </th>
                  <th className="px-6 py-3 text-left font-medium">Enabled</th>
                  <th className="px-6 py-3 text-right font-medium">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => (
                  <tr
                    key={rule.id}
                    className="border-b border-[var(--color-border-muted)] hover:bg-[var(--color-bg-elevated)]/50"
                  >
                    <td className="px-6 py-3 font-medium text-[var(--color-text-primary)]">
                      {rule.name}
                    </td>
                    <td className="px-6 py-3">
                      <span className="px-2 py-0.5 rounded bg-[var(--color-bg-elevated)] text-[var(--color-text-secondary)] text-xs font-mono">
                        {rule.metric_type}
                      </span>
                    </td>
                    <td className="px-6 py-3 text-[var(--color-text-secondary)]">
                      {conditionLabels[rule.condition] ?? rule.condition}{" "}
                      {rule.threshold}
                    </td>
                    <td className="px-6 py-3 text-[var(--color-text-secondary)]">
                      {rule.duration}s
                    </td>
                    <td className="px-6 py-3">
                      <span
                        className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${
                          rule.severity === "critical"
                            ? "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"
                            : "bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]"
                        }`}
                      >
                        {rule.severity}
                      </span>
                    </td>
                    <td className="px-6 py-3">
                      <button
                        onClick={() => handleToggleEnabled(rule)}
                        className="relative inline-flex items-center cursor-pointer"
                      >
                        <div
                          className={`w-9 h-5 rounded-full transition-colors ${
                            rule.enabled
                              ? "bg-[var(--color-accent-cyan)]"
                              : "bg-[var(--color-bg-elevated)]"
                          }`}
                        >
                          <div
                            className={`absolute top-[2px] w-4 h-4 bg-white rounded-full transition-transform ${
                              rule.enabled
                                ? "translate-x-[18px]"
                                : "translate-x-[2px]"
                            }`}
                          />
                        </div>
                      </button>
                    </td>
                    <td className="px-6 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => handleEdit(rule)}
                          className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-cyan)] hover:bg-[var(--color-accent-cyan)]/10"
                          title="Edit rule"
                        >
                          <Pencil className="w-4 h-4" />
                        </button>
                        {deleteConfirm === rule.id ? (
                          <div className="flex items-center gap-1">
                            <button
                              onClick={() => handleDelete(rule.id)}
                              className="px-2 py-1 text-xs rounded bg-[var(--color-accent-red)] text-white"
                            >
                              Confirm
                            </button>
                            <button
                              onClick={() => setDeleteConfirm(null)}
                              className="px-2 py-1 text-xs rounded text-[var(--color-text-muted)] hover:bg-[var(--color-bg-elevated)]"
                            >
                              Cancel
                            </button>
                          </div>
                        ) : (
                          <button
                            onClick={() => setDeleteConfirm(rule.id)}
                            className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-red)] hover:bg-[var(--color-accent-red)]/10"
                            title="Delete rule"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Form Modal */}
      {showForm && (
        <AlertRuleForm
          rule={editingRule}
          onSave={handleFormSave}
          onClose={handleFormClose}
        />
      )}
    </div>
  );
}
