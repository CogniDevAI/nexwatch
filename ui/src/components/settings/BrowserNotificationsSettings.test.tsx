import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MockPocketBase } from "@/test/mockPocketbase";

// Self-contained factories (own dynamic import, no outer-scope references) —
// same pattern as NotificationChannels.test.tsx.
vi.mock("@/lib/pocketbase", async () => {
  const { createMockPocketBase } = await import("@/test/mockPocketbase");
  return { default: createMockPocketBase() };
});
vi.mock("@/lib/api", () => ({
  apiFetch: vi.fn(),
}));

import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { useAuthStore } from "@/stores/authStore";
import { ToastProvider } from "@/components/ui/Toast";
import { BrowserNotificationsSettings } from "@/components/settings/BrowserNotificationsSettings";

const mockPb = pb as unknown as MockPocketBase;

// A syntactically valid base64url string of the right length for a raw
// uncompressed P-256 public key (65 bytes) — content is arbitrary random
// bytes, not a real VAPID key, since urlBase64ToUint8Array only needs
// valid base64url input to decode, never a cryptographically real key.
const VAPID_PUBLIC_KEY =
  "501_QnvaUocREc9uEmh8PPfiAo6kOpqgJJEKgy6UMhrixUa2kg98yXgPr4HH3A-ZBB9GZz05MwpMkaChcK0_T94";

function makeSubscription(endpoint: string) {
  return {
    endpoint,
    toJSON: () => ({ keys: { p256dh: "p256dh-value", auth: "auth-value" } }),
    unsubscribe: vi.fn().mockResolvedValue(true),
  };
}

/** Installs window.Notification/PushManager and navigator.serviceWorker so
 *  isPushSupported() reports true, plus a controllable pushManager mock.
 *  Returns the mocks so each test can drive getSubscription()/subscribe(). */
function installPushSupport() {
  const getSubscription = vi.fn().mockResolvedValue(null);
  const subscribe = vi.fn();
  const requestPermission = vi.fn().mockResolvedValue("granted");

  Object.defineProperty(window, "Notification", {
    configurable: true,
    writable: true,
    value: Object.assign(function MockNotification() {}, {
      permission: "default",
      requestPermission,
    }),
  });
  Object.defineProperty(window, "PushManager", {
    configurable: true,
    writable: true,
    value: function MockPushManager() {},
  });
  Object.defineProperty(window.navigator, "serviceWorker", {
    configurable: true,
    writable: true,
    value: {
      ready: Promise.resolve({ pushManager: { getSubscription, subscribe } }),
    },
  });

  return { getSubscription, subscribe, requestPermission };
}

function uninstallPushSupport() {
  delete (window as { Notification?: unknown }).Notification;
  delete (window as { PushManager?: unknown }).PushManager;
  delete (window.navigator as { serviceWorker?: unknown }).serviceWorker;
}

function signIn() {
  useAuthStore.setState({
    isAuthenticated: true,
    token: "t",
    user: { id: "me", email: "me@example.com", name: "Me", avatar: "", role: "viewer" },
  });
}

function renderPanel() {
  return render(
    <ToastProvider>
      <BrowserNotificationsSettings />
    </ToastProvider>,
  );
}

beforeEach(() => {
  useAuthStore.setState({ isAuthenticated: false, user: null, token: null });
  mockPb.collection("push_subscriptions").getFullList.mockReset();
  mockPb.collection("push_subscriptions").getFullList.mockResolvedValue([]);
  mockPb.collection("push_subscriptions").create.mockReset();
  mockPb.collection("push_subscriptions").create.mockResolvedValue({ id: "sub-new" });
  mockPb.collection("push_subscriptions").delete.mockReset();
  mockPb.collection("push_subscriptions").delete.mockResolvedValue(true);
  vi.mocked(apiFetch).mockReset();
  vi.mocked(apiFetch).mockResolvedValue(
    new Response(JSON.stringify({ public_key: VAPID_PUBLIC_KEY }), { status: 200 }),
  );
});

afterEach(() => {
  uninstallPushSupport();
});

describe("BrowserNotificationsSettings — unsupported browser", () => {
  it("shows the unsupported message and never queries push_subscriptions", () => {
    signIn();
    renderPanel();

    expect(screen.getByText(/does not support push notifications/i)).toBeInTheDocument();
    expect(mockPb.collection("push_subscriptions").getFullList).not.toHaveBeenCalled();
  });
});

describe("BrowserNotificationsSettings — enable flow", () => {
  it("requests permission, subscribes, and creates the subscription record", async () => {
    signIn();
    const { subscribe } = installPushSupport();
    subscribe.mockResolvedValue(makeSubscription("https://push.example.com/new-device"));

    renderPanel();

    await userEvent.click(await screen.findByRole("button", { name: "Enable on this device" }));

    await waitFor(() => {
      expect(mockPb.collection("push_subscriptions").create).toHaveBeenCalledTimes(1);
    });

    expect(subscribe).toHaveBeenCalledWith(
      expect.objectContaining({
        userVisibleOnly: true,
        applicationServerKey: expect.any(Uint8Array),
      }),
    );

    const payload = mockPb.collection("push_subscriptions").create.mock.calls[0][0] as {
      user_id: string;
      endpoint: string;
      p256dh: string;
      auth: string;
    };
    expect(payload).toMatchObject({
      user_id: "me",
      endpoint: "https://push.example.com/new-device",
      p256dh: "p256dh-value",
      auth: "auth-value",
    });

    expect(await screen.findByRole("button", { name: "Enabled on this device" })).toBeDisabled();
  });

  it("shows an error toast and does not create a record when permission is denied", async () => {
    signIn();
    const { requestPermission, subscribe } = installPushSupport();
    requestPermission.mockResolvedValue("denied");

    renderPanel();

    await userEvent.click(await screen.findByRole("button", { name: "Enable on this device" }));

    expect(await screen.findByText(/permission was not granted/i)).toBeInTheDocument();
    expect(subscribe).not.toHaveBeenCalled();
    expect(mockPb.collection("push_subscriptions").create).not.toHaveBeenCalled();
  });
});

describe("BrowserNotificationsSettings — device list and remove flow", () => {
  it("lists the user's own devices and removes one, unsubscribing the current device", async () => {
    signIn();
    const { getSubscription } = installPushSupport();
    const existing = makeSubscription("https://push.example.com/this-device");
    getSubscription.mockResolvedValue(existing);

    mockPb.collection("push_subscriptions").getFullList.mockResolvedValue([
      {
        id: "sub-1",
        user_id: "me",
        endpoint: "https://push.example.com/this-device",
        p256dh: "p256dh-value",
        auth: "auth-value",
        user_agent: "Test Browser 1.0",
      },
    ]);

    renderPanel();

    const deviceLabel = await screen.findByTitle("Test Browser 1.0");
    expect(deviceLabel).toHaveTextContent("Test Browser 1.0 (this device)");

    await userEvent.click(screen.getByRole("button", { name: /remove device/i }));

    await waitFor(() => {
      expect(mockPb.collection("push_subscriptions").delete).toHaveBeenCalledWith("sub-1");
    });
    expect(existing.unsubscribe).toHaveBeenCalledTimes(1);
    expect(screen.queryByTitle("Test Browser 1.0")).not.toBeInTheDocument();
  });

  it("shows a message when the user has no devices enabled yet", async () => {
    signIn();
    installPushSupport();
    mockPb.collection("push_subscriptions").getFullList.mockResolvedValue([]);

    renderPanel();

    expect(await screen.findByText("No devices enabled yet.")).toBeInTheDocument();
  });
});
