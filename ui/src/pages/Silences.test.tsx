import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { MockPocketBase } from "@/test/mockPocketbase";

vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});
vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { Silences } from "@/pages/Silences";
import { useAuthStore } from "@/stores/authStore";
import { useSilencesStore } from "@/stores/silencesStore";

const mockPb = pb as unknown as MockPocketBase;

function setViewer() {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "me", email: "viewer@example.com", name: "", avatar: "", role: "viewer" },
  });
}

beforeEach(() => {
  setViewer();
  mockPb.collection("agents").getFullList.mockReset();
  mockPb.collection("agents").getFullList.mockResolvedValue([]);
  mockPb.collection("checks").getFullList.mockReset();
  mockPb
    .collection("checks")
    .getFullList.mockResolvedValue([{ id: "c1", name: "billing-api", tags: ["billing"] }]);
  vi.mocked(apiFetch).mockReset();
  vi.mocked(apiFetch).mockResolvedValue(
    new Response(
      JSON.stringify({
        silences: [
          {
            id: "s1",
            covered_agent_ids: [],
            covered_check_ids: ["c1"],
          },
        ],
      }),
      { status: 200 },
    ),
  );

  useSilencesStore.setState({
    silences: [
      {
        id: "s1",
        name: "Billing maintenance",
        reason: "",
        agent_id: "",
        tags: [],
        check_ids: ["c1"],
        starts_at: new Date(Date.now() - 60_000).toISOString(),
        ends_at: new Date(Date.now() + 3_600_000).toISOString(),
        created_by: "op@example.com",
        collectionId: "silences",
        collectionName: "silences",
        created: "",
        updated: "",
      },
    ],
    loading: false,
    error: null,
  });
});

describe("Silences — covered checks", () => {
  it("shows the check's name as the scope for a check-scoped silence", async () => {
    render(<Silences />);

    expect(await screen.findByText("billing-api")).toBeInTheDocument();
  });

  it("shows a Covered checks column with the count from the active-silences endpoint", async () => {
    render(<Silences />);

    expect(await screen.findByText("Covered checks")).toBeInTheDocument();
    // The row's "Covered agents"/"Covered checks" cells are both rendered;
    // the check-scoped fixture above covers exactly one check.
    const row = (await screen.findByText("Billing maintenance")).closest("tr");
    expect(row).not.toBeNull();
    expect(row!.textContent).toContain("1");
  });
});
