import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TagInput } from "./TagInput";

describe("TagInput", () => {
  it("adds a tag on Enter and clears the draft", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={[]} onChange={onChange} aria-label="Tags" />);

    const input = screen.getByLabelText("Tags");
    await user.type(input, "web{Enter}");

    expect(onChange).toHaveBeenCalledWith(["web"]);
  });

  it("adds a tag on comma", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={[]} onChange={onChange} aria-label="Tags" />);

    await user.type(screen.getByLabelText("Tags"), "db,");

    expect(onChange).toHaveBeenCalledWith(["db"]);
  });

  it("removes the last chip on Backspace when the input is empty", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={["web", "db"]} onChange={onChange} aria-label="Tags" />);

    await user.click(screen.getByLabelText("Tags"));
    await user.keyboard("{Backspace}");

    expect(onChange).toHaveBeenCalledWith(["web"]);
  });

  it("removes a specific chip via its remove button", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={["web", "db"]} onChange={onChange} aria-label="Tags" />);

    await user.click(screen.getByRole("button", { name: "Remove tag web" }));

    expect(onChange).toHaveBeenCalledWith(["db"]);
  });

  it("dedupes a new tag case-insensitively against existing chips", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={["Web"]} onChange={onChange} aria-label="Tags" />);

    await user.type(screen.getByLabelText("Tags"), "web{Enter}");

    expect(onChange).not.toHaveBeenCalled();
  });

  it("ignores an empty or whitespace-only draft", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TagInput value={[]} onChange={onChange} aria-label="Tags" />);

    await user.type(screen.getByLabelText("Tags"), "   {Enter}");

    expect(onChange).not.toHaveBeenCalled();
  });

  it("shows suggestions excluding tags already added, filtered by the draft", async () => {
    const user = userEvent.setup();
    render(
      <TagInput
        value={["web"]}
        onChange={vi.fn()}
        suggestions={["web", "db", "cache"]}
        aria-label="Tags"
      />,
    );

    const input = screen.getByLabelText("Tags");
    await user.click(input);

    // "web" is already a chip, so only "db" and "cache" should be offered.
    expect(await screen.findByRole("button", { name: "db" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "cache" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "web" })).not.toBeInTheDocument();

    await user.type(input, "ca");
    expect(await screen.findByRole("button", { name: "cache" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "db" })).not.toBeInTheDocument();
  });

  it("adds a tag by clicking a suggestion", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TagInput value={[]} onChange={onChange} suggestions={["web", "db"]} aria-label="Tags" />,
    );

    await user.click(screen.getByLabelText("Tags"));
    await user.click(await screen.findByRole("button", { name: "web" }));

    expect(onChange).toHaveBeenCalledWith(["web"]);
  });
});
