import { useEffect, useState } from "react";
import { Container, Package, Play, Square, RotateCw, ArrowUpCircle } from "lucide-react";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import type { DockerAction, DockerContainer } from "@/types";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator, type Status } from "@/components/ui/StatusIndicator";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Toggle } from "@/components/ui/Toggle";
import { useToast } from "@/components/ui/toastContext";
import { useAuthStore } from "@/stores/authStore";

interface DockerTabProps {
  agentId: string;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const idx = Math.min(i, units.length - 1);
  return `${(bytes / Math.pow(1024, idx)).toFixed(1)} ${units[idx]}`;
}

/** Short, human-scannable form of a "sha256:<hex>" digest, e.g. "a1b2c3d4e5f6". */
function shortDigest(digest: string): string {
  const hex = digest.startsWith("sha256:") ? digest.slice("sha256:".length) : digest;
  return hex.slice(0, 12);
}

const STATUS_MAP: Record<DockerContainer["status"], Status> = {
  running: "ok",
  stopped: "critical",
  exited: "critical",
  dead: "critical",
  paused: "warning",
  restarting: "warning",
  removing: "offline",
};

const ACTION_LABEL: Record<DockerAction, string> = {
  start: "Start",
  stop: "Stop",
  restart: "Restart",
};

const ACTION_VERB_PAST: Record<DockerAction, string> = {
  start: "started",
  stop: "stopped",
  restart: "restarted",
};

/** Which actions make sense to offer for a container's current status —
 *  offering "start" on an already-running container (or "stop"/"restart"
 *  on one that's already stopped) is just confusing, not useful. */
function availableActions(status: DockerContainer["status"]): DockerAction[] {
  return status === "running" ? ["stop", "restart"] : ["start"];
}

interface PendingConfirm {
  container: DockerContainer;
  action: DockerAction;
}

