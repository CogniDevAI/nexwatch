import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import { apiFetch } from "@/lib/api";
import { UpdateAgentModal } from "@/components/agents/UpdateAgentModal";
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

function renderModal(onClose = vi.fn()) {
  return {
    onClose,
    ...render(
      <ToastProvider>
        <UpdateAgentModal agent={makeAgent()} defaultVersion="0.9.1" onClose={onClose} />
      </ToastProvider>,
    ),
  };
}

beforeEach(() => {
  vi.mocked(apiFetch).mockReset();
});

describe("UpdateAgentModal", () => {
  it("pre-fills the target version field with defaultVersion", () => {
    renderModal();
    expect(screen.getByLabelText("Target version")).toHaveValue("0.9.1");
  });

  it("calls the update endpoint with the edited version and closes on success", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    const onClose = vi.fn();
    renderModal(onClose);

    const input = screen.getByLabelText("Target version");
    await userEvent.clear(input);
    await userEvent.type(input, "0.9.2");
    await userEvent.click(screen.getByRole("button", { name: "Update agent" }));

    expect(apiFetch).toHaveBeenCalledWith("/api/custom/agents/agent-1/update", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ version: "0.9.2" }),
    });
    await screen.findByText(/Update to 0.9.2 requested/);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows the server's error and does not close on failure", async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ ok: false, error: "agent os/arch is not known yet" }), {
        status: 400,
      }),
    );
    const onClose = vi.fn();
    renderModal(onClose);

    await userEvent.click(screen.getByRole("button", { name: "Update agent" }));

    await screen.findByText("agent os/arch is not known yet");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("disables the confirm button when the version field is empty", async () => {
    renderModal();
    const input = screen.getByLabelText("Target version");
    await userEvent.clear(input);
    expect(screen.getByRole("button", { name: "Update agent" })).toBeDisabled();
  });
});
