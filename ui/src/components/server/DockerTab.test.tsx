import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
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
import { DockerTab } from "@/components/server/DockerTab";
import { useAuthStore } from "@/stores/authStore";
import { ToastProvider } from "@/components/ui/Toast";

const mockPb = pb as unknown as MockPocketBase;
const mockApiFetch = apiFetch as unknown as ReturnType<
  typeof vi.fn<(path: string, init?: RequestInit) => Promise<Response>>
>;

function makeContainer(overrides: Record<string, unknown> = {}) {
  return {
    id: "dc1",
    agent_id: "agent-1",
    container_id: "0123456789abcdef0123456789abcdef01234567",
    name: "web-nginx",
    image: "nginx:alpine",
    status: "running",
    cpu_percent: 2.5,
    memory_usage: 1024 * 1024 * 50,
    memory_limit: 1024 * 1024 * 512,
    network_rx: 1024,
    network_tx: 2048,
    created: "2026-09-05 10:00:00.000Z",
    updated: "2026-09-05 10:00:00.000Z",
    collectionId: "c1",
    collectionName: "docker_containers",
    ...overrides,
  };
}

function renderTab() {
  return render(
    <ToastProvider>
      <DockerTab agentId="agent-1" />
    </ToastProvider>,
  );
}

function setRole(role: "viewer" | "operator" | "admin") {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "u1", email: "u@example.com", name: "", avatar: "", role },
  });
}

beforeEach(() => {
  setRole("operator");

  mockPb.collection("docker_containers").getFullList.mockReset();
  mockPb.collection("docker_containers").getFullList.mockResolvedValue([]);
  mockPb.collection("docker_containers").subscribe.mockReset();
  mockPb.collection("docker_containers").subscribe.mockResolvedValue(vi.fn());

  mockApiFetch.mockReset();
  mockApiFetch.mockResolvedValue(
    new Response(JSON.stringify({ ok: true, state: "exited" }), { status: 200 }),
  );
});

describe("DockerTab — actions menu and confirmation", () => {
  it("shows Stop/Restart (not Start) for a running container to an operator", async () => {
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([makeContainer()]);

    renderTab();

    await screen.findByText("web-nginx");
    expect(screen.getByRole("button", { name: /Stop/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Restart/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Start/ })).not.toBeInTheDocument();
  });

  it("shows Start (not Stop/Restart) for a stopped container", async () => {
    mockPb
      .collection("docker_containers")
      .getFullList.mockResolvedValue([makeContainer({ status: "exited" })]);

    renderTab();

    await screen.findByText("web-nginx");
    expect(screen.getByRole("button", { name: /Start/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Stop/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Restart/ })).not.toBeInTheDocument();
  });

  it("renders no action buttons for a viewer", async () => {
    setRole("viewer");
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([makeContainer()]);

    renderTab();

    await screen.findByText("web-nginx");
    expect(screen.queryByRole("button", { name: /Stop|Restart|Start/ })).not.toBeInTheDocument();
  });

  it("opens a confirmation modal naming the container and action before calling the endpoint", async () => {
    const user = userEvent.setup();
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([makeContainer()]);

    renderTab();
    await screen.findByText("web-nginx");

    await user.click(screen.getByRole("button", { name: /Restart/ }));

    // The confirmation dialog must name both the container and the action —
    // the endpoint must not be called until it's explicitly confirmed.
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/web-nginx/)).toBeInTheDocument();
    expect(within(dialog).getByRole("heading", { name: "Restart container" })).toBeInTheDocument();
    expect(mockApiFetch).not.toHaveBeenCalled();

    // Two "Restart" buttons now exist: the row action and the modal's
    // confirm button (in DOM order) — click the modal's.
    await user.click(screen.getAllByRole("button", { name: "Restart" })[1]);

    await waitFor(() => {
      expect(mockApiFetch).toHaveBeenCalledWith(
        "/api/custom/agents/agent-1/docker/0123456789abcdef0123456789abcdef01234567/restart",
        { method: "POST" },
      );
    });
  });

  it("calls the exact start endpoint path for a stopped container", async () => {
    const user = userEvent.setup();
    mockPb
      .collection("docker_containers")
      .getFullList.mockResolvedValue([
        makeContainer({ status: "exited", container_id: "abc123abc123" }),
      ]);

    renderTab();
    await screen.findByText("web-nginx");

    await user.click(screen.getByRole("button", { name: /Start/ }));
    await user.click(screen.getAllByRole("button", { name: "Start" })[1]);

    await waitFor(() => {
      expect(mockApiFetch).toHaveBeenCalledWith(
        "/api/custom/agents/agent-1/docker/abc123abc123/start",
        { method: "POST" },
      );
    });
  });

  it("shows a success toast when the agent reports the action succeeded", async () => {
    const user = userEvent.setup();
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([makeContainer()]);
    mockApiFetch.mockResolvedValue(
      new Response(JSON.stringify({ ok: true, state: "running" }), { status: 200 }),
    );

    renderTab();
    await screen.findByText("web-nginx");

    await user.click(screen.getByRole("button", { name: /Restart/ }));
    await user.click(screen.getAllByRole("button", { name: "Restart" })[1]);

    expect(await screen.findByText(/web-nginx restarted/)).toBeInTheDocument();
  });

  it("shows an error toast with the agent's error when the action fails", async () => {
    const user = userEvent.setup();
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([makeContainer()]);
    mockApiFetch.mockResolvedValue(
      new Response(JSON.stringify({ ok: false, error: "no such container" }), { status: 502 }),
    );

    renderTab();
    await screen.findByText("web-nginx");

    await user.click(screen.getByRole("button", { name: /Restart/ }));
    await user.click(screen.getAllByRole("button", { name: "Restart" })[1]);

    expect(await screen.findByText("no such container")).toBeInTheDocument();
  });
});

describe("DockerTab — update available badge and filter", () => {
  it("renders the update-available badge with the remote digest on hover", async () => {
    mockPb.collection("docker_containers").getFullList.mockResolvedValue([
      makeContainer({
        update_available: true,
        image_digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        remote_digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      }),
    ]);

    renderTab();

    const badge = await screen.findByText("Update available");
    expect(badge.closest("span")).toHaveAttribute("title", expect.stringContaining("bbbbbbbbbbbb"));
  });

  it("does not render the badge for a container with no update available", async () => {
    mockPb
      .collection("docker_containers")
      .getFullList.mockResolvedValue([makeContainer({ update_available: false })]);

    renderTab();

    await screen.findByText("web-nginx");
    expect(screen.queryByText("Update available")).not.toBeInTheDocument();
  });

  it("filters to only containers with an update available when the toggle is on", async () => {
    const user = userEvent.setup();
    mockPb
      .collection("docker_containers")
      .getFullList.mockResolvedValue([
        makeContainer({ id: "dc1", name: "up-to-date", update_available: false }),
        makeContainer({ id: "dc2", name: "outdated", update_available: true }),
      ]);

    renderTab();
    await screen.findByText("up-to-date");
    expect(screen.getByText("outdated")).toBeInTheDocument();

    await user.click(screen.getByRole("switch"));

    expect(screen.queryByText("up-to-date")).not.toBeInTheDocument();
    expect(screen.getByText("outdated")).toBeInTheDocument();
  });
});
