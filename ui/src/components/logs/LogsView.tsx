import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Copy, ScrollText, Radio } from "lucide-react";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { useToast } from "@/components/ui/toastContext";
import {
  buildLogsQuery,
  buildLogsRealtimeFilter,
  formatLogLineForCopy,
  formatLogTimestamp,
  LEVEL_META,
  LEVELS,
  LOGS_RENDER_CAP,
  resolveTimeRange,
  TIME_RANGE_PRESETS,
  type LogLevel,
  type TimeRangePreset,
} from "@/lib/logs";
import type { Agent, LogEntry, LogsQueryResponse } from "@/types";
import { useAgentStore } from "@/stores/agentStore";
import { Panel } from "@/components/ui/Panel";
import { Input, Select } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { Skeleton } from "@/components/ui/Skeleton";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";

const PAGE_LIMIT = 100;
const SEARCH_DEBOUNCE_MS = 300;
const SCROLL_PAUSE_THRESHOLD_PX = 4;

interface LogsViewProps {
  /** When set, the view is locked to this agent (host detail's "Logs"
   *  tab) and the agent selector is hidden. When omitted, an "All agents"
   *  / one-agent selector is shown (the standalone /logs page). */
  agentId?: string;
}

/** Shared log search/live-tail view: agent selector (unless `agentId` is
 *  fixed), level chips, unit filter, search box, time range presets, a
 *  monospace log list, "Load older" pagination, and a "Live" toggle. Used
 *  by both the standalone Logs page and the host detail Logs tab. */
