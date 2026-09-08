import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { MockPocketBase } from "@/test/mockPocketbase";
import type { AuditLogEntry, Role } from "@/types";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { AuditLog } from "@/pages/settings/AuditLog";
import { useAuthStore } from "@/stores/authStore";

const mockPb = pb as unknown as MockPocketBase;

function setCurrentUser(role: Role): void {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "u1@example.com", name: "", avatar: "", role },
  });
}

function makeEntry(overrides: Partial<AuditLogEntry> = {}): AuditLogEntry {
  return {
    id: "al1",
    actor_id: "u1",
    actor_email: "admin@example.com",
    actor_role: "admin",
    action: "docker.restart",
    target_type: "docker_container",
    target_id: "0123456789ab",
    agent_id: "agent-1",
    details: JSON.stringify({ hostname: "web-01" }),
    result: "success",
    ip: "203.0.113.5",
    request_id: "req-123",
    created: "2026-09-05 10:00:00.000Z",
    updated: "2026-09-05 10:00:00.000Z",
    collectionId: "c1",
    collectionName: "audit_log",
    ...overrides,
  };
}

function listResponse(items: AuditLogEntry[], page = 1, totalPages = 1) {
  return { page, perPage: 50, totalItems: items.length, totalPages, items };
}

function renderAuditPage() {
  return render(
    <MemoryRouter initialEntries={["/settings/audit"]}>
      <Routes>
        <Route path="/settings" element={<div>Settings Page</div>} />
        <Route path="/settings/audit" element={<AuditLog />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
  mockPb.collection("audit_log").getList.mockReset();
  mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([]));
});

describe("AuditLog — access control", () => {
  it("redirects a viewer back to /settings", async () => {
    setCurrentUser("viewer");
    renderAuditPage();

    expect(await screen.findByText("Settings Page")).toBeInTheDocument();
    expect(mockPb.collection("audit_log").getList).not.toHaveBeenCalled();
  });

  it("renders the page for an operator", async () => {
    setCurrentUser("operator");
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()]));

    renderAuditPage();

    expect(await screen.findByText("docker.restart")).toBeInTheDocument();
  });
});

describe("AuditLog — rendering rows", () => {
  beforeEach(() => setCurrentUser("admin"));

  it("shows empty state on a genuinely empty result", async () => {
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([]));

    renderAuditPage();

    expect(await screen.findByText("No audit entries yet")).toBeInTheDocument();
  });

  it("shows an error state when the fetch fails, not the empty state", async () => {
    mockPb.collection("audit_log").getList.mockRejectedValue(new Error("network down"));

    renderAuditPage();

    expect(await screen.findByText("network down")).toBeInTheDocument();
    expect(screen.queryByText("No audit entries yet")).not.toBeInTheDocument();
  });

  it("renders time, actor, action, target, agent, result, and request id", async () => {
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()]));

    renderAuditPage();

    await screen.findByText("docker.restart");
    expect(screen.getByText("admin@example.com")).toBeInTheDocument();
    expect(screen.getByText(/docker_container/)).toBeInTheDocument();
    expect(screen.getByText("agent-1")).toBeInTheDocument();
    expect(screen.getByText("Success")).toBeInTheDocument();
    expect(screen.getByText("req-123")).toBeInTheDocument();
  });

  it("shows a Failure badge for a failed action", async () => {
    mockPb
      .collection("audit_log")
      .getList.mockResolvedValue(listResponse([makeEntry({ result: "failure" })]));

    renderAuditPage();

    expect(await screen.findByText("Failure")).toBeInTheDocument();
  });

  it("expands a row to show the parsed details JSON", async () => {
    const user = userEvent.setup();
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()]));

    renderAuditPage();
    await screen.findByText("docker.restart");

    expect(screen.queryByText(/"hostname"/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show details" }));

    expect(await screen.findByText(/"hostname": "web-01"/)).toBeInTheDocument();
  });
});

describe("AuditLog — filters", () => {
  beforeEach(() => setCurrentUser("admin"));

  it("re-queries with an action filter after debounce", async () => {
    const user = userEvent.setup();
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()]));

    renderAuditPage();
    await screen.findByText("docker.restart");
    mockPb.collection("audit_log").getList.mockClear();

    await user.type(screen.getByLabelText("Filter by action"), "docker");

    await waitFor(() => {
      expect(mockPb.collection("audit_log").getList).toHaveBeenCalledWith(
        1,
        50,
        expect.objectContaining({ filter: expect.stringContaining("docker") }),
      );
    });
  });

  it("re-queries with an actor filter after debounce", async () => {
    const user = userEvent.setup();
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()]));

    renderAuditPage();
    await screen.findByText("docker.restart");
    mockPb.collection("audit_log").getList.mockClear();

    await user.type(screen.getByLabelText("Filter by actor"), "admin@example.com");

    await waitFor(() => {
      expect(mockPb.collection("audit_log").getList).toHaveBeenCalledWith(
        1,
        50,
        expect.objectContaining({ filter: expect.stringContaining("admin@example.com") }),
      );
    });
  });
});

describe("AuditLog — pagination", () => {
  beforeEach(() => setCurrentUser("admin"));

  it("shows a Load more button when more pages exist and appends the next page", async () => {
    const user = userEvent.setup();
    mockPb
      .collection("audit_log")
      .getList.mockResolvedValueOnce(listResponse([makeEntry({ id: "al1" })], 1, 2));

    renderAuditPage();
    await screen.findByText("docker.restart");

    mockPb
      .collection("audit_log")
      .getList.mockResolvedValueOnce(
        listResponse([makeEntry({ id: "al2", action: "alert_rule.update" })], 2, 2),
      );

    await user.click(screen.getByRole("button", { name: "Load more" }));

    expect(await screen.findByText("alert_rule.update")).toBeInTheDocument();
    // The first page's entry must still be visible — Load more appends.
    expect(screen.getByText("docker.restart")).toBeInTheDocument();
  });

  it("does not show a Load more button when there is only one page", async () => {
    mockPb.collection("audit_log").getList.mockResolvedValue(listResponse([makeEntry()], 1, 1));

    renderAuditPage();

    await screen.findByText("docker.restart");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });
});
