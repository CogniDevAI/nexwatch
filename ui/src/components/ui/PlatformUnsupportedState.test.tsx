import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { PlatformUnsupportedState } from "./PlatformUnsupportedState";

describe("PlatformUnsupportedState", () => {
  it("renders the platform in the heading", () => {
    render(<PlatformUnsupportedState feature="Misconfigurations" platform="Windows" />);
    expect(screen.getByText("Not available on Windows")).toBeInTheDocument();
  });

  it("names the feature in the description", () => {
    render(<PlatformUnsupportedState feature="Oracle DB monitoring" platform="Windows" />);
    expect(
      screen.getByText("Oracle DB monitoring is not supported on Windows agents."),
    ).toBeInTheDocument();
  });
});
