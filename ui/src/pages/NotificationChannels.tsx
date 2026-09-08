import { useState, useEffect, useCallback } from "react";
import {
  Plus,
  Pencil,
  Trash2,
  Send,
  Mail,
  Globe,
  MessageCircle,
  Slack,
  Users,
  Siren,
  BellRing,
  Radio,
  Smartphone,
} from "lucide-react";
import { BrowserNotificationsSettings } from "@/components/settings/BrowserNotificationsSettings";
import type { NotificationChannel } from "@/types";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { useAuthStore } from "@/stores/authStore";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, PanelHeader } from "@/components/ui/Panel";
import { Modal } from "@/components/ui/Modal";
import { Input, Select, Label } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { useToast } from "@/components/ui/toastContext";
import { usePageTitle } from "@/hooks/usePageTitle";

type ChannelType = NotificationChannel["type"];

const CHANNEL_TYPES: { value: ChannelType; label: string; icon: typeof Mail }[] = [
  { value: "email", label: "Email (SMTP)", icon: Mail },
  { value: "webhook", label: "Webhook", icon: Globe },
  { value: "telegram", label: "Telegram", icon: MessageCircle },
  { value: "discord", label: "Discord", icon: MessageCircle },
  { value: "slack", label: "Slack", icon: Slack },
  { value: "teams", label: "Microsoft Teams", icon: Users },
  { value: "pagerduty", label: "PagerDuty", icon: Siren },
  { value: "ntfy", label: "ntfy", icon: BellRing },
  { value: "gotify", label: "Gotify", icon: Radio },
  { value: "webpush", label: "Browser push", icon: Smartphone },
];

interface ConfigField {
  key: string;
  label: string;
  type: "text" | "number" | "password" | "select";
  placeholder: string;
  required: boolean;
  /** One-sentence hint shown under the field, e.g. where to find a value. */
  helpText?: string;
  /** Required when type is "select" — the closed set of choices. */
  options?: { value: string; label: string }[];
  /** Native min/max, honored only when type is "number". */
  min?: number;
  max?: number;
}

const CONFIG_FIELDS: Record<ChannelType, ConfigField[]> = {
  email: [
    {
      key: "host",
      label: "SMTP host",
      type: "text",
      placeholder: "smtp.gmail.com",
      required: true,
    },
    { key: "port", label: "Port", type: "number", placeholder: "587", required: true },
    {
      key: "from",
      label: "From address",
      type: "text",
      placeholder: "alerts@example.com",
      required: true,
    },
    {
      key: "to",
      label: "To address",
      type: "text",
      placeholder: "admin@example.com",
      required: true,
    },
    {
      key: "username",
      label: "Username",
      type: "text",
      placeholder: "user@example.com",
      required: false,
    },
    {
      key: "password",
      label: "Password",
      type: "password",
      placeholder: "app password",
      required: false,
    },
  ],
  webhook: [
    {
      key: "url",
      label: "Webhook URL",
      type: "text",
      placeholder: "https://example.com/webhook",
      required: true,
    },
    { key: "method", label: "HTTP method", type: "text", placeholder: "POST", required: false },
    {
      key: "headers",
      label: "Headers (JSON)",
      type: "text",
      placeholder: '{"Authorization": "Bearer ..."}',
      required: false,
    },
  ],
  telegram: [
    {
      key: "bot_token",
      label: "Bot token",
      type: "password",
      placeholder: "123456:ABC-DEF...",
      required: true,
    },
    {
      key: "chat_id",
      label: "Chat ID",
      type: "text",
      placeholder: "-1001234567890",
      required: true,
    },
  ],
  discord: [
    {
      key: "webhook_url",
      label: "Webhook URL",
      type: "text",
      placeholder: "https://discord.com/api/webhooks/...",
      required: true,
    },
  ],
  slack: [
    {
      key: "webhook_url",
      label: "Webhook URL",
      type: "text",
      placeholder: "https://hooks.slack.com/services/...",
      required: true,
      helpText: "Create an Incoming Webhook under your Slack workspace's app settings.",
    },
  ],
  teams: [
    {
      key: "webhook_url",
      label: "Webhook URL",
      type: "text",
      placeholder: "https://example.webhook.office.com/webhookb2/...",
      required: true,
      helpText: "Add an Incoming Webhook connector to the target Teams channel.",
    },
  ],
  pagerduty: [
    {
      key: "routing_key",
      label: "Integration key",
      type: "password",
      placeholder: "32-character integration key",
      required: true,
      helpText: "The Integration Key from a PagerDuty service's Events API v2 integration.",
    },
  ],
  ntfy: [
    {
      key: "server_url",
      label: "Server URL",
      type: "text",
      placeholder: "https://ntfy.sh",
      required: false,
      helpText: "Leave blank to use the public ntfy.sh server, or set your own instance.",
    },
    {
      key: "topic",
      label: "Topic",
      type: "text",
      placeholder: "nexwatch-alerts",
      required: true,
      helpText: "Anyone who knows this topic name can read it, so pick something hard to guess.",
    },
    {
      key: "token",
      label: "Access token",
      type: "password",
      placeholder: "tk_...",
      required: false,
      helpText: "Only needed for a protected topic on your own ntfy server.",
    },
    {
      key: "priority",
      label: "Priority",
      type: "number",
      placeholder: "1-5",
      required: false,
      min: 1,
      max: 5,
      helpText:
        "1 (min) to 5 (max/urgent). Leave blank to pick a default from the alert's severity: 5 once critical, 3 otherwise, 2 once resolved.",
    },
  ],
  gotify: [
    {
      key: "server_url",
      label: "Server URL",
      type: "text",
      placeholder: "https://gotify.example.com",
      required: true,
      helpText: "The base URL of your self-hosted Gotify server.",
    },
    {
      key: "app_token",
      label: "Application token",
      type: "password",
      placeholder: "A_...",
      required: true,
      helpText: "Create an application in Gotify and paste its generated token.",
    },
    {
      key: "priority",
      label: "Priority",
      type: "number",
      placeholder: "0-10",
      required: false,
      min: 0,
      max: 10,
      helpText:
        "0 (lowest) to 10 (highest). Leave blank to pick a default from the alert's severity: 8 once critical, 5 for warning, 2 once resolved.",
    },
  ],
  webpush: [
    {
      key: "audience",
      label: "Audience",
      type: "select",
      placeholder: "",
      required: false,
      helpText:
        "Who receives this notification, based on each subscribed device's user role. A user must first enable browser notifications for their own device below.",
      options: [
        { value: "all", label: "All users" },
        { value: "admins", label: "Admins only" },
        { value: "operators", label: "Operators only" },
      ],
    },
  ],
};

