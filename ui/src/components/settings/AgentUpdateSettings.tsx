import { useState, useEffect, useCallback } from "react";
import { RefreshCw } from "lucide-react";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Input, Label, FieldCaption } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/toastContext";
import { upsertSetting, loadSettingsMap, asString } from "@/lib/settings";
import { fetchLatestVersion } from "@/lib/agentUpdates";

interface AgentUpdateSettingsProps {
  canEdit: boolean;
}

// Matches internal/hub/api/update_routes.go's own defaultReleaseBaseURL,
// shown as the field's placeholder when no override is set.
const DEFAULT_RELEASE_BASE_URL = "https://github.com/CogniDevAI/nexwatch/releases/download";

/** Admin section for F9 (agent self-update): the target version agents
 *  compare themselves against, the release download base, and a read-only
 *  view of the latest published GitHub release. Signature verification
 *  policy is explained but not configured here — it's each agent's own
 *  local "update_require_signature" setting, enforced agent-side. */
export function AgentUpdateSettings({ canEdit }: AgentUpdateSettingsProps) {
  const { showToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [targetVersion, setTargetVersion] = useState("");
  const [releaseBaseURL, setReleaseBaseURL] = useState("");

  const [latestVersion, setLatestVersion] = useState("");
  const [latestPublishedAt, setLatestPublishedAt] = useState("");
  const [latestError, setLatestError] = useState("");
  const [refreshing, setRefreshing] = useState(false);

  const loadLatest = useCallback(async () => {
    setRefreshing(true);
    try {
      const info = await fetchLatestVersion();
      setLatestVersion(info.version);
      setLatestPublishedAt(info.publishedAt);
      setLatestError(info.error ?? "");
    } catch (err) {
      setLatestVersion("");
      setLatestError(err instanceof Error ? err.message : "Failed to fetch the latest release");
    } finally {
      setRefreshing(false);
    }
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const settings = await loadSettingsMap();
      setTargetVersion(asString(settings.agent_target_version, ""));
      setReleaseBaseURL(asString(settings.agent_release_base_url, ""));
    } catch {
      // Settings collection may not have entries yet — use defaults.
    } finally {
      setLoading(false);
    }
    await loadLatest();
  }, [loadLatest]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await upsertSetting("agent_target_version", targetVersion.trim());
      await upsertSetting("agent_release_base_url", releaseBaseURL.trim());
      showToast("Agent update settings saved", "success");
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : "Failed to save agent update settings",
        "error",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Panel>
      <PanelHeader
        title="Agent updates"
        description="Control which version agents flag as behind, and where release assets are downloaded from."
      />
      <PanelBody className="space-y-4">
        <div>
          <Label htmlFor="agent-target-version">Target version</Label>
          <Input
            id="agent-target-version"
            value={targetVersion}
            onChange={(e) => setTargetVersion(e.target.value)}
            placeholder="e.g. 0.9.1 — leave empty to compare against the latest release"
            disabled={loading || !canEdit}
          />
          <FieldCaption>
            Agents reporting an older version than this show an "Update available" badge on the
            Agents page. Leave empty to compare against the latest published GitHub release instead.
          </FieldCaption>
        </div>

        <div>
          <Label htmlFor="agent-release-base-url">Release base URL</Label>
          <Input
            id="agent-release-base-url"
            value={releaseBaseURL}
            onChange={(e) => setReleaseBaseURL(e.target.value)}
            placeholder={DEFAULT_RELEASE_BASE_URL}
            disabled={loading || !canEdit}
          />
          <FieldCaption>
            Where agents download release assets from — the full URL is built as{" "}
            <code className="text-[var(--color-ink)]">{"<base>/v<version>/<asset>"}</code>. Point
            this at an internal mirror for an offline or air-gapped fleet.
          </FieldCaption>
        </div>

        {canEdit && (
          <Button
            variant="primary"
            size="sm"
            onClick={() => void handleSave()}
            disabled={saving || loading}
          >
            {saving ? "Saving…" : "Save"}
          </Button>
        )}

        <div className="rounded-[var(--radius-control)] border border-[var(--color-line-soft)] bg-[var(--color-panel-raised)] p-3">
          <p className="text-xs text-[var(--color-ink-muted)]">
            Every self-update verifies the release's SHA-256 checksum before installing it. GPG
            signature verification runs automatically whenever a release publishes one, and each
            agent's own <code className="text-[var(--color-ink)]">update_require_signature</code>{" "}
            config option can make it mandatory — that policy is enforced locally on each agent, not
            here.
          </p>
        </div>

        <div className="border-t border-[var(--color-line-soft)] pt-4">
          <div className="mb-1.5 flex items-center justify-between">
            <p className="text-xs font-semibold text-[var(--color-ink-muted)]">
              Latest published release
            </p>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void loadLatest()}
              disabled={refreshing}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`}
                aria-hidden="true"
              />
              Refresh
            </Button>
          </div>
          {latestError ? (
            <p className="text-xs text-[var(--color-ink-faint)]">
              Could not fetch the latest release: {latestError}
            </p>
          ) : latestVersion ? (
            <p className="text-sm text-[var(--color-ink-muted)]">
              <span className="font-mono text-[var(--color-ink)]">{latestVersion}</span>
              {latestPublishedAt && (
                <span className="text-[var(--color-ink-faint)]">
                  {" "}
                  · published {new Date(latestPublishedAt).toLocaleDateString()}
                </span>
              )}
            </p>
          ) : (
            <p className="text-xs text-[var(--color-ink-faint)]">
              {refreshing ? "Loading…" : "Not available"}
            </p>
          )}
        </div>
      </PanelBody>
    </Panel>
  );
}
