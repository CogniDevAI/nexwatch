import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import { StatusPage } from "@/pages/StatusPage";
import type { PublicStatusResponse } from "@/types";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function statusResponse(overrides: Partial<PublicStatusResponse> = {}): PublicStatusResponse {
  return {
    title: "Acme status",
    description: "Real-time status of Acme's services",
    overall: "operational",
    updated_at: new Date().toISOString(),
    items: [],
    ...overrides,
  };
}

describe("StatusPage", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("shows a friendly disabled state on a 404 response", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(404, { error: "status page is not enabled" }));

    render(<StatusPage />);

    expect(await screen.findByText("This status page is not enabled")).toBeInTheDocument();
  });

  it("shows an error state with a retry action on a server error", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(500, { error: "boom" }));

    render(<StatusPage />);

    expect(await screen.findByText("Couldn't load status")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("renders the title, overall banner, and items when enabled", async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse(
        200,
        statusResponse({
          overall: "degraded",
          items: [
            {
              type: "check",
              label: "Billing API",
              status: "operational",
              uptime_24h: 100,
              uptime_7d: 100,
              uptime_30d: 99.98,
              latency_ms: 42,
              daily: [{ date: "2026-09-04", uptime: 100, incidents: 0 }],
            },
            {
              type: "agent",
              label: "Web tier",
              status: "degraded",
              uptime_24h: 95,
              uptime_7d: 97,
              uptime_30d: 98,
              daily: [{ date: "2026-09-04", uptime: 95, incidents: 1 }],
            },
          ],
        }),
      ),
    );

    render(<StatusPage />);

    expect(await screen.findByText("Acme status")).toBeInTheDocument();
    expect(screen.getByText("Some systems degraded")).toBeInTheDocument();
    expect(screen.getByText("Billing API")).toBeInTheDocument();
    expect(screen.getByText("Web tier")).toBeInTheDocument();
    expect(screen.getByText("99.98%")).toBeInTheDocument();

    // Never renders a raw id or internal field name — only what the API
    // response's admin-typed label/type/status fields provide.
    expect(screen.queryByText(/latency_ms/i)).not.toBeInTheDocument();
  });

  it("shows an empty-monitoring message when enabled but no items are configured", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(200, statusResponse({ items: [] })));

    render(<StatusPage />);

    await waitFor(() => {
      expect(screen.getByText("Nothing is being monitored yet.")).toBeInTheDocument();
    });
  });
});
