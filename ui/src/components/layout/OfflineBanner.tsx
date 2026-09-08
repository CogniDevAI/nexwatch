import { useEffect, useState } from "react";
import { WifiOff } from "lucide-react";

/**
 * F11 — installable PWA. A thin banner across the top of the app while the
 * browser reports itself offline (the "online"/"offline" window events).
 * The app shell and its last-fetched data stay visible (served from the
 * service worker's precache and each page's already-loaded state), but
 * live fetches, mutations, and realtime subscriptions will fail — this
 * tells the operator why before they hit a confusing, unexplained error.
 */
export function OfflineBanner() {
  const [isOffline, setIsOffline] = useState(() => !navigator.onLine);

  useEffect(() => {
    const handleOnline = () => setIsOffline(false);
    const handleOffline = () => setIsOffline(true);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  if (!isOffline) return null;

  return (
    <div
      role="status"
      className="flex flex-shrink-0 items-center justify-center gap-2 border-b border-[var(--color-warn)]/25 bg-[var(--color-warn)]/10 px-4 py-2 text-xs font-medium text-[var(--color-warn)]"
    >
      <WifiOff className="h-3.5 w-3.5 flex-shrink-0" aria-hidden="true" />
      <span>You&apos;re offline. Showing cached data until your connection returns.</span>
    </div>
  );
}
