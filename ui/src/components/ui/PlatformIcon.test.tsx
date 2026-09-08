import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { PlatformIcon } from "./PlatformIcon";

describe("PlatformIcon", () => {
  it("renders a distinct icon per known platform", () => {
    const { container: windowsIcon } = render(<PlatformIcon platform="windows" />);
    const { container: linuxIcon } = render(<PlatformIcon platform="linux" />);
    const { container: darwinIcon } = render(<PlatformIcon platform="darwin" />);

    const windowsSvg = windowsIcon.querySelector("svg")?.getAttribute("class");
    const linuxSvg = linuxIcon.querySelector("svg")?.getAttribute("class");
    const darwinSvg = darwinIcon.querySelector("svg")?.getAttribute("class");

    expect(windowsSvg).toBeTruthy();
    expect(linuxSvg).toBeTruthy();
    expect(darwinSvg).toBeTruthy();
    expect(new Set([windowsSvg, linuxSvg, darwinSvg]).size).toBe(3);
  });

  it("is case-insensitive on the platform string", () => {
    const { container: lower } = render(<PlatformIcon platform="windows" />);
    const { container: upper } = render(<PlatformIcon platform="Windows" />);
    expect(lower.querySelector("svg")?.getAttribute("class")).toBe(
      upper.querySelector("svg")?.getAttribute("class"),
    );
  });

  it("falls back to a generic icon for an unknown or missing platform", () => {
    const { container: unknown } = render(<PlatformIcon platform="freebsd" />);
    const { container: missing } = render(<PlatformIcon platform={undefined} />);
    expect(unknown.querySelector("svg")?.getAttribute("class")).toBe(
      missing.querySelector("svg")?.getAttribute("class"),
    );
  });
});
