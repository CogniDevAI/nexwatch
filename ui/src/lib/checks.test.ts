import { describe, expect, it } from "vitest";
import { checkStatus, formatLatency, formatUptime, summarizeChecks } from "@/lib/checks";
import type { CheckSummary } from "@/types";

function makeSummary(overrides: Partial<CheckSummary> = {}): CheckSummary {
  return {
    id: "c1",
    name: "test",
    type: "http",
    target: "https://example.com",
    latency_ms: 10,
    cert_expiring_soon: false,
    uptime_24h: 100,
    uptime_7d: 100,
    enabled: true,
    ...overrides,
  };
}

describe("checkStatus", () => {
  it("reads as offline when the check has never run", () => {
    expect(checkStatus(makeSummary({ status: undefined }))).toBe("offline");
  });

  it("reads as critical when down", () => {
    expect(checkStatus(makeSummary({ status: "down" }))).toBe("critical");
  });

  it("reads as warning when up but the certificate is expiring soon", () => {
    expect(checkStatus(makeSummary({ status: "up", cert_expiring_soon: true }))).toBe("warning");
  });

  it("reads as ok when up with no expiring certificate", () => {
    expect(checkStatus(makeSummary({ status: "up", cert_expiring_soon: false }))).toBe("ok");
  });
});

describe("summarizeChecks", () => {
  it("counts up, down, and expiring-soon independently", () => {
    const checks = [
      makeSummary({ status: "up" }),
      makeSummary({ status: "up", cert_expiring_soon: true }),
      makeSummary({ status: "down" }),
      makeSummary({ status: "down", cert_expiring_soon: true }),
      makeSummary({ status: undefined }),
    ];
    expect(summarizeChecks(checks)).toEqual({ up: 2, down: 2, expiringSoon: 2 });
  });

  it("returns zeros for an empty list", () => {
    expect(summarizeChecks([])).toEqual({ up: 0, down: 0, expiringSoon: 0 });
  });
});

describe("formatLatency", () => {
  it("formats sub-second latency in milliseconds", () => {
    expect(formatLatency(42)).toBe("42 ms");
  });

  it("formats latency at or above 1000ms in seconds", () => {
    expect(formatLatency(1500)).toBe("1.5 s");
  });
});

describe("formatUptime", () => {
  it("formats a percentage to two decimal places", () => {
    expect(formatUptime(99.9)).toBe("99.90%");
  });
});
