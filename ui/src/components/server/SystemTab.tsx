import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { HardDrive, Network, Search, Activity } from "lucide-react";
import pb from "@/lib/pocketbase";

// ─── Types ────────────────────────────────────────────────────────────────────

interface SystemTabProps {
  agentId: string;
}

interface DiskIODevice {
  name: string;
  read_bytes_per_sec: number;
  write_bytes_per_sec: number;
  read_count_per_sec: number;
  write_count_per_sec: number;
  io_time_ms: number;
}

interface DiskIOData {
  devices: DiskIODevice[];
}

interface PortCount {
  port: number;
  established_count: number;
}

interface ConnectionsData {
  summary: Record<string, number>;
  total: number;
  by_port: PortCount[];
}

interface SystemdService {
  name: string;
  sub: string;      // running | failed | dead | exited | etc.
  active: string;   // active | activating | inactive | failed
  load: string;
  description: string;
}

interface ServicesData {
  services: SystemdService[];
  total: number;
  running: number;
  failed: number;
  other: number;
}

// ─── Utilities ────────────────────────────────────────────────────────────────

const REFRESH_INTERVAL = 15_000;

function formatBytesPerSec(bps: number): string {
  if (bps >= 1_048_576) return `${(bps / 1_048_576).toFixed(1)} MB/s`;
  if (bps >= 1_024) return `${(bps / 1_024).toFixed(1)} KB/s`;
  return `${bps.toFixed(0)} B/s`;
}

// ─── Disk I/O Section ─────────────────────────────────────────────────────────

