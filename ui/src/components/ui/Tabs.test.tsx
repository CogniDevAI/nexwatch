import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Tabs, type TabItem } from "@/components/ui/Tabs";

type Key = "a" | "b" | "c";

const items: TabItem<Key>[] = [
  { key: "a", label: "Alpha" },
  { key: "b", label: "Bravo" },
  { key: "c", label: "Charlie" },
];

/** A thin stateful wrapper so keyboard navigation tests exercise the same
 *  controlled-component flow a real caller (ServerDetail, Agents) uses:
 *  onChange updates the active key, which flows back in as a new prop. */
function ControlledTabs({ initial = "a" }: { initial?: Key }) {
  const [active, setActive] = useState<Key>(initial);
  return <Tabs items={items} activeKey={active} onChange={setActive} aria-label="Test tabs" />;
}

describe("Tabs", () => {
  it("renders a tablist with one tab per item and marks the active one selected", () => {
    render(<ControlledTabs />);

    expect(screen.getByRole("tablist", { name: "Test tabs" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Alpha" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Bravo" })).toHaveAttribute("aria-selected", "false");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveAttribute("aria-selected", "false");
  });

  it("only the active tab is in the natural tab order (roving tabindex)", () => {
    render(<ControlledTabs />);

    expect(screen.getByRole("tab", { name: "Alpha" })).toHaveAttribute("tabIndex", "0");
    expect(screen.getByRole("tab", { name: "Bravo" })).toHaveAttribute("tabIndex", "-1");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveAttribute("tabIndex", "-1");
  });

  it("calls onChange when a tab is clicked", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Tabs items={items} activeKey="a" onChange={onChange} aria-label="Test tabs" />);

    await user.click(screen.getByRole("tab", { name: "Bravo" }));
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("ArrowRight moves selection to the next tab and wraps past the last one", async () => {
    const user = userEvent.setup();
    render(<ControlledTabs />);

    screen.getByRole("tab", { name: "Alpha" }).focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Bravo" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Bravo" })).toHaveFocus();

    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveAttribute("aria-selected", "true");

    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Alpha" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Alpha" })).toHaveFocus();
  });

  it("ArrowLeft moves selection to the previous tab and wraps before the first one", async () => {
    const user = userEvent.setup();
    render(<ControlledTabs />);

    screen.getByRole("tab", { name: "Alpha" }).focus();
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveFocus();
  });

  it("Home and End jump to the first and last tab", async () => {
    const user = userEvent.setup();
    render(<ControlledTabs initial="b" />);

    screen.getByRole("tab", { name: "Bravo" }).focus();
    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "Charlie" })).toHaveAttribute("aria-selected", "true");

    await user.keyboard("{Home}");
    expect(screen.getByRole("tab", { name: "Alpha" })).toHaveAttribute("aria-selected", "true");
  });

  it("renders an icon when the tab item provides one", () => {
    function Icon({ className }: { className?: string }) {
      return <svg data-testid="tab-icon" className={className} />;
    }
    render(
      <Tabs
        items={[{ key: "a", label: "With icon", icon: Icon }]}
        activeKey="a"
        onChange={vi.fn()}
        aria-label="Icon tabs"
      />,
    );
    expect(screen.getByTestId("tab-icon")).toBeInTheDocument();
  });
});
