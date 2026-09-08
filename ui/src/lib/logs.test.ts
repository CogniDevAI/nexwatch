import { describe, expect, it } from "vitest";
import {
  buildLogsQuery,
  buildLogsRealtimeFilter,
  formatLogLineForCopy,
  resolveTimeRange,
} from "@/lib/logs";
import type { LogEntry } from "@/types";

describe("buildLogsQuery", () => {
  it("omits every empty/undefined filter", () => {
    expect(buildLogsQuery({})).toBe("");
  });

  it("includes only the filters that are set", () => {
    const qs = buildLogsQuery({ agentId: "a1", level: "error" });
    const params = new URLSearchParams(qs);
    expect(params.get("agent_id")).toBe("a1");
    expect(params.get("level")).toBe("error");
    expect(params.has("unit")).toBe(false);
    expect(params.has("q")).toBe(false);
  });

  it("includes every supported filter when all are set", () => {
    const qs = buildLogsQuery({
      agentId: "a1",
      level: "warning",
      unit: "nginx.service",
      q: "refused",
      since: 1000,
      until: 2000,
      limit: 50,
      before: "2000_rec1",
    });
    const params = new URLSearchParams(qs);
    expect(params.get("agent_id")).toBe("a1");
    expect(params.get("level")).toBe("warning");
    expect(params.get("unit")).toBe("nginx.service");
    expect(params.get("q")).toBe("refused");
    expect(params.get("since")).toBe("1000");
    expect(params.get("until")).toBe("2000");
    expect(params.get("limit")).toBe("50");
    expect(params.get("before")).toBe("2000_rec1");
  });

  it("omits a falsy limit of 0 the same as an unset one", () => {
    // limit uses `!= null` (not truthiness) so an explicit 0 is preserved —
    // regression check for that intentional choice.
    const qs = buildLogsQuery({ limit: 0 });
    expect(new URLSearchParams(qs).get("limit")).toBe("0");
  });
});

describe("buildLogsRealtimeFilter", () => {
  it("returns an empty string when nothing narrows the subscription", () => {
    expect(buildLogsRealtimeFilter({})).toBe("");
  });

  it("combines every set clause with &&", () => {
    const filter = buildLogsRealtimeFilter({
      agentId: "a1",
      level: "error",
      unit: "nginx.service",
    });
    expect(filter).toBe('agent_id = "a1" && level = "error" && unit = "nginx.service"');
  });

  it("escapes double quotes in a search term", () => {
    const filter = buildLogsRealtimeFilter({ q: 'say "hi"' });
    expect(filter).toBe('message ~ "say \\"hi\\""');
  });
});

describe("resolveTimeRange", () => {
  it("resolves a preset to a since bound relative to now", () => {
    const before = Date.now();
    const { since, until } = resolveTimeRange("15m");
    const after = Date.now();
    expect(until).toBeUndefined();
    expect(since).toBeGreaterThanOrEqual(before - 15 * 60 * 1000);
    expect(since).toBeLessThanOrEqual(after - 15 * 60 * 1000);
  });

  it("passes through explicit custom bounds unchanged", () => {
    const result = resolveTimeRange("custom", { since: 100, until: 200 });
    expect(result).toEqual({ since: 100, until: 200 });
  });

  it("custom with no explicit bounds means no time restriction", () => {
    expect(resolveTimeRange("custom")).toEqual({});
  });
});

describe("formatLogLineForCopy", () => {
  it("includes the unit when present", () => {
    const entry: LogEntry = {
      id: "1",
      agent_id: "a1",
      ts: Date.parse("2026-01-01T00:00:00Z"),
      source: "journald",
      unit: "nginx.service",
      level: "error",
      message: "connection refused",
    };
    const line = formatLogLineForCopy(entry);
    expect(line).toContain("ERROR");
    expect(line).toContain("nginx.service");
    expect(line).toContain("connection refused");
  });

  it("omits the unit segment when absent", () => {
    const entry: LogEntry = {
      id: "2",
      agent_id: "a1",
      ts: Date.parse("2026-01-01T00:00:00Z"),
      source: "file:/var/log/app.log",
      level: "info",
      message: "started",
    };
    const line = formatLogLineForCopy(entry);
    expect(line.split("\t")).toHaveLength(3); // timestamp, level, message — no unit column
  });
});
