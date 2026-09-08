import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MockPocketBase } from "@/test/mockPocketbase";

// Self-contained factories (own dynamic import, no outer-scope references) so
// Vitest's module hoisting can't race them against a not-yet-initialized
// top-level const — matches the pattern used by AlertRuleForm.test.tsx and
// Users.test.tsx.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});
vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { NotificationChannels } from "@/pages/NotificationChannels";
import { useAuthStore } from "@/stores/authStore";
import { ToastProvider } from "@/components/ui/Toast";

const mockPb = pb as unknown as MockPocketBase;

function setOperator() {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "me", email: "op@example.com", name: "Op", avatar: "", role: "operator" },
  });
}

function renderPage() {
  return render(
    <ToastProvider>
      <NotificationChannels />
    </ToastProvider>,
  );
}

/**
 * A required config field's <Label required> appends a "*" marker to its
 * text (see components/ui/Field.tsx), so its accessible name is e.g.
 * "Webhook URL*" rather than the plain field label. This matches any label
 * that starts with the given text, required marker or not.
 */
function labelStartingWith(text: string): RegExp {
  return new RegExp(`^${text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`);
}

beforeEach(() => {
  useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
  mockPb.collection("notification_channels").getFullList.mockReset();
  mockPb.collection("notification_channels").getFullList.mockResolvedValue([]);
  mockPb.collection("notification_channels").create.mockReset();
  mockPb.collection("notification_channels").create.mockResolvedValue({ id: "new-1" });
  vi.mocked(apiFetch).mockReset();
});

async function openNewChannelForm() {
  setOperator();
  renderPage();
  await screen.findByText("No notification channels configured");
  await userEvent.click(screen.getByRole("button", { name: "Add channel" }));
  await screen.findByLabelText("Channel type");
}

describe("NotificationChannels — new channel types render their fields", () => {
  it.each([
    ["Slack", "slack", ["Webhook URL"]],
    ["Microsoft Teams", "teams", ["Webhook URL"]],
    ["PagerDuty", "pagerduty", ["Integration key"]],
    ["ntfy", "ntfy", ["Server URL", "Topic", "Access token", "Priority"]],
    ["Gotify", "gotify", ["Server URL", "Application token", "Priority"]],
    ["Browser push", "webpush", ["Audience"]],
  ])("selecting %s shows its config fields", async (label, _type, fieldLabels) => {
    await openNewChannelForm();

    await userEvent.selectOptions(screen.getByLabelText("Channel type"), label);

    for (const fieldLabel of fieldLabels) {
      expect(screen.getByLabelText(labelStartingWith(fieldLabel))).toBeInTheDocument();
    }
  });

  it("switching away from a type clears its previously entered config", async () => {
    await openNewChannelForm();

    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Slack");
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Webhook URL")),
      "https://hooks.slack.com/services/x",
    );

    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "PagerDuty");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Slack");

    expect(screen.getByLabelText(labelStartingWith("Webhook URL"))).toHaveValue("");
  });
});

