import { useState, useEffect } from "react";
import { X } from "lucide-react";
import type { AlertRule, Agent, NotificationChannel } from "@/types";
import pb from "@/lib/pocketbase";

interface AlertRuleFormProps {
  rule?: AlertRule | null;
  onSave: () => void;
  onClose: () => void;
}

const METRIC_TYPES = [
  { value: "cpu", label: "CPU" },
  { value: "memory", label: "Memory" },
  { value: "disk", label: "Disk" },
  { value: "network", label: "Network" },
  { value: "docker", label: "Docker" },
];

const CONDITIONS = [
  { value: "gt", label: "Greater than (>)" },
  { value: "lt", label: "Less than (<)" },
  { value: "eq", label: "Equal to (=)" },
];

const SEVERITIES = [
  { value: "warning", label: "Warning" },
  { value: "critical", label: "Critical" },
];

export function AlertRuleForm({ rule, onSave, onClose }: AlertRuleFormProps) {
  const [name, setName] = useState(rule?.name ?? "");
  const [metricType, setMetricType] = useState(rule?.metric_type ?? "cpu");
  const [condition, setCondition] = useState(rule?.condition ?? "gt");
  const [threshold, setThreshold] = useState(rule?.threshold ?? 90);
  const [duration, setDuration] = useState(rule?.duration ?? 300);
  const [severity, setSeverity] = useState(rule?.severity ?? "warning");
  const [agentId, setAgentId] = useState(rule?.agent_id ?? "");
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [selectedChannels, setSelectedChannels] = useState<string[]>(
    rule?.notification_channels ?? [],
  );

  const [agents, setAgents] = useState<Agent[]>([]);
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      try {
        const [agentRecords, channelRecords] = await Promise.all([
          pb.collection("agents").getFullList<Agent>({ sort: "hostname" }),
          pb
            .collection("notification_channels")
            .getFullList<NotificationChannel>({
              sort: "name",
              filter: "enabled = true",
            }),
        ]);
        setAgents(agentRecords);
        setChannels(channelRecords);
      } catch {
        // Silently handle — dropdowns will be empty.
      }
    };
    loadData();
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);

    const data = {
      name,
      metric_type: metricType,
      condition,
      threshold,
      duration,
      severity,
      agent_id: agentId || "",
      enabled,
      notification_channels: selectedChannels,
    };

    try {
      if (rule) {
        await pb.collection("alert_rules").update(rule.id, data);
      } else {
        await pb.collection("alert_rules").create(data);
      }
      onSave();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to save alert rule",
      );
    } finally {
      setSaving(false);
    }
  };

  const toggleChannel = (channelId: string) => {
    setSelectedChannels((prev) =>
      prev.includes(channelId)
        ? prev.filter((id) => id !== channelId)
        : [...prev, channelId],
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-lg mx-4 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-[var(--color-border-default)]">
          <h3 className="text-lg font-semibold">
            {rule ? "Edit Alert Rule" : "New Alert Rule"}
          </h3>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)]"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="p-6 space-y-4 max-h-[70vh] overflow-y-auto">
          {error && (
            <div className="px-4 py-2 rounded-lg bg-[var(--color-accent-red)]/10 border border-[var(--color-accent-red)]/20 text-[var(--color-accent-red)] text-sm">
              {error}
            </div>
          )}

          {/* Name */}
          <div>
            <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
              Rule Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="e.g., High CPU Alert"
              className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
            />
          </div>

          {/* Metric Type + Condition row */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                Metric Type
              </label>
              <select
                value={metricType}
                onChange={(e) => setMetricType(e.target.value as AlertRule["metric_type"])}
                className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
              >
                {METRIC_TYPES.map((mt) => (
                  <option key={mt.value} value={mt.value}>
                    {mt.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                Condition
              </label>
              <select
                value={condition}
                onChange={(e) => setCondition(e.target.value as AlertRule["condition"])}
                className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
              >
                {CONDITIONS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* Threshold + Duration row */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                Threshold
              </label>
              <input
                type="number"
                step="any"
                value={threshold}
                onChange={(e) => setThreshold(parseFloat(e.target.value) || 0)}
                required
                className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                Duration (seconds)
              </label>
              <input
                type="number"
                value={duration}
                onChange={(e) => setDuration(parseInt(e.target.value) || 0)}
                required
                min={0}
                className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
              />
            </div>
          </div>

          {/* Severity */}
          <div>
            <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
              Severity
            </label>
            <select
              value={severity}
              onChange={(e) => setSeverity(e.target.value as AlertRule["severity"])}
              className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
            >
              {SEVERITIES.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </div>

          {/* Agent Selector */}
          <div>
            <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
              Agent (optional — empty = all agents)
            </label>
            <select
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
            >
              <option value="">All agents</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.hostname || a.id}
                </option>
              ))}
            </select>
          </div>

          {/* Notification Channels */}
          <div>
            <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
              Notification Channels
            </label>
            {channels.length === 0 ? (
              <p className="text-xs text-[var(--color-text-muted)]">
                No enabled channels available. Configure them in Settings.
              </p>
            ) : (
              <div className="space-y-2">
                {channels.map((ch) => (
                  <label
                    key={ch.id}
                    className="flex items-center gap-2 cursor-pointer"
                  >
                    <input
                      type="checkbox"
                      checked={selectedChannels.includes(ch.id)}
                      onChange={() => toggleChannel(ch.id)}
                      className="rounded border-[var(--color-border-default)] bg-[var(--color-bg-primary)] text-[var(--color-accent-cyan)] focus:ring-[var(--color-accent-cyan)]"
                    />
                    <span className="text-sm text-[var(--color-text-primary)]">
                      {ch.name}
                    </span>
                    <span className="text-xs px-1.5 py-0.5 rounded bg-[var(--color-bg-elevated)] text-[var(--color-text-muted)]">
                      {ch.type}
                    </span>
                  </label>
                ))}
              </div>
            )}
          </div>

          {/* Enabled Toggle */}
          <div className="flex items-center gap-2">
            <label className="relative inline-flex items-center cursor-pointer">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
                className="sr-only peer"
              />
              <div className="w-9 h-5 bg-[var(--color-bg-elevated)] peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:start-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-[var(--color-accent-cyan)]" />
            </label>
            <span className="text-sm text-[var(--color-text-secondary)]">
              Enabled
            </span>
          </div>

          {/* Actions */}
          <div className="flex items-center justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] rounded-lg hover:bg-[var(--color-bg-elevated)]"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving}
              className="px-4 py-2 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {saving ? "Saving..." : rule ? "Update Rule" : "Create Rule"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
