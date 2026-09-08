import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AckControl } from "./AckControl";

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import { apiFetch } from "@/lib/api";

beforeEach(() => {
  vi.mocked(apiFetch).mockReset();
  vi.mocked(apiFetch).mockResolvedValue(new Response("{}", { status: 200 }));
});

describe("AckControl — not yet acknowledged", () => {
  it("renders nothing for a viewer (canManage=false)", () => {
    const { container } = render(
      <AckControl alertId="al1" acknowledgedAt="" acknowledgedBy="" canManage={false} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders an Acknowledge button for an operator and calls the ack endpoint", async () => {
    const user = userEvent.setup();
    render(<AckControl alertId="al1" acknowledgedAt="" acknowledgedBy="" canManage={true} />);

    await user.click(screen.getByRole("button", { name: "Acknowledge" }));

    await waitFor(() => {
      expect(apiFetch).toHaveBeenCalledWith("/api/custom/alerts/al1/ack", { method: "POST" });
    });
  });
});

describe("AckControl — already acknowledged", () => {
  const acknowledgedAt = new Date().toISOString();

  it("shows who acknowledged it and when, without an Undo button for a viewer", () => {
    render(
      <AckControl
        alertId="al1"
        acknowledgedAt={acknowledgedAt}
        acknowledgedBy="crq8el54kiupuxf (admin@nexwatch.local)"
        canManage={false}
      />,
    );

    expect(
      screen.getByText(/Acknowledged by crq8el54kiupuxf \(admin@nexwatch\.local\)/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Undo" })).not.toBeInTheDocument();
  });

  it("shows an Undo button for an operator that calls the unack endpoint", async () => {
    const user = userEvent.setup();
    render(
      <AckControl
        alertId="al1"
        acknowledgedAt={acknowledgedAt}
        acknowledgedBy="admin@nexwatch.local"
        canManage={true}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Undo" }));

    await waitFor(() => {
      expect(apiFetch).toHaveBeenCalledWith("/api/custom/alerts/al1/unack", { method: "POST" });
    });
  });
});
