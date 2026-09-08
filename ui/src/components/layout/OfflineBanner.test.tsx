import { afterEach, describe, expect, it } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { OfflineBanner } from "@/components/layout/OfflineBanner";

/** navigator.onLine is a read-only getter in jsdom by default; this
 *  overrides it per-test so mount-time state (the banner's initial
 *  useState(() => !navigator.onLine)) can be exercised both ways. */
function setOnLine(value: boolean) {
  Object.defineProperty(window.navigator, "onLine", {
    configurable: true,
    value,
  });
}

afterEach(() => {
  setOnLine(true);
});

describe("OfflineBanner", () => {
  it("renders nothing while online", () => {
    setOnLine(true);
    render(<OfflineBanner />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("renders the offline message when mounted while already offline", () => {
    setOnLine(false);
    render(<OfflineBanner />);
    expect(screen.getByRole("status")).toHaveTextContent(/you're offline/i);
  });

  it('shows the banner when the browser fires an "offline" event', () => {
    setOnLine(true);
    render(<OfflineBanner />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new Event("offline"));
    });

    expect(screen.getByRole("status")).toHaveTextContent(/you're offline/i);
  });

  it('hides the banner again when the browser fires an "online" event', () => {
    setOnLine(false);
    render(<OfflineBanner />);
    expect(screen.getByRole("status")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new Event("online"));
    });

    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
