import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { HardDrive, Network, Search, Activity } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { Panel, PanelHeader, PanelBody } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Input } from "@/components/ui/Field";

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
  sub: string; // running | failed | dead | exited | etc.
  active: string; // active | activating | inactive | failed
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
    <Panel>
      <PanelHeader icon={<HardDrive className="h-4 w-4" />} title="Disk I/O" />
      {devices.length === 0 ? (
        <EmptyState
          icon={HardDrive}
          title="No disk I/O data"
          description="No disk I/O data reported yet. Make sure the diskio collector is enabled."
        />
      ) : (
        <Table>
          <thead>
            <tr className="border-b border-[var(--color-line)]">
              <Th>Name</Th>
              <Th align="right">Read/s</Th>
              <Th align="right">Write/s</Th>
              <Th align="right">Read</Th>
              <Th align="right">Write</Th>
              <Th align="right">I/O time</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[var(--color-line-soft)]">
            {devices.map((dev, idx) => (
              <tr key={dev.name} className={rowClass(idx)}>
                <Td className="font-mono text-xs font-medium">{dev.name}</Td>
                <Td align="right" className="font-mono tabular-nums">
                  {dev.read_count_per_sec?.toFixed(0) ?? "0"}/s
                </Td>
                <Td align="right" className="font-mono tabular-nums">
                  {dev.write_count_per_sec?.toFixed(0) ?? "0"}/s
                </Td>
                <Td align="right" className="font-mono text-[var(--color-signal)] tabular-nums">
                  {formatBytesPerSec(dev.read_bytes_per_sec ?? 0)}
                </Td>
                <Td align="right" className="font-mono text-[var(--color-warn)] tabular-nums">
                  {formatBytesPerSec(dev.write_bytes_per_sec ?? 0)}
                </Td>
                <Td align="right" className="font-mono tabular-nums">
                  {dev.io_time_ms?.toFixed(0) ?? "0"} ms
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Panel>
  );
}

// ─── TCP Connections Section ───────────────────────────────────────────────────

function ConnectionsSection({ data }: { data: ConnectionsData }) {
  const summary = data.summary ?? {};
  const byPort = useMemo(
    () =>
      [...(data.by_port ?? [])]
        .sort((a, b) => b.established_count - a.established_count)
        .slice(0, 20),
    [data.by_port],
  );

  const established = summary["established"] ?? 0;
  const timeWait = summary["time_wait"] ?? 0;
  const closeWait = summary["close_wait"] ?? 0;
  const listen = summary["listen"] ?? 0;
  const total = data.total ?? 0;

  return (
    <Panel>
      <PanelHeader
        icon={<Network className="h-4 w-4" />}
        title="TCP connections"
        badge={
          <span className="text-xs font-normal text-[var(--color-ink-faint)] tabular-nums">
            {total} total
          </span>
        }
      />

      {total === 0 ? (
        <EmptyState
          icon={Network}
          title="No connection data"
          description="No connection data reported yet. Make sure the connections collector is enabled."
        />
      ) : (
        <PanelBody className="space-y-5">
          {/* Summary chips */}
          <div className="flex flex-wrap gap-4 text-sm">
            <span className="text-[var(--color-ink-muted)]">
              Established{" "}
              <span className="font-mono font-medium text-[var(--color-ok)] tabular-nums">
                {established}
              </span>
            </span>
            <span className="text-[var(--color-ink-muted)]">
              Time-wait{" "}
              <span className="font-mono font-medium text-[var(--color-warn)] tabular-nums">
                {timeWait}
              </span>
            </span>
            <span className="text-[var(--color-ink-muted)]">
              Close-wait{" "}
              <span
                className={`font-mono font-medium tabular-nums ${
                  closeWait > 10 ? "text-[var(--color-critical)]" : "text-[var(--color-ink)]"
                }`}
              >
                {closeWait}
              </span>
            </span>
            <span className="text-[var(--color-ink-muted)]">
              Listen{" "}
              <span className="font-mono font-medium text-[var(--color-ink)] tabular-nums">
                {listen}
              </span>
            </span>
          </div>

          {/* Top ports table */}
          {byPort.length > 0 && (
            <Table>
              <thead>
                <tr className="border-b border-[var(--color-line)]">
                  <Th>Port</Th>
                  <Th align="right">Established connections</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {byPort.map((p, idx) => (
                  <tr key={`${p.port}-${idx}`} className={rowClass(idx)}>
                    <Td className="font-mono font-medium tabular-nums">{p.port}</Td>
                    <Td align="right" className="font-mono text-[var(--color-ok)] tabular-nums">
                      {p.established_count}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </PanelBody>
      )}
    </Panel>
  );
}

// ─── Services Section ─────────────────────────────────────────────────────────

function serviceStatus(state: string): "ok" | "critical" | "offline" {
  const normalized = (state ?? "").toLowerCase();
  if (normalized === "running" || normalized === "active") return "ok";
  if (normalized === "failed") return "critical";
  return "offline";
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
    <Panel>
      <PanelHeader
        icon={<Activity className="h-4 w-4" />}
        title="Services"
        badge={
          <span className="flex items-center gap-2 text-xs font-normal">
            <span className="text-[var(--color-ok)]">{data.running ?? 0} running</span>
            {(data.failed ?? 0) > 0 && (
              <span className="text-[var(--color-critical)]">{data.failed} failed</span>
            )}
          </span>
        }
        actions={
          <div className="relative">
            <Search
              className="absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2 text-[var(--color-ink-faint)]"
              aria-hidden="true"
            />
            <Input
              type="search"
              placeholder="Search services…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              aria-label="Search services"
              className="w-full py-1.5 pl-8 text-xs sm:w-56"
            />
          </div>
        }
      />

      {sorted.length === 0 ? (
        <EmptyState
          icon={Activity}
          title="No service data"
          description="No service data reported yet. Make sure the systemd collector is enabled."
        />
      ) : filtered.length === 0 ? (
        <PanelBody>
          <p className="text-center text-sm text-[var(--color-ink-muted)]">
            No services match &ldquo;{search}&rdquo;.
          </p>
        </PanelBody>
      ) : (
        <Table>
          <thead>
            <tr className="border-b border-[var(--color-line)]">
              <Th>Name</Th>
              <Th>State</Th>
              <Th>Description</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[var(--color-line-soft)]">
            {filtered.map((svc, idx) => (
              <tr key={svc.name} className={rowClass(idx)}>
                <Td className="max-w-[240px] font-mono text-xs font-medium">
                  <span className="block truncate" title={svc.name}>
                    {svc.name}
                  </span>
                </Td>
                <Td>
                  <StatusIndicator
                    status={serviceStatus(svc.sub ?? svc.active ?? "")}
                    label={svc.sub ?? svc.active}
                  />
                </Td>
                <Td className="max-w-[400px] text-[var(--color-ink-muted)]">
                  <span className="block truncate" title={svc.description}>
                    {svc.description || "—"}
                  </span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Panel>
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

  const fetchAll = useCallback(
    async (showLoading = false) => {
      if (showLoading) setLoading(true);
      try {
        const [diskioRes, connectionsRes, servicesRes] = await Promise.all([
          apiFetch(`/api/custom/agents/${agentId}/diskio`),
          apiFetch(`/api/custom/agents/${agentId}/connections`),
          apiFetch(`/api/custom/agents/${agentId}/services`),
        ]);

        const [diskioJson, connectionsJson, servicesJson] = await Promise.all([
          diskioRes.ok
            ? (diskioRes.json() as Promise<DiskIOData>)
            : Promise.resolve({ devices: [] }),
          connectionsRes.ok
            ? (connectionsRes.json() as Promise<ConnectionsData>)
            : Promise.resolve({ summary: {}, total: 0, by_port: [] }),
          servicesRes.ok
            ? (servicesRes.json() as Promise<ServicesData>)
            : Promise.resolve({ services: [], total: 0, running: 0, failed: 0, other: 0 }),
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
    },
    [agentId],
  );

  useEffect(() => {
    void fetchAll(true);
    intervalRef.current = setInterval(() => fetchAll(false), REFRESH_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchAll]);

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <ErrorState
          title="Couldn't load system data"
          description="Could not fetch system metrics from this agent. Check that the agent is online."
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <DiskIOSection data={diskio ?? { devices: [] }} />
      <ConnectionsSection data={connections ?? { summary: {}, total: 0, by_port: [] }} />
      <ServicesSection
        data={services ?? { services: [], total: 0, running: 0, failed: 0, other: 0 }}
      />
    </div>
  );
}
