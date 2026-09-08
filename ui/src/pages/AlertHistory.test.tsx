import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { AlertHistory } from "@/pages/AlertHistory";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import type { Check } from "@/types";

const mockPb = pb as unknown as MockPocketBase;

function makeCheck(overrides: Partial<Check> & Pick<Check, "id" | "name">): Check {
  return {
    target: "",
    type: "http",
    interval_seconds: 60,
    timeout_seconds: 5,
    method: "GET",
    expected_status: 200,
    expected_body_contains: "",
    verify_tls: true,
    tls_expiry_warn_days: 14,
    failures_before_down: 1,
    enabled: true,
    tags: [],
    collectionId: "c2",
    collectionName: "checks",
    created: "",
    updated: "",
    ...overrides,
  };
}

function makeAlert(overrides: Record<string, unknown> = {}) {
  return {
    id: "al1",
    rule_id: "r1",
    agent_id: "a1",
    status: "firing",
    value: 91.2,
    message: "cpu high",
    fired_at: "2026-09-05 10:00:00.000Z",
    resolved_at: "",
    silenced: false,
    acknowledged_at: "",
    acknowledged_by: "",
    escalated_at: "",
    created: "2026-09-05 10:00:00.000Z",
    updated: "2026-09-05 10:00:00.000Z",
    collectionId: "c1",
    collectionName: "alerts",
    ...overrides,
  };
}

beforeEach(() => {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "op@example.com", name: "", avatar: "", role: "operator" },
  });
  mockPb.collection("alerts").getFullList.mockReset();
  mockPb.collection("alerts").getFullList.mockResolvedValue([]);
  mockPb.collection("alerts").subscribe.mockReset();
  mockPb.collection("alerts").subscribe.mockResolvedValue(vi.fn());
  mockPb.collection("alert_rules").getFullList.mockReset();
  mockPb.collection("alert_rules").getFullList.mockResolvedValue([]);
  // Agents/checks now come from the shared stores (AppShell owns fetching
  // them in production) rather than a page-local getFullList — seed them
  // directly instead of mocking pb for these two collections.
  useAgentStore.setState({ agents: [] });
  useChecksStore.setState({ checks: [] });
});

describe("AlertHistory — filters", () => {
  it("renders Acknowledged and Silenced filters alongside the existing ones", async () => {
    render(<AlertHistory />);
    await screen.findByLabelText("Filter by status");

    expect(screen.getByLabelText("Filter by acknowledged")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by silenced")).toBeInTheDocument();
  });

  it("refetches with an acknowledged_at clause when 'Acknowledged: yes' is chosen", async () => {
    const user = userEvent.setup();
    render(<AlertHistory />);
    await screen.findByLabelText("Filter by status");

    mockPb.collection("alerts").getFullList.mockClear();
    await user.selectOptions(screen.getByLabelText("Filter by acknowledged"), "yes");

    await waitFor(() => {
      expect(mockPb.collection("alerts").getFullList).toHaveBeenCalledWith(
        expect.objectContaining({ filter: expect.stringContaining("acknowledged_at != ''") }),
      );
    });
  });

  it("refetches with a silenced clause when 'Silenced: yes' is chosen", async () => {
    const user = userEvent.setup();
    render(<AlertHistory />);
    await screen.findByLabelText("Filter by status");

    mockPb.collection("alerts").getFullList.mockClear();
    await user.selectOptions(screen.getByLabelText("Filter by silenced"), "yes");

    await waitFor(() => {
      expect(mockPb.collection("alerts").getFullList).toHaveBeenCalledWith(
        expect.objectContaining({ filter: expect.stringContaining("silenced = true") }),
      );
    });
  });
});

describe("AlertHistory — check-based alerts", () => {
  it("shows the check's name (not a blank host) and links to /checks for a check-based alert", async () => {
    useChecksStore.setState({
      checks: [
        makeCheck({ id: "chk1", name: "billing-api", target: "https://billing.internal/health" }),
      ],
    });
    mockPb
      .collection("alerts")
      .getFullList.mockResolvedValue([
        makeAlert({ agent_id: "", check_id: "chk1", message: "check_down: billing-api is down" }),
      ]);

    render(
      <MemoryRouter>
        <AlertHistory />
      </MemoryRouter>,
    );

    // The row renders once in the desktop table and once in the mobile
    // card layout (see DESIGN.md — both exist in the DOM at once; only CSS
    // media queries decide which one a real viewport shows), so assert on
    // "at least one match" rather than a single element.
    const links = await screen.findAllByRole("link", { name: "billing-api" });
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      expect(link).toHaveAttribute("href", "/checks");
    }
  });

  it("keeps a check-based alert visible under the default 'All agents' filter", async () => {
    useChecksStore.setState({
      checks: [makeCheck({ id: "chk2", name: "cdn-edge", target: "cdn.example.com" })],
    });
    mockPb
      .collection("alerts")
      .getFullList.mockResolvedValue([makeAlert({ id: "al2", agent_id: "", check_id: "chk2" })]);

    render(
      <MemoryRouter>
        <AlertHistory />
      </MemoryRouter>,
    );
    await screen.findByLabelText("Filter by agent");

    expect(screen.getByLabelText("Filter by agent")).toHaveValue("");
    expect((await screen.findAllByRole("link", { name: "cdn-edge" })).length).toBeGreaterThan(0);
    // The agent_id filter clause must be omitted entirely under "All
    // agents", not sent as an empty-string equality that would exclude
    // every check-based alert (agent_id = '').
    expect(mockPb.collection("alerts").getFullList).toHaveBeenCalledWith(
      expect.objectContaining({ filter: undefined }),
    );
  });
});

describe("AlertHistory — flags and acknowledgement", () => {
  it("shows Silenced and Escalated badges and an Acknowledge button for a firing alert", async () => {
    mockPb
      .collection("alerts")
      .getFullList.mockResolvedValue([
        makeAlert({ silenced: true, escalated_at: "2026-09-05 10:05:00.000Z" }),
      ]);

    render(<AlertHistory />);

    expect((await screen.findAllByText("Silenced")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("Escalated").length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: "Acknowledge" }).length).toBeGreaterThan(0);
  });

  it("shows the acknowledged summary instead of an Acknowledge button once acknowledged", async () => {
    mockPb.collection("alerts").getFullList.mockResolvedValue([
      makeAlert({
        acknowledged_at: "2026-09-05 10:05:00.000Z",
        acknowledged_by: "admin@nexwatch.local",
      }),
    ]);

    render(<AlertHistory />);

    expect(
      (await screen.findAllByText(/Acknowledged by admin@nexwatch\.local/)).length,
    ).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: "Acknowledge" })).not.toBeInTheDocument();
  });
});
