import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UpdateAvailableBadge, UpdateStatusChip } from "@/components/agents/UpdateBadges";

describe("UpdateAvailableBadge", () => {
  it("renders the target version in its tooltip", () => {
    render(<UpdateAvailableBadge targetVersion="0.9.1" />);
    expect(screen.getByText("Update available")).toHaveAttribute(
      "title",
      "Update to 0.9.1 available",
    );
  });
});

describe("UpdateStatusChip", () => {
  it.each(["", "idle", "done", undefined] as const)("renders nothing for %s", (status) => {
    const { container } = render(<UpdateStatusChip status={status} />);
    expect(container).toBeEmptyDOMElement();
  });

  it.each([
    ["started", "Starting…"],
    ["downloading", "Downloading…"],
    ["verifying", "Verifying…"],
    ["installing", "Installing…"],
    ["restarting", "Restarting…"],
  ] as const)("shows the %s stage label", (status, label) => {
    render(<UpdateStatusChip status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("shows a failed chip with the error as a tooltip", () => {
    render(<UpdateStatusChip status="failed" error="checksum mismatch" />);
    expect(screen.getByText("Update failed")).toHaveAttribute("title", "checksum mismatch");
  });

  it("shows a Restart required chip with an explanatory tooltip", () => {
    render(<UpdateStatusChip status="restart_required" />);
    expect(screen.getByText("Restart required")).toHaveAttribute(
      "title",
      expect.stringContaining("restarts"),
    );
  });

  it("does not treat restart_required as an in-progress spinner state", () => {
    render(<UpdateStatusChip status="restart_required" />);
    expect(screen.queryByText("Restarting…")).not.toBeInTheDocument();
  });

  it("shows a Retry action only when onRetry is provided, and calls it on click", async () => {
    const onRetry = vi.fn();
    render(<UpdateStatusChip status="failed" onRetry={onRetry} />);
    const retryButton = screen.getByRole("button", { name: "Retry" });
    await userEvent.click(retryButton);
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("omits the Retry action when onRetry is not provided", () => {
    render(<UpdateStatusChip status="failed" />);
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });
});
