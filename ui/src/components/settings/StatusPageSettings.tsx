import { useState, useEffect, useCallback } from "react";
import { Plus, Trash2, ExternalLink } from "lucide-react";
import pb from "@/lib/pocketbase";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Input, Textarea, Label, FieldCaption, Select } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { useToast } from "@/components/ui/toastContext";
import { upsertSetting, loadSettingsMap, asBool, asString, asNumber } from "@/lib/settings";
import type { Agent, Check, StatusPageItemConfig } from "@/types";

const UPTIME_DAYS_OPTIONS = [7, 30, 60, 90];

interface StatusPageSettingsProps {
  canEdit: boolean;
}

/** Admin section for configuring the public status page (see /status and
 *  GET /api/public/status). Items are an allowlist: only checks/agents
 *  explicitly added here — with an admin-typed public label — ever appear
 *  on the page. See ui/DESIGN.md § Public status page. */
export function StatusPageSettings({ canEdit }: StatusPageSettingsProps) {
  const { showToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const [enabled, setEnabled] = useState(false);
  const [title, setTitle] = useState("Service status");
  const [description, setDescription] = useState("");
  const [showUptimeDays, setShowUptimeDays] = useState(30);
  const [items, setItems] = useState<StatusPageItemConfig[]>([]);

  const [checks, setChecks] = useState<Check[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [addType, setAddType] = useState<"check" | "agent">("check");
  const [addId, setAddId] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [settings, checkRecords, agentRecords] = await Promise.all([
        loadSettingsMap(),
        pb.collection("checks").getFullList<Check>({ sort: "name" }),
        pb.collection("agents").getFullList<Agent>({ sort: "hostname" }),
      ]);
      setEnabled(asBool(settings.status_page_enabled, false));
      setTitle(asString(settings.status_page_title, "Service status"));
      setDescription(asString(settings.status_page_description, ""));
      setShowUptimeDays(asNumber(settings.status_page_show_uptime_days, 30));
      const rawItems = settings.status_page_items;
      setItems(Array.isArray(rawItems) ? (rawItems as StatusPageItemConfig[]) : []);
      setChecks(checkRecords);
      setAgents(agentRecords);
    } catch {
      // Keep defaults — settings collection may not have entries yet.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const sourceOptions = addType === "check" ? checks : agents;
  const sourceLabel = (id: string): string =>
    addType === "check"
      ? (checks.find((c) => c.id === id)?.name ?? "")
      : (agents.find((a) => a.id === id)?.hostname ?? "");

  const handleAddItem = () => {
    if (!addId) return;
    setItems((prev) => [...prev, { type: addType, id: addId, label: sourceLabel(addId) }]);
    setAddId("");
  };

  const handleRemoveItem = (index: number) => {
    setItems((prev) => prev.filter((_, i) => i !== index));
  };

  const handleLabelChange = (index: number, label: string) => {
    setItems((prev) => prev.map((it, i) => (i === index ? { ...it, label } : it)));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await Promise.all([
        upsertSetting("status_page_enabled", enabled),
        upsertSetting("status_page_title", title),
        upsertSetting("status_page_description", description),
        upsertSetting("status_page_show_uptime_days", showUptimeDays),
        upsertSetting("status_page_items", items),
      ]);
      showToast("Status page settings saved", "success");
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : "Failed to save status page settings",
        "error",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Panel>
      <PanelHeader
        title="Status page"
        description="A public page customers can check without logging in."
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
            label="Enable status page"
          />
          <span className="text-sm text-[var(--color-ink)]">Enable status page</span>
          {enabled && (
            <a
              href="/status"
              target="_blank"
              rel="noreferrer"
              className="ml-auto inline-flex items-center gap-1 text-sm text-[var(--color-signal)] hover:underline"
            >
              View page <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
            </a>
          )}
        </div>

        <div>
          <Label htmlFor="status-page-title">Title</Label>
          <Input
            id="status-page-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            disabled={loading || !canEdit}
          />
        </div>

        <div>
          <Label htmlFor="status-page-description">Description</Label>
          <Textarea
            id="status-page-description"
            rows={2}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={loading || !canEdit}
            placeholder="Real-time status of our services"
          />
        </div>

        <div>
          <FieldCaption>Uptime history length</FieldCaption>
          <div className="flex flex-wrap gap-2">
            {UPTIME_DAYS_OPTIONS.map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => setShowUptimeDays(d)}
                disabled={loading || !canEdit}
                className={`rounded-[var(--radius-control)] px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                  showUptimeDays === d
                    ? "border border-[var(--color-signal)]/30 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]"
                }`}
              >
                {d} days
              </button>
            ))}
          </div>
        </div>

        <div>
          <FieldCaption>Items shown on the page</FieldCaption>
          {items.length === 0 ? (
            <p className="text-xs text-[var(--color-ink-faint)]">
              No items yet — add a check or agent below. Only the label you type here is ever shown
              publicly.
            </p>
          ) : (
            <div className="space-y-2">
              {items.map((item, i) => (
                <div key={`${item.type}-${item.id}-${i}`} className="flex items-center gap-2">
                  <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                    {item.type}
                  </span>
                  <Input
                    value={item.label}
                    onChange={(e) => handleLabelChange(i, e.target.value)}
                    disabled={!canEdit}
                    placeholder="Public label"
                    className="flex-1"
                  />
                  {canEdit && (
                    <IconButton
                      aria-label={`Remove ${item.label || item.type}`}
                      onClick={() => handleRemoveItem(i)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </IconButton>
                  )}
                </div>
              ))}
            </div>
          )}

          {canEdit && (
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <Select
                value={addType}
                onChange={(e) => {
                  setAddType(e.target.value as "check" | "agent");
                  setAddId("");
                }}
                className="w-auto"
              >
                <option value="check">Check</option>
                <option value="agent">Agent</option>
              </Select>
              <Select
                value={addId}
                onChange={(e) => setAddId(e.target.value)}
                className="w-auto min-w-[10rem]"
              >
                <option value="">Select…</option>
                {sourceOptions.map((opt) => (
                  <option key={opt.id} value={opt.id}>
                    {addType === "check" ? (opt as Check).name : (opt as Agent).hostname}
                  </option>
                ))}
              </Select>
              <Button variant="secondary" size="sm" onClick={handleAddItem} disabled={!addId}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add
              </Button>
            </div>
          )}
        </div>
      </PanelBody>
    </Panel>
  );
}
