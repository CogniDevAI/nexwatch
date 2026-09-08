import { useState, useEffect, useCallback } from "react";
import { Save } from "lucide-react";
import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Input, FieldCaption, Label } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/toastContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import { StatusPageSettings } from "@/components/settings/StatusPageSettings";
import { WeeklyReportSettings } from "@/components/settings/WeeklyReportSettings";
import { PrometheusSettings } from "@/components/settings/PrometheusSettings";
import { AgentUpdateSettings } from "@/components/settings/AgentUpdateSettings";

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
  usePageTitle("Settings");

  const hasRole = useAuthStore((s) => s.hasRole);
  const canEditSettings = hasRole("admin");
  const { showToast } = useToast();
  const [retentionDays, setRetentionDays] = useState(30);
  const [collectionInterval, setCollectionInterval] = useState(10);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const records = await pb.collection("settings").getFullList<SettingsRecord>({});

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
    void loadSettings();
  }, [loadSettings]);

  const handleSave = async () => {
    setSaving(true);

    try {
      await upsertSetting("retention_days", String(retentionDays));
      await upsertSetting("collection_interval", String(collectionInterval));
      showToast("Settings saved", "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to save settings", "error");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <PageHeader
        title="Settings"
        actions={
          canEditSettings && (
            <Button variant="primary" onClick={handleSave} disabled={saving || loading}>
              <Save className="h-4 w-4" aria-hidden="true" />
              {saving ? "Saving…" : "Save settings"}
            </Button>
          )
        }
      />

      {!canEditSettings && (
        <div className="mb-6 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] px-4 py-3 text-sm text-[var(--color-ink-muted)]">
          You need the admin role to change these settings.
        </div>
      )}

      <div className="space-y-6">
        {/* Data Retention Settings */}
        <Panel>
          <PanelHeader
            title="Data retention"
            description="Configure how long metric data is retained before being purged."
          />
          <PanelBody className="space-y-4">
            <div>
              <FieldCaption>Retention period</FieldCaption>
              <div className="flex flex-wrap items-center gap-2">
                {RETENTION_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setRetentionDays(opt.value)}
                    disabled={loading || !canEditSettings}
                    className={`rounded-[var(--radius-control)] px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                      retentionDays === opt.value
                        ? "border border-[var(--color-signal)]/30 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                        : "border border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]"
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
                <span className="text-sm text-[var(--color-ink-faint)]">or</span>
                <Input
                  type="number"
                  min={1}
                  max={365}
                  value={retentionDays}
                  onChange={(e) => setRetentionDays(Math.max(1, parseInt(e.target.value) || 1))}
                  disabled={loading || !canEditSettings}
                  aria-label="Custom retention period in days"
                  className="max-w-24"
                />
                <span className="text-sm text-[var(--color-ink-faint)]">days</span>
              </div>
              <p className="mt-2 text-xs text-[var(--color-ink-faint)]">
                Raw metrics older than this will be automatically downsampled and eventually purged.
                Downsampled data (1m, 5m, 1h aggregations) is retained for longer periods.
              </p>
            </div>
          </PanelBody>
        </Panel>

        {/* General Settings */}
        <Panel>
          <PanelHeader title="General" description="General platform configuration options." />
          <PanelBody>
            <Label htmlFor="collection-interval">Default collection interval (seconds)</Label>
            <Input
              id="collection-interval"
              type="number"
              value={collectionInterval}
              onChange={(e) => setCollectionInterval(Math.max(1, parseInt(e.target.value) || 10))}
              min={1}
              max={300}
              disabled={loading || !canEditSettings}
              className="max-w-xs"
            />
            <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
              Recommended interval for new agents. Each agent can override this in its local config.
            </p>
          </PanelBody>
        </Panel>

        <StatusPageSettings canEdit={canEditSettings} />
        <WeeklyReportSettings canEdit={canEditSettings} />
        <PrometheusSettings canEdit={canEditSettings} />
        <AgentUpdateSettings canEdit={canEditSettings} />
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
