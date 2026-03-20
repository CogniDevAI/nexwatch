import { useState, useEffect, useCallback } from "react";
import { Save, Check, AlertTriangle } from "lucide-react";
import pb from "@/lib/pocketbase";

const RETENTION_OPTIONS = [
  { label: "7 days", value: 7 },
  { label: "14 days", value: 14 },
  { label: "30 days", value: 30 },
  { label: "60 days", value: 60 },
  { label: "90 days", value: 90 },
];

interface SettingsRecord {
  id: string;
  key: string;
  value: string;
}

export function Settings() {
  const [retentionDays, setRetentionDays] = useState(30);
  const [collectionInterval, setCollectionInterval] = useState(10);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [feedback, setFeedback] = useState<{
    type: "success" | "error";
    message: string;
  } | null>(null);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const records = await pb
        .collection("settings")
        .getFullList<SettingsRecord>({});

      for (const record of records) {
        switch (record.key) {
          case "retention_days": {
            const parsed = parseInt(record.value);
            if (!isNaN(parsed)) setRetentionDays(parsed);
            break;
          }
          case "collection_interval": {
            const parsed = parseInt(record.value);
            if (!isNaN(parsed)) setCollectionInterval(parsed);
            break;
          }
        }
      }
    } catch {
      // Settings collection may not have entries yet — use defaults.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  const handleSave = async () => {
    setSaving(true);
    setFeedback(null);

    try {
      // Upsert retention_days.
      await upsertSetting("retention_days", String(retentionDays));
      // Upsert collection_interval.
      await upsertSetting("collection_interval", String(collectionInterval));

      setFeedback({
        type: "success",
        message: "Settings saved successfully.",
      });
      // Auto-clear feedback after 3s.
      setTimeout(() => setFeedback(null), 3000);
    } catch (err) {
      setFeedback({
        type: "error",
        message:
          err instanceof Error ? err.message : "Failed to save settings.",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <h2 className="text-2xl font-semibold mb-6">Settings</h2>

      {/* Feedback banner */}
      {feedback && (
        <div
          className={`mb-6 px-4 py-3 rounded-lg border text-sm flex items-center gap-2 ${
            feedback.type === "success"
              ? "bg-[var(--color-accent-green)]/10 border-[var(--color-accent-green)]/20 text-[var(--color-accent-green)]"
              : "bg-[var(--color-accent-red)]/10 border-[var(--color-accent-red)]/20 text-[var(--color-accent-red)]"
          }`}
        >
          {feedback.type === "success" ? (
            <Check className="w-4 h-4 flex-shrink-0" />
          ) : (
            <AlertTriangle className="w-4 h-4 flex-shrink-0" />
          )}
          {feedback.message}
        </div>
      )}

      <div className="space-y-6">
        {/* Data Retention Settings */}
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
          <div className="px-6 py-4 border-b border-[var(--color-border-default)]">
            <h3 className="text-lg font-medium">Data Retention</h3>
            <p className="text-sm text-[var(--color-text-muted)] mt-1">
              Configure how long metric data is retained before being purged.
            </p>
          </div>
          <div className="p-6 space-y-5">
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-2">
                Retention Period
              </label>
              <div className="flex flex-wrap gap-2">
                {RETENTION_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    onClick={() => setRetentionDays(opt.value)}
                    disabled={loading}
                    className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                      retentionDays === opt.value
                        ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)] border border-[var(--color-accent-cyan)]/30"
                        : "bg-[var(--color-bg-primary)] text-[var(--color-text-secondary)] border border-[var(--color-border-default)] hover:border-[var(--color-border-default)] hover:text-[var(--color-text-primary)]"
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
              <p className="text-xs text-[var(--color-text-muted)] mt-2">
                Raw metrics older than this will be automatically downsampled
                and eventually purged. Downsampled data (1m, 5m, 1h
                aggregations) is retained for longer periods.
              </p>
            </div>

            {/* Retention slider for visual feedback */}
            <div>
              <input
                type="range"
                min={7}
                max={90}
                step={1}
                value={retentionDays}
                onChange={(e) => setRetentionDays(parseInt(e.target.value))}
                disabled={loading}
                className="w-full max-w-md accent-[var(--color-accent-cyan)]"
              />
              <p className="text-sm text-[var(--color-text-primary)] mt-1">
                <span className="font-mono font-medium">
                  {retentionDays}
                </span>{" "}
                days
              </p>
            </div>
          </div>
        </div>

        {/* General Settings */}
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
          <div className="px-6 py-4 border-b border-[var(--color-border-default)]">
            <h3 className="text-lg font-medium">General</h3>
            <p className="text-sm text-[var(--color-text-muted)] mt-1">
              General platform configuration options.
            </p>
          </div>
          <div className="p-6 space-y-4">
            <div>
              <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                Default Collection Interval (seconds)
              </label>
              <input
                type="number"
                value={collectionInterval}
                onChange={(e) =>
                  setCollectionInterval(
                    Math.max(1, parseInt(e.target.value) || 10),
                  )
                }
                min={1}
                max={300}
                disabled={loading}
                className="w-full max-w-xs px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
              />
              <p className="text-xs text-[var(--color-text-muted)] mt-1">
                Recommended interval for new agents. Each agent can override
                this in its local config.
              </p>
            </div>
          </div>
        </div>

        {/* Save button */}
        <div className="flex justify-end">
          <button
            onClick={handleSave}
            disabled={saving || loading}
            className="flex items-center gap-2 px-6 py-2.5 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
          >
            <Save className="w-4 h-4" />
            {saving ? "Saving..." : "Save Settings"}
          </button>
        </div>
      </div>
    </div>
  );
}

/** Upsert a setting: find existing by key, update or create. */
async function upsertSetting(key: string, value: string): Promise<void> {
  try {
    // Try to find existing setting.
    const existing = await pb
      .collection("settings")
      .getFirstListItem<{ id: string }>(`key = '${key}'`);
    // Update existing.
    await pb.collection("settings").update(existing.id, { value });
  } catch {
    // Not found — create new.
    await pb.collection("settings").create({ key, value });
  }
}
