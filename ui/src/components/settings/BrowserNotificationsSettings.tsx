import { useCallback, useEffect, useState } from "react";
import { Bell, BellRing, Download, Smartphone, Trash2 } from "lucide-react";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { useAuthStore } from "@/stores/authStore";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/toastContext";
import type { PushSubscriptionRecord } from "@/types";

// Reuses lib.dom.d.ts's own NotificationPermission union directly (rather
// than re-declaring an identical local type) so Notification.permission /
// requestPermission() never need a same-type assertion to satisfy it.
type PermissionState = NotificationPermission;

/** Chrome/Edge-only event fired before the browser would show its own
 *  install prompt; calling preventDefault() suppresses that native prompt
 *  so this panel can offer the same action as one more button here
 *  instead, and re-fire it later via event.prompt(). Not in TypeScript's
 *  DOM lib, so declared narrowly to just what this component uses. */
interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
}

/** Detects Web Push support without assuming HTTPS/localhost — a
 *  non-secure context simply never defines these globals, so checking for
 *  their presence covers both "old browser" and "not served over HTTPS"
 *  in one condition, matching this panel's one explanatory sentence for
 *  the unsupported state. */
function isPushSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    "Notification" in window &&
    "serviceWorker" in navigator &&
    "PushManager" in window
  );
}

/** Converts the VAPID public key (base64url, as returned by the hub) into
 *  the raw byte array PushManager.subscribe's applicationServerKey expects. */
function urlBase64ToUint8Array(base64Url: string): Uint8Array {
  const padding = "=".repeat((4 - (base64Url.length % 4)) % 4);
  const base64 = (base64Url + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const output = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) output[i] = raw.charCodeAt(i);
  return output;
}

/**
 * F11 — installable PWA + Web Push. Lets the current user enable push
 * notifications on this device, send themselves a test notification, see
 * every device they have enabled, and remove one — plus a one-click
 * "Add to home screen" hint when the browser offers its install prompt.
 * Mounted at the top of the Notifications settings page (unlike the
 * channel list below it, this is personal to the signed-in user, not an
 * operator-managed fleet-wide setting, so it renders for any role).
 */