export function LogsView({ agentId: fixedAgentId }: LogsViewProps) {
  const { showToast } = useToast();
  const agents = useAgentStore((s) => s.agents);

  const [selectedAgentId, setSelectedAgentId] = useState(fixedAgentId ?? "");
  const effectiveAgentId = fixedAgentId ?? selectedAgentId;

  const [level, setLevel] = useState<LogLevel | "">("");
  const [unit, setUnit] = useState("");
  const [units, setUnits] = useState<string[]>([]);
  const [searchInput, setSearchInput] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [rangePreset, setRangePreset] = useState<TimeRangePreset>("15m");
  const [customSince, setCustomSince] = useState<string>("");
  const [customUntil, setCustomUntil] = useState<string>("");

  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [nextBefore, setNextBefore] = useState<string | undefined>(undefined);

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [live, setLive] = useState(false);
  const [paused, setPaused] = useState(false);
  const [pendingEntries, setPendingEntries] = useState<LogEntry[]>([]);

  const listRef = useRef<HTMLDivElement>(null);

  // Debounce the search box the same way AuditLog's free-text filters do
  // (DESIGN.md §12) — a keystroke shouldn't fire its own request.
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(searchInput), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const timeRange = useMemo(() => {
    const custom = {
      since: customSince ? new Date(customSince).getTime() : undefined,
      until: customUntil ? new Date(customUntil).getTime() : undefined,
    };
    return resolveTimeRange(rangePreset, custom);
  }, [rangePreset, customSince, customUntil]);

  const fetchPage = useCallback(
    async (before?: string) => {
      const qs = buildLogsQuery({
        agentId: effectiveAgentId,
        level,
        unit,
        q: debouncedQuery,
        since: timeRange.since,
        until: timeRange.until,
        limit: PAGE_LIMIT,
        before,
      });
      const res = await apiFetch(`/api/custom/logs${qs ? `?${qs}` : ""}`);
      if (!res.ok) throw new Error(`Request failed with status ${res.status}`);
      return (await res.json()) as LogsQueryResponse;
    },
    [effectiveAgentId, level, unit, debouncedQuery, timeRange.since, timeRange.until],
  );

  // Initial/refresh load whenever a filter changes.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setExpandedId(null);

    fetchPage()
      .then((data) => {
        if (cancelled) return;
        setEntries(data.entries);
        setNextBefore(data.next_before);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load logs");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [fetchPage]);

  // Units dropdown, scoped to the current agent selection (or every agent).
  useEffect(() => {
    let cancelled = false;
    const qs = effectiveAgentId ? `?agent_id=${encodeURIComponent(effectiveAgentId)}` : "";
    apiFetch(`/api/custom/logs/units${qs}`)
      .then((res) => (res.ok ? res.json() : { units: [] }))
      .then((data: { units?: string[] }) => {
        if (!cancelled) setUnits(data.units ?? []);
      })
      .catch(() => {
        if (!cancelled) setUnits([]);
      });
    return () => {
      cancelled = true;
    };
  }, [effectiveAgentId]);

  const handleLoadOlder = async () => {
    if (!nextBefore || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const data = await fetchPage(nextBefore);
      setEntries((prev) => [...prev, ...data.entries].slice(0, LOGS_RENDER_CAP));
      setNextBefore(data.next_before);
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to load older logs", "error");
    } finally {
      setLoadingOlder(false);
    }
  };

  // Live tail: subscribe to realtime "logs" events matching the current
  // filters. New entries prepend directly unless the user has scrolled
  // away from the top, in which case they buffer into pendingEntries
  // until the "N new lines" pill is clicked.
  useEffect(() => {
    if (!live) {
      setPaused(false);
      setPendingEntries([]);
      return;
    }

    const filter = buildLogsRealtimeFilter({
      agentId: effectiveAgentId,
      level,
      unit,
      q: debouncedQuery,
    });

    const unsubscribePromise = pb.collection("logs").subscribe<LogEntry>(
      "*",
      (event) => {
        if (event.action !== "create") return;
        setPaused((currentlyPaused) => {
          if (currentlyPaused) {
            setPendingEntries((prev) => [event.record, ...prev]);
          } else {
            setEntries((prev) => [event.record, ...prev].slice(0, LOGS_RENDER_CAP));
          }
          return currentlyPaused;
        });
      },
      filter ? { filter } : undefined,
    );

    return () => {
      void unsubscribePromise.then((unsub) => unsub());
    };
  }, [live, effectiveAgentId, level, unit, debouncedQuery]);

  const handleScroll = () => {
    const el = listRef.current;
    if (!el) return;
    if (el.scrollTop > SCROLL_PAUSE_THRESHOLD_PX) {
      setPaused(true);
    }
  };

  const handleResumeLive = () => {
    setEntries((prev) => [...pendingEntries, ...prev].slice(0, LOGS_RENDER_CAP));
    setPendingEntries([]);
    setPaused(false);
    listRef.current?.scrollTo({ top: 0 });
  };

  const handleCopyLine = async (entry: LogEntry) => {
    try {
      await navigator.clipboard.writeText(formatLogLineForCopy(entry));
      showToast("Log line copied");
    } catch {
      showToast("Failed to copy log line", "error");
    }
  };

  return (
    <div className="space-y-4">
      <Panel className="space-y-3 p-4">
        <div className="flex flex-wrap items-end gap-3">
          {!fixedAgentId && (
            <div className="w-48">
              <Select
                aria-label="Agent"
                value={selectedAgentId}
                onChange={(e) => setSelectedAgentId(e.target.value)}
              >
                <option value="">All agents</option>
                {agents.map((a: Agent) => (
                  <option key={a.id} value={a.id}>
                    {a.hostname || a.id}
                  </option>
                ))}
              </Select>
            </div>
          )}

          <div className="w-48">
            <Select aria-label="Unit" value={unit} onChange={(e) => setUnit(e.target.value)}>
              <option value="">All units</option>
              {units.map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </Select>
          </div>

          <div className="min-w-48 flex-1">
            <Input
              type="search"
              aria-label="Search logs"
              placeholder="Search message text…"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
            />
          </div>

          <div
            role="radiogroup"
            aria-label="Time range"
            className="inline-flex gap-0.5 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-1"
          >
            {TIME_RANGE_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                role="radio"
                aria-checked={rangePreset === p.value}
                onClick={() => setRangePreset(p.value)}
                className={`rounded-[var(--radius-chip)] px-3 py-1.5 text-xs font-medium transition-colors ${
                  rangePreset === p.value
                    ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "text-[var(--color-ink-muted)] hover:bg-[var(--color-panel-raised)] hover:text-[var(--color-ink)]"
                }`}
              >
                {p.label}
              </button>
            ))}
            <button
              type="button"
              role="radio"
              aria-checked={rangePreset === "custom"}
              onClick={() => setRangePreset("custom")}
              className={`rounded-[var(--radius-chip)] px-3 py-1.5 text-xs font-medium transition-colors ${
                rangePreset === "custom"
                  ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                  : "text-[var(--color-ink-muted)] hover:bg-[var(--color-panel-raised)] hover:text-[var(--color-ink)]"
              }`}
            >
              Custom
            </button>
          </div>

          <div className="ml-auto flex items-center gap-2">
            <Toggle checked={live} onChange={setLive} label="Live tail" />
            <span className="flex items-center gap-1 text-sm text-[var(--color-ink-muted)]">
              <Radio
                className={`h-3.5 w-3.5 ${live ? "text-[var(--color-signal)]" : ""}`}
                aria-hidden="true"
              />
              Live{live && paused ? " (paused)" : ""}
            </span>
          </div>
        </div>

        {rangePreset === "custom" && (
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-1.5 text-xs text-[var(--color-ink-muted)]">
              From
              <input
                type="datetime-local"
                value={customSince}
                onChange={(e) => setCustomSince(e.target.value)}
                className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-2 py-1 text-xs text-[var(--color-ink)]"
              />
            </label>
            <label className="flex items-center gap-1.5 text-xs text-[var(--color-ink-muted)]">
              To
              <input
                type="datetime-local"
                value={customUntil}
                onChange={(e) => setCustomUntil(e.target.value)}
                className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-2 py-1 text-xs text-[var(--color-ink)]"
              />
            </label>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => setLevel("")}
            className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
              level === ""
                ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
            }`}
          >
            All levels
          </button>
          {LEVELS.map((l) => (
            <button
              key={l}
              type="button"
              onClick={() => setLevel(level === l ? "" : l)}
              className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                level === l
                  ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                  : `border-[var(--color-line)] hover:border-[var(--color-ink-faint)] ${LEVEL_META[l].textClass}`
              }`}
            >
              {LEVEL_META[l].label}
            </button>
          ))}
        </div>
      </Panel>

      <Panel className="overflow-hidden">
        {pendingEntries.length > 0 && (
          <div className="flex justify-center border-b border-[var(--color-line)] bg-[var(--color-panel-raised)] py-1.5">
            <button
              type="button"
              onClick={handleResumeLive}
              className="rounded-[var(--radius-chip)] bg-[var(--color-signal)]/15 px-3 py-1 text-xs font-medium text-[var(--color-signal)] hover:bg-[var(--color-signal)]/25"
            >
              {pendingEntries.length} new line{pendingEntries.length === 1 ? "" : "s"} — click to
              show
            </button>
          </div>
        )}

        {loading ? (
          <div className="p-5">
            <Skeleton className="h-64 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load logs"
            description={error}
            action={
              <Button
                variant="primary"
                size="sm"
                onClick={() => {
                  setError(null);
                  setLoading(true);
                }}
              >
                Try again
              </Button>
            }
          />
        ) : entries.length === 0 ? (
          <EmptyState
            icon={ScrollText}
            title="No log lines match these filters"
            description="Widen the time range or clear a filter to see more."
          />
        ) : (
          <>
            <div
              ref={listRef}
              onScroll={handleScroll}
              className="max-h-[60vh] overflow-y-auto font-mono text-xs"
            >
              {entries.map((entry) => {
                const expanded = expandedId === entry.id;
                const hasFields = entry.fields && Object.keys(entry.fields).length > 0;
                return (
                  <div key={entry.id} className="border-b border-[var(--color-line-soft)]">
                    <div
                      title={entry.source}
                      className="flex items-start gap-2 px-3 py-1.5 hover:bg-[var(--color-panel-raised)]"
                    >
                      <span className="shrink-0 text-[var(--color-ink-faint)]">
                        {formatLogTimestamp(entry.ts)}
                      </span>
                      <span
                        className={`w-16 shrink-0 uppercase ${LEVEL_META[entry.level].textClass}`}
                      >
                        {entry.level}
                      </span>
                      {entry.unit && (
                        <span className="hidden shrink-0 text-[var(--color-ink-muted)] sm:inline">
                          {entry.unit}
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={() => hasFields && setExpandedId(expanded ? null : entry.id)}
                        className={`min-w-0 flex-1 truncate text-left text-[var(--color-ink)] ${hasFields ? "cursor-pointer" : "cursor-text"}`}
                      >
                        {entry.message}
                      </button>
                      <IconButton
                        aria-label="Copy line"
                        title="Copy line"
                        onClick={() => void handleCopyLine(entry)}
                        className="shrink-0"
                      >
                        <Copy className="h-3.5 w-3.5" aria-hidden="true" />
                      </IconButton>
                    </div>
                    {expanded && hasFields && (
                      <pre className="overflow-x-auto bg-[var(--color-void)]/40 px-3 py-2 text-[var(--color-ink-muted)]">
                        {JSON.stringify(entry.fields, null, 2)}
                      </pre>
                    )}
                  </div>
                );
              })}
              {entries.length >= LOGS_RENDER_CAP && (
                <p className="px-3 py-2 text-center text-[var(--color-ink-faint)]">
                  Showing the most recent {LOGS_RENDER_CAP.toLocaleString()} lines. Narrow your
                  filters to see fewer at once.
                </p>
              )}
            </div>

            {nextBefore && (
              <div className="flex justify-center border-t border-[var(--color-line)] py-3">
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={loadingOlder}
                  onClick={() => void handleLoadOlder()}
                >
                  {loadingOlder ? "Loading…" : "Load older"}
                </Button>
              </div>
            )}
          </>
        )}
      </Panel>
    </div>
  );
}
