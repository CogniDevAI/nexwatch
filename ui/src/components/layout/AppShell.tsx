import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import {
  LayoutDashboard,
  Bell,
  BellRing,
  Settings,
  Menu,
  X,
  LogOut,
  Monitor,
  MessageSquare,
  Server,
} from "lucide-react";
import { useAuthStore } from "@/stores/authStore";

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/agents", label: "Agents", icon: Server },
  { to: "/alerts", label: "Alert Rules", icon: Bell },
  { to: "/alerts/history", label: "Alert History", icon: BellRing },
  { to: "/settings", label: "Settings", icon: Settings },
  { to: "/settings/notifications", label: "Notifications", icon: MessageSquare },
];

export function AppShell() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const logout = useAuthStore((s) => s.logout);
  const user = useAuthStore((s) => s.user);

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Mobile overlay */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 w-64 flex-shrink-0 bg-[var(--color-bg-surface)] border-r border-[var(--color-border-default)] flex flex-col transform transition-transform duration-200 ease-in-out lg:relative lg:translate-x-0 ${
          sidebarOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        {/* Logo */}
        <div className="h-16 flex items-center justify-between px-6 border-b border-[var(--color-border-default)]">
          <div className="flex items-center gap-2.5">
            <Monitor className="w-5 h-5 text-[var(--color-accent-cyan)]" />
            <h1 className="text-xl font-bold">
              <span className="text-[var(--color-accent-cyan)]">Nex</span>
              <span className="text-[var(--color-accent-purple)]">Watch</span>
            </h1>
          </div>
          <button
            onClick={() => setSidebarOpen(false)}
            className="lg:hidden text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Navigation */}
        <nav className="flex-1 py-4 px-3 space-y-1">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              onClick={() => setSidebarOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-[var(--color-accent-cyan)]/10 text-[var(--color-accent-cyan)]"
                    : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)]"
                }`
              }
            >
              <item.icon className="w-[18px] h-[18px]" />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>

        {/* Footer / User */}
        <div className="px-4 py-4 border-t border-[var(--color-border-default)]">
          <div className="flex items-center justify-between">
            <div className="min-w-0">
              <p className="text-sm font-medium text-[var(--color-text-primary)] truncate">
                {user?.name || user?.email || "Admin"}
              </p>
              <p className="text-xs text-[var(--color-text-muted)]">
                NexWatch v0.1.0
              </p>
            </div>
            <button
              onClick={logout}
              title="Sign out"
              className="p-2 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-red)] hover:bg-[var(--color-accent-red)]/10 transition-colors"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Mobile header */}
        <header className="h-16 flex items-center px-4 border-b border-[var(--color-border-default)] bg-[var(--color-bg-surface)] lg:hidden">
          <button
            onClick={() => setSidebarOpen(true)}
            className="p-2 -ml-2 rounded-lg text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)] transition-colors"
          >
            <Menu className="w-5 h-5" />
          </button>
          <div className="ml-3 flex items-center gap-2">
            <Monitor className="w-4 h-4 text-[var(--color-accent-cyan)]" />
            <span className="text-lg font-bold">
              <span className="text-[var(--color-accent-cyan)]">Nex</span>
              <span className="text-[var(--color-accent-purple)]">Watch</span>
            </span>
          </div>
        </header>

        <main className="flex-1 overflow-auto bg-[var(--color-bg-primary)]">
          <div className="p-6">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