export function BrowserNotificationsSettings() {
  const { showToast } = useToast();
  const userId = useAuthStore((s) => s.user?.id);
  const [supported] = useState(isPushSupported);

  const [permission, setPermission] = useState<PermissionState>(() =>
    supported ? Notification.permission : "default",
  );
  const [subscriptions, setSubscriptions] = useState<PushSubscriptionRecord[]>([]);
  const [currentEndpoint, setCurrentEndpoint] = useState<string | null>(null);
  const [loading, setLoading] = useState(supported);
  const [enabling, setEnabling] = useState(false);
  const [testing, setTesting] = useState(false);
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null);

  const loadSubscriptions = useCallback(async () => {
    if (!supported || !userId) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const records = await pb
        .collection("push_subscriptions")
        .getFullList<PushSubscriptionRecord>({ sort: "-created" });
      setSubscriptions(records);
    } catch {
      setSubscriptions([]);
    } finally {
      setLoading(false);
    }
  }, [supported, userId]);

  useEffect(() => {
    void loadSubscriptions();
  }, [loadSubscriptions]);

  // Learn whether this exact browser/device already has an active
  // subscription, so "Enable on this device" reflects reality even after
  // a reload (the button itself lives only in React state otherwise).
  useEffect(() => {
    if (!supported) return;
    let cancelled = false;
    navigator.serviceWorker.ready
      .then((registration) => registration.pushManager.getSubscription())
      .then((sub) => {
        if (!cancelled) setCurrentEndpoint(sub?.endpoint ?? null);
      })
      .catch(() => {
        // No active service worker registration yet (e.g. first paint
        // before it finishes installing) — treat as "not enabled".
      });
    return () => {
      cancelled = true;
    };
  }, [supported]);

  useEffect(() => {
    const handler = (event: Event) => {
      event.preventDefault();
      setInstallPrompt(event as BeforeInstallPromptEvent);
    };
    window.addEventListener("beforeinstallprompt", handler);
    return () => window.removeEventListener("beforeinstallprompt", handler);
  }, []);

  const handleEnable = async () => {
    if (!supported || !userId) return;
    setEnabling(true);
    try {
      const result = await Notification.requestPermission();
      setPermission(result);
      if (result !== "granted") {
        showToast("Notification permission was not granted", "error");
        return;
      }

      const registration = await navigator.serviceWorker.ready;
      const keyResponse = await apiFetch("/api/custom/push/vapid-public-key");
      if (!keyResponse.ok) throw new Error("Failed to fetch the VAPID public key");
      const { public_key: publicKey } = (await keyResponse.json()) as { public_key: string };

      const subscription =
        (await registration.pushManager.getSubscription()) ??
        (await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: urlBase64ToUint8Array(publicKey),
        }));

      setCurrentEndpoint(subscription.endpoint);

      const alreadySaved = subscriptions.some((s) => s.endpoint === subscription.endpoint);
      if (!alreadySaved) {
        const json = subscription.toJSON();
        await pb.collection("push_subscriptions").create({
          user_id: userId,
          endpoint: subscription.endpoint,
          p256dh: json.keys?.p256dh ?? "",
          auth: json.keys?.auth ?? "",
          user_agent: navigator.userAgent,
        });
        await loadSubscriptions();
      }

      showToast("Browser notifications enabled on this device", "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to enable notifications", "error");
    } finally {
      setEnabling(false);
    }
  };

  const handleRemove = async (subscription: PushSubscriptionRecord) => {
    try {
      await pb.collection("push_subscriptions").delete(subscription.id);
      if (supported && subscription.endpoint === currentEndpoint) {
        const registration = await navigator.serviceWorker.ready;
        const active = await registration.pushManager.getSubscription();
        await active?.unsubscribe();
        setCurrentEndpoint(null);
      }
      setSubscriptions((prev) => prev.filter((s) => s.id !== subscription.id));
      showToast("Device removed", "success");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to remove device", "error");
    }
  };

  const handleTest = async () => {
    setTesting(true);
    try {
      const response = await apiFetch("/api/custom/push/test", { method: "POST" });
      if (!response.ok) throw new Error("Failed to send test notification");
      const data = (await response.json()) as { results?: { success: boolean }[] };
      const results = data.results ?? [];
      if (results.length === 0) {
        showToast("No devices enabled yet — enable notifications on this device first", "error");
        return;
      }
      const succeeded = results.filter((r) => r.success).length;
      showToast(
        `Test sent to ${succeeded} of ${results.length} device(s)`,
        succeeded > 0 ? "success" : "error",
      );
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to send test notification", "error");
    } finally {
      setTesting(false);
    }
  };

  const handleInstall = async () => {
    if (!installPrompt) return;
    await installPrompt.prompt();
    setInstallPrompt(null);
  };

  if (!supported) {
    return (
      <Panel>
        <PanelHeader
          title="Browser notifications"
          description="Get alert notifications even when NexWatch isn't open in a tab."
        />
        <PanelBody>
          <p className="text-sm text-[var(--color-ink-faint)]">
            Your browser does not support push notifications, or NexWatch isn&apos;t served over
            HTTPS — both push and installing NexWatch as an app require it.
          </p>
        </PanelBody>
      </Panel>
    );
  }

  return (
    <Panel>
      <PanelHeader
        title="Browser notifications"
        description="Get alert notifications even when NexWatch isn't open in a tab."
      />
      <PanelBody className="space-y-4">
        {installPrompt && (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius-control)] border border-[var(--color-signal)]/25 bg-[var(--color-signal)]/10 px-4 py-3">
            <span className="text-sm text-[var(--color-ink)]">
              Install NexWatch for quicker access and notifications.
            </span>
            <Button size="sm" variant="secondary" onClick={() => void handleInstall()}>
              <Download className="h-4 w-4" aria-hidden="true" />
              Add to home screen
            </Button>
          </div>
        )}

        <div className="flex items-center gap-2">
          <span className="text-sm text-[var(--color-ink-muted)]">Permission:</span>
          <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-2 py-0.5 text-xs text-[var(--color-ink-faint)]">
            {permission}
          </span>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleEnable()}
            disabled={enabling || currentEndpoint !== null}
          >
            <Bell className="h-4 w-4" aria-hidden="true" />
            {enabling
              ? "Enabling…"
              : currentEndpoint
                ? "Enabled on this device"
                : "Enable on this device"}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleTest()}
            disabled={testing}
          >
            <BellRing className="h-4 w-4" aria-hidden="true" />
            {testing ? "Sending…" : "Send test"}
          </Button>
        </div>

        <div>
          <p className="mb-2 text-sm font-medium text-[var(--color-ink-muted)]">Your devices</p>
          {loading ? (
            <p className="text-sm text-[var(--color-ink-faint)]">Loading…</p>
          ) : subscriptions.length === 0 ? (
            <p className="text-sm text-[var(--color-ink-faint)]">No devices enabled yet.</p>
          ) : (
            <ul className="divide-y divide-[var(--color-line-soft)]">
              {subscriptions.map((sub) => (
                <li key={sub.id} className="flex items-center justify-between gap-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <Smartphone
                      className="h-4 w-4 flex-shrink-0 text-[var(--color-ink-faint)]"
                      aria-hidden="true"
                    />
                    <span
                      className="truncate text-sm text-[var(--color-ink)]"
                      title={sub.user_agent}
                    >
                      {sub.user_agent || "Unknown device"}
                      {sub.endpoint === currentEndpoint ? " (this device)" : ""}
                    </span>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => void handleRemove(sub)}
                    aria-label={`Remove device ${sub.user_agent || sub.id}`}
                  >
                    <Trash2 className="h-4 w-4" aria-hidden="true" />
                    Remove
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </PanelBody>
    </Panel>
  );
}
