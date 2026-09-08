import { useState, useEffect, useCallback, useMemo } from "react";
import { Eye, Send } from "lucide-react";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Input, Label, FieldCaption } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { Modal } from "@/components/ui/Modal";
import { useToast } from "@/components/ui/toastContext";
import { upsertSetting, loadSettingsMap, asBool, asString, asNumber } from "@/lib/settings";
import type { NotificationChannel, ReportPreview, ReportSendResponse } from "@/types";

const CRON_PRESETS = [
  { label: "Monday 08:00", value: "0 8 * * 1" },
  { label: "Daily 08:00", value: "0 8 * * *" },
] as const;

interface WeeklyReportSettingsProps {
  canEdit: boolean;
}

/** Admin section for the scheduled weekly fleet email report. See
 *  internal/hub/report and the README's "Weekly report" section. */
export function WeeklyReportSettings({ canEdit }: WeeklyReportSettingsProps) {
  const { showToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const [enabled, setEnabled] = useState(false);
  const [cron, setCron] = useState("0 8 * * 1");
  const [customCron, setCustomCron] = useState(false);
  const [periodDays, setPeriodDays] = useState(7);
  const [channelIds, setChannelIds] = useState<string[]>([]);
  const [emailChannels, setEmailChannels] = useState<NotificationChannel[]>([]);

  const [previewOpen, setPreviewOpen] = useState(false);
  const [preview, setPreview] = useState<ReportPreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [sending, setSending] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [settings, channels] = await Promise.all([
        loadSettingsMap(),
        pb.collection("notification_channels").getFullList<NotificationChannel>({
          sort: "name",
          filter: "type = 'email'",
        }),
      ]);
      setEnabled(asBool(settings.report_enabled, false));
      const savedCron = asString(settings.report_cron, "0 8 * * 1");
      setCron(savedCron);
      setCustomCron(!CRON_PRESETS.some((p) => p.value === savedCron));
      setPeriodDays(asNumber(settings.report_period_days, 7));
      const rawChannelIds = settings.report_channel_ids;
      setChannelIds(Array.isArray(rawChannelIds) ? (rawChannelIds as string[]) : []);
      setEmailChannels(channels);
    } catch {
      // Keep defaults.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggleChannel = (id: string) => {
    setChannelIds((prev) => (prev.includes(id) ? prev.filter((c) => c !== id) : [...prev, id]));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await Promise.all([
        upsertSetting("report_enabled", enabled),
        upsertSetting("report_cron", cron),
        upsertSetting("report_period_days", periodDays),
        upsertSetting("report_channel_ids", channelIds),
      ]);
      showToast("Weekly report settings saved", "success");
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : "Failed to save weekly report settings",
        "error",
      );
    } finally {
      setSaving(false);
    }
  };

  const handlePreview = async () => {
    setPreviewLoading(true);
    setPreviewOpen(true);
    try {
      const response = await apiFetch(`/api/custom/reports/preview?period_days=${periodDays}`);
      if (!response.ok) throw new Error(`Request failed with status ${response.status}`);
      setPreview((await response.json()) as ReportPreview);
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to build report preview", "error");
      setPreviewOpen(false);
    } finally {
      setPreviewLoading(false);
    }
  };

  const handleSendNow = async () => {
    setSending(true);
    try {
      const response = await apiFetch("/api/custom/reports/send", { method: "POST" });
      if (!response.ok) throw new Error(`Request failed with status ${response.status}`);
      const body = (await response.json()) as ReportSendResponse;
      if (body.results.length === 0) {
        showToast("No report channels configured", "error");
      } else {
        const failures = body.results.filter((r) => !r.success);
        if (failures.length === 0) {
          showToast(`Report sent to ${body.results.length} channel(s)`, "success");
        } else {
          showToast(
            `Report sent with ${failures.length} failure(s): ${failures[0]?.error ?? "unknown error"}`,
            "error",
          );
        }
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to send report", "error");
    } finally {
      setSending(false);
    }
  };

  const previewIframeSrcDoc = useMemo(() => preview?.html ?? "", [preview]);

  return (
    <Panel>
      <PanelHeader
        title="Weekly report"
        description="A scheduled fleet summary email — alerts, resource peaks, checks, CVEs, and log volume."
        actions={
          canEdit && (
            <Button variant="primary" size="sm" onClick={handleSave} disabled={saving || loading}>
              {saving ? "Saving…" : "Save"}
            </Button>
          )
        }
      />
      <PanelBody className="space-y-4">
        <div className="flex items-center gap-2">
          <Toggle
            checked={enabled}
            onChange={setEnabled}
            disabled={loading || !canEdit}
            label="Enable weekly report"
          />
          <span className="text-sm text-[var(--color-ink)]">Enable scheduled sending</span>
        </div>

        <div>
          <FieldCaption>Schedule</FieldCaption>
          <div className="flex flex-wrap items-center gap-2">
            {CRON_PRESETS.map((preset) => (
              <button
                key={preset.value}
                type="button"
                onClick={() => {
                  setCron(preset.value);
                  setCustomCron(false);
                }}
                disabled={loading || !canEdit}
                className={`rounded-[var(--radius-control)] px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50 ${
                  !customCron && cron === preset.value
                    ? "border border-[var(--color-signal)]/30 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-ink-muted)]"
                }`}
              >
                {preset.label}
              </button>
            ))}
            <button
              type="button"
              onClick={() => setCustomCron(true)}
              disabled={loading || !canEdit}
              className={`rounded-[var(--radius-control)] px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50 ${
                customCron
                  ? "border border-[var(--color-signal)]/30 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                  : "border border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-ink-muted)]"
              }`}
            >
              Custom
            </button>
            {customCron && (
              <Input
                value={cron}
                onChange={(e) => setCron(e.target.value)}
                disabled={loading || !canEdit}
                placeholder="0 8 * * 1"
                className="w-40 font-mono"
                aria-label="Custom cron expression"
              />
            )}
          </div>
          <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
            Standard 5-field cron syntax, evaluated in the hub's local timezone.
          </p>
        </div>

        <div>
          <Label htmlFor="report-period-days">Period (days)</Label>
          <Input
            id="report-period-days"
            type="number"
            min={1}
            max={90}
            value={periodDays}
            onChange={(e) => setPeriodDays(Math.max(1, parseInt(e.target.value) || 7))}
            disabled={loading || !canEdit}
            className="max-w-24"
          />
        </div>

        <div>
          <FieldCaption>Channels (email only)</FieldCaption>
          {emailChannels.length === 0 ? (
            <p className="text-xs text-[var(--color-ink-faint)]">
              No email channels configured. Add one in Notifications.
            </p>
          ) : (
            <div className="space-y-2">
              {emailChannels.map((ch) => (
                <label key={ch.id} className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={channelIds.includes(ch.id)}
                    onChange={() => toggleChannel(ch.id)}
                    disabled={!canEdit}
                    className="rounded border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                  />
                  <span className="text-sm text-[var(--color-ink)]">{ch.name}</span>
                </label>
              ))}
            </div>
          )}
        </div>

        <div className="flex flex-wrap gap-2 border-t border-[var(--color-line)] pt-4">
          <Button variant="secondary" size="sm" onClick={handlePreview} disabled={previewLoading}>
            <Eye className="h-4 w-4" aria-hidden="true" />
            {previewLoading ? "Building…" : "Preview"}
          </Button>
          {canEdit && (
            <Button variant="accent" size="sm" onClick={handleSendNow} disabled={sending}>
              <Send className="h-4 w-4" aria-hidden="true" />
              {sending ? "Sending…" : "Send now"}
            </Button>
          )}
        </div>
      </PanelBody>

      {previewOpen && (
        <Modal
          title={preview?.subject ?? "Weekly report preview"}
          onClose={() => setPreviewOpen(false)}
          maxWidth="max-w-3xl"
        >
          <div className="p-4">
            {previewLoading ? (
              <p className="text-sm text-[var(--color-ink-muted)]">Building report…</p>
            ) : (
              <iframe
                title="Weekly report preview"
                srcDoc={previewIframeSrcDoc}
                className="h-[70vh] w-full rounded-[var(--radius-control)] border border-[var(--color-line)] bg-white"
              />
            )}
          </div>
        </Modal>
      )}
    </Panel>
  );
}
