import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { AlertRuleForm } from "@/components/alerts/AlertRuleForm";

const mockPb = pb as unknown as MockPocketBase;

beforeEach(() => {
  mockPb.collection("agents").getFullList.mockReset();
  mockPb.collection("agents").getFullList.mockResolvedValue([
    { id: "a1", hostname: "web-01", tags: ["web", "prod"] },
    { id: "a2", hostname: "db-01", tags: ["db"] },
  ]);
  mockPb.collection("notification_channels").getFullList.mockReset();
  mockPb.collection("notification_channels").getFullList.mockResolvedValue([]);
  mockPb.collection("checks").getFullList.mockReset();
  mockPb.collection("checks").getFullList.mockResolvedValue([
    { id: "chk1", name: "Billing API" },
    { id: "chk2", name: "Postgres" },
  ]);
  mockPb.collection("alert_rules").create.mockReset();
  mockPb.collection("alert_rules").update.mockReset();
});

function renderForm(onSave = vi.fn(), onClose = vi.fn()) {
  return render(<AlertRuleForm onSave={onSave} onClose={onClose} />);
}

describe("AlertRuleForm — conditional fields by metric type", () => {
  it("shows condition and threshold for a resource metric (cpu, the default)", async () => {
    renderForm();
    await screen.findByLabelText("Metric type");

    expect(screen.getByLabelText("Condition")).toBeInTheDocument();
    expect(screen.getByLabelText("Threshold")).toBeInTheDocument();
    expect(screen.queryByLabelText("Process name")).not.toBeInTheDocument();
  });

  it("hides condition/threshold and shows a target field for process_down", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "process_down");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Threshold")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Process name")).toBeInTheDocument();
  });

  it("hides condition/threshold and shows a target field for service_failed", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "service_failed");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Service name")).toBeInTheDocument();
  });

  it("hides condition/threshold and the target field for agent_offline", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "agent_offline");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Threshold")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Process name")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Service name")).not.toBeInTheDocument();
  });

  it("keeps the duration field for every metric type", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "agent_offline");
    expect(screen.getByLabelText("Duration")).toBeInTheDocument();
  });

  it("hides Condition but keeps Threshold and shows a pattern field for log_match", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "log_match");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Match count threshold")).toBeInTheDocument();
    expect(screen.getByLabelText("Log pattern")).toBeInTheDocument();
    expect(screen.getByLabelText("Duration")).toBeInTheDocument();
  });
});

describe("AlertRuleForm — targeting radio", () => {
  it("defaults to 'All agents' with no tag input or agent select shown", async () => {
    renderForm();
    await screen.findByLabelText("Metric type");

    expect(screen.getByRole("radio", { name: "All agents" })).toBeChecked();
    expect(screen.queryByLabelText("Target tags")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Target agent")).not.toBeInTheDocument();
  });

  it("shows a tag input when 'Agents with tags' is selected", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.click(screen.getByRole("radio", { name: "Agents with tags" }));

    expect(screen.getByLabelText("Target tags")).toBeInTheDocument();
    expect(screen.queryByLabelText("Target agent")).not.toBeInTheDocument();
  });

  it("shows an agent select when 'One agent' is selected", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.click(screen.getByRole("radio", { name: "One agent" }));

    expect(await screen.findByLabelText("Target agent")).toBeInTheDocument();
    expect(screen.queryByLabelText("Target tags")).not.toBeInTheDocument();
  });

  it("clears the agent selection when switching back to 'All agents'", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.click(screen.getByRole("radio", { name: "One agent" }));
    await user.selectOptions(await screen.findByLabelText("Target agent"), "a1");
    await user.click(screen.getByRole("radio", { name: "All agents" }));

    expect(screen.queryByLabelText("Target agent")).not.toBeInTheDocument();
  });
});

describe("AlertRuleForm — validation", () => {
  it("blocks submission and shows an inline error when the name is empty", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.click(screen.getByRole("button", { name: "Create rule" }));

    expect(await screen.findByText("Enter a name for this rule.")).toBeInTheDocument();
    expect(mockPb.collection("alert_rules").create).not.toHaveBeenCalled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("requires at least one tag when targeting is 'Agents with tags'", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "Tagged rule");
    await user.click(screen.getByRole("radio", { name: "Agents with tags" }));
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    expect(await screen.findByText("Add at least one tag.")).toBeInTheDocument();
    expect(mockPb.collection("alert_rules").create).not.toHaveBeenCalled();
  });

  it("requires a target for process_down", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "Nginx down");
    await user.selectOptions(screen.getByLabelText("Metric type"), "process_down");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    expect(
      await screen.findByText("Enter a process name or command-line substring to match."),
    ).toBeInTheDocument();
    expect(mockPb.collection("alert_rules").create).not.toHaveBeenCalled();
  });

  it("submits a valid resource-metric rule targeting all agents", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "High CPU");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "High CPU",
          metric_type: "cpu",
          agent_id: "",
          target_tags: [],
          target: "",
          escalation_after: 0,
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});