describe("NotificationChannels — submit sends the expected config shape", () => {
  it("creates a Slack channel with webhook_url in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "Ops Slack");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Slack");
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Webhook URL")),
      "https://hooks.slack.com/services/T000/B000/XXXX",
    );
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    expect(mockPb.collection("notification_channels").create).toHaveBeenCalledTimes(1);
    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      name: string;
      type: string;
      config: string;
      enabled: boolean;
    };
    expect(payload.name).toBe("Ops Slack");
    expect(payload.type).toBe("slack");
    expect(JSON.parse(payload.config)).toEqual({
      webhook_url: "https://hooks.slack.com/services/T000/B000/XXXX",
    });
  });

  it("creates a PagerDuty channel with routing_key in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "On-call PagerDuty");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "PagerDuty");
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Integration key")),
      "rk_abcdef1234567890",
    );
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(payload.type).toBe("pagerduty");
    expect(JSON.parse(payload.config)).toEqual({ routing_key: "rk_abcdef1234567890" });
  });

  it("creates an ntfy channel with topic, optional server_url and token in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "ntfy mobile");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "ntfy");
    await userEvent.type(screen.getByLabelText(labelStartingWith("Topic")), "nexwatch-alerts");
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(payload.type).toBe("ntfy");
    expect(JSON.parse(payload.config)).toEqual({
      server_url: "",
      topic: "nexwatch-alerts",
      token: "",
      priority: 0,
    });
  });

  it("creates an ntfy channel with an explicit priority in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "ntfy urgent");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "ntfy");
    await userEvent.type(screen.getByLabelText(labelStartingWith("Topic")), "nexwatch-alerts");
    await userEvent.type(screen.getByLabelText(labelStartingWith("Priority")), "5");
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(JSON.parse(payload.config)).toMatchObject({ priority: 5 });
  });

  it("creates a Gotify channel with server_url and app_token in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "Gotify self-hosted");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Gotify");
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Server URL")),
      "https://gotify.example.com",
    );
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Application token")),
      "A_secret_token",
    );
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(payload.type).toBe("gotify");
    expect(JSON.parse(payload.config)).toEqual({
      server_url: "https://gotify.example.com",
      app_token: "A_secret_token",
      priority: 0,
    });
  });

  it("creates a Gotify channel with an explicit priority in config", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "Gotify high-priority");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Gotify");
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Server URL")),
      "https://gotify.example.com",
    );
    await userEvent.type(
      screen.getByLabelText(labelStartingWith("Application token")),
      "A_secret_token",
    );
    await userEvent.type(screen.getByLabelText(labelStartingWith("Priority")), "9");
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(JSON.parse(payload.config)).toMatchObject({ priority: 9 });
  });

  it('creates a Browser push channel defaulting to the "All users" audience', async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "Everyone's browsers");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Browser push");
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(payload.type).toBe("webpush");
    expect(JSON.parse(payload.config)).toEqual({ audience: "all" });
  });

  it("creates a Browser push channel with an explicitly chosen audience", async () => {
    await openNewChannelForm();

    await userEvent.type(screen.getByLabelText("Channel name"), "Admins only push");
    await userEvent.selectOptions(screen.getByLabelText("Channel type"), "Browser push");
    await userEvent.selectOptions(screen.getByLabelText(labelStartingWith("Audience")), "admins");
    await userEvent.click(screen.getByRole("button", { name: "Create channel" }));

    const payload = mockPb.collection("notification_channels").create.mock.calls[0][0] as {
      type: string;
      config: string;
    };
    expect(payload.type).toBe("webpush");
    expect(JSON.parse(payload.config)).toEqual({ audience: "admins" });
  });
});

describe("NotificationChannels — list and test flow", () => {
  it("shows the channel's raw type label in the list", async () => {
    setOperator();
    mockPb.collection("notification_channels").getFullList.mockReset();
    mockPb.collection("notification_channels").getFullList.mockResolvedValue([
      {
        id: "c1",
        name: "Ops Slack",
        type: "slack",
        enabled: true,
        config: { webhook_url: "https://hooks.slack.com/services/x" },
      },
    ]);

    renderPage();

    expect(await screen.findByText("Ops Slack")).toBeInTheDocument();
    expect(screen.getByText("slack")).toBeInTheDocument();
  });

  it("sends the test notification request for a PagerDuty channel", async () => {
    setOperator();
    mockPb.collection("notification_channels").getFullList.mockReset();
    mockPb.collection("notification_channels").getFullList.mockResolvedValue([
      {
        id: "pd1",
        name: "On-call PagerDuty",
        type: "pagerduty",
        enabled: true,
        config: { routing_key: "rk_123" },
      },
    ]);
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ status: "ok" }), { status: 200 }),
    );

    renderPage();
    await screen.findByText("On-call PagerDuty");

    await userEvent.click(
      screen.getByRole("button", { name: "Send test notification to On-call PagerDuty" }),
    );

    expect(apiFetch).toHaveBeenCalledWith("/api/custom/notifications/pd1/test", {
      method: "POST",
    });
  });
});
