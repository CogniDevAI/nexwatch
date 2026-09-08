import { create } from "zustand";
import pb from "@/lib/pocketbase";
import type { Role } from "@/types";

interface User {
  id: string;
  email: string;
  name: string;
  avatar: string;
  role: Role;
}

// Role hierarchy used by hasRole() — must match internal/hub/api/authz.go.
const ROLE_RANK: Record<Role, number> = {
  viewer: 0,
  operator: 1,
  admin: 2,
};

interface AuthState {
  isAuthenticated: boolean;
  user: User | null;
  token: string | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  restoreAuth: () => void;
  /** Returns true if the current user's role meets or exceeds min. */
  hasRole: (min: Role) => boolean;
}

/** Builds a User from whatever record is currently in pb.authStore. */
function userFromAuthStore(): User | null {
  const record = pb.authStore.record;
  if (!record) return null;

  const role: Role = pb.authStore.isSuperuser
    ? "admin"
    : ((record.role as Role | undefined) ?? "viewer");

  return {
    id: record.id,
    email: record.email ?? "",
    name: record.name ?? "",
    avatar: record.avatar ?? "",
    role,
  };
}

/**
 * A session persisted by the PocketBase SDK is restored synchronously here so the
 * very first render already knows the user is signed in. Restoring it later from an
 * effect would let ProtectedRoute redirect to /login before the session is read.
 */
const hasStoredSession = pb.authStore.isValid && !!pb.authStore.record;

export const useAuthStore = create<AuthState>((set, get) => ({
  isAuthenticated: hasStoredSession,
  user: hasStoredSession ? userFromAuthStore() : null,
  token: hasStoredSession ? pb.authStore.token : null,

  login: async (email: string, password: string) => {
    try {
      // Regular dashboard users authenticate against "users" first.
      await pb.collection("users").authWithPassword(email, password);
    } catch {
      // Fall back to superusers (e.g. the PocketBase-created admin).
      await pb.collection("_superusers").authWithPassword(email, password);
    }

    set({
      isAuthenticated: true,
      user: userFromAuthStore(),
      token: pb.authStore.token,
    });
  },

  logout: () => {
    pb.authStore.clear();
    set({
      isAuthenticated: false,
      user: null,
      token: null,
    });
  },

  restoreAuth: () => {
    if (pb.authStore.isValid && pb.authStore.record) {
      set({
        isAuthenticated: true,
        user: userFromAuthStore(),
        token: pb.authStore.token,
      });
    }
  },

  hasRole: (min: Role) => {
    const role = get().user?.role ?? "viewer";
    return ROLE_RANK[role] >= ROLE_RANK[min];
  },
}));
