import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import { apiFetch } from "@/lib/api";
import { UpdateAllModal } from "@/components/agents/UpdateAllModal";
import { ToastProvider } from "@/components/ui/Toast";
import type { Agent } from "@/types";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    collectionId: "agents",
    collectionName: "agents",
    created: "",
    updated: "",
    name: "web-01",
    hostname: "web-01",
    os: "ubuntu 22.04",
    ip: "10.0.0.4",
    version: "0.9.0",
    status: "online",
    last_seen: "",
    tags: [],
    arch: "amd64",
    platform: "linux",
    update_status: "",
    update_error: "",
    update_target_version: "",
    update_requested_at: "",
    update_requested_by: "",
    ...overrides,
  };
}

beforeEach(() => {
  vi.mocked(apiFetch).mockReset();
});

describe("UpdateAllModal", () => {
  it("lists the affected agents and disables confirm when there are none", () => {
    render(
      <ToastProvider>
        <UpdateAllModal affectedAgents={[]} defaultVersion="0.9.1" onClose={vi.fn()} />
      </ToastProvider>,
    );
    expect(screen.getByText(/No connected agents are currently behind/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Update.*agent\(s\)/ })).toBeDisabled();
  });

  it("calls update-all with only_outdated true and the edited version", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(
        JSON.stringify({ version: "0.9.2", results: [{ agent_id: "agent-1", ok: true }] }),
        { status: 200 },
      ),
    );
    const onClose = vi.fn();
    const agents = [makeAgent({ id: "agent-1", hostname: "web-01", version: "0.9.0" })];

    render(
      <ToastProvider>
        <UpdateAllModal affectedAgents={agents} defaultVersion="0.9.1" onClose={onClose} />
      </ToastProvider>,
    );

    expect(screen.getByText("web-01", { exact: false })).toBeInTheDocument();

    const input = screen.getByLabelText("Target version");
    await userEvent.clear(input);
    await userEvent.type(input, "0.9.2");
    await userEvent.click(screen.getByRole("button", { name: /Update 1 agent\(s\)/ }));

    expect(apiFetch).toHaveBeenCalledWith("/api/custom/agents/update-all", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ version: "0.9.2", only_outdated: true }),
    });
    await screen.findByText(/Update to 0.9.2 requested for 1 agent/);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("reports partial failures without hiding the successes", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          version: "0.9.1",
          results: [
            { agent_id: "a1", ok: true },
            { agent_id: "a2", ok: false, error: "not connected" },
          ],
        }),
        { status: 200 },
      ),
    );
    const agents = [makeAgent({ id: "a1" }), makeAgent({ id: "a2", hostname: "web-02" })];

    render(
      <ToastProvider>
        <UpdateAllModal affectedAgents={agents} defaultVersion="0.9.1" onClose={vi.fn()} />
      </ToastProvider>,
    );

    await userEvent.click(screen.getByRole("button", { name: /Update 2 agent\(s\)/ }));

    await screen.findByText(/1 failed to acknowledge/);
  });
});
