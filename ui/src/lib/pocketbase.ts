import PocketBase, {
  type RecordModel,
  type RecordSubscription,
  type UnsubscribeFunc,
} from "pocketbase";

function resolvePocketBaseUrl(): string {
  if (import.meta.env.VITE_PB_URL) return import.meta.env.VITE_PB_URL;

  if (import.meta.env.DEV) {
    return `http://${window.location.hostname}:8090`;
  }

  return window.location.origin;
}

const pb = new PocketBase(resolvePocketBaseUrl());

// Disable auto-cancellation so multiple concurrent requests don't cancel each other
pb.autoCancellation(false);

type RealtimeIssue = {
  collection: string;
  message: string;
};

const realtimeIssues = new Map<string, RealtimeIssue>();
const realtimeListeners = new Set<(issues: RealtimeIssue[]) => void>();

function emitRealtimeStatus() {
  const issues = Array.from(realtimeIssues.values());
  for (const listener of realtimeListeners) listener(issues);
}

export function onRealtimeStatusChange(listener: (issues: RealtimeIssue[]) => void): () => void {
  realtimeListeners.add(listener);
  listener(Array.from(realtimeIssues.values()));
  return () => realtimeListeners.delete(listener);
}

export function subscribeToCollection<T extends RecordModel>(
  collection: string,
  topic: string,
  callback: (event: RecordSubscription<T>) => void,
): () => void {
  let active = true;
  let unsubscribe: UnsubscribeFunc | null = null;

  void pb
    .collection(collection)
    .subscribe<T>(topic, callback)
    .then((nextUnsubscribe) => {
      if (!active) {
        void nextUnsubscribe();
        return;
      }
      unsubscribe = nextUnsubscribe;
      if (realtimeIssues.delete(collection)) emitRealtimeStatus();
    })
    .catch((err: unknown) => {
      if (!active) return;
      const message = err instanceof Error ? err.message : "subscription failed";
      console.warn(`[realtime] ${collection} subscription unavailable: ${message}`);
      realtimeIssues.set(collection, { collection, message });
      emitRealtimeStatus();
    });

  return () => {
    active = false;
    try {
      void unsubscribe?.();
    } catch (err) {
      const message = err instanceof Error ? err.message : "unsubscribe failed";
      console.warn(`[realtime] ${collection} unsubscribe failed: ${message}`);
    }
  };
}

export default pb;
