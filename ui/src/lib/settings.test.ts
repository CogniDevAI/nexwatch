import { describe, expect, it, vi, beforeEach } from "vitest";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { upsertSetting, loadSettingsMap, asString, asBool, asNumber } from "@/lib/settings";

const mockPb = pb as unknown as MockPocketBase;

describe("asString / asBool / asNumber", () => {
  it("returns the value when it matches the expected type", () => {
    expect(asString("hello", "fallback")).toBe("hello");
    expect(asBool(true, false)).toBe(true);
    expect(asNumber(42, 0)).toBe(42);
  });

  it("returns the fallback for a mismatched or missing type", () => {
    expect(asString(undefined, "fallback")).toBe("fallback");
    expect(asString(42, "fallback")).toBe("fallback");
    expect(asBool("true", false)).toBe(false);
    expect(asBool(undefined, true)).toBe(true);
    expect(asNumber("30", 7)).toBe(7);
    expect(asNumber(Number.NaN, 7)).toBe(7);
  });
});

describe("upsertSetting", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("JSON-encodes the value before creating a new record when none exists", async () => {
    const getFirstListItem = vi.fn().mockRejectedValue(new Error("not found"));
    const create = vi.fn().mockResolvedValue({ id: "s1" });
    mockPb.collection = vi.fn().mockReturnValue({ getFirstListItem, create }) as never;

    await upsertSetting("status_page_enabled", true);

    expect(create).toHaveBeenCalledWith({ key: "status_page_enabled", value: "true" });
  });

  it("JSON-encodes a string value with quotes, and updates an existing record", async () => {
    const getFirstListItem = vi.fn().mockResolvedValue({ id: "existing-1" });
    const update = vi.fn().mockResolvedValue({ id: "existing-1" });
    mockPb.collection = vi.fn().mockReturnValue({ getFirstListItem, update }) as never;

    await upsertSetting("status_page_title", "Acme status");

    expect(update).toHaveBeenCalledWith("existing-1", { value: '"Acme status"' });
  });
});

describe("loadSettingsMap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("decodes every record's JSON value into a plain map", async () => {
    const getFullList = vi.fn().mockResolvedValue([
      { id: "1", key: "status_page_enabled", value: "true" },
      { id: "2", key: "status_page_title", value: '"Acme status"' },
      { id: "3", key: "status_page_show_uptime_days", value: "30" },
    ]);
    mockPb.collection = vi.fn().mockReturnValue({ getFullList }) as never;

    const map = await loadSettingsMap();

    expect(map).toEqual({
      status_page_enabled: true,
      status_page_title: "Acme status",
      status_page_show_uptime_days: 30,
    });
  });

  it("skips a record whose value fails to parse as JSON", async () => {
    const getFullList = vi.fn().mockResolvedValue([
      { id: "1", key: "good", value: "1" },
      { id: "2", key: "bad", value: "not json" },
    ]);
    mockPb.collection = vi.fn().mockReturnValue({ getFullList }) as never;

    const map = await loadSettingsMap();

    expect(map).toEqual({ good: 1 });
  });
});
