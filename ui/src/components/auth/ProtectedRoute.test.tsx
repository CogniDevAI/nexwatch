import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

// The factory is self-contained (its own dynamic import, no references to
// outer-scope variables) so Vitest's module hoisting can't race it against
// a not-yet-initialized top-level const. authStore imports the PocketBase
// client at module scope, so it must be mocked even though this test never
// calls it directly.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import { ProtectedRoute } from "@/components/auth/ProtectedRoute";
import { useAuthStore } from "@/stores/authStore";

function renderProtectedRoute() {
  return render(
    <MemoryRouter initialEntries={["/dashboard"]}>
      <Routes>
        <Route path="/login" element={<div>Login Page</div>} />
        <Route element={<ProtectedRoute />}>
          <Route path="/dashboard" element={<div>Secret Dashboard</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("ProtectedRoute", () => {
  beforeEach(() => {
    useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
  });

  it("redirects to /login when the user is not authenticated", () => {
    renderProtectedRoute();

    expect(screen.getByText("Login Page")).toBeInTheDocument();
    expect(screen.queryByText("Secret Dashboard")).not.toBeInTheDocument();
  });

  it("renders the protected child route when the user is authenticated", () => {
    useAuthStore.setState({
      isAuthenticated: true,
      token: "t",
      user: { id: "1", email: "a@example.com", name: "A", avatar: "", role: "admin" },
    });

    renderProtectedRoute();

    expect(screen.getByText("Secret Dashboard")).toBeInTheDocument();
    expect(screen.queryByText("Login Page")).not.toBeInTheDocument();
  });
});
