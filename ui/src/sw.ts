/// <reference lib="webworker" />

// NexWatch service worker (F11 — PWA + Web Push notifications).
//
// Scope: precache the built app shell (JS/CSS/HTML, self-hosted fonts,
// icons — see vite.config.ts's injectManifest.globPatterns) and handle
// incoming Web Push messages. It deliberately does NOT add any runtime
// caching route: "/api/*", "/ws/*", and "/_/" (PocketBase's own routes)
// are never matched by the precache manifest (they live outside dist/)
// and there is no other fetch handler here, so every such request passes
// straight through to the network exactly as if this worker did not
// exist.
import { precacheAndRoute } from "workbox-precaching";

declare const self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);

// registerType: "prompt" (vite.config.ts) means a new worker installs and
// waits rather than activating immediately — main.tsx's "Update available"
// toast calls updateSW(), which posts this exact message to the waiting
// worker via workbox-window's Workbox#messageSkipWaiting(). Without this
// listener the waiting worker would never advance past "waiting", and the
// toast's "Reload" action would do nothing.
self.addEventListener("message", (event: ExtendableMessageEvent) => {
  if (event.data?.type === "SKIP_WAITING") {
    void self.skipWaiting();
  }
});

// Web Push payload shape sent by the hub's "webpush" notification channel
// (internal/hub/push.Payload) and by POST /api/custom/push/test
// (internal/hub/api/push_routes.go) — keep these two in sync.
interface PushPayload {
  title: string;
  body: string;
  url?: string;
  severity?: string;
  tag?: string;
  timestamp?: number;
}

self.addEventListener("push", (event: PushEvent) => {
  let payload: PushPayload = { title: "NexWatch", body: "You have a new alert." };
  try {
    if (event.data) payload = { ...payload, ...(event.data.json() as PushPayload) };
  } catch {
    // Malformed or empty payload — fall back to the generic message above
    // rather than dropping the notification entirely.
  }

  const url = payload.url ?? "/";

  event.waitUntil(
    self.registration.showNotification(payload.title, {
      body: payload.body,
      tag: payload.tag,
      // A shared tag replaces the previous notification with the same
      // tag in the tray instead of stacking duplicates (e.g. repeated
      // firings of the same alert rule) — see buildAlertPayload's tag.
      icon: "/pwa-192x192.png",
      badge: "/pwa-192x192.png",
      data: { url },
    }),
  );
});

// Clicking a notification focuses an already-open NexWatch tab on its
// target URL when one exists, or opens a new window/tab otherwise — a
// closed app must still be reachable from a notification received while
// it wasn't running.
self.addEventListener("notificationclick", (event: NotificationEvent) => {
  event.notification.close();
  const targetURL: string = event.notification.data?.url ?? "/";

  event.waitUntil(
    (async () => {
      const clientsList = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const absoluteTarget = new URL(targetURL, self.location.origin).href;

      for (const client of clientsList) {
        if (client.url === absoluteTarget && "focus" in client) {
          await client.focus();
          return;
        }
      }
      // No exact match — focus any open NexWatch window and navigate it,
      // rather than always opening a new tab.
      for (const client of clientsList) {
        if ("focus" in client && "navigate" in client) {
          await client.focus();
          await client.navigate(absoluteTarget);
          return;
        }
      }
      await self.clients.openWindow(absoluteTarget);
    })(),
  );
});
