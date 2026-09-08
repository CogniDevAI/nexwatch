import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import {
  LayoutDashboard,
  Bell,
  BellRing,
  BellOff,
  Radio,
  Settings as SettingsIcon,
  Menu,
  X,
  LogOut,
  MessageSquare,
  Server,
  Users,
  ScrollText,
  FileText,
} from "lucide-react";
import { onRealtimeStatusChange } from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useAlertsStore } from "@/stores/alertsStore";
import { useSilencesStore } from "@/stores/silencesStore";
import { useChecksStore } from "@/stores/checksStore";
import { useActiveAlerts } from "@/hooks/useActiveAlerts";
import { useFleetHealth } from "@/hooks/useFleetHealth";
import { useSilences, silenceBucket } from "@/hooks/useSilences";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import type { Status } from "@/components/ui/status";
import { Wordmark, LogoMark } from "@/components/ui/Logo";
import { SignalRail } from "@/components/layout/SignalRail";
import { OfflineBanner } from "@/components/layout/OfflineBanner";
import { UpdateAvailableToast } from "@/components/layout/UpdateAvailableToast";

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  /** Exact-match only — prevents a parent link from staying highlighted
   *  while a nested child route is active. */
  end?: boolean;
  children?: NavItem[];
  /** A quiet count badge next to the label — only rendered when > 0. */
  count?: number;
}

interface NavGroup {
  heading: string;
  items: NavItem[];
}

/** Small neutral count chip for a nav item — never a status color, since
 *  the count itself isn't a severity signal (the item's own icon and the
 *  page it links to already carry that). Kept deliberately quiet: same
 *  muted tone regardless of how large the count gets. */
/** One posture reading in the top context bar: glyph + count + plain-language
 *  noun, so the bar answers "how many, of what" without a legend. */
function PostureReading({
  status,
  count,
  label,
}: {
  status: Status;
  count: number;
  label: string;
}) {
  return (
    <span className="flex items-center gap-1.5">
      <StatusIndicator status={status} dotOnly />
      <span className="font-mono text-xs text-[var(--color-ink)] tabular-nums">{count}</span>
      <span className="text-xs text-[var(--color-ink-muted)]">{label}</span>
    </span>
  );
}

function NavCountBadge({ count }: { count: number }) {
  return (
    <span className="text-2xs ml-auto rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 font-medium text-[var(--color-ink-muted)] tabular-nums">
      {count}
    </span>
  );
}

const baseNavGroups: NavGroup[] = [
  {
    heading: "Now",
    items: [{ to: "/", label: "Operations", icon: LayoutDashboard, end: true }],
  },
  {
    heading: "Fleet",
    items: [
      { to: "/agents", label: "Agents", icon: Server },
      { to: "/checks", label: "Checks", icon: Radio },
    ],
  },
  {
    heading: "Signals",
    items: [
      { to: "/alerts/history", label: "Alert history", icon: BellRing },
      { to: "/logs", label: "Logs", icon: FileText },
    ],
  },
  {
    heading: "Response",
    items: [
      { to: "/alerts", label: "Alert rules", icon: Bell },
      { to: "/alerts/silences", label: "Silences", icon: BellOff },
    ],
  },
  {
    heading: "Admin",
    items: [
      {
        to: "/settings",
        label: "Settings",
        icon: SettingsIcon,
        end: true,
        children: [{ to: "/settings/notifications", label: "Notifications", icon: MessageSquare }],
      },
    ],
  },
];

