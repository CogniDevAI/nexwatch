import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import { apiFetch } from "@/lib/api";
import { CveScanTab } from "@/components/server/CveScanTab";
import type { CveScanData } from "@/types";

const mockApiFetch = apiFetch as unknown as ReturnType<
  typeof vi.fn<(path: string, init?: RequestInit) => Promise<Response>>
>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

function makeData(overrides: Partial<CveScanData> = {}): CveScanData {
  return {
    scanner: "trivy",
    scanner_version: "0.50.1",
    db_updated_at: "2026-09-04T00:00:00Z",
    scanned_at: "2026-09-05T10:00:00Z",
    duration_ms: 4200,
    stale: false,
    available: true,
    error: "",
    targets: [
      {
        kind: "host",
        ref: "/",
        counts: { critical: 1, high: 1, medium: 0, low: 0, unknown: 0 },
        fixable: 1,
        findings: [
          {
            id: "CVE-2023-0001",
            severity: "critical",
            package: "openssl",
            installed: "3.0.2",
            fixed: "3.0.2-fixed",
            title: "openssl heap overflow",
            target: "Ubuntu 22.04",
          },
          {
            id: "CVE-2023-0002",
            severity: "high",
            package: "curl",
            installed: "7.81.0",
            fixed: "",
            title: "curl info disclosure",
            target: "Ubuntu 22.04",
          },
        ],
      },
    ],
    totals: { critical: 1, high: 1, medium: 0, low: 0, unknown: 0, fixable: 1 },
    ...overrides,
  };
}

function renderTab() {
  return render(<CveScanTab agentId="agent-1" />);
}

beforeEach(() => {
  mockApiFetch.mockReset();
});

describe("CveScanTab — content state", () => {
  it("renders severity tiles and the scanner line from a fixture payload", async () => {
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();

    await screen.findByText("CVE-2023-0001");
    expect(screen.queryByText("No known CVEs found")).not.toBeInTheDocument();
    // "Critical"/"High" also appear as per-row SeverityBadge labels below,
    // so assert at least one match rather than a unique one.
    expect(screen.getAllByText("Critical").length).toBeGreaterThan(0);
    expect(screen.getAllByText("High").length).toBeGreaterThan(0);
    expect(screen.getByText("Fixable")).toBeInTheDocument();
    expect(screen.getByText(/trivy/)).toBeInTheDocument();
    expect(screen.getByText(/v0\.50\.1/)).toBeInTheDocument();
    expect(screen.getByText("CVE-2023-0001")).toBeInTheDocument();
    expect(screen.getByText("CVE-2023-0002")).toBeInTheDocument();
  });

  it("links a CVE id to the NVD detail page", async () => {
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();

    const link = await screen.findByRole("link", { name: "CVE-2023-0001" });
    expect(link).toHaveAttribute("href", "https://nvd.nist.gov/vuln/detail/CVE-2023-0001");
  });

  it("shows a stale badge when stale is true", async () => {
    mockApiFetch.mockResolvedValue(jsonResponse(makeData({ stale: true })));
    renderTab();

    expect(await screen.findByText("Stale")).toBeInTheDocument();
  });

  it("shows the empty state when available but no findings exist", async () => {
    mockApiFetch.mockResolvedValue(
      jsonResponse(
        makeData({
          targets: [
            {
              kind: "host",
              ref: "/",
              counts: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0 },
              fixable: 0,
              findings: [],
            },
          ],
          totals: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, fixable: 0 },
        }),
      ),
    );
    renderTab();

    expect(await screen.findByText("No known CVEs found")).toBeInTheDocument();
  });
});

describe("CveScanTab — filters", () => {
  it("filters findings by severity", async () => {
    const user = userEvent.setup();
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();
    await screen.findByText("CVE-2023-0001");

    await user.selectOptions(screen.getByLabelText("Filter by severity"), "high");

    expect(screen.queryByText("CVE-2023-0001")).not.toBeInTheDocument();
    expect(screen.getByText("CVE-2023-0002")).toBeInTheDocument();
  });

  it("filters to fixable-only findings", async () => {
    const user = userEvent.setup();
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();
    await screen.findByText("CVE-2023-0001");

    await user.click(screen.getByRole("switch", { name: "Fixable only" }));

    expect(screen.getByText("CVE-2023-0001")).toBeInTheDocument(); // has a fix
    expect(screen.queryByText("CVE-2023-0002")).not.toBeInTheDocument(); // no fix
  });

  it("filters by search text across id, package, and title", async () => {
    const user = userEvent.setup();
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();
    await screen.findByText("CVE-2023-0001");

    await user.type(screen.getByPlaceholderText(/Search CVE ID/), "openssl");

    expect(screen.getByText("CVE-2023-0001")).toBeInTheDocument();
    expect(screen.queryByText("CVE-2023-0002")).not.toBeInTheDocument();
  });

  it("shows a no-match message per target when filters exclude everything", async () => {
    const user = userEvent.setup();
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();
    await screen.findByText("CVE-2023-0001");

    await user.type(screen.getByPlaceholderText(/Search CVE ID/), "nonexistent-package");

    expect(await screen.findByText("No findings match the current filters.")).toBeInTheDocument();
  });
});

describe("CveScanTab — not-available state", () => {
  it("shows install hints for Trivy and Grype when no scanner is installed", async () => {
    mockApiFetch.mockResolvedValue(
      jsonResponse(
        makeData({
          available: false,
          scanner: "",
          error: "no supported scanner found (install trivy or grype)",
          targets: [],
          totals: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, fixable: 0 },
        }),
      ),
    );
    renderTab();

    expect(await screen.findByText("No CVE scanner installed")).toBeInTheDocument();
    expect(screen.getByText("Install Trivy")).toBeInTheDocument();
    expect(screen.getByText("Install Grype")).toBeInTheDocument();
    expect(screen.getByText(/aquasecurity\/trivy/)).toBeInTheDocument();
    expect(screen.getByText(/anchore\/grype/)).toBeInTheDocument();
  });

  it("shows a pending-first-run message distinct from the install hints", async () => {
    mockApiFetch.mockResolvedValue(
      jsonResponse(
        makeData({
          available: false,
          scanner: "",
          error: "scan pending: the first run has not completed yet",
          targets: [],
          totals: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, fixable: 0 },
        }),
      ),
    );
    renderTab();

    expect(await screen.findByText("First scan is running")).toBeInTheDocument();
    expect(screen.queryByText("Install Trivy")).not.toBeInTheDocument();
  });
});

describe("CveScanTab — error state", () => {
  it("shows an error state with retry on a failed fetch", async () => {
    const user = userEvent.setup();
    mockApiFetch
      .mockResolvedValueOnce(jsonResponse({}, 500))
      .mockResolvedValueOnce(jsonResponse(makeData()));
    renderTab();

    expect(await screen.findByText("Couldn't load the CVE scan report")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Try again" }));

    await waitFor(() => {
      expect(screen.getByText("CVE-2023-0001")).toBeInTheDocument();
    });
  });
});

describe("CveScanTab — table content", () => {
  it("renders package/installed/fixed columns and a dash for an unfixed CVE", async () => {
    mockApiFetch.mockResolvedValue(jsonResponse(makeData()));
    renderTab();
    await screen.findByText("CVE-2023-0001");

    const row = screen.getByText("CVE-2023-0002").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("curl")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
  });
});
