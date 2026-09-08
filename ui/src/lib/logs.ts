import type { LogEntry } from "@/types";

export type LogLevel = LogEntry["level"];

export const LEVELS: LogLevel[] = ["error", "warning", "info", "debug"];

interface LevelMeta {
  label: string;
  textClass: string;
}

/** Level -> color mapping, reusing the shared status token palette (error
 *  reads as critical-red, warning as amber; info/debug are plain ink
 *  shades since they aren't actionable signals). See DESIGN.md §1. */
export const LEVEL_META: Record<LogLevel, LevelMeta> = {
  error: { label: "Error", textClass: "text-[var(--color-critical)]" },
  warning: { label: "Warning", textClass: "text-[var(--color-warn)]" },
  info: { label: "Info", textClass: "text-[var(--color-ink-muted)]" },
  debug: { label: "Debug", textClass: "text-[var(--color-ink-faint)]" },
};

/** Rendering every row past this count gets expensive for no benefit — the
 *  list is capped here with a notice instead. */
export const LOGS_RENDER_CAP = 2000;

export type TimeRangePreset = "15m" | "1h" | "24h" | "custom";

export const TIME_RANGE_PRESETS: { value: Exclude<TimeRangePreset, "custom">; label: string }[] = [
  { value: "15m", label: "15 min" },
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
];

const TIME_RANGE_PRESET_SECONDS: Record<Exclude<TimeRangePreset, "custom">, number> = {
  "15m": 15 * 60,
  "1h": 60 * 60,
  "24h": 24 * 60 * 60,
};

/** Resolves a time range preset (or explicit custom bounds) to a
 *  since/until pair in unix milliseconds. "custom" with no explicit bounds
 *  returns undefined for both (no time restriction). */
export function resolveTimeRange(
  preset: TimeRangePreset,
  custom: { since?: number; until?: number } = {},
): { since?: number; until?: number } {
  if (preset === "custom") return custom;
  return { since: Date.now() - TIME_RANGE_PRESET_SECONDS[preset] * 1000, until: undefined };
}

export interface LogQueryFilters {
  agentId?: string;
  level?: LogLevel | "";
  unit?: string;
  q?: string;
  since?: number;
  until?: number;
  limit?: number;
  before?: string;
}

/** Builds the GET /api/custom/logs query string from a set of filters,
 *  omitting empty/undefined values so an unfiltered request has no stray
 *  "agent_id=&level=" noise. */
export function buildLogsQuery(filters: LogQueryFilters): string {
  const params = new URLSearchParams();
  if (filters.agentId) params.set("agent_id", filters.agentId);
  if (filters.level) params.set("level", filters.level);
  if (filters.unit) params.set("unit", filters.unit);
  if (filters.q) params.set("q", filters.q);
  if (filters.since != null) params.set("since", String(filters.since));
  if (filters.until != null) params.set("until", String(filters.until));
  if (filters.limit != null) params.set("limit", String(filters.limit));
  if (filters.before) params.set("before", filters.before);
  return params.toString();
}

/** Escapes a value for embedding in a PocketBase filter expression's
 *  double-quoted string literal. */
function escapeFilterValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

/** Builds the realtime subscribe filter expression matching the current
 *  search filters (excluding time range/cursor, which don't apply to a
 *  live "from now on" stream). Returns "" when nothing narrows the
 *  subscription (every log entry matches). */
export function buildLogsRealtimeFilter(
  filters: Pick<LogQueryFilters, "agentId" | "level" | "unit" | "q">,
): string {
  const clauses: string[] = [];
  if (filters.agentId) clauses.push(`agent_id = "${escapeFilterValue(filters.agentId)}"`);
  if (filters.level) clauses.push(`level = "${escapeFilterValue(filters.level)}"`);
  if (filters.unit) clauses.push(`unit = "${escapeFilterValue(filters.unit)}"`);
  if (filters.q) clauses.push(`message ~ "${escapeFilterValue(filters.q)}"`);
  return clauses.join(" && ");
}

/** Formats a unix-milliseconds timestamp as a compact local time string
 *  suitable for a dense monospace log list. */
export function formatLogTimestamp(ts: number): string {
  return new Date(ts).toLocaleString(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** One plain-text line for the "copy line" action: timestamp, level, unit
 *  (if any), and message, tab-separated. */
export function formatLogLineForCopy(entry: LogEntry): string {
  const parts = [formatLogTimestamp(entry.ts), entry.level.toUpperCase()];
  if (entry.unit) parts.push(entry.unit);
  parts.push(entry.message);
  return parts.join("\t");
}
