import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { ShieldAlert } from "lucide-react";
import pb from "@/lib/pocketbase";
import type { VulnerabilityData, VulnerabilityItem } from "@/types";

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

const SEVERITY_STYLES: Record<
  VulnerabilityItem["severity"],
  { badge: string; card: string; bg: string }
> = {
  critical: {
    badge: "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]",
    card: "border-l-[var(--color-accent-red)]",
    bg: "bg-[var(--color-accent-red)]",
  },
  high: {
    badge: "bg-[#f97316]/10 text-[#f97316]",
    card: "border-l-[#f97316]",
    bg: "bg-[#f97316]",
  },
  medium: {
    badge: "bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]",
    card: "border-l-[var(--color-accent-yellow)]",
    bg: "bg-[var(--color-accent-yellow)]",
  },
  low: {
    badge: "bg-[var(--color-accent-cyan)]/10 text-[var(--color-accent-cyan)]",
    card: "border-l-[var(--color-accent-cyan)]",
    bg: "bg-[var(--color-accent-cyan)]",
  },
  info: {
    badge: "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]",
    card: "border-l-[var(--color-text-muted)]",
    bg: "bg-[var(--color-text-muted)]",
  },
};

function SummaryCard({
  label,
  count,
  severity,
}: {
  label: string;
  count: number;
  severity: VulnerabilityItem["severity"];
}) {
  const style = SEVERITY_STYLES[severity];
  return (
    <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
      <div className={`w-2 h-2 rounded-full ${style.bg} mx-auto mb-2`} />
      <div className="text-2xl font-bold text-[var(--color-text-primary)] tabular-nums">
        {count}
      </div>
      <div className="text-xs text-[var(--color-text-muted)] mt-1 capitalize">
        {label}
      </div>
    </div>
  );
}

export function VulnerabilitiesTab({ agentId }: VulnerabilitiesTabProps) {
  const [data, setData] = useState<VulnerabilityData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchData = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/vulnerabilities`, {
        headers: { Authorization: pb.authStore.token },
      });
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
          total: json.total ?? (json.items?.length ?? 0),
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
  }, [agentId]);

  useEffect(() => {
    fetchData(true);

    intervalRef.current = setInterval(() => fetchData(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchData]);

  const sortedItems = useMemo(() => {
    if (!data) return [];
    return [...data.items].sort(
      (a, b) => SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity],
    );
  }, [data]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <ShieldAlert className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
          Loading vulnerabilities...
        </span>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <ShieldAlert className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          No data yet
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          No vulnerability data has been reported by this agent. Make sure the
          vulnerability scanner is enabled.
        </p>
      </div>
    );
  }

  return (
    <div>
      {/* Summary cards */}
      <div className="grid grid-cols-3 sm:grid-cols-6 gap-3 mb-6">
        <SummaryCard label="Critical" count={data.summary.critical} severity="critical" />
        <SummaryCard label="High" count={data.summary.high} severity="high" />
        <SummaryCard label="Medium" count={data.summary.medium} severity="medium" />
        <SummaryCard label="Low" count={data.summary.low} severity="low" />
        <SummaryCard label="Info" count={data.summary.info} severity="info" />
        <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
          <div className="w-2 h-2 rounded-full bg-[var(--color-accent-purple)] mx-auto mb-2" />
          <div className="text-2xl font-bold text-[var(--color-text-primary)] tabular-nums">
            {data.summary.total}
          </div>
          <div className="text-xs text-[var(--color-text-muted)] mt-1">Total</div>
        </div>
      </div>

      {/* Vulnerability items */}
      {sortedItems.length === 0 ? (
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
          <ShieldAlert className="w-10 h-10 text-[var(--color-accent-green)] mx-auto mb-3" />
          <h3 className="text-base font-medium text-[var(--color-text-primary)]">
            No vulnerabilities found
          </h3>
        </div>
      ) : (
        <div className="space-y-2">
          {sortedItems.map((item, idx) => {
            const style = SEVERITY_STYLES[item.severity];
            return (
              <div
                key={`${item.name}-${idx}`}
                className={`rounded-lg border border-[var(--color-border-default)] border-l-4 ${style.card} bg-[var(--color-bg-surface)] p-4 transition-colors hover:bg-[var(--color-bg-elevated)]`}
              >
                <div className="flex items-start gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap mb-1">
                      <span
                        className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium uppercase ${style.badge}`}
                      >
                        {item.severity}
                      </span>
                      <span className="font-medium text-[var(--color-text-primary)]">
                        {item.name}
                      </span>
                    </div>
                    <p className="text-sm text-[var(--color-text-secondary)]">
                      {item.description}
                    </p>
                    {item.recommendation && (
                      <p className="text-sm text-[var(--color-text-muted)] mt-2 pl-3 border-l-2 border-[var(--color-border-default)]">
                        {item.recommendation}
                      </p>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
