import { useState, useEffect, useCallback } from "react";
import {
  Plus,
  Pencil,
  Trash2,
  X,
  Send,
  Mail,
  Globe,
  MessageCircle,
} from "lucide-react";
import type { NotificationChannel } from "@/types";
import pb from "@/lib/pocketbase";

type ChannelType = NotificationChannel["type"];

const CHANNEL_TYPES: { value: ChannelType; label: string; icon: typeof Mail }[] = [
  { value: "email", label: "Email (SMTP)", icon: Mail },
  { value: "webhook", label: "Webhook", icon: Globe },
  { value: "telegram", label: "Telegram", icon: MessageCircle },
  { value: "discord", label: "Discord", icon: MessageCircle },
];

interface ConfigField {
  key: string;
  label: string;
  type: "text" | "number" | "password";
  placeholder: string;
  required: boolean;
}

const CONFIG_FIELDS: Record<ChannelType, ConfigField[]> = {
  email: [
    { key: "host", label: "SMTP Host", type: "text", placeholder: "smtp.gmail.com", required: true },
    { key: "port", label: "Port", type: "number", placeholder: "587", required: true },
    { key: "from", label: "From Address", type: "text", placeholder: "alerts@example.com", required: true },
    { key: "to", label: "To Address", type: "text", placeholder: "admin@example.com", required: true },
    { key: "username", label: "Username", type: "text", placeholder: "user@example.com", required: false },
    { key: "password", label: "Password", type: "password", placeholder: "app password", required: false },
  ],
  webhook: [
    { key: "url", label: "Webhook URL", type: "text", placeholder: "https://example.com/webhook", required: true },
    { key: "method", label: "HTTP Method", type: "text", placeholder: "POST", required: false },
    { key: "headers", label: "Headers (JSON)", type: "text", placeholder: '{"Authorization": "Bearer ..."}', required: false },
  ],
  telegram: [
    { key: "bot_token", label: "Bot Token", type: "password", placeholder: "123456:ABC-DEF...", required: true },
    { key: "chat_id", label: "Chat ID", type: "text", placeholder: "-1001234567890", required: true },
  ],
  discord: [
    { key: "webhook_url", label: "Webhook URL", type: "text", placeholder: "https://discord.com/api/webhooks/...", required: true },
  ],
};

