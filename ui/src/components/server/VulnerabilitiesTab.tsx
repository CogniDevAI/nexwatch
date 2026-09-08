import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { ShieldAlert, ShieldCheck } from "lucide-react";
import { apiFetch } from "@/lib/api";
import type { VulnerabilityData, VulnerabilityItem } from "@/types";
import { MetricTile } from "@/components/ui/MetricTile";
import { SeverityBadge } from "@/components/ui/SeverityBadge";
import { EmptyState } from "@/components/ui/EmptyState";
import { Skeleton } from "@/components/ui/Skeleton";

interface VulnerabilitiesTabProps {
  agentId: string;
}

const REFRESH_INTERVAL = 60_000;

const SEVERITY_ORDER: Record<VulnerabilityItem["severity"], number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
  info: 4,
};

const SEVERITY_BORDER: Record<VulnerabilityItem["severity"], string> = {
  critical: "border-l-[var(--color-critical)]",
  high: "border-l-[var(--color-severity-high)]",
  medium: "border-l-[var(--color-warn)]",
  low: "border-l-[var(--color-signal)]",
  info: "border-l-[var(--color-ink-faint)]",
};

const SEVERITY_TONE: Record<
  VulnerabilityItem["severity"],
  "default" | "ok" | "warning" | "critical"
> = {
  critical: "critical",
  high: "critical",
  medium: "warning",
  low: "default",
  info: "default",
};

export function VulnerabilitiesTab({ agentId }: VulnerabilitiesTabProps) {
  const [data, setData] = useState<VulnerabilityData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchData = useCallback(
    async (showLoading = false) => {
      if (showLoading) setLoading(true);
      try {
        const res = await apiFetch(`/api/custom/agents/${agentId}/vulnerabilities`);
        if (!res.ok) {
          setData(null);
          setError(true);
          return;
        }
        // API returns { items, summary: {critical,high,medium,low}, total }.
        // Frontend VulnerabilityData expects summary to also include 'info' and 'total'.
        const json = await res.json();
        const rawSummary = json.summary ?? {};
        const mapped: VulnerabilityData = {
          items: json.items ?? [],
          summary: {
            critical: rawSummary.critical ?? 0,
            high: rawSummary.high ?? 0,
            medium: rawSummary.medium ?? 0,
            low: rawSummary.low ?? 0,
            info: rawSummary.info ?? 0,
            total: json.total ?? json.items?.length ?? 0,
          },
        };
        setData(mapped);
        setError(false);
      } catch {
        setData(null);
        setError(true);
      } finally {
        setLoading(false);
      }
    },
    [agentId],
  );

  useEffect(() => {
    void fetchData(true);

    intervalRef.current = setInterval(() => fetchData(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchData]);

  const sortedItems = useMemo(() => {
    if (!data) return [];
    return [...data.items].sort((a, b) => SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity]);
  }, [data]);

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={ShieldAlert}
          title="No data yet"
          description="No vulnerability data has been reported by this agent. Make sure the vulnerability scanner is enabled."
        />
      </div>
    );
  }

  return (
    <div>
      {/* Summary tiles */}
      <div className="mb-6 grid grid-cols-3 gap-3 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-5 sm:grid-cols-6">
        <MetricTile label="Critical" value={data.summary.critical} tone={SEVERITY_TONE.critical} />
        <MetricTile label="High" value={data.summary.high} tone={SEVERITY_TONE.high} />
        <MetricTile label="Medium" value={data.summary.medium} tone={SEVERITY_TONE.medium} />
        <MetricTile label="Low" value={data.summary.low} tone={SEVERITY_TONE.low} />
        <MetricTile label="Info" value={data.summary.info} tone={SEVERITY_TONE.info} />
        <MetricTile label="Total" value={data.summary.total} />
      </div>

      {/* Vulnerability items */}
      {sortedItems.length === 0 ? (
        <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
          <EmptyState icon={ShieldCheck} title="No vulnerabilities found" />
        </div>
      ) : (
        <div className="space-y-2">
          {sortedItems.map((item, idx) => (
            <div
              key={`${item.name}-${idx}`}
              className={`rounded-[var(--radius-panel)] border border-l-4 border-[var(--color-line)] ${SEVERITY_BORDER[item.severity]} bg-[var(--color-panel)] p-4 transition-colors hover:bg-[var(--color-panel-raised)]`}
            >
              <div className="mb-1 flex flex-wrap items-center gap-2">
                <SeverityBadge severity={item.severity} />
                <span className="font-medium text-[var(--color-ink)]">{item.name}</span>
              </div>
              <p className="text-sm text-[var(--color-ink-muted)]">{item.description}</p>
              {item.recommendation && (
                <p className="mt-2 border-l-2 border-[var(--color-line)] pl-3 text-sm text-[var(--color-ink-faint)]">
                  {item.recommendation}
                </p>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
