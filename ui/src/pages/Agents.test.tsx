import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
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
import { Agents } from "@/pages/Agents";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useAlertsStore } from "@/stores/alertsStore";
import type { Agent } from "@/types";

const mockPb = pb as unknown as MockPocketBase;

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "a1",
    name: "web-01",
    hostname: "web-01",
    os: "Ubuntu 22.04",
    ip: "10.0.0.4",
    version: "0.3.1",
    status: "online",
    last_seen: "2026-09-05T10:00:00.000Z",
    tags: [],
    arch: "amd64",
    platform: "linux",
    update_status: "",
    update_error: "",
    update_target_version: "",
    update_requested_at: "",
    update_requested_by: "",
    collectionId: "c1",
    collectionName: "agents",
    created: "",
    updated: "",
    ...overrides,
  };
}
const mockApiFetch = apiFetch as unknown as ReturnType<
  typeof vi.fn<(path: string, init?: RequestInit) => Promise<Response>>
>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

beforeEach(() => {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "op@example.com", name: "", avatar: "", role: "operator" },
  });
  useAgentStore.setState({ agents: [], loading: false, error: null });
  useAlertsStore.setState({ firingAlerts: [], rules: {}, loading: false, error: null });

  mockApiFetch.mockReset();
  mockApiFetch.mockImplementation((path: string) => {
    if (path.includes("/token")) {
      return Promise.resolve(jsonResponse({ token: "plaintext-token-abc123" }));
    }
    if (path.includes("/latest-version")) {
      return Promise.resolve(jsonResponse({ version: "", published_at: "", cached: false }));
    }
    return Promise.resolve(jsonResponse({}));
  });

  mockPb.collection("agents").create.mockReset();
  mockPb.collection("agents").create.mockResolvedValue({
    id: "new-agent-1",
    name: "web-01",
    hostname: "web-01",
  });
  mockPb.collection("settings").getFullList.mockReset();
  mockPb.collection("settings").getFullList.mockResolvedValue([]);
});

async function openAddAgentModalWithToken() {
  const user = userEvent.setup();
  render(<Agents />);

  await user.click(await screen.findByRole("button", { name: /add agent/i }));
  await user.type(screen.getByLabelText(/agent name/i), "web-01");
  await user.click(screen.getByRole("button", { name: /generate token/i }));

  // Step 2 (install command) only renders once the token mint resolves.
  await screen.findByText(/agent token generated/i);
  return user;
}

describe("Agents — Add agent modal OS tabs", () => {
  it("defaults to the Linux tab, showing the Standard and Oracle curl commands", async () => {
    await openAddAgentModalWithToken();

    const linuxTab = screen.getByRole("tab", { name: /linux/i });
    expect(linuxTab).toHaveAttribute("aria-selected", "true");

    expect(screen.getByText(/standard \(linux\)/i)).toBeInTheDocument();
    expect(screen.getByText(/oracle db/i)).toBeInTheDocument();
    expect(screen.getAllByText(/curl -fsSL/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/powershell/i)).not.toBeInTheDocument();
  });

  it("switches to the Windows tab and shows the PowerShell install command", async () => {
    const user = await openAddAgentModalWithToken();

    await user.click(screen.getByRole("tab", { name: /windows/i }));

    const windowsTab = screen.getByRole("tab", { name: /windows/i });
    expect(windowsTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /linux/i })).toHaveAttribute("aria-selected", "false");

    expect(screen.getByText(/run as administrator/i)).toBeInTheDocument();
    expect(screen.getByText(/install-agent\.ps1/)).toBeInTheDocument();
    expect(screen.getByText(/Invoke-WebRequest/)).toBeInTheDocument();
    // The Linux-only blocks should no longer be rendered while on the
    // Windows tab.
    expect(screen.queryByText(/standard \(linux\)/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/oracle db/i)).not.toBeInTheDocument();
  });

  it("includes the generated token and hub URL in the Windows command", async () => {
    const user = await openAddAgentModalWithToken();
    await user.click(screen.getByRole("tab", { name: /windows/i }));

    const command = screen.getByText(/Invoke-WebRequest/).textContent ?? "";
    expect(command).toContain("plaintext-token-abc123");
    expect(command).toContain("install-agent.ps1");
    expect(command).toContain("-Hub");
    expect(command).toContain("-Token");
  });

  it("resets to the Linux tab after the modal is closed and reopened", async () => {
    const user = await openAddAgentModalWithToken();
    await user.click(screen.getByRole("tab", { name: /windows/i }));
    expect(screen.getByRole("tab", { name: /windows/i })).toHaveAttribute("aria-selected", "true");

    // handleCloseAddModal resets installOS back to "linux" alongside the
    // rest of the modal's step-1/step-2 state. Modal renders two elements
    // sharing the "Close dialog" label (the backdrop and the header "X"
    // button) — either triggers the same onClose handler.
    const [closeButton] = screen.getAllByRole("button", { name: /close dialog/i });
    await user.click(closeButton!);

    await user.click(screen.getByRole("button", { name: /add agent/i }));
    await user.type(screen.getByLabelText(/agent name/i), "web-02");
    await user.click(screen.getByRole("button", { name: /generate token/i }));
    await screen.findByText(/agent token generated/i);

    expect(screen.getByRole("tab", { name: /linux/i })).toHaveAttribute("aria-selected", "true");
  });
});

describe("Agents — row actions overflow menu (R2)", () => {
  // Four labeled ghost buttons don't fit a row at desktop table widths or on
  // a 390px mobile card — Update and Edit tags stay direct buttons, while
  // Regenerate token and Delete move into a "More actions" menu shared by
  // both the desktop table and the mobile card layout.
  it("keeps Edit tags as a direct button and moves Regenerate token/Delete into a menu", async () => {
    useAgentStore.setState({ agents: [makeAgent()], loading: false, error: null });
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Agents />
      </MemoryRouter>,
    );

    await screen.findAllByText("web-01");

    // Direct actions are visible without opening anything.
    expect(screen.getAllByRole("button", { name: "Edit tags" }).length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: "Regenerate token" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();

    const [menuTrigger] = screen.getAllByRole("button", { name: "More actions for web-01" });
    await user.click(menuTrigger!);

    expect(screen.getByRole("menuitem", { name: "Regenerate token" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
  });

  it("choosing Delete from the menu shows the same inline Confirm/Cancel row it always did", async () => {
    useAgentStore.setState({ agents: [makeAgent()], loading: false, error: null });
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Agents />
      </MemoryRouter>,
    );

    await screen.findAllByText("web-01");

    const [menuTrigger] = screen.getAllByRole("button", { name: "More actions for web-01" });
    await user.click(menuTrigger!);
    await user.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(screen.getAllByRole("button", { name: "Confirm" }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: "Cancel" }).length).toBeGreaterThan(0);
  });
});
