import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bug, ShieldCheck, Terminal } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import type { CveFinding, CveScanData, CveScanTarget } from "@/types";
import { MetricTile } from "@/components/ui/MetricTile";
import { SeverityBadge, type Severity } from "@/components/ui/SeverityBadge";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Button } from "@/components/ui/Button";
import { Input, Select } from "@/components/ui/Field";
import { Toggle } from "@/components/ui/Toggle";

interface CveScanTabProps {
  agentId: string;
}

const REFRESH_INTERVAL = 60_000;

const TRIVY_INSTALL_CMD =
  "curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin";
const GRYPE_INSTALL_CMD =
  "curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin";

const SEVERITY_ORDER: Record<CveFinding["severity"], number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
  unknown: 4,
};

const SEVERITY_FILTERS: { value: Severity | "all"; label: string }[] = [
  { value: "all", label: "All severities" },
  { value: "critical", label: "Critical" },
  { value: "high", label: "High" },
  { value: "medium", label: "Medium" },
  { value: "low", label: "Low" },
  { value: "unknown", label: "Unknown" },
];

function targetLabel(target: CveScanTarget): string {
  return target.kind === "host" ? "Host filesystem" : target.ref;
}

/** A monospace install-command block with a fixed, non-editable command —
 *  matches the Agents page's install-command panel styling. */
function InstallCommand({ label, command }: { label: string; command: string }) {
  return (
    <div className="text-left">
      <p className="mb-1 text-xs font-medium text-[var(--color-ink-muted)]">{label}</p>
      <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-3 py-2 font-mono text-xs text-[var(--color-ink)]">
        {command}
      </pre>
    </div>
  );
}