export function AppShell() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [realtimeIssueCount, setRealtimeIssueCount] = useState(0);
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);
  const hasRole = useAuthStore((s) => s.hasRole);
  const asideRef = useRef<HTMLElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);

  // Quiet nav counts — both are pure selectors over stores AppShell already
  // owns (see the fetch-once effect below), so reading them here adds no
  // extra requests. See DESIGN.md §10.
  const { alerts: firingAlerts } = useActiveAlerts();
  const { silences } = useSilences();
  const { fleet } = useFleetHealth();
  const activeSilenceCount = silences.filter((s) => silenceBucket(s) === "active").length;

  const posture = {
    ok: fleet.filter((a) => a.status === "ok").length,
    warning: fleet.filter((a) => a.status === "warning").length,
    critical: fleet.filter((a) => a.status === "critical").length,
    offline: fleet.filter((a) => a.status === "offline").length,
  };

  useEffect(() => {
    return onRealtimeStatusChange((issues) => setRealtimeIssueCount(issues.length));
  }, []);

  // Single owner of the agents/alerts fetch + realtime subscription for the
  // whole authenticated session. AppShell mounts exactly once per session
  // (every protected route renders inside it), so this is the one place
  // that should trigger these — every other consumer (SignalRail, Dashboard,
  // Agents, ServerDetail) only reads the resulting store state via
  // useFleetHealth/useActiveAlerts and never fetches or subscribes itself.
  // Before this, each of those consumers fetched and subscribed
  // independently, producing a request storm. See DESIGN.md §10.
  useEffect(() => {
    void useAgentStore.getState().fetchAgents();
    const unsubscribeAgents = useAgentStore.getState().subscribeToAgents();
    void useAlertsStore.getState().fetchAlerts();
    const unsubscribeAlerts = useAlertsStore.getState().subscribeToAlerts();
    void useSilencesStore.getState().fetchSilences();
    const unsubscribeSilences = useSilencesStore.getState().subscribeToSilences();
    void useChecksStore.getState().fetchChecks();
    const unsubscribeChecks = useChecksStore.getState().subscribeToChecks();

    return () => {
      unsubscribeAgents();
      unsubscribeAlerts();
      unsubscribeSilences();
      unsubscribeChecks();
    };
  }, []);

  // Mobile drawer: move focus in, trap Tab within it, and close on Escape —
  // it renders as a full overlay below the lg breakpoint, so it needs the
  // same keyboard behavior as any other dismissible overlay.
  useEffect(() => {
    if (!sidebarOpen) return;

    const aside = asideRef.current;
    if (!aside) return;

    const focusable = aside.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    first?.focus();

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setSidebarOpen(false);
        menuButtonRef.current?.focus();
        return;
      }
      if (e.key !== "Tab" || focusable.length === 0) return;
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first?.focus();
      }
    }

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [sidebarOpen]);

  // Users and Audit log are Settings sub-pages, nested under Settings
  // rather than sitting as top-level items (see DESIGN.md §3). Audit log
  // matches its collection's own read rule (operator or admin — see the
  // "audit_log" migration); Users stays admin-only.
  const groups: NavGroup[] = baseNavGroups.map((group) => ({
    ...group,
    items: group.items.map((item) => {
      if (item.to === "/alerts/history") {
        return { ...item, count: firingAlerts.length };
      }
      if (item.to === "/alerts/silences") {
        return { ...item, count: activeSilenceCount };
      }
      if (item.to !== "/settings") return item;

      const children = [...(item.children ?? [])];
      if (hasRole("operator")) {
        children.push({ to: "/settings/audit", label: "Audit log", icon: ScrollText });
      }
      if (hasRole("admin")) {
        children.push({ to: "/settings/users", label: "Users", icon: Users });
      }
      return { ...item, children };
    }),
  }));

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-[var(--color-void)]">
      {/* F11 — installable PWA: offline state and pending-update prompt,
          visible from every page since they render here rather than per-page. */}
      <OfflineBanner />
      <UpdateAvailableToast />
      {realtimeIssueCount > 0 && (
        <div className="border-b border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 px-4 py-2 text-center text-xs text-[var(--color-warn)]">
          Live updates degraded for {realtimeIssueCount} stream
          {realtimeIssueCount === 1 ? "" : "s"}. Polling and manual refresh remain available.
        </div>
      )}

      <div className="flex flex-1 overflow-hidden">
        {/* Mobile overlay */}
        {sidebarOpen && (
          <button
            type="button"
            aria-label="Close sidebar"
            className="fixed inset-0 z-40 bg-black/50 lg:hidden"
            onClick={() => setSidebarOpen(false)}
          />
        )}

        {/* Sidebar */}
        <aside
          ref={asideRef}
          className={`fixed inset-y-0 left-0 z-50 flex w-60 flex-shrink-0 transform flex-col border-r border-[var(--color-line)] bg-[var(--color-void-lift)] transition-transform duration-200 ease-in-out lg:relative lg:translate-x-0 ${
            sidebarOpen ? "translate-x-0" : "-translate-x-full"
          }`}
        >
          {/* Logo */}
          <div className="flex h-14 flex-shrink-0 items-center justify-between border-b border-[var(--color-line)] px-4">
            <Wordmark />
            <button
              onClick={() => setSidebarOpen(false)}
              aria-label="Close sidebar"
              className="text-[var(--color-ink-muted)] transition-colors hover:text-[var(--color-ink)] lg:hidden"
            >
              <X className="h-5 w-5" />
            </button>
          </div>

          {/* Navigation */}
          <nav
            className="flex-1 space-y-4 overflow-y-auto px-2 py-4"
            aria-label="Operator workflow"
          >
            {groups.map((group) => (
              <section key={group.heading} aria-labelledby={`nav-${group.heading.toLowerCase()}`}>
                <h2
                  id={`nav-${group.heading.toLowerCase()}`}
                  className="text-2xs px-3 pb-1.5 font-medium text-[var(--color-ink-faint)]"
                >
                  {group.heading}
                </h2>
                <div className="space-y-1">
                  {group.items.map((item) => (
                    <div key={item.to}>
                      <NavLink
                        to={item.to}
                        end={item.end}
                        onClick={() => setSidebarOpen(false)}
                        className={({ isActive }) =>
                          `flex items-center gap-2.5 border-l-2 px-3 py-2 text-sm transition-colors ${
                            isActive
                              ? "border-[var(--color-signal)] bg-[var(--color-signal)]/10 font-medium text-[var(--color-signal)]"
                              : "border-transparent text-[var(--color-ink-muted)] hover:bg-[var(--color-panel)] hover:text-[var(--color-ink)]"
                          }`
                        }
                      >
                        <item.icon className="h-[18px] w-[18px]" aria-hidden="true" />
                        <span>{item.label}</span>
                        {!!item.count && <NavCountBadge count={item.count} />}
                      </NavLink>

                      {item.children && (
                        <div className="mt-0.5 mb-0.5 ml-6 space-y-0.5 border-l border-[var(--color-line)] pl-2">
                          {item.children.map((child) => (
                            <NavLink
                              key={child.to}
                              to={child.to}
                              onClick={() => setSidebarOpen(false)}
                              className={({ isActive }) =>
                                `flex items-center gap-2.5 px-2.5 py-1.5 text-xs transition-colors ${
                                  isActive
                                    ? "font-medium text-[var(--color-signal)]"
                                    : "text-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                                }`
                              }
                            >
                              <child.icon className="h-3.5 w-3.5" aria-hidden="true" />
                              <span>{child.label}</span>
                            </NavLink>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </nav>

          {/* Signal Rail — the fleet health strip, visible from every page */}
          <SignalRail />

          {/* Footer / User */}
          <div className="flex-shrink-0 border-t border-[var(--color-line)] px-4 py-4">
            <div className="flex items-center justify-between">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-[var(--color-ink)]">
                  {user?.name || user?.email || "Admin"}
                </p>
                <p className="text-2xs text-[var(--color-ink-faint)]">NexWatch v0.1.0</p>
              </div>
              <button
                onClick={logout}
                aria-label="Sign out"
                title="Sign out"
                className="rounded-[var(--radius-control)] p-2 text-[var(--color-ink-faint)] transition-colors hover:bg-[var(--color-critical)]/10 hover:text-[var(--color-critical)]"
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          </div>
        </aside>

        {/* Main Content */}
        <div className="flex flex-1 flex-col overflow-hidden">
          {/* Context bar — the operations frame around every page: how to get
                back to the nav on a phone, what the fleet currently looks like,
                and whether anything is firing. All of it reads the same stores
                this shell already owns, so it costs no extra request (§10). */}
          <header className="flex h-14 flex-shrink-0 items-center gap-3 border-b border-[var(--color-line)] bg-[var(--color-void-lift)] px-4">
            <button
              ref={menuButtonRef}
              onClick={() => setSidebarOpen(true)}
              aria-label="Open sidebar"
              className="-ml-2 rounded-[var(--radius-control)] p-2 text-[var(--color-ink-muted)] transition-colors hover:bg-[var(--color-panel)] hover:text-[var(--color-ink)] lg:hidden"
            >
              <Menu className="h-5 w-5" />
            </button>
            <div className="flex items-center gap-2 lg:hidden">
              <LogoMark className="h-5 w-5" />
              <span className="text-base font-semibold text-[var(--color-ink)]">NexWatch</span>
            </div>

            {fleet.length > 0 && (
              <div
                className="hidden items-center gap-5 sm:flex"
                aria-label={`Fleet posture: ${fleet.length} agents`}
              >
                <PostureReading status="ok" count={posture.ok} label="operational" />
                {posture.warning > 0 && (
                  <PostureReading status="warning" count={posture.warning} label="warning" />
                )}
                {posture.critical > 0 && (
                  <PostureReading status="critical" count={posture.critical} label="critical" />
                )}
                {posture.offline > 0 && (
                  <PostureReading status="offline" count={posture.offline} label="offline" />
                )}
              </div>
            )}

            <div className="ml-auto flex items-center gap-4">
              {firingAlerts.length > 0 ? (
                <Link
                  to="/alerts/history"
                  className="flex items-center gap-1.5 text-xs font-medium text-[var(--color-critical)] hover:underline"
                >
                  <BellRing className="h-3.5 w-3.5" aria-hidden="true" />
                  {firingAlerts.length} firing
                </Link>
              ) : (
                <span className="hidden text-xs text-[var(--color-ink-faint)] sm:inline">
                  No firing alerts
                </span>
              )}
              <span className="text-2xs hidden font-mono text-[var(--color-ink-faint)] md:inline">
                {realtimeIssueCount > 0 ? "Live updates degraded" : "Live"}
              </span>
            </div>
          </header>

          <main className="flex-1 overflow-auto bg-[var(--color-void)]">
            <div className="mx-auto max-w-[1600px] p-4 sm:p-6">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  );
}
