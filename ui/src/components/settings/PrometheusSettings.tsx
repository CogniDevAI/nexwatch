import { useState, useEffect, useCallback } from "react";
import { Copy, Check, KeyRound, Ban } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Button, IconButton } from "@/components/ui/Button";
import { useToast } from "@/components/ui/toastContext";
import { upsertSetting, loadSettingsMap, asString } from "@/lib/settings";

/** Admin section for the Prometheus text-exposition endpoint (GET
 *  /metrics). The token itself is never readable after generation — only
 *  its SHA-256 hash is stored server-side (see
 *  internal/hub/api/prometheus.go) — so this panel only ever shows
 *  "set"/"not set", never the value, except for the one response right
 *  after generating it. */
interface PrometheusSettingsProps {
  canEdit: boolean;
}

export function PrometheusSettings({ canEdit }: PrometheusSettingsProps) {
  const { showToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [tokenSet, setTokenSet] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedSnippet, setCopiedSnippet] = useState(false);
  const [baseURL, setBaseURL] = useState(window.location.origin);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const settings = await loadSettingsMap();
      const hash = asString(settings.prometheus_token, "");
      setTokenSet(hash !== "");
      const configuredBase = asString(settings.public_base_url, "");
      setBaseURL(configuredBase || window.location.origin);
    } catch {
      // Keep defaults.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleGenerate = async () => {
    setGenerating(true);
    setNewToken(null);
    try {
      const response = await apiFetch("/api/custom/prometheus/token", { method: "POST" });
      if (!response.ok) throw new Error(`Request failed with status ${response.status}`);
      const body = (await response.json()) as { token: string };
      setNewToken(body.token);
      setTokenSet(true);
      showToast("Prometheus token generated", "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to generate token", "error");
    } finally {
      setGenerating(false);
    }
  };

  const handleDisable = async () => {
    setDisabling(true);
    try {
      await upsertSetting("prometheus_token", "");
      setTokenSet(false);
      setNewToken(null);
      showToast("Prometheus exposition disabled", "success");
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : "Failed to disable Prometheus exposition",
        "error",
      );
    } finally {
      setDisabling(false);
    }
  };

  const handleCopyToken = async () => {
    if (!newToken) return;
    try {
      await navigator.clipboard.writeText(newToken);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback: the token is still selectable in the box.
    }
  };

  const scheme = baseURL.startsWith("https") ? "https" : "http";
  const host = baseURL.replace(/^https?:\/\//, "");
  const scrapeConfig = `scrape_configs:
  - job_name: nexwatch
    scheme: ${scheme}
    bearer_token: '${newToken ?? "YOUR_TOKEN_HERE"}'
    static_configs:
      - targets: ['${host}']`;

  const handleCopySnippet = async () => {
    try {
      await navigator.clipboard.writeText(scrapeConfig);
      setCopiedSnippet(true);
      setTimeout(() => setCopiedSnippet(false), 2000);
    } catch {
      // Fallback: the snippet is still selectable in the box.
    }
  };

  return (
    <Panel>
      <PanelHeader
        title="Prometheus"
        description="Expose fleet, agent, and check metrics for scraping at GET /metrics."
      />
      <PanelBody className="space-y-4">
        <div className="flex items-center gap-2">
          <span className="text-sm text-[var(--color-ink-muted)]">Token status:</span>
          <span
            className={`rounded-[var(--radius-chip)] px-2 py-0.5 text-xs font-medium ${
              tokenSet
                ? "bg-[var(--color-ok)]/15 text-[var(--color-ok)]"
                : "bg-[var(--color-panel-raised)] text-[var(--color-ink-faint)]"
            }`}
          >
            {loading ? "…" : tokenSet ? "Set" : "Not set"}
          </span>
        </div>

        {canEdit ? (
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" size="sm" onClick={handleGenerate} disabled={generating}>
              <KeyRound className="h-4 w-4" aria-hidden="true" />
              {generating ? "Generating…" : tokenSet ? "Regenerate token" : "Generate token"}
            </Button>
            {tokenSet && (
              <Button variant="danger" size="sm" onClick={handleDisable} disabled={disabling}>
                <Ban className="h-4 w-4" aria-hidden="true" />
                {disabling ? "Disabling…" : "Disable"}
              </Button>
            )}
          </div>
        ) : (
          <p className="text-xs text-[var(--color-ink-faint)]">
            You need the admin role to generate or disable this token.
          </p>
        )}

        {newToken && (
          <div>
            <p className="mb-1.5 text-xs font-semibold text-[var(--color-warn)]">
              Copy this token now — it will not be shown again.
            </p>
            <div className="relative">
              <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-4 font-mono text-xs break-all whitespace-pre-wrap text-[var(--color-ink)]">
                {newToken}
              </pre>
              <IconButton
                aria-label="Copy Prometheus token"
                onClick={handleCopyToken}
                className="absolute top-2 right-2 border border-[var(--color-line)] bg-[var(--color-panel)]"
              >
                {copied ? (
                  <Check className="h-4 w-4 text-[var(--color-ok)]" />
                ) : (
                  <Copy className="h-4 w-4" />
                )}
              </IconButton>
            </div>
          </div>
        )}

        <div>
          <p className="mb-1.5 text-xs font-semibold text-[var(--color-ink-muted)]">
            Scrape config
          </p>
          <div className="relative">
            <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-4 font-mono text-xs break-all whitespace-pre-wrap text-[var(--color-ink)]">
              {scrapeConfig}
            </pre>
            <IconButton
              aria-label="Copy scrape config"
              onClick={handleCopySnippet}
              className="absolute top-2 right-2 border border-[var(--color-line)] bg-[var(--color-panel)]"
            >
              {copiedSnippet ? (
                <Check className="h-4 w-4 text-[var(--color-ok)]" />
              ) : (
                <Copy className="h-4 w-4" />
              )}
            </IconButton>
          </div>
          <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
            Replace YOUR_TOKEN_HERE with a generated token — this snippet does not embed a live
            token unless you just generated one above.
          </p>
        </div>
      </PanelBody>
    </Panel>
  );
}
