import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { SilenceForm } from "@/components/silences/SilenceForm";
import { useAuthStore } from "@/stores/authStore";

const mockPb = pb as unknown as MockPocketBase;

beforeEach(() => {
  mockPb.collection("agents").getFullList.mockReset();
  mockPb.collection("agents").getFullList.mockResolvedValue([
    { id: "a1", hostname: "web-01", tags: ["web"] },
    { id: "a2", hostname: "db-01", tags: ["db"] },
  ]);
  mockPb.collection("checks").getFullList.mockReset();
  mockPb.collection("checks").getFullList.mockResolvedValue([
    { id: "c1", name: "billing-api", tags: ["billing"] },
    { id: "c2", name: "cdn-edge", tags: ["cdn"] },
  ]);
  mockPb.collection("silences").create.mockReset();
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "operator@example.com", name: "", avatar: "", role: "operator" },
  });
});

interface RenderFormOptions {
  initialAgentId?: string;
  initialAgentHostname?: string;
  onSave?: () => void;
  onClose?: () => void;
}

function renderForm(options: RenderFormOptions = {}) {
  const onSave = options.onSave ?? vi.fn();
  const onClose = options.onClose ?? vi.fn();
  const view = render(
    <SilenceForm
      initialAgentId={options.initialAgentId}
      initialAgentHostname={options.initialAgentHostname}
      onSave={onSave}
      onClose={onClose}
    />,
  );
  return { onSave, onClose, ...view };
}

describe("SilenceForm — scope switching", () => {
  it("defaults to 'All agents' scope with no tag input or agent select", () => {
    renderForm();
    expect(screen.getByRole("radio", { name: "All agents" })).toBeChecked();
    expect(screen.queryByLabelText("Silence tags")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Silenced agent")).not.toBeInTheDocument();
  });

  it("shows a tag input when scope is Tags", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("radio", { name: "Tags" }));
    expect(screen.getByLabelText("Silence tags")).toBeInTheDocument();
  });

  it("shows an agent select when scope is One agent", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("radio", { name: "One agent" }));
    expect(await screen.findByLabelText("Silenced agent")).toBeInTheDocument();
  });

  it("prefills scope to One agent and the name when initialAgentId is given", () => {
    renderForm({ initialAgentId: "a1", initialAgentHostname: "web-01" });

    expect(screen.getByRole("radio", { name: "One agent" })).toBeChecked();
    expect(screen.getByLabelText("Name")).toHaveValue("Maintenance: web-01");
  });

  it("shows a tag input when scope is Checks with tags", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("radio", { name: "Checks with tags" }));
    expect(screen.getByLabelText("Silence check tags")).toBeInTheDocument();
  });

  it("shows a check select when scope is One check", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("radio", { name: "One check" }));
    expect(await screen.findByLabelText("Silenced check")).toBeInTheDocument();
    expect(await screen.findByRole("option", { name: "billing-api" })).toBeInTheDocument();
  });

  it("clears the check selection when switching away from One check", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("radio", { name: "One check" }));
    await user.selectOptions(await screen.findByLabelText("Silenced check"), "billing-api");

    await user.click(screen.getByRole("radio", { name: "All agents" }));
    await user.click(screen.getByRole("radio", { name: "One check" }));

    expect(screen.getByLabelText("Silenced check")).toHaveValue("");
  });
});

describe("SilenceForm — validation", () => {
  it("requires a name", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("Enter a name for this silence.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("requires at least one tag when scope is Tags", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("radio", { name: "Tags" }));
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("Add at least one tag.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("requires an agent when scope is One agent", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("radio", { name: "One agent" }));
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("Choose an agent.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("requires at least one tag when scope is Checks with tags", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("radio", { name: "Checks with tags" }));
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("Add at least one tag.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("requires a check when scope is One check", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("radio", { name: "One check" }));
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("Choose a check.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("creates a silence scoped to one check with check_ids set", async () => {
    const user = userEvent.setup();
    mockPb.collection("silences").create.mockResolvedValue({ id: "s-check" });
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Billing API maintenance");
    await user.click(screen.getByRole("radio", { name: "One check" }));
    await user.selectOptions(await screen.findByLabelText("Silenced check"), "billing-api");
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    await waitFor(() => {
      expect(mockPb.collection("silences").create).toHaveBeenCalledWith(
        expect.objectContaining({
          agent_id: "",
          tags: [],
          check_ids: ["c1"],
        }),
      );
    });
  });

  it("rejects a custom end time that is before a custom start time", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("radio", { name: "At a time" }));
    await user.clear(screen.getByLabelText("Start time"));
    await user.type(screen.getByLabelText("Start time"), "2026-06-01T12:00");

    await user.click(screen.getByRole("button", { name: "Custom…" }));
    await user.clear(screen.getByLabelText("End time"));
    await user.type(screen.getByLabelText("End time"), "2026-06-01T10:00");

    await user.click(screen.getByRole("button", { name: "Create silence" }));

    expect(await screen.findByText("End must be after start.")).toBeInTheDocument();
    expect(mockPb.collection("silences").create).not.toHaveBeenCalled();
  });

  it("creates a silence with the default now-start and 1h-end window", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("silences").create.mockResolvedValue({ id: "s1" });
    renderForm({ onSave });

    await user.type(screen.getByLabelText("Name"), "Patch window");
    await user.click(screen.getByRole("button", { name: "Create silence" }));

    await waitFor(() => {
      expect(mockPb.collection("silences").create).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Patch window",
          agent_id: "",
          tags: [],
          created_by: "operator@example.com",
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);

    const payload = mockPb.collection("silences").create.mock.calls[0]![0] as {
      starts_at: string;
      ends_at: string;
    };
    const durationMs = new Date(payload.ends_at).getTime() - new Date(payload.starts_at).getTime();
    expect(durationMs).toBe(3600_000);
  });
});
