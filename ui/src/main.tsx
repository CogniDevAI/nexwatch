import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { registerSW } from "virtual:pwa-register";
import App from "./App";
import { usePwaStore } from "./stores/pwaStore";
import "./styles/globals.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

// Registers the service worker (F11 — installable PWA). registerType:
// "prompt" (vite.config.ts) means a new worker installs and waits rather
// than activating itself, so onNeedRefresh — not an automatic reload — is
// how the app learns an update is ready; UpdateAvailableToast (mounted in
// AppShell) reads pwaStore to render the "Reload" prompt and calls the
// updateServiceWorker function captured here. No-op (registerSW resolves
// to a no-op updater) in a browser without service worker support, and
// never registered at all in dev (vite.config.ts's devOptions.enabled is
// false).
const updateServiceWorker = registerSW({
  onNeedRefresh() {
    usePwaStore.getState().setNeedRefresh(true);
  },
});
usePwaStore.getState().setUpdateServiceWorker(updateServiceWorker);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
