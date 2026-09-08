import { create } from "zustand";

/**
 * Bridges the service worker registration lifecycle (registered once in
 * main.tsx, outside the React tree via virtual:pwa-register) into React
 * state, so a component mounted anywhere in the app (see
 * UpdateAvailableToast in AppShell) can render an "Update available"
 * prompt and trigger the reload. See ui/src/sw.ts for the corresponding
 * "message" listener that actually applies the update.
 */
interface PwaState {
  /** True once a new service worker has installed and is waiting to
   *  activate (registerType: "prompt" in vite.config.ts never activates
   *  it automatically). */
  needRefresh: boolean;
  /** Set by main.tsx to the function virtual:pwa-register's registerSW()
   *  returns; calling it messages the waiting worker to skip waiting and
   *  reloads the page once it takes control. Null until the service
   *  worker has registered at all (e.g. unsupported browser, dev mode). */
  updateServiceWorker: ((reloadPage?: boolean) => Promise<void>) | null;
  setNeedRefresh: (value: boolean) => void;
  setUpdateServiceWorker: (fn: ((reloadPage?: boolean) => Promise<void>) | null) => void;
}

export const usePwaStore = create<PwaState>((set) => ({
  needRefresh: false,
  updateServiceWorker: null,
  setNeedRefresh: (value) => set({ needRefresh: value }),
  setUpdateServiceWorker: (fn) => set({ updateServiceWorker: fn }),
}));