export function NotificationChannels() {
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ id: string; success: boolean; message: string } | null>(null);

  const fetchChannels = useCallback(async () => {
    setLoading(true);
    try {
      const records = await pb
        .collection("notification_channels")
        .getFullList<NotificationChannel>({ sort: "-created" });
      setChannels(records);
    } catch {
      // Handle silently.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchChannels();
  }, [fetchChannels]);

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("notification_channels").delete(id);
      setChannels((prev) => prev.filter((c) => c.id !== id));
      setDeleteConfirm(null);
    } catch {
      // Handle silently.
    }
  };

  const handleTest = async (channel: NotificationChannel) => {
    setTesting(channel.id);
    setTestResult(null);
    try {
      const response = await fetch(
        `${pb.baseURL}/api/custom/notifications/${channel.id}/test`,
        {
          method: "POST",
          headers: {
            Authorization: pb.authStore.token ?? "",
          },
        },
      );
      const data = await response.json();
      if (response.ok) {
        setTestResult({ id: channel.id, success: true, message: "Test notification sent!" });
      } else {
        setTestResult({
          id: channel.id,
          success: false,
          message: data.error ?? "Test failed",
        });
      }
    } catch (err) {
      setTestResult({
        id: channel.id,
        success: false,
        message: err instanceof Error ? err.message : "Network error",
      });
    } finally {
      setTesting(null);
    }
  };

  const handleEdit = (channel: NotificationChannel) => {
    setEditingChannel(channel);
    setShowForm(true);
  };

  const handleFormSave = () => {
    setShowForm(false);
    setEditingChannel(null);
    fetchChannels();
  };

  const handleFormClose = () => {
    setShowForm(false);
    setEditingChannel(null);
  };

  const typeIcon = (type: ChannelType) => {
    const found = CHANNEL_TYPES.find((t) => t.value === type);
    return found ? found.icon : Globe;
  };

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Notification Channels</h2>

      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        <div className="px-6 py-4 border-b border-[var(--color-border-default)] flex items-center justify-between">
          <h3 className="text-lg font-medium">Channels</h3>
          <button
            onClick={() => {
              setEditingChannel(null);
              setShowForm(true);
            }}
            className="flex items-center gap-2 px-4 py-2 bg-[var(--color-accent-purple)] text-white text-sm font-medium rounded-lg hover:opacity-90 transition-opacity"
          >
            <Plus className="w-4 h-4" />
            Add Channel
          </button>
        </div>

        {loading ? (
          <div className="p-6 text-center">
            <p className="text-sm text-[var(--color-text-muted)]">Loading channels...</p>
          </div>
        ) : channels.length === 0 ? (
          <div className="p-6">
            <p className="text-sm text-[var(--color-text-secondary)]">
              No notification channels configured. Add email, webhook, Telegram, or Discord to receive alerts.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-[var(--color-border-muted)]">
            {channels.map((channel) => {
              const Icon = typeIcon(channel.type);
              return (
                <div
                  key={channel.id}
                  className="px-6 py-4 flex items-center gap-4 hover:bg-[var(--color-bg-elevated)]/50"
                >
                  <div className="flex-shrink-0 w-10 h-10 rounded-lg bg-[var(--color-bg-elevated)] flex items-center justify-center">
                    <Icon className="w-5 h-5 text-[var(--color-accent-purple)]" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-medium text-[var(--color-text-primary)] truncate">
                        {channel.name}
                      </p>
                      <span className="px-2 py-0.5 rounded bg-[var(--color-bg-elevated)] text-[var(--color-text-muted)] text-xs">
                        {channel.type}
                      </span>
                      <span
                        className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                          channel.enabled
                            ? "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]"
                            : "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]"
                        }`}
                      >
                        {channel.enabled ? "Active" : "Disabled"}
                      </span>
                    </div>
                    {testResult?.id === channel.id && (
                      <p
                        className={`text-xs mt-1 ${
                          testResult.success
                            ? "text-[var(--color-accent-green)]"
                            : "text-[var(--color-accent-red)]"
                        }`}
                      >
                        {testResult.message}
                      </p>
                    )}
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => handleTest(channel)}
                      disabled={testing === channel.id}
                      className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-green)] hover:bg-[var(--color-accent-green)]/10 disabled:opacity-50"
                      title="Send test notification"
                    >
                      <Send className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => handleEdit(channel)}
                      className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-cyan)] hover:bg-[var(--color-accent-cyan)]/10"
                      title="Edit channel"
                    >
                      <Pencil className="w-4 h-4" />
                    </button>
                    {deleteConfirm === channel.id ? (
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => handleDelete(channel.id)}
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
                        onClick={() => setDeleteConfirm(channel.id)}
                        className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-red)] hover:bg-[var(--color-accent-red)]/10"
                        title="Delete channel"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Channel Form Modal */}
      {showForm && (
        <ChannelForm
          channel={editingChannel}
          onSave={handleFormSave}
          onClose={handleFormClose}
        />
      )}
    </div>
  );
}

// --- Channel Form Component ---

interface ChannelFormProps {
  channel?: NotificationChannel | null;
  onSave: () => void;
  onClose: () => void;
}

function ChannelForm({ channel, onSave, onClose }: ChannelFormProps) {
  const [name, setName] = useState(channel?.name ?? "");
  const [channelType, setChannelType] = useState<ChannelType>(channel?.type ?? "email");
  const [enabled, setEnabled] = useState(channel?.enabled ?? true);
  const [configValues, setConfigValues] = useState<Record<string, string>>(() => {
    if (!channel?.config) return {};
    const vals: Record<string, string> = {};
    for (const [k, v] of Object.entries(channel.config)) {
      vals[k] = typeof v === "object" ? JSON.stringify(v) : String(v ?? "");
    }
    return vals;
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fields = CONFIG_FIELDS[channelType] ?? [];

  const handleConfigChange = (key: string, value: string) => {
    setConfigValues((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);

    // Build config object from form values.
    const config: Record<string, unknown> = {};
    for (const field of fields) {
      const val = configValues[field.key] ?? "";
      if (field.type === "number") {
        config[field.key] = parseInt(val) || 0;
      } else if (field.key === "headers" && val) {
        try {
          config[field.key] = JSON.parse(val);
        } catch {
          config[field.key] = val;
        }
      } else {
        config[field.key] = val;
      }
    }

    const data = {
      name,
      type: channelType,
      config: JSON.stringify(config),
      enabled,
    };

    try {
      if (channel) {
        await pb.collection("notification_channels").update(channel.id, data);
      } else {
        await pb.collection("notification_channels").create(data);
      }
      onSave();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save channel");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-lg mx-4 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-[var(--color-border-default)]">
          <h3 className="text-lg font-semibold">
            {channel ? "Edit Channel" : "New Notification Channel"}
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
              Channel Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="e.g., Ops Team Discord"
              className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
            />
          </div>

          {/* Type */}
          <div>
            <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
              Channel Type
            </label>
            <select
              value={channelType}
              onChange={(e) => {
                setChannelType(e.target.value as ChannelType);
                setConfigValues({});
              }}
              disabled={!!channel} // Don't allow type change on edit.
              className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)] disabled:opacity-60"
            >
              {CHANNEL_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {ct.label}
                </option>
              ))}
            </select>
          </div>

          {/* Dynamic Config Fields */}
          <div className="space-y-3">
            <p className="text-xs font-medium text-[var(--color-text-muted)] uppercase tracking-wider">
              Configuration
            </p>
            {fields.map((field) => (
              <div key={field.key}>
                <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                  {field.label}
                  {field.required && (
                    <span className="text-[var(--color-accent-red)] ml-0.5">*</span>
                  )}
                </label>
                <input
                  type={field.type}
                  value={configValues[field.key] ?? ""}
                  onChange={(e) => handleConfigChange(field.key, e.target.value)}
                  required={field.required}
                  placeholder={field.placeholder}
                  className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
                />
              </div>
            ))}
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
              className="px-4 py-2 bg-[var(--color-accent-purple)] text-white text-sm font-medium rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {saving ? "Saving..." : channel ? "Update Channel" : "Create Channel"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
