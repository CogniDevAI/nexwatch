import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { Checks } from "@/pages/Checks";
import { useAuthStore } from "@/stores/authStore";
import { useChecksStore } from "@/stores/checksStore";

const mockPb = pb as unknown as MockPocketBase;
const mockApiFetch = apiFetch as unknown as ReturnType<
  typeof vi.fn<(path: string, init?: RequestInit) => Promise<Response>>
>;

function makeCheck(overrides: Record<string, unknown> = {}) {
  return {
    id: "chk1",
    name: "Billing API",
    type: "http",
    target: "https://billing.internal/health",
    interval_seconds: 60,
    timeout_seconds: 5,
    method: "GET",
    expected_status: 200,
    expected_body_contains: "",
    verify_tls: true,
    tls_expiry_warn_days: 14,
    failures_before_down: 2,
    enabled: true,
    tags: [],
    created: "2026-09-05 10:00:00.000Z",
    updated: "2026-09-05 10:00:00.000Z",
    collectionId: "c1",
    collectionName: "checks",
    ...overrides,
  };
}

function summaryResponse(checks: Record<string, unknown>[]) {
  return new Response(JSON.stringify({ checks, total: checks.length }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function makeSummaryEntry(overrides: Record<string, unknown> = {}) {
  return {
    id: "chk1",
    name: "Billing API",
    type: "http",
    target: "https://billing.internal/health",
    status: "up",
    latency_ms: 42,
    last_checked_at: "2026-09-05T10:00:00Z",
    cert_expiring_soon: false,
    uptime_24h: 100,
    uptime_7d: 100,
    enabled: true,
    ...overrides,
  };
}

beforeEach(() => {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "op@example.com", name: "", avatar: "", role: "operator" },
  });
  useChecksStore.setState({ checks: [], loading: false, error: null });

  mockPb.collection("checks").getFullList.mockReset();
  mockPb.collection("checks").getFullList.mockResolvedValue([]);
  mockPb.collection("checks").subscribe.mockReset();
  mockPb.collection("checks").subscribe.mockResolvedValue(vi.fn());
  mockPb.collection("checks").delete.mockReset();

  mockApiFetch.mockReset();
  mockApiFetch.mockResolvedValue(summaryResponse([]));
});

describe("Checks — summary strip counts", () => {
  it("shows up/down/expiring-soon counts derived from the summary endpoint", async () => {
    // Checks (like agents/alerts/silences) is fetched once by AppShell in
    // production — a standalone page render seeds the shared store
    // directly instead, matching that ownership. See DESIGN.md §10.
    useChecksStore.setState({
      checks: [
        makeCheck({ id: "chk1", name: "Billing API" }),
        makeCheck({ id: "chk2", name: "Postgres", type: "tcp" }),
        makeCheck({ id: "chk3", name: "Gateway ping", type: "icmp" }),
      ],
      loading: false,
      error: null,
    });
    mockApiFetch.mockResolvedValue(
      summaryResponse([
        makeSummaryEntry({ id: "chk1", status: "up", cert_expiring_soon: true }),
        makeSummaryEntry({ id: "chk2", name: "Postgres", status: "down" }),
        makeSummaryEntry({ id: "chk3", name: "Gateway ping", status: "up" }),
      ]),
    );

    render(<Checks />);

    expect(await screen.findByText("2 up")).toBeInTheDocument();
    expect(screen.getByText("1 down")).toBeInTheDocument();
    expect(screen.getByText("1 certificate expiring soon")).toBeInTheDocument();
  });
});

describe("Checks — run now", () => {
  it("calls the run endpoint and refreshes the summary", async () => {
    const user = userEvent.setup();
    useChecksStore.setState({ checks: [makeCheck()], loading: false, error: null });
    mockApiFetch.mockImplementation((path: string) => {
      if (path.endsWith("/run")) {
        return Promise.resolve(
          new Response(JSON.stringify({ id: "res1", check_id: "chk1", status: "up" }), {
            status: 200,
          }),
        );
      }
      return Promise.resolve(summaryResponse([makeSummaryEntry()]));
    });

    render(<Checks />);
    await screen.findByText("Billing API");

    const summaryCallsBefore = mockApiFetch.mock.calls.filter(([p]) =>
      String(p).endsWith("/summary"),
    ).length;

    await user.click(screen.getByRole("button", { name: "Run Billing API now" }));

    await waitFor(() => {
      expect(mockApiFetch).toHaveBeenCalledWith(
        "/api/custom/checks/chk1/run",
        expect.objectContaining({ method: "POST" }),
      );
    });

    await waitFor(() => {
      const summaryCallsAfter = mockApiFetch.mock.calls.filter(([p]) =>
        String(p).endsWith("/summary"),
      ).length;
      expect(summaryCallsAfter).toBeGreaterThan(summaryCallsBefore);
    });
  });
});

describe("Checks — empty state", () => {
  it("shows an empty state and a create action when there are no checks", async () => {
    render(<Checks />);

    expect(await screen.findByText("No checks configured")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New check" })).toBeInTheDocument();
  });
});
