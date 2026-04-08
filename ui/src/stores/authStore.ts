import { create } from "zustand";
import pb from "@/lib/pocketbase";

interface User {
  id: string;
  email: string;
  name: string;
  avatar: string;
}

interface AuthState {
  isAuthenticated: boolean;
  user: User | null;
  token: string | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  restoreAuth: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  isAuthenticated: false,
  user: null,
  token: null,

  login: async (email: string, password: string) => {
    const authData = await pb
      .collection("_superusers")
      .authWithPassword(email, password);

    const user: User = {
      id: authData.record.id,
      email: authData.record.email ?? "",
      name: authData.record.name ?? "",
      avatar: authData.record.avatar ?? "",
    };

    set({
      isAuthenticated: true,
      user,
      token: authData.token,
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
      const record = pb.authStore.record;
      set({
        isAuthenticated: true,
        user: {
          id: record.id,
          email: record.email ?? "",
          name: record.name ?? "",
          avatar: record.avatar ?? "",
        },
        token: pb.authStore.token,
      });
    }
  },
}));
