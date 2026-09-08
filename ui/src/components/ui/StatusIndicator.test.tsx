import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatusIndicator } from "./StatusIndicator";

describe("StatusIndicator", () => {
  it("maps each status to its default label", () => {
    render(<StatusIndicator status="ok" />);
    expect(screen.getByText("Operational")).toBeInTheDocument();

    render(<StatusIndicator status="warning" />);
    expect(screen.getByText("Warning")).toBeInTheDocument();

    render(<StatusIndicator status="critical" />);
    expect(screen.getByText("Critical")).toBeInTheDocument();

    render(<StatusIndicator status="offline" />);
    expect(screen.getByText("Offline")).toBeInTheDocument();
  });

  it("accepts a custom label overriding the default", () => {
    render(<StatusIndicator status="critical" label="Failed" />);
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.queryByText("Critical")).not.toBeInTheDocument();
  });

  it("exposes an accessible name via role=img when rendered dot-only", () => {
    render(<StatusIndicator status="ok" dotOnly />);
    expect(screen.getByRole("img", { name: "Operational" })).toBeInTheDocument();
  });

  it("uses the custom label as the accessible name when dot-only", () => {
    render(<StatusIndicator status="offline" dotOnly label="web-01: offline" />);
    expect(screen.getByRole("img", { name: "web-01: offline" })).toBeInTheDocument();
  });
});
