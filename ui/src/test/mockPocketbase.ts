import { vi } from "vitest";
import type { Role } from "@/types";

/**
 * Minimal shape of a PocketBase auth record as used by authStore/Users:
 * just enough fields for role derivation and display, not the full SDK type.
 */
export interface MockAuthRecord {
  id: string;
  email?: string;
  name?: string;
  avatar?: string;
  role?: Role;
}

export interface MockCollection {
  authWithPassword: ReturnType<typeof vi.fn>;
  getFullList: ReturnType<typeof vi.fn>;
  /** Paged list — used by AuditLog, unlike every other page which reads a
   *  full collection at once via getFullList. */
  getList: ReturnType<typeof vi.fn>;
  create: ReturnType<typeof vi.fn>;
  update: ReturnType<typeof vi.fn>;
  delete: ReturnType<typeof vi.fn>;
  /** Realtime subscribe — resolves to an unsubscribe function by default, so
   *  pages that call `.subscribe("*", ...)` on mount (Dashboard, Agents,
   *  AlertHistory, ...) don't need every test to stub it individually. */
  subscribe: ReturnType<typeof vi.fn>;
}

export interface MockAuthStore {
  token: string;
  record: MockAuthRecord | null;
  isSuperuser: boolean;
  isValid: boolean;
  clear: ReturnType<typeof vi.fn>;
  save: ReturnType<typeof vi.fn>;
}

export interface MockPocketBase {
  baseUrl: string;
  authStore: MockAuthStore;
  collection: ReturnType<typeof vi.fn> & ((name: string) => MockCollection);
}

function createCollection(): MockCollection {
  return {
    authWithPassword: vi.fn(),
    getFullList: vi.fn(),
    getList: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    subscribe: vi.fn().mockResolvedValue(vi.fn()),
  };
}

/**
 * Builds a fresh fake PocketBase client for a single test file/case. Used
 * with `vi.mock("@/lib/pocketbase", ...)` to replace the real SDK instance
 * so tests never hit the network. Each call returns independent state and
 * independent vi.fn() mocks, so tests do not leak auth state into one
 * another.
 */
export function createMockPocketBase(baseUrl = "http://localhost:8090"): MockPocketBase {
  const collections = new Map<string, MockCollection>();

  const authStore: MockAuthStore = {
    token: "",
    record: null,
    isSuperuser: false,
    isValid: false,
    clear: vi.fn(),
    save: vi.fn(),
  };
  authStore.clear.mockImplementation(() => {
    authStore.token = "";
    authStore.record = null;
    authStore.isSuperuser = false;
    authStore.isValid = false;
  });

  const collection = vi.fn((name: string) => {
    let existing = collections.get(name);
    if (!existing) {
      existing = createCollection();
      collections.set(name, existing);
    }
    return existing;
  }) as MockPocketBase["collection"];

  return { baseUrl, authStore, collection };
}

/** Sets authStore fields as if a successful login/restore already happened. */
export function signInAs(
  pbMock: MockPocketBase,
  record: MockAuthRecord,
  options: { isSuperuser?: boolean; token?: string } = {},
): void {
  pbMock.authStore.token = options.token ?? "mock-token";
  pbMock.authStore.record = record;
  pbMock.authStore.isSuperuser = options.isSuperuser ?? false;
  pbMock.authStore.isValid = true;
}
