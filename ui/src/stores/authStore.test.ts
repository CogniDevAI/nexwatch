import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MockPocketBase } from "@/test/mockPocketbase";

// The factory is self-contained (its own dynamic import, no references to
// outer-scope variables) so Vitest's module hoisting can't race it against
// a not-yet-initialized top-level const.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});

import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import type { Role } from "@/types";

const mockPb = pb as unknown as MockPocketBase;

function resetAuthStore(): void {
  useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
}

function resetMockPb(): void {
  mockPb.authStore.token = "";
  mockPb.authStore.record = null;
  mockPb.authStore.isSuperuser = false;
  mockPb.authStore.isValid = false;
  mockPb.authStore.clear.mockClear();
  mockPb.collection("users").authWithPassword.mockReset();
  mockPb.collection("_superusers").authWithPassword.mockReset();
}

beforeEach(() => {
  resetAuthStore();
  resetMockPb();
});

describe("useAuthStore.login", () => {
  it("authenticates via the users collection and stores the record's role", async () => {
    mockPb.collection("users").authWithPassword.mockImplementation(() => {
      mockPb.authStore.token = "tok-user";
      mockPb.authStore.record = {
        id: "u1",
        email: "ann@example.com",
        name: "Ann",
        role: "operator",
      };
      mockPb.authStore.isSuperuser = false;
      mockPb.authStore.isValid = true;
      return {};
    });

    await useAuthStore.getState().login("ann@example.com", "pw");

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.token).toBe("tok-user");
    expect(state.user).toEqual({
      id: "u1",
      email: "ann@example.com",
      name: "Ann",
      avatar: "",
      role: "operator",
    });
    expect(mockPb.collection("_superusers").authWithPassword).not.toHaveBeenCalled();
  });

  it("falls back to _superusers when the users collection rejects, and grants admin", async () => {
    mockPb.collection("users").authWithPassword.mockRejectedValue(new Error("invalid credentials"));
    mockPb.collection("_superusers").authWithPassword.mockImplementation(() => {
      mockPb.authStore.token = "tok-admin";
      mockPb.authStore.record = { id: "root", email: "root@example.com" };
      mockPb.authStore.isSuperuser = true;
      mockPb.authStore.isValid = true;
      return {};
    });

    await useAuthStore.getState().login("root@example.com", "pw");

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.token).toBe("tok-admin");
    expect(state.user?.role).toBe("admin");
    expect(mockPb.collection("users").authWithPassword).toHaveBeenCalledWith(
      "root@example.com",
      "pw",
    );
    expect(mockPb.collection("_superusers").authWithPassword).toHaveBeenCalledWith(
      "root@example.com",
      "pw",
    );
  });

  it("rejects and leaves the store logged out when both collections fail", async () => {
    mockPb.collection("users").authWithPassword.mockRejectedValue(new Error("bad creds"));
    mockPb.collection("_superusers").authWithPassword.mockRejectedValue(new Error("bad creds too"));

    await expect(useAuthStore.getState().login("nobody@example.com", "pw")).rejects.toThrow(
      "bad creds too",
    );

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
  });
});

describe("useAuthStore.hasRole", () => {
  const cases: Array<[Role, Role, boolean]> = [
    ["viewer", "viewer", true],
    ["viewer", "operator", false],
    ["viewer", "admin", false],
    ["operator", "viewer", true],
    ["operator", "operator", true],
    ["operator", "admin", false],
    ["admin", "viewer", true],
    ["admin", "operator", true],
    ["admin", "admin", true],
  ];

  it.each(cases)("role=%s hasRole(%s) -> %s", (role, min, expected) => {
    useAuthStore.setState({
      isAuthenticated: true,
      token: "t",
      user: { id: "1", email: "a@example.com", name: "A", avatar: "", role },
    });

    expect(useAuthStore.getState().hasRole(min)).toBe(expected);
  });

  it("treats a missing user as viewer rank", () => {
    resetAuthStore();

    expect(useAuthStore.getState().hasRole("viewer")).toBe(true);
    expect(useAuthStore.getState().hasRole("operator")).toBe(false);
    expect(useAuthStore.getState().hasRole("admin")).toBe(false);
  });
});

describe("useAuthStore.restoreAuth", () => {
  it("restores an admin user from a _superusers auth record", () => {
    mockPb.authStore.isValid = true;
    mockPb.authStore.isSuperuser = true;
    mockPb.authStore.token = "tok-s";
    mockPb.authStore.record = { id: "root", email: "root@example.com" };

    useAuthStore.getState().restoreAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.token).toBe("tok-s");
    expect(state.user?.role).toBe("admin");
  });

  it("restores an operator role from a users collection record", () => {
    mockPb.authStore.isValid = true;
    mockPb.authStore.isSuperuser = false;
    mockPb.authStore.token = "tok-u";
    mockPb.authStore.record = { id: "u2", email: "op@example.com", role: "operator" };

    useAuthStore.getState().restoreAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.user?.role).toBe("operator");
  });

  it("does nothing when there is no valid stored session", () => {
    mockPb.authStore.isValid = false;
    mockPb.authStore.record = null;

    useAuthStore.getState().restoreAuth();

    expect(useAuthStore.getState().isAuthenticated).toBe(false);
    expect(useAuthStore.getState().user).toBeNull();
  });
});

describe("useAuthStore.logout", () => {
  it("clears pb.authStore and resets the store", () => {
    useAuthStore.setState({
      isAuthenticated: true,
      token: "t",
      user: { id: "1", email: "a@example.com", name: "A", avatar: "", role: "admin" },
    });

    useAuthStore.getState().logout();

    expect(mockPb.authStore.clear).toHaveBeenCalledTimes(1);
    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
  });
});
