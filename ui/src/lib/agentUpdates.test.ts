import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import { apiFetch } from "@/lib/api";
import {
  isUpdateInProgress,
  updateStatusLabel,
  isVersionOlder,
  updateAvailable,
  fetchLatestVersion,
} from "@/lib/agentUpdates";

describe("isUpdateInProgress", () => {
  it.each(["started", "downloading", "verifying", "installing", "restarting"] as const)(
    "is true for %s",
    (status) => {
      expect(isUpdateInProgress(status)).toBe(true);
    },
  );

  it.each(["", "idle", "done", "failed", "restart_required", undefined] as const)(
    "is false for %s",
    (status) => {
      expect(isUpdateInProgress(status)).toBe(false);
    },
  );
});

describe("updateStatusLabel", () => {
  it("labels every in-progress stage", () => {
    expect(updateStatusLabel("started")).toBe("Starting…");
    expect(updateStatusLabel("downloading")).toBe("Downloading…");
    expect(updateStatusLabel("verifying")).toBe("Verifying…");
    expect(updateStatusLabel("installing")).toBe("Installing…");
    expect(updateStatusLabel("restarting")).toBe("Restarting…");
  });

  it("labels failed distinctly", () => {
    expect(updateStatusLabel("failed")).toBe("Update failed");
  });

  it("labels restart_required distinctly", () => {
    expect(updateStatusLabel("restart_required")).toBe("Restart required");
  });

  it.each(["", "idle", "done", undefined] as const)("returns empty for %s", (status) => {
    expect(updateStatusLabel(status)).toBe("");
  });
});

describe("isVersionOlder", () => {
  it("reports true when current is behind target", () => {
    expect(isVersionOlder("0.9.0", "0.9.1")).toBe(true);
    expect(isVersionOlder("0.8.9", "0.9.0")).toBe(true);
    expect(isVersionOlder("1.0.0", "2.0.0")).toBe(true);
  });

  it("reports false when current is equal or ahead", () => {
    expect(isVersionOlder("0.9.1", "0.9.1")).toBe(false);
    expect(isVersionOlder("0.9.2", "0.9.1")).toBe(false);
    expect(isVersionOlder("1.0.0", "0.9.9")).toBe(false);
  });

  it("tolerates a leading 'v' on either side", () => {
    expect(isVersionOlder("v0.9.0", "0.9.1")).toBe(true);
    expect(isVersionOlder("0.9.0", "v0.9.1")).toBe(true);
  });

  it("falls back to string inequality for unparsable versions", () => {
    expect(isVersionOlder("dev", "0.9.1")).toBe(true);
    expect(isVersionOlder("0.9.1", "0.9.1")).toBe(false);
  });

  it("returns false when either side is empty", () => {
    expect(isVersionOlder("", "0.9.1")).toBe(false);
    expect(isVersionOlder("0.9.0", "")).toBe(false);
  });
});

describe("updateAvailable", () => {
  it("is true when the agent is behind compareVersion", () => {
    expect(updateAvailable({ version: "0.9.0" }, "0.9.1")).toBe(true);
  });

  it("is false when the agent is current or compareVersion is unset", () => {
    expect(updateAvailable({ version: "0.9.1" }, "0.9.1")).toBe(false);
    expect(updateAvailable({ version: "0.9.0" }, "")).toBe(false);
  });
});

describe("fetchLatestVersion", () => {
  beforeEach(() => {
    vi.mocked(apiFetch).mockReset();
  });

  it("parses a successful response", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(
        JSON.stringify({ version: "0.9.5", published_at: "2026-01-01T00:00:00Z", cached: true }),
        { status: 200 },
      ),
    );

    const info = await fetchLatestVersion();
    expect(info).toEqual({
      version: "0.9.5",
      publishedAt: "2026-01-01T00:00:00Z",
      cached: true,
      error: undefined,
    });
    expect(apiFetch).toHaveBeenCalledWith("/api/custom/agents/latest-version");
  });

  it("surfaces a soft error from the hub without throwing", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ error: "network unreachable" }), { status: 200 }),
    );

    const info = await fetchLatestVersion();
    expect(info.version).toBe("");
    expect(info.error).toBe("network unreachable");
  });

  it("throws on a non-2xx transport failure", async () => {
    vi.mocked(apiFetch).mockResolvedValue(new Response("", { status: 500 }));
    await expect(fetchLatestVersion()).rejects.toThrow();
  });
});