export function CveScanTab({ agentId }: CveScanTabProps) {
  const [data, setData] = useState<CveScanData | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const [severityFilter, setSeverityFilter] = useState<Severity | "all">("all");
  const [fixableOnly, setFixableOnly] = useState(false);
  const [search, setSearch] = useState("");

  const fetchData = useCallback(
    async (showLoading: boolean) => {
      if (showLoading) {
        setLoading(true);
        setLoadError(null);
      }
      try {
        const res = await apiFetch(`/api/custom/agents/${agentId}/cve`);
        if (!res.ok) {
          throw new Error(`Request failed (${res.status})`);
        }
        const json: CveScanData = await res.json();
        setData(json);
        setLoadError(null);
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : "Failed to load CVE scan data");
      } finally {
        setLoading(false);
      }
    },
    [agentId],
  );

  useEffect(() => {
    void fetchData(true);
  }, [fetchData, reloadToken]);

  useEffect(() => {
    intervalRef.current = setInterval(() => fetchData(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchData]);

  const matchesFilters = useCallback(
    (finding: CveFinding) => {
      if (severityFilter !== "all" && finding.severity !== severityFilter) return false;
      if (fixableOnly && !finding.fixed) return false;
      if (search.trim()) {
        const needle = search.trim().toLowerCase();
        const haystack = `${finding.id} ${finding.package} ${finding.title}`.toLowerCase();
        if (!haystack.includes(needle)) return false;
      }
      return true;
    },
    [severityFilter, fixableOnly, search],
  );

  const filteredTargets = useMemo(() => {
    if (!data) return [];
    return data.targets.map((target) => ({
      target,
      findings: [...target.findings]
        .filter(matchesFilters)
        .sort((a, b) => SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity]),
    }));
  }, [data, matchesFilters]);

  const hasAnyFindings = data ? data.targets.some((t) => t.findings.length > 0) : false;

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <ErrorState
          title="Couldn't load the CVE scan report"
          description={loadError}
          action={
            <Button variant="secondary" size="sm" onClick={() => setReloadToken((n) => n + 1)}>
              Try again
            </Button>
          }
        />
      </div>
    );
  }

  if (!data) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <ErrorState title="No response from the hub" />
      </div>
    );
  }

  if (!data.available) {
    const pending = data.error.toLowerCase().includes("pending");
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        {pending ? (
          <EmptyState
            icon={Bug}
            title="First scan is running"
            description="The agent's background CVE scan has not completed its first run yet. This can take a few minutes — check back shortly."
          />
        ) : (
          <div className="p-10 text-center">
            <Terminal
              className="mx-auto mb-4 h-10 w-10 text-[var(--color-ink-faint)]"
              aria-hidden="true"
            />
            <h3 className="text-base font-semibold text-[var(--color-ink)]">
              No CVE scanner installed
            </h3>
            <p className="mx-auto mt-1.5 max-w-prose text-sm text-[var(--color-ink-muted)]">
              {data.error || "Install Trivy or Grype on the agent host to enable CVE scanning."}
            </p>
            <div className="mx-auto mt-5 flex max-w-md flex-col gap-3">
              <InstallCommand label="Install Trivy" command={TRIVY_INSTALL_CMD} />
              <InstallCommand label="Install Grype" command={GRYPE_INSTALL_CMD} />
            </div>
          </div>
        )}
      </div>
    );
  }

  return (
    <div>
      {/* Summary tiles */}
      <div className="mb-4 grid grid-cols-3 gap-3 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-5 sm:grid-cols-5">
        <MetricTile label="Critical" value={data.totals.critical} tone="critical" />
        <MetricTile label="High" value={data.totals.high} tone="critical" />
        <MetricTile label="Medium" value={data.totals.medium} tone="warning" />
        <MetricTile label="Low" value={data.totals.low} />
        <MetricTile label="Fixable" value={data.totals.fixable} tone="ok" />
      </div>

      {/* Scanner line */}
      <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] px-4 py-2.5 text-xs text-[var(--color-ink-muted)]">
        <span>
          Scanner:{" "}
          <span className="font-medium text-[var(--color-ink)]">
            {data.scanner}
            {data.scanner_version ? ` v${data.scanner_version}` : ""}
          </span>
        </span>
        {data.db_updated_at && (
          <span>
            DB updated{" "}
            <span className="font-medium text-[var(--color-ink)]">
              {timeSince(data.db_updated_at)}
            </span>
          </span>
        )}
        {data.scanned_at && (
          <span>
            Scanned{" "}
            <span className="font-medium text-[var(--color-ink)]">
              {timeSince(data.scanned_at)}
            </span>
          </span>
        )}
        {data.stale && (
          <span className="rounded-[var(--radius-chip)] border border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 px-2 py-0.5 font-medium text-[var(--color-warn)]">
            Stale
          </span>
        )}
        {data.error && <span className="text-[var(--color-critical)]">{data.error}</span>}
      </div>

      {/* Filters */}
      <div className="mb-4 flex flex-wrap items-end gap-3 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-4">
        <div className="w-40">
          <Select
            aria-label="Filter by severity"
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value as Severity | "all")}
          >
            {SEVERITY_FILTERS.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </Select>
        </div>
        <div className="flex items-center gap-2">
          <Toggle checked={fixableOnly} onChange={setFixableOnly} label="Fixable only" />
          <span className="text-sm text-[var(--color-ink-muted)]">Fixable only</span>
        </div>
        <div className="min-w-48 flex-1">
          <Input
            type="search"
            placeholder="Search CVE ID, package, or title…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>

      {!hasAnyFindings ? (
        <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
          <EmptyState icon={ShieldCheck} title="No known CVEs found" />
        </div>
      ) : (
        <div className="space-y-6">
          {filteredTargets.map(({ target, findings }) => (
            <div key={`${target.kind}-${target.ref}`}>
              <div className="mb-2 flex items-center justify-between">
                <h4 className="text-sm font-semibold text-[var(--color-ink)]">
                  {targetLabel(target)}
                </h4>
                <span className="text-xs text-[var(--color-ink-muted)]">
                  {findings.length} of {target.findings.length} finding
                  {target.findings.length === 1 ? "" : "s"}
                </span>
              </div>
              {findings.length === 0 ? (
                <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-6 text-center text-sm text-[var(--color-ink-muted)]">
                  No findings match the current filters.
                </div>
              ) : (
                <Table>
                  <thead>
                    <tr>
                      <Th>CVE ID</Th>
                      <Th>Severity</Th>
                      <Th>Package</Th>
                      <Th>Installed</Th>
                      <Th>Fixed</Th>
                      <Th>Title</Th>
                    </tr>
                  </thead>
                  <tbody>
                    {findings.map((f, idx) => (
                      <tr key={`${f.id}-${f.package}-${f.target}-${idx}`} className={rowClass(idx)}>
                        <Td>
                          {f.id.startsWith("CVE-") ? (
                            <a
                              href={`https://nvd.nist.gov/vuln/detail/${f.id}`}
                              target="_blank"
                              rel="noreferrer"
                              className="font-mono text-[var(--color-signal)] hover:underline"
                            >
                              {f.id}
                            </a>
                          ) : (
                            <span className="font-mono">{f.id}</span>
                          )}
                        </Td>
                        <Td>
                          <SeverityBadge severity={f.severity} />
                        </Td>
                        <Td className="font-mono">{f.package}</Td>
                        <Td className="font-mono text-[var(--color-ink-muted)]">{f.installed}</Td>
                        <Td className="font-mono text-[var(--color-ink-muted)]">
                          {f.fixed || "—"}
                        </Td>
                        <Td className="text-[var(--color-ink-muted)]" title={f.title}>
                          {f.title || "—"}
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
