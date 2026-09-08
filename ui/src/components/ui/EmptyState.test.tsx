import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Server } from "lucide-react";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders the heading and description", () => {
    render(
      <EmptyState icon={Server} title="No agents connected" description="Add an agent to begin." />,
    );

    expect(screen.getByText("No agents connected")).toBeInTheDocument();
    expect(screen.getByText("Add an agent to begin.")).toBeInTheDocument();
  });

  it("omits the description paragraph when none is given", () => {
    render(<EmptyState icon={Server} title="No agents connected" />);
    expect(screen.queryByText(/add an agent/i)).not.toBeInTheDocument();
  });

  it("renders its action and fires it on click", async () => {
    const onAdd = vi.fn();
    const user = userEvent.setup();

    render(
      <EmptyState
        icon={Server}
        title="No agents connected"
        action={<button onClick={onAdd}>Add agent</button>}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Add agent" }));
    expect(onAdd).toHaveBeenCalledTimes(1);
  });
});