export function NotificationChannels() {
  usePageTitle("Notifications");

  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const { showToast } = useToast();
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);

  const fetchChannels = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const records = await pb
        .collection("notification_channels")
        .getFullList<NotificationChannel>({ sort: "-created" });
      setChannels(records);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load notification channels");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchChannels();
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
    try {
      const response = await apiFetch(`/api/custom/notifications/${channel.id}/test`, {
        method: "POST",
      });
      const data = await response.json();
      if (response.ok) {
        showToast(`Test notification sent to ${channel.name}`, "success");
      } else {
        showToast(data.error ?? "Test notification failed", "error");
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Network error", "error");
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
    void fetchChannels();
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
      <PageHeader
        title="Notifications"
        actions={
          canManage && (
            <Button
              variant="primary"
              onClick={() => {
                setEditingChannel(null);
                setShowForm(true);
              }}
            >
              <Plus className="h-4 w-4" aria-hidden="true" />
              Add channel
            </Button>
          )
        }
      />

      <div className="space-y-6">
        <BrowserNotificationsSettings />

        <Panel>
          <PanelHeader title="Channels" />

          {loading ? (
            <div className="p-5">
              <Skeleton className="h-32 w-full" />
            </div>
          ) : error ? (
            <ErrorState
              title="Couldn't load notification channels"
              description={error}
              action={
                <Button variant="primary" size="sm" onClick={() => void fetchChannels()}>
                  Try again
                </Button>
              }
            />
          ) : channels.length === 0 ? (
            <EmptyState
              icon={Send}
              title="No notification channels configured"
              description="Add email, Slack, Teams, PagerDuty, Discord, Telegram, ntfy, Gotify, or a webhook to receive alerts."
            />
          ) : (
            <div className="divide-y divide-[var(--color-line-soft)]">
              {channels.map((channel) => {
                const Icon = typeIcon(channel.type);
                return (
                  <div
                    key={channel.id}
                    className="flex items-center gap-4 px-5 py-4 hover:bg-[var(--color-panel-raised)]/50"
                  >
                    <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-[var(--radius-control)] bg-[var(--color-panel-raised)]">
                      <Icon className="h-5 w-5 text-[var(--color-signal)]" aria-hidden="true" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <p className="truncate text-sm font-medium text-[var(--color-ink)]">
                          {channel.name}
                        </p>
                        <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-2 py-0.5 text-xs text-[var(--color-ink-faint)]">
                          {channel.type}
                        </span>
                        <StatusIndicator
                          status={channel.enabled ? "ok" : "offline"}
                          label={channel.enabled ? "Active" : "Disabled"}
                        />
                      </div>
                    </div>
                    <div className="flex items-center gap-1">
                      {!canManage ? (
                        <span className="px-2 text-xs text-[var(--color-ink-faint)]">—</span>
                      ) : (
                        <>
                          <IconButton
                            aria-label={`Send test notification to ${channel.name}`}
                            onClick={() => handleTest(channel)}
                            disabled={testing === channel.id}
                            className="hover:!bg-[var(--color-ok)]/10 hover:!text-[var(--color-ok)]"
                          >
                            <Send className="h-4 w-4" />
                          </IconButton>
                          <IconButton
                            aria-label={`Edit channel ${channel.name}`}
                            onClick={() => handleEdit(channel)}
                          >
                            <Pencil className="h-4 w-4" />
                          </IconButton>
                          {deleteConfirm === channel.id ? (
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="danger"
                                onClick={() => handleDelete(channel.id)}
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
                              aria-label={`Delete channel ${channel.name}`}
                              onClick={() => setDeleteConfirm(channel.id)}
                              className="hover:!bg-[var(--color-critical)]/10 hover:!text-[var(--color-critical)]"
                            >
                              <Trash2 className="h-4 w-4" />
                            </IconButton>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Panel>
      </div>

      {/* Channel Form Modal */}
      {showForm && (
        <ChannelForm channel={editingChannel} onSave={handleFormSave} onClose={handleFormClose} />
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
      // The typeof-object branch above already routes objects through
      // JSON.stringify; this branch only ever sees primitives at runtime,
      // but TS can't narrow `unknown` past a negated typeof-object check.
      // eslint-disable-next-line @typescript-eslint/no-base-to-string -- see comment above
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

  /** The effective value for one config field: whatever the user entered,
   *  or — for a "select" field only — its first option, matching what an
   *  untouched <Select> already displays as selected. Every other field
   *  type has no such implicit default, so it falls back to "". */
  const fieldValue = (field: ConfigField): string =>
    configValues[field.key] || (field.type === "select" ? (field.options?.[0]?.value ?? "") : "");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);

    // Build config object from form values. A "select" field falls back to
    // its first option when the user never touched the control — this must
    // match the Select's own displayed fallback below exactly, or the
    // submitted config silently disagrees with what the form visibly shows
    // as selected (see fieldValue).
    const config: Record<string, unknown> = {};
    for (const field of fields) {
      const val = fieldValue(field);
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
    <Modal title={channel ? "Edit channel" : "New notification channel"} onClose={onClose}>
      <form onSubmit={handleSubmit} className="max-h-[70vh] space-y-4 overflow-y-auto p-6">
        {error && (
          <div
            role="alert"
            className="rounded-[var(--radius-control)] border border-[var(--color-critical)]/25 bg-[var(--color-critical)]/10 px-4 py-2 text-sm text-[var(--color-critical)]"
          >
            {error}
          </div>
        )}

        <div>
          <Label htmlFor="channel-name">Channel name</Label>
          <Input
            id="channel-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder="e.g., Ops team Discord"
          />
        </div>

        <div>
          <Label htmlFor="channel-type">Channel type</Label>
          <Select
            id="channel-type"
            value={channelType}
            onChange={(e) => {
              setChannelType(e.target.value as ChannelType);
              setConfigValues({});
            }}
            disabled={!!channel} // Don't allow type change on edit.
          >
            {CHANNEL_TYPES.map((ct) => (
              <option key={ct.value} value={ct.value}>
                {ct.label}
              </option>
            ))}
          </Select>
        </div>

        <div className="space-y-3">
          <p className="text-sm font-medium text-[var(--color-ink-muted)]">Configuration</p>
          {fields.map((field) => (
            <div key={field.key}>
              <Label htmlFor={`channel-config-${field.key}`} required={field.required}>
                {field.label}
              </Label>
              {field.type === "select" ? (
                <Select
                  id={`channel-config-${field.key}`}
                  value={fieldValue(field)}
                  onChange={(e) => handleConfigChange(field.key, e.target.value)}
                >
                  {field.options?.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </Select>
              ) : (
                <Input
                  id={`channel-config-${field.key}`}
                  type={field.type}
                  value={configValues[field.key] ?? ""}
                  onChange={(e) => handleConfigChange(field.key, e.target.value)}
                  required={field.required}
                  placeholder={field.placeholder}
                  min={field.type === "number" ? field.min : undefined}
                  max={field.type === "number" ? field.max : undefined}
                />
              )}
              {field.helpText && (
                <p className="mt-1 text-xs text-[var(--color-ink-faint)]">{field.helpText}</p>
              )}
            </div>
          ))}
        </div>

        <div className="flex items-center gap-2">
          <Toggle checked={enabled} onChange={setEnabled} label="Channel enabled" />
          <span className="text-sm text-[var(--color-ink-muted)]">Enabled</span>
        </div>

        <div className="flex items-center justify-end gap-3 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Saving…" : channel ? "Save changes" : "Create channel"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
