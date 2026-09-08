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
import { LogsView } from "@/components/logs/LogsView";
import { useAgentStore } from "@/stores/agentStore";
import { ToastProvider } from "@/components/ui/Toast";
import type { LogEntry } from "@/types";

const mockPb = pb as unknown as MockPocketBase;
const mockApiFetch = apiFetch as unknown as ReturnType<
  typeof vi.fn<(path: string, init?: RequestInit) => Promise<Response>>
>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

function makeEntry(overrides: Partial<LogEntry> = {}): LogEntry {
  return {
    id: "l1",
    agent_id: "agent-1",
    ts: Date.parse("2026-09-05T10:00:00Z"),
    source: "journald",
    unit: "nginx.service",
    level: "error",
    message: "connection refused",
    ...overrides,
  };
}

function renderView(agentId?: string) {
  return render(
    <ToastProvider>
      <LogsView agentId={agentId} />
    </ToastProvider>,
  );
}

beforeEach(() => {
  mockApiFetch.mockReset();
  mockApiFetch.mockImplementation((path: string) => {
    if (path.includes("/logs/units")) {
      return Promise.resolve(jsonResponse({ units: ["nginx.service"] }));
    }
    return Promise.resolve(jsonResponse({ entries: [] }));
  });
  useAgentStore.setState({
    agents: [
      {
        id: "agent-1",
        hostname: "web-01",
        os: "linux",
        ip: "",
        version: "",
        status: "online",
        last_seen: "",
        tags: [],
        created: "",
        updated: "",
        collectionId: "c",
        collectionName: "agents",
      },
    ],
    loading: false,
    error: null,
  });
});

describe("LogsView — filters build the right query string", () => {
  it("includes the fixed agent_id when scoped to one agent", async () => {
    mockApiFetch.mockResolvedValueOnce(jsonResponse({ entries: [makeEntry()] }));
    renderView("agent-1");

    await waitFor(() => {
      const call = mockApiFetch.mock.calls.find(([p]) => p.startsWith("/api/custom/logs?"));
      expect(call).toBeDefined();
    });

    const call = mockApiFetch.mock.calls.find(([p]) => p.startsWith("/api/custom/logs?"))!;
    const params = new URLSearchParams(call[0].split("?")[1]);
    expect(params.get("agent_id")).toBe("agent-1");
  });

  it("adds level to the query string when a level chip is selected", async () => {
    const user = userEvent.setup();
    mockApiFetch.mockResolvedValue(jsonResponse({ entries: [] }));
    renderView("agent-1");

    await screen.findByRole("button", { name: "Error" });
    mockApiFetch.mockClear();
    await user.click(screen.getByRole("button", { name: "Error" }));

    await waitFor(() => {
      const call = mockApiFetch.mock.calls.find(([p]) => p.startsWith("/api/custom/logs?"));
      expect(call).toBeDefined();
      const params = new URLSearchParams(call![0].split("?")[1]);
      expect(params.get("level")).toBe("error");
    });
  });

  it("shows the agent selector only when no fixed agentId is given", async () => {
    renderView();
    expect(await screen.findByLabelText("Agent")).toBeInTheDocument();
  });

  it("hides the agent selector when scoped to one agent", async () => {
    renderView("agent-1");
    await screen.findByLabelText("Unit");
    expect(screen.queryByLabelText("Agent")).not.toBeInTheDocument();
  });
});

describe("LogsView — cursor pagination", () => {
  it("appends older entries and requests the next page with the returned cursor", async () => {
    mockApiFetch.mockImplementation((path: string) => {
      if (path.includes("/logs/units")) return Promise.resolve(jsonResponse({ units: [] }));
      if (path.includes("before=")) {
        return Promise.resolve(
          jsonResponse({ entries: [makeEntry({ id: "older", message: "oldest entry" })] }),
        );
      }
      return Promise.resolve(
        jsonResponse({
          entries: [makeEntry({ id: "newer", message: "newest entry" })],
          next_before: "1000_newer",
        }),
      );
    });

    const user = userEvent.setup();
    renderView("agent-1");

    await screen.findByText("newest entry");
    const loadOlder = await screen.findByRole("button", { name: "Load older" });
    await user.click(loadOlder);

    await screen.findByText("oldest entry");
    expect(screen.getByText("newest entry")).toBeInTheDocument();

    const secondCall = mockApiFetch.mock.calls.find(([p]) => p.includes("before="));
    expect(secondCall).toBeDefined();
    expect(secondCall![0]).toContain("before=1000_newer");
  });
});

describe("LogsView — live toggle", () => {
  it("subscribes on enabling Live and unsubscribes on disabling it", async () => {
    const unsubscribe = vi.fn();
    mockPb.collection("logs").subscribe.mockResolvedValue(unsubscribe);

    const user = userEvent.setup();
    renderView("agent-1");
    await screen.findByLabelText("Unit");

    await user.click(screen.getByRole("switch", { name: "Live tail" }));

    await waitFor(() => {
      expect(mockPb.collection("logs").subscribe).toHaveBeenCalledWith(
        "*",
        expect.any(Function),
        expect.objectContaining({ filter: expect.stringContaining("agent_id") }),
      );
    });

    await user.click(screen.getByRole("switch", { name: "Live tail" }));

    await waitFor(() => {
      expect(unsubscribe).toHaveBeenCalled();
    });
  });
});
