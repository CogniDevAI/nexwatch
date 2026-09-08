import { useEffect, useState, useRef, useCallback } from "react";
import { Shield, Check, X, AlertTriangle, SkipForward } from "lucide-react";
import { apiFetch } from "@/lib/api";
import type { HardeningData, HardeningCheck } from "@/types";
import { MetricTile } from "@/components/ui/MetricTile";
import { SeverityBadge } from "@/components/ui/SeverityBadge";
import { EmptyState } from "@/components/ui/EmptyState";
import { Skeleton } from "@/components/ui/Skeleton";

interface HardeningTabProps {
  agentId: string;
}

const REFRESH_INTERVAL = 60_000;

function scoreColor(score: number): string {
  if (score >= 80) return "var(--color-ok)";
  if (score >= 50) return "var(--color-warn)";
  return "var(--color-critical)";
}

function CheckIcon({ status }: { status: HardeningCheck["status"] }) {
  switch (status) {
    case "pass":
      return <Check className="h-4 w-4 text-[var(--color-ok)]" aria-hidden="true" />;
    case "fail":
      return <X className="h-4 w-4 text-[var(--color-critical)]" aria-hidden="true" />;
    case "warn":
      return <AlertTriangle className="h-4 w-4 text-[var(--color-warn)]" aria-hidden="true" />;
    case "skip":
      return <SkipForward className="h-4 w-4 text-[var(--color-ink-faint)]" aria-hidden="true" />;
  }
}

function borderColor(status: HardeningCheck["status"]): string {
  switch (status) {
    case "pass":
      return "border-l-[var(--color-ok)]";
    case "fail":
      return "border-l-[var(--color-critical)]";
    case "warn":
      return "border-l-[var(--color-warn)]";
    case "skip":
      return "border-l-[var(--color-ink-faint)]";
  }
}

export function HardeningTab({ agentId }: HardeningTabProps) {
  const [data, setData] = useState<HardeningData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchData = useCallback(
    async (showLoading = false) => {
      if (showLoading) setLoading(true);
      try {
        const res = await apiFetch(`/api/custom/agents/${agentId}/hardening`);
        if (!res.ok) {
          setData(null);
          setError(true);
          return;
        }
        const json = (await res.json()) as HardeningData;
        setData(json);
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

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={Shield}
          title="No data yet"
          description="No hardening data has been reported by this agent. Make sure the hardening collector is enabled."
        />
      </div>
    );
  }

  const color = scoreColor(data.score);
  const circumference = 2 * Math.PI * 54;
  const strokeDashoffset = circumference - (data.score / 100) * circumference;

  return (
    <div>
      {/* Score + Summary */}
      <div className="mb-6 grid grid-cols-1 gap-6 rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-6 md:grid-cols-[auto_1fr]">
        {/* Circular score */}
        <div className="flex items-center justify-center">
          <div className="relative h-36 w-36">
            <svg
              viewBox="0 0 120 120"
              className="h-full w-full -rotate-90"
              role="img"
              aria-label={`Hardening score: ${data.score} out of 100`}
            >
              <circle
                cx="60"
                cy="60"
                r="54"
                fill="none"
                stroke="var(--color-line)"
                strokeWidth="8"
              />
              <circle
                cx="60"
                cy="60"
                r="54"
                fill="none"
                stroke={color}
                strokeWidth="8"
                strokeLinecap="round"
                strokeDasharray={circumference}
                strokeDashoffset={strokeDashoffset}
                className="transition-all duration-700 ease-out"
              />
            </svg>
            <div className="absolute inset-0 flex flex-col items-center justify-center">
              <span className="font-mono text-3xl font-semibold tabular-nums" style={{ color }}>
                {data.score}
              </span>
              <span className="text-xs text-[var(--color-ink-faint)]">/ 100</span>
            </div>
          </div>
        </div>

        {/* Summary tiles */}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <MetricTile label="Total checks" value={data.total} />
          <MetricTile label="Passed" value={data.passed} tone="ok" />
          <MetricTile label="Failed" value={data.failed} tone="critical" />
          <MetricTile label="Warnings" value={data.warnings} tone="warning" />
        </div>
      </div>

      {/* Checks list */}
      <div className="space-y-2">
        {data.checks.map((check, idx) => (
          <div
            key={`${check.name}-${idx}`}
            className={`rounded-[var(--radius-panel)] border border-l-4 border-[var(--color-line)] ${borderColor(check.status)} bg-[var(--color-panel)] p-4 transition-colors hover:bg-[var(--color-panel-raised)]`}
          >
            <div className="flex items-start gap-3">
              <div className="mt-0.5">
                <CheckIcon status={check.status} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium text-[var(--color-ink)]">{check.name}</span>
                  <SeverityBadge severity={check.severity} />
                </div>
                <p className="mt-1 text-sm text-[var(--color-ink-muted)]">{check.description}</p>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