export function DockerTab({ agentId }: DockerTabProps) {
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const [onlyUpdates, setOnlyUpdates] = useState(false);
  const [confirm, setConfirm] = useState<PendingConfirm | null>(null);
  const [pendingContainerId, setPendingContainerId] = useState<string | null>(null);

  const hasRole = useAuthStore((s) => s.hasRole);
  const canManage = hasRole("operator");
  const { showToast } = useToast();

  // Fetch containers
  useEffect(() => {
    let cancelled = false;

    async function fetchContainers() {
      setLoading(true);
      setLoadError(null);
      try {
        const records = await pb.collection("docker_containers").getFullList<DockerContainer>({
          filter: `agent_id = "${agentId}"`,
          sort: "name",
        });
        if (!cancelled) setContainers(records);
      } catch (err) {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : "Failed to load containers");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void fetchContainers();
    return () => {
      cancelled = true;
    };
  }, [agentId, reloadToken]);

  // Real-time subscription
  useEffect(() => {
    const unsubPromise = pb
      .collection("docker_containers")
      .subscribe<DockerContainer>("*", (event) => {
        // Only handle events for this agent
        if (event.record.agent_id !== agentId) return;

        setContainers((prev) => {
          switch (event.action) {
            case "create":
              return [...prev, event.record].sort((a, b) => a.name.localeCompare(b.name));
            case "update":
              return prev.map((c) => (c.id === event.record.id ? event.record : c));
            case "delete":
              return prev.filter((c) => c.id !== event.record.id);
            default:
              return prev;
          }
        });
      });

    return () => {
      void unsubPromise.then((unsub) => unsub());
    };
  }, [agentId]);

  async function runAction(container: DockerContainer, action: DockerAction) {
    setConfirm(null);
    setPendingContainerId(container.id);
    try {
      const response = await apiFetch(
        `/api/custom/agents/${agentId}/docker/${container.container_id}/${action}`,
        { method: "POST" },
      );
      const data: { ok?: boolean; state?: string; error?: string } = await response.json();
      if (response.ok && data.ok) {
        showToast(`${container.name} ${ACTION_VERB_PAST[action]}`, "success");
      } else {
        showToast(data.error ?? `Failed to ${action} ${container.name}`, "error");
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Network error", "error");
    } finally {
      setPendingContainerId(null);
    }
  }

  if (loading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <ErrorState
          title="Couldn't load containers"
          description={loadError}
          action={
            <Button variant="secondary" size="sm" onClick={() => setReloadToken((n) => n + 1)}>
              Try again
            </Button>
          }
        />
      </div>
    );
  }

  if (containers.length === 0) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)]">
        <EmptyState
          icon={Package}
          title="No containers found"
          description="This agent is not reporting any Docker containers. Make sure the Docker collector is enabled and the Docker socket is accessible."
        />
      </div>
    );
  }

  const updatesAvailableCount = containers.filter((c) => c.update_available).length;
  const visibleContainers = onlyUpdates ? containers.filter((c) => c.update_available) : containers;

  return (
    <div className="space-y-3">
      {updatesAvailableCount > 0 && (
        <div className="flex items-center gap-2.5 text-sm text-[var(--color-ink-muted)]">
          <Toggle
            checked={onlyUpdates}
            onChange={setOnlyUpdates}
            label="Show only containers with an update available"
          />
          <span>Updates available ({updatesAvailableCount})</span>
        </div>
      )}

      {visibleContainers.length === 0 ? (
        <p className="p-6 text-center text-sm text-[var(--color-ink-muted)]">
          No containers match the current filter.
        </p>
      ) : (
        <Table>
          <thead>
            <tr className="border-b border-[var(--color-line)]">
              <Th>Name</Th>
              <Th>Image</Th>
              <Th>Status</Th>
              <Th align="right">CPU</Th>
              <Th align="right">Memory</Th>
              <Th align="right">Network I/O</Th>
              {canManage && <Th align="right">Actions</Th>}
            </tr>
          </thead>
          <tbody className="divide-y divide-[var(--color-line-soft)]">
            {visibleContainers.map((c, idx) => {
              const isPending = pendingContainerId === c.id;
              return (
                <tr key={c.id} className={rowClass(idx)}>
                  <Td className="font-medium">
                    <div className="flex items-center gap-2">
                      <Container
                        className="h-3.5 w-3.5 flex-shrink-0 text-[var(--color-ink-faint)]"
                        aria-hidden="true"
                      />
                      <span className="max-w-[200px] truncate">{c.name}</span>
                    </div>
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">
                    <div className="flex items-center gap-2">
                      <span className="block max-w-[180px] truncate">{c.image}</span>
                      {c.update_available && (
                        <span
                          title={
                            c.remote_digest
                              ? `Registry digest: ${shortDigest(c.remote_digest)}`
                              : "A newer image is available in the registry"
                          }
                          className="text-2xs inline-flex flex-shrink-0 items-center gap-1 rounded-full border border-[var(--color-warn)]/40 bg-[var(--color-warn)]/10 px-1.5 py-0.5 font-medium text-[var(--color-warn)]"
                        >
                          <ArrowUpCircle className="h-3 w-3" aria-hidden="true" />
                          Update available
                        </span>
                      )}
                    </div>
                  </Td>
                  <Td>
                    <StatusIndicator status={STATUS_MAP[c.status] ?? "critical"} label={c.status} />
                  </Td>
                  <Td align="right" className="font-mono tabular-nums">
                    {c.cpu_percent.toFixed(1)}%
                  </Td>
                  <Td align="right" className="font-mono tabular-nums">
                    {formatBytes(c.memory_usage)}
                    <span className="text-[var(--color-ink-faint)]">
                      {" "}
                      / {formatBytes(c.memory_limit)}
                    </span>
                  </Td>
                  <Td
                    align="right"
                    className="font-mono text-[var(--color-ink-muted)] tabular-nums"
                  >
                    <span className="text-[var(--color-ok)]">{formatBytes(c.network_rx)}</span>
                    <span className="text-[var(--color-ink-faint)]"> / </span>
                    <span className="text-[var(--color-signal)]">{formatBytes(c.network_tx)}</span>
                  </Td>
                  {canManage && (
                    <Td align="right">
                      <div className="flex items-center justify-end gap-1">
                        {availableActions(c.status).map((action) => {
                          const Icon =
                            action === "start" ? Play : action === "stop" ? Square : RotateCw;
                          return (
                            <Button
                              key={action}
                              size="sm"
                              variant="ghost"
                              disabled={isPending}
                              onClick={() => setConfirm({ container: c, action })}
                            >
                              <Icon
                                className={`h-3.5 w-3.5 ${isPending ? "animate-pulse" : ""}`}
                                aria-hidden="true"
                              />
                              <span>{isPending ? "Working…" : ACTION_LABEL[action]}</span>
                            </Button>
                          );
                        })}
                      </div>
                    </Td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}

      {confirm && (
        <Modal title={`${ACTION_LABEL[confirm.action]} container`} onClose={() => setConfirm(null)}>
          <div className="space-y-4 p-6">
            <p className="text-sm text-[var(--color-ink-muted)]">
              {ACTION_LABEL[confirm.action]}{" "}
              <strong className="text-[var(--color-ink)]">{confirm.container.name}</strong>? This
              sends the action to the agent immediately.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirm(null)}>
                Cancel
              </Button>
              <Button
                variant={confirm.action === "stop" ? "danger" : "primary"}
                onClick={() => void runAction(confirm.container, confirm.action)}
              >
                {ACTION_LABEL[confirm.action]}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