describe("AlertRuleForm — cve_count", () => {
  it("keeps condition/threshold and shows the CVE scope select for cve_count", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "cve_count");

    expect(screen.getByLabelText("Condition")).toBeInTheDocument();
    expect(screen.getByLabelText("Threshold")).toBeInTheDocument();
    expect(screen.queryByLabelText("Process name")).not.toBeInTheDocument();
    const scope = screen.getByLabelText("Count");
    expect(scope).toBeInTheDocument();
    expect(scope).toHaveValue("");
  });

  it("submits cve_count with an empty target when 'Critical + high severity' is left selected", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "cve_count");
    await user.type(screen.getByLabelText("Rule name"), "Too many CVEs");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          metric_type: "cve_count",
          target: "",
        }),
      );
    });
  });

  it("submits cve_count with target='critical' when 'Critical only' is selected", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "cve_count");
    await user.type(screen.getByLabelText("Rule name"), "Critical CVEs");
    await user.selectOptions(screen.getByLabelText("Count"), "critical");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          metric_type: "cve_count",
          target: "critical",
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});

describe("AlertRuleForm — log_match", () => {
  it("requires a pattern for log_match", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "Disk errors");
    await user.selectOptions(screen.getByLabelText("Metric type"), "log_match");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    expect(
      await screen.findByText("Enter a substring or /regex/ pattern to match."),
    ).toBeInTheDocument();
    expect(mockPb.collection("alert_rules").create).not.toHaveBeenCalled();
  });

  it("submits log_match with the pattern as target and the entered threshold", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "log_match");
    await user.type(screen.getByLabelText("Rule name"), "Disk write errors");
    await user.type(screen.getByLabelText("Log pattern"), "disk write error");
    const thresholdInput = screen.getByLabelText("Match count threshold");
    await user.clear(thresholdInput);
    await user.type(thresholdInput, "5");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          metric_type: "log_match",
          target: "disk write error",
          threshold: 5,
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});

describe("AlertRuleForm — check-based rule types", () => {
  it("hides condition/threshold and the agent targeting radio for check_down, showing a check selector instead", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "check_down");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Threshold")).not.toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: "All agents" })).not.toBeInTheDocument();
    expect(await screen.findByLabelText("Check")).toBeInTheDocument();
  });

  it("shows a 'warn within days' field instead of Condition/Threshold for cert_expiry", async () => {
    const user = userEvent.setup();
    renderForm();
    await screen.findByLabelText("Metric type");

    await user.selectOptions(screen.getByLabelText("Metric type"), "cert_expiry");

    expect(screen.queryByLabelText("Condition")).not.toBeInTheDocument();
    expect(
      screen.getByLabelText("Warn when certificate expires within (days)"),
    ).toBeInTheDocument();
    expect(await screen.findByLabelText("Check")).toBeInTheDocument();
  });

  it("submits check_down scoped to a specific check", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "Billing check down");
    await user.selectOptions(screen.getByLabelText("Metric type"), "check_down");
    await user.selectOptions(await screen.findByLabelText("Check"), "chk1");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Billing check down",
          metric_type: "check_down",
          check_id: "chk1",
          agent_id: "",
          target_tags: [],
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("submits cert_expiry with an empty check_id when 'All checks' is left selected", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    mockPb.collection("alert_rules").create.mockResolvedValue({ id: "r1" });
    renderForm(onSave);
    await screen.findByLabelText("Metric type");

    await user.type(screen.getByLabelText("Rule name"), "Cert expiring");
    await user.selectOptions(screen.getByLabelText("Metric type"), "cert_expiry");
    const daysInput = screen.getByLabelText("Warn when certificate expires within (days)");
    await user.clear(daysInput);
    await user.type(daysInput, "7");
    await user.click(screen.getByRole("button", { name: "Create rule" }));

    await waitFor(() => {
      expect(mockPb.collection("alert_rules").create).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "Cert expiring",
          metric_type: "cert_expiry",
          check_id: "",
          threshold: 7,
        }),
      );
    });
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});
