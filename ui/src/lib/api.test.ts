import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch } from "@/lib/api";
import type { MockPocketBase } from "@/test/mockPocketbase";

// The factory is self-contained (its own dynamic import, no references to
// outer-scope variables) so Vitest's module hoisting can't race it against
// a not-yet-initialized top-level const.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase("http://localhost:8090") };
});

import pb from "@/lib/pocketbase";
const mockPb = pb as unknown as MockPocketBase;

function jsonResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("apiFetch", () => {
  const originalLocation = window.location;

  beforeEach(() => {
    mockPb.authStore.token = "";
    mockPb.authStore.record = null;
    mockPb.authStore.isSuperuser = false;
    mockPb.authStore.isValid = false;
    mockPb.authStore.clear.mockClear();

    vi.stubGlobal("fetch", vi.fn());

    // jsdom's location.href setter throws "Not implemented: navigation", so
    // swap in a plain writable object for the duration of each test.
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, href: "" },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: originalLocation,
    });
  });

  it("resolves a relative path against pb.baseUrl", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200));

    await apiFetch("/api/custom/metrics?agent=abc");

    expect(fetch).toHaveBeenCalledTimes(1);
    const [url] = vi.mocked(fetch).mock.calls[0]!;
    expect(url).toBe("http://localhost:8090/api/custom/metrics?agent=abc");
  });

  it("always sets the Authorization header from pb.authStore.token", async () => {
    mockPb.authStore.token = "secret-token";
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200));

    await apiFetch("/api/custom/agents");

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const headers = init!.headers as Headers;
    expect(headers.get("Authorization")).toBe("secret-token");
  });

  it("falls back to an empty Authorization header when there is no token", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200));

    await apiFetch("/api/custom/agents");

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const headers = init!.headers as Headers;
    expect(headers.get("Authorization")).toBe("");
  });

  it("preserves caller-provided headers alongside Authorization", async () => {
    mockPb.authStore.token = "secret-token";
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200));

    await apiFetch("/api/custom/agents", {
      headers: { "Content-Type": "application/json", "X-Custom": "1" },
    });

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const headers = init!.headers as Headers;
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(headers.get("X-Custom")).toBe("1");
    expect(headers.get("Authorization")).toBe("secret-token");
  });

  it("on a 401 response, clears auth and redirects to /login", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(401));

    const response = await apiFetch("/api/custom/agents");

    expect(mockPb.authStore.clear).toHaveBeenCalledTimes(1);
    expect(window.location.href).toBe("/login");
    expect(response.status).toBe(401);
  });

  it("returns non-401 responses untouched, without clearing auth or navigating", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200, { ok: true }));

    const response = await apiFetch("/api/custom/agents");

    expect(mockPb.authStore.clear).not.toHaveBeenCalled();
    expect(window.location.href).toBe("");
    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ ok: true });
  });
});