function DiskIOSection({ data }: { data: DiskIOData }) {
  const devices = data.devices ?? [];

  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
      <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex items-center gap-2">
        <HardDrive className="w-4 h-4 text-[var(--color-text-muted)]" />
        <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">Disk I/O</h3>
      </div>

      {devices.length === 0 ? (
        <div className="p-10 text-center">
          <HardDrive className="w-10 h-10 text-[var(--color-text-muted)] mx-auto mb-3" />
          <p className="text-sm text-[var(--color-text-secondary)]">
            No disk I/O data reported yet. Make sure the diskio collector is enabled.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border-default)]">
                {(["NAME", "READ/s", "WRITE/s", "READ MB/s", "WRITE MB/s", "IO TIME"] as const).map((col) => (
                  <th
                    key={col}
                    className={`px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)] ${
                      col === "NAME" ? "text-left" : "text-right"
                    }`}
                  >
                    {col}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {devices.map((dev, idx) => (
                <tr
                  key={dev.name}
                  className={`transition-colors ${
                    idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-bg-elevated)]/30"
                  } hover:bg-[var(--color-bg-elevated)]`}
                >
                  <td className="px-5 py-3 font-medium text-[var(--color-text-primary)] font-mono text-xs">
                    {dev.name}
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-[var(--color-text-secondary)]">
                    {dev.read_count_per_sec?.toFixed(0) ?? "0"}/s
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-[var(--color-text-secondary)]">
                    {dev.write_count_per_sec?.toFixed(0) ?? "0"}/s
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-[var(--color-accent-cyan)]">
                    {formatBytesPerSec(dev.read_bytes_per_sec ?? 0)}
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-[#8b5cf6]">
                    {formatBytesPerSec(dev.write_bytes_per_sec ?? 0)}
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-[var(--color-text-secondary)]">
                    {dev.io_time_ms?.toFixed(0) ?? "0"} ms
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ─── TCP Connections Section ───────────────────────────────────────────────────

function ConnectionsSection({ data }: { data: ConnectionsData }) {
  const summary = data.summary ?? {};
  const byPort = useMemo(
    () => [...(data.by_port ?? [])].sort((a, b) => b.established_count - a.established_count).slice(0, 20),
    [data.by_port],
  );

  const established = summary["established"] ?? 0;
  const timeWait = summary["time_wait"] ?? 0;
  const closeWait = summary["close_wait"] ?? 0;
  const listen = summary["listen"] ?? 0;
  const total = data.total ?? 0;

  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
      <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex items-center gap-2">
        <Network className="w-4 h-4 text-[var(--color-text-muted)]" />
        <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">TCP Connections</h3>
        <span className="ml-auto text-xs text-[var(--color-text-muted)] tabular-nums">
          {total} total
        </span>
      </div>

      {total === 0 ? (
        <div className="p-10 text-center">
          <Network className="w-10 h-10 text-[var(--color-text-muted)] mx-auto mb-3" />
          <p className="text-sm text-[var(--color-text-secondary)]">
            No connection data reported yet. Make sure the connections collector is enabled.
          </p>
        </div>
      ) : (
        <div className="p-5 space-y-5">
          {/* Summary chips */}
          <div className="flex flex-wrap gap-2">
            <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]">
              ESTABLISHED
              <span className="tabular-nums font-bold">{established}</span>
            </span>
            <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]">
              TIME_WAIT
              <span className="tabular-nums font-bold">{timeWait}</span>
            </span>
            <span
              className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium ${
                closeWait > 10
                  ? "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]"
                  : "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]"
              }`}
            >
              CLOSE_WAIT
              <span className="tabular-nums font-bold">{closeWait}</span>
            </span>
            <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]">
              LISTEN
              <span className="tabular-nums font-bold">{listen}</span>
            </span>
            <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium bg-[var(--color-bg-elevated)] text-[var(--color-text-secondary)]">
              TOTAL
              <span className="tabular-nums font-bold">{total}</span>
            </span>
          </div>

          {/* Top ports table */}
          {byPort.length > 0 && (
            <div className="rounded-xl border border-[var(--color-border-default)] overflow-hidden">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-[var(--color-border-default)]">
                      <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">
                        Port
                      </th>
                      <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider bg-[var(--color-bg-surface)]">
                        Established Connections
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--color-border-muted)]">
                    {byPort.map((p, idx) => (
                      <tr
                         key={`${p.port}-${idx}`}
                        className={`transition-colors ${
                          idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-bg-elevated)]/30"
                        } hover:bg-[var(--color-bg-elevated)]`}
                       >
                         <td className="px-5 py-3 font-medium text-[var(--color-text-primary)] tabular-nums">
                           {p.port}
                         </td>
                         <td className="px-5 py-3 text-right tabular-nums text-[var(--color-accent-green)]">
                           {p.established_count}
                         </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Services Section ─────────────────────────────────────────────────────────

function ServiceStateBadge({ state }: { state: string }) {
  const normalized = (state ?? "").toLowerCase();
  let classes: string;

  if (normalized === "running" || normalized === "active") {
    classes = "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]";
  } else if (normalized === "failed") {
    classes = "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]";
  } else {
    classes = "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]";
  }

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${classes}`}>
      {state}
    </span>
  );
}

function ServicesSection({ data }: { data: ServicesData }) {
  const [search, setSearch] = useState("");

  const sorted = useMemo(() => {
    const services = data.services ?? [];
    return [...services].sort((a, b) => {
      const order = (s: string) => {
        const n = (s ?? "").toLowerCase();
        if (n === "failed") return 0;
        if (n === "running" || n === "active") return 1;
        return 2;
      };
      return order(a.sub) - order(b.sub);
    });
  }, [data.services]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    if (!q) return sorted;
    return sorted.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        (s.description ?? "").toLowerCase().includes(q) ||
        (s.sub ?? "").toLowerCase().includes(q),
    );
  }, [sorted, search]);

  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
      <div className="px-5 py-4 border-b border-[var(--color-border-default)] flex flex-col sm:flex-row sm:items-center gap-3">
        <div className="flex items-center gap-2 flex-1">
          <Activity className="w-4 h-4 text-[var(--color-text-muted)]" />
          <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">Services</h3>
          <span className="ml-2 inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]">
            {data.running ?? 0} running
          </span>
          {(data.failed ?? 0) > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]">
              {data.failed} failed
            </span>
          )}
        </div>
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-[var(--color-text-muted)]" />
          <input
            type="text"
            placeholder="Search services..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-8 pr-4 py-1.5 rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-elevated)] text-xs text-[var(--color-text-primary)] placeholder:text-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-accent-cyan)] transition-colors w-full sm:w-56"
          />
        </div>
      </div>

      {sorted.length === 0 ? (
        <div className="p-10 text-center">
          <Activity className="w-10 h-10 text-[var(--color-text-muted)] mx-auto mb-3" />
          <p className="text-sm text-[var(--color-text-secondary)]">
            No service data reported yet. Make sure the systemd collector is enabled.
          </p>
        </div>
      ) : filtered.length === 0 ? (
        <div className="p-8 text-center">
          <p className="text-sm text-[var(--color-text-secondary)]">
            No services match &ldquo;{search}&rdquo;.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border-default)]">
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Name
                </th>
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  State
                </th>
                <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider sticky top-0 bg-[var(--color-bg-surface)]">
                  Description
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {filtered.map((svc, idx) => (
                <tr
                  key={svc.name}
                  className={`transition-colors ${
                    idx % 2 === 0 ? "bg-transparent" : "bg-[var(--color-bg-elevated)]/30"
                  } hover:bg-[var(--color-bg-elevated)]`}
                >
                  <td className="px-5 py-3 font-medium text-[var(--color-text-primary)] font-mono text-xs max-w-[240px]">
                    <span className="truncate block" title={svc.name}>
                      {svc.name}
                    </span>
                  </td>
                  <td className="px-5 py-3">
                    <ServiceStateBadge state={svc.sub ?? svc.active ?? ""} />
                  </td>
                  <td className="px-5 py-3 text-[var(--color-text-secondary)] max-w-[400px]">
                    <span className="truncate block" title={svc.description}>
                      {svc.description || "—"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ─── Root component ───────────────────────────────────────────────────────────

export function SystemTab({ agentId }: SystemTabProps) {
  const [diskio, setDiskio] = useState<DiskIOData | null>(null);
  const [connections, setConnections] = useState<ConnectionsData | null>(null);
  const [services, setServices] = useState<ServicesData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchAll = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const headers = { Authorization: pb.authStore.token };
      const [diskioRes, connectionsRes, servicesRes] = await Promise.all([
        fetch(`/api/custom/agents/${agentId}/diskio`, { headers }),
        fetch(`/api/custom/agents/${agentId}/connections`, { headers }),
        fetch(`/api/custom/agents/${agentId}/services`, { headers }),
      ]);

      const [diskioJson, connectionsJson, servicesJson] = await Promise.all([
        diskioRes.ok ? (diskioRes.json() as Promise<DiskIOData>) : Promise.resolve({ devices: [] }),
        connectionsRes.ok ? (connectionsRes.json() as Promise<ConnectionsData>) : Promise.resolve({ summary: {}, total: 0, by_port: [] }),
        servicesRes.ok ? (servicesRes.json() as Promise<ServicesData>) : Promise.resolve({ services: [], total: 0, running: 0, failed: 0, other: 0 }),
      ]);

      setDiskio(diskioJson);
      setConnections(connectionsJson);
      setServices(servicesJson);
      setError(false);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchAll(true);
    intervalRef.current = setInterval(() => fetchAll(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchAll]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Activity className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">Loading system data...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <Activity className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">Failed to load system data</h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          Could not fetch system metrics from this agent. Check that the agent is online.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <DiskIOSection data={diskio ?? { devices: [] }} />
      <ConnectionsSection data={connections ?? { summary: {}, total: 0, by_port: [] }} />
      <ServicesSection data={services ?? { services: [], total: 0, running: 0, failed: 0, other: 0 }} />
    </div>
  );
}
