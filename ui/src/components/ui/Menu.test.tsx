import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Menu } from "@/components/ui/Menu";

describe("Menu", () => {
  it("hides the menu items until the trigger is opened", () => {
    render(
      <Menu
        aria-label="More actions for web-01"
        items={[{ label: "Delete", onSelect: vi.fn() }]}
      />,
    );

    const trigger = screen.getByRole("button", { name: "More actions for web-01" });
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("menuitem", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("opens on click, focuses the first item, and calls onSelect then closes when an item is chosen", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <Menu
        aria-label="More actions for web-01"
        items={[
          { label: "Regenerate token", onSelect: vi.fn() },
          { label: "Delete", onSelect, danger: true },
        ]}
      />,
    );

    const trigger = screen.getByRole("button", { name: "More actions for web-01" });
    await user.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("menuitem", { name: "Regenerate token" })).toHaveFocus();

    await user.click(screen.getByRole("menuitem", { name: "Delete" }));

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menuitem", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("Escape closes the menu and returns focus to the trigger", async () => {
    const user = userEvent.setup();
    render(
      <Menu
        aria-label="More actions for web-01"
        items={[{ label: "Delete", onSelect: vi.fn() }]}
      />,
    );

    const trigger = screen.getByRole("button", { name: "More actions for web-01" });
    await user.click(trigger);
    expect(screen.getByRole("menu")).toBeInTheDocument();

    await user.keyboard("{Escape}");

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it("a click outside the menu closes it", async () => {
    const user = userEvent.setup();
    render(
      <div>
        <button type="button">Outside</button>
        <Menu
          aria-label="More actions for web-01"
          items={[{ label: "Delete", onSelect: vi.fn() }]}
        />
      </div>,
    );

    await user.click(screen.getByRole("button", { name: "More actions for web-01" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Outside" }));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });
});
