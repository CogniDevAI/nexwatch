import { useEffect, useState, useRef, useCallback } from "react";
import {
  Shield,
  Check,
  X,
  AlertTriangle,
  SkipForward,
} from "lucide-react";
import pb from "@/lib/pocketbase";
import type { HardeningData, HardeningCheck } from "@/types";

interface HardeningTabProps {
  agentId: string;
}

const REFRESH_INTERVAL = 60_000;

function scoreColor(score: number): string {
  if (score >= 80) return "var(--color-accent-green)";
  if (score >= 50) return "var(--color-accent-yellow)";
  return "var(--color-accent-red)";
}

function severityBadge(severity: HardeningCheck["severity"]) {
  const map: Record<string, string> = {
    critical: "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]",
    high: "bg-[#f97316]/10 text-[#f97316]",
    medium: "bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]",
    low: "bg-[var(--color-accent-cyan)]/10 text-[var(--color-accent-cyan)]",
  };

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium uppercase ${map[severity] ?? map.low}`}
    >
      {severity}
    </span>
  );
}

function CheckIcon({ status }: { status: HardeningCheck["status"] }) {
  switch (status) {
    case "pass":
      return <Check className="w-4 h-4 text-[var(--color-accent-green)]" />;
    case "fail":
      return <X className="w-4 h-4 text-[var(--color-accent-red)]" />;
    case "warn":
      return <AlertTriangle className="w-4 h-4 text-[var(--color-accent-yellow)]" />;
    case "skip":
      return <SkipForward className="w-4 h-4 text-[var(--color-text-muted)]" />;
  }
}

function borderColor(status: HardeningCheck["status"]): string {
  switch (status) {
    case "pass":
      return "border-l-[var(--color-accent-green)]";
    case "fail":
      return "border-l-[var(--color-accent-red)]";
    case "warn":
      return "border-l-[var(--color-accent-yellow)]";
    case "skip":
      return "border-l-[var(--color-text-muted)]";
  }
}

export function HardeningTab({ agentId }: HardeningTabProps) {
  const [data, setData] = useState<HardeningData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchData = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const res = await fetch(`/api/custom/agents/${agentId}/hardening`, {
        headers: { Authorization: pb.authStore.token },
      });
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
  }, [agentId]);

  useEffect(() => {
    fetchData(true);

    intervalRef.current = setInterval(() => fetchData(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchData]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Shield className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
          Loading hardening data...
        </span>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <Shield className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          No data yet
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          No hardening data has been reported by this agent. Make sure the
          hardening collector is enabled.
        </p>
      </div>
    );
  }

  const color = scoreColor(data.score);
  const circumference = 2 * Math.PI * 54;
  const strokeDashoffset = circumference - (data.score / 100) * circumference;

  return (
    <div>
      {/* Score + Summary */}
      <div className="grid grid-cols-1 md:grid-cols-[auto_1fr] gap-6 mb-6">
        {/* Circular score */}
        <div className="flex items-center justify-center">
          <div className="relative w-36 h-36">
            <svg viewBox="0 0 120 120" className="w-full h-full -rotate-90">
              {/* Background circle */}
              <circle
                cx="60"
                cy="60"
                r="54"
                fill="none"
                stroke="var(--color-border-default)"
                strokeWidth="8"
              />
              {/* Progress circle */}
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
              <span
                className="text-3xl font-bold tabular-nums"
                style={{ color }}
              >
                {data.score}
              </span>
              <span className="text-xs text-[var(--color-text-muted)]">
                / 100
              </span>
            </div>
          </div>
        </div>

        {/* Summary cards */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
            <div className="text-2xl font-bold text-[var(--color-text-primary)] tabular-nums">
              {data.total}
            </div>
            <div className="text-xs text-[var(--color-text-muted)] mt-1">Total Checks</div>
          </div>
          <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
            <div className="text-2xl font-bold text-[var(--color-accent-green)] tabular-nums">
              {data.passed}
            </div>
            <div className="text-xs text-[var(--color-text-muted)] mt-1">Passed</div>
          </div>
          <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
            <div className="text-2xl font-bold text-[var(--color-accent-red)] tabular-nums">
              {data.failed}
            </div>
            <div className="text-xs text-[var(--color-text-muted)] mt-1">Failed</div>
          </div>
          <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 text-center">
            <div className="text-2xl font-bold text-[var(--color-accent-yellow)] tabular-nums">
              {data.warnings}
            </div>
            <div className="text-xs text-[var(--color-text-muted)] mt-1">Warnings</div>
          </div>
        </div>
      </div>

      {/* Checks list */}
      <div className="space-y-2">
        {data.checks.map((check, idx) => (
          <div
            key={`${check.name}-${idx}`}
            className={`rounded-lg border border-[var(--color-border-default)] border-l-4 ${borderColor(check.status)} bg-[var(--color-bg-surface)] p-4 transition-colors hover:bg-[var(--color-bg-elevated)]`}
          >
            <div className="flex items-start gap-3">
              <div className="mt-0.5">
                <CheckIcon status={check.status} />
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-[var(--color-text-primary)]">
                    {check.name}
                  </span>
                  {severityBadge(check.severity)}
                </div>
                <p className="text-sm text-[var(--color-text-secondary)] mt-1">
                  {check.description}
                </p>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
