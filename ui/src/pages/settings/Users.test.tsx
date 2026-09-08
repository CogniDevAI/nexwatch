import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { MockPocketBase } from "@/test/mockPocketbase";
import type { Role, User } from "@/types";

// The factory is self-contained (its own dynamic import, no references to
// outer-scope variables) so Vitest's module hoisting can't race it against
// a not-yet-initialized top-level const.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { Users } from "@/pages/settings/Users";
import { useAuthStore } from "@/stores/authStore";
import { ToastProvider } from "@/components/ui/Toast";

const mockPb = pb as unknown as MockPocketBase;

function setCurrentUser(id: string, role: Role): void {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id, email: `${id}@example.com`, name: id, avatar: "", role },
  });
}

function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: "u1",
    email: "user1@example.com",
    name: "User One",
    role: "viewer",
    verified: true,
    created: "2026-01-01 00:00:00.000Z",
    updated: "2026-01-01 00:00:00.000Z",
    collectionId: "_pb_users_auth_",
    collectionName: "users",
    ...overrides,
  };
}

function renderUsersPage() {
  return render(
    <ToastProvider>
      <MemoryRouter initialEntries={["/settings/users"]}>
        <Routes>
          <Route path="/settings" element={<div>Settings Page</div>} />
          <Route path="/settings/users" element={<Users />} />
        </Routes>
      </MemoryRouter>
    </ToastProvider>,
  );
}

beforeEach(() => {
  useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
  mockPb.collection("users").getFullList.mockReset();
  mockPb.collection("users").create.mockReset();
  mockPb.collection("users").update.mockReset();
  mockPb.collection("users").delete.mockReset();
});

describe("Users page — access control", () => {
  it("redirects a non-admin user to /settings", async () => {
    setCurrentUser("me", "operator");
    mockPb.collection("users").getFullList.mockResolvedValue([]);

    renderUsersPage();

    expect(await screen.findByText("Settings Page")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Users" })).not.toBeInTheDocument();
  });

  it("lets an admin user see the Users page", async () => {
    setCurrentUser("me", "admin");
    mockPb
      .collection("users")
      .getFullList.mockResolvedValue([
        makeUser({ id: "me", email: "me@example.com", role: "admin" }),
      ]);

    renderUsersPage();

    expect(await screen.findByRole("heading", { name: "Users" })).toBeInTheDocument();
  });
});

describe("Users page — user list", () => {
  it("renders the user list fetched from PocketBase", async () => {
    setCurrentUser("me", "admin");
    mockPb
      .collection("users")
      .getFullList.mockResolvedValue([
        makeUser({ id: "me", email: "me@example.com", name: "Me", role: "admin" }),
        makeUser({ id: "u2", email: "other@example.com", name: "Other", role: "viewer" }),
      ]);

    renderUsersPage();

    expect(await screen.findByText("me@example.com")).toBeInTheDocument();
    expect(screen.getByText("other@example.com")).toBeInTheDocument();
    expect(mockPb.collection("users").getFullList).toHaveBeenCalledWith({ sort: "-created" });
  });
});

describe("Users page — create user", () => {
  it("calls create with the entered email, password, and role", async () => {
    const user = userEvent.setup();
    setCurrentUser("me", "admin");
    mockPb
      .collection("users")
      .getFullList.mockResolvedValue([
        makeUser({ id: "me", email: "me@example.com", role: "admin" }),
      ]);
    mockPb
      .collection("users")
      .create.mockResolvedValue(makeUser({ id: "new1", email: "new@example.com" }));

    const { container } = renderUsersPage();
    await screen.findByText("me@example.com");

    await user.click(screen.getByRole("button", { name: /add user/i }));

    const form = screen.getByRole("button", { name: /create user/i }).closest("form")!;
    await user.type(within(form).getByPlaceholderText("user@example.com"), "new@example.com");

    const passwordInputs = container.querySelectorAll<HTMLInputElement>('input[type="password"]');
    expect(passwordInputs).toHaveLength(2);
    await user.type(passwordInputs[0]!, "password123");
    await user.type(passwordInputs[1]!, "password123");

    await user.click(screen.getByRole("button", { name: /create user/i }));

    await waitFor(() => {
      expect(mockPb.collection("users").create).toHaveBeenCalledWith({
        email: "new@example.com",
        password: "password123",
        passwordConfirm: "password123",
        name: "",
        role: "viewer",
        emailVisibility: true,
      });
    });
  });
});

describe("Users page — self-protection", () => {
  it("disables the current user's own role select and delete button, but not others'", async () => {
    setCurrentUser("me", "admin");
    mockPb
      .collection("users")
      .getFullList.mockResolvedValue([
        makeUser({ id: "me", email: "me@example.com", role: "admin" }),
        makeUser({ id: "u2", email: "other@example.com", role: "viewer" }),
      ]);

    renderUsersPage();
    await screen.findByText("me@example.com");

    const myRow = screen.getByText("me@example.com").closest("tr")!;
    const otherRow = screen.getByText("other@example.com").closest("tr")!;

    expect(within(myRow).getByRole("combobox")).toBeDisabled();
    expect(within(otherRow).getByRole("combobox")).not.toBeDisabled();

    expect(within(myRow).getByRole("button")).toBeDisabled();
    expect(within(otherRow).getByRole("button")).not.toBeDisabled();
  });
});
