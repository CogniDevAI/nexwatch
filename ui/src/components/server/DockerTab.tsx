import { useEffect, useState } from "react";
import { Container, Package } from "lucide-react";
import pb from "@/lib/pocketbase";
import type { DockerContainer } from "@/types";

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

function StatusBadge({
  status,
}: {
  status: DockerContainer["status"];
}) {
  const styles: Record<string, string> = {
    running:
      "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]",
    stopped: "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]",
    exited: "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]",
    paused:
      "bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]",
    restarting:
      "bg-[var(--color-accent-yellow)]/10 text-[var(--color-accent-yellow)]",
    removing:
      "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]",
    dead: "bg-[var(--color-accent-red)]/10 text-[var(--color-accent-red)]",
  };

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${styles[status] ?? styles.stopped}`}
    >
      {status}
    </span>
  );
}

export function DockerTab({ agentId }: DockerTabProps) {
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [loading, setLoading] = useState(true);

  // Fetch containers
  useEffect(() => {
    async function fetch() {
      try {
        const records = await pb
          .collection("docker_containers")
          .getFullList<DockerContainer>({
            filter: `agent = "${agentId}"`,
            sort: "name",
          });
        setContainers(records);
      } catch {
        // Collection might not exist yet — that's fine
        setContainers([]);
      } finally {
        setLoading(false);
      }
    }

    fetch();
  }, [agentId]);

  // Real-time subscription
  useEffect(() => {
    const unsubPromise = pb
      .collection("docker_containers")
      .subscribe<DockerContainer>("*", (event) => {
        // Only handle events for this agent
        if (event.record.agent !== agentId) return;

        setContainers((prev) => {
          switch (event.action) {
            case "create":
              return [...prev, event.record].sort((a, b) =>
                a.name.localeCompare(b.name),
              );
            case "update":
              return prev.map((c) =>
                c.id === event.record.id ? event.record : c,
              );
            case "delete":
              return prev.filter((c) => c.id !== event.record.id);
            default:
              return prev;
          }
        });
      });

    return () => {
      unsubPromise.then((unsub) => unsub());
    };
  }, [agentId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Container className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
        <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
          Loading containers...
        </span>
      </div>
    );
  }

  if (containers.length === 0) {
    return (
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-10 text-center">
        <Package className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
        <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
          No containers found
        </h3>
        <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto">
          This agent is not reporting any Docker containers. Make sure the Docker
          collector is enabled and the Docker socket is accessible.
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border-default)]">
              <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                Name
              </th>
              <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                Image
              </th>
              <th className="text-left px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                Status
              </th>
              <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                CPU %
              </th>
              <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                Memory
              </th>
              <th className="text-right px-5 py-3 text-xs font-semibold text-[var(--color-text-secondary)] uppercase tracking-wider">
                Network I/O
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[var(--color-border-muted)]">
            {containers.map((c) => (
              <tr
                key={c.id}
                className="hover:bg-[var(--color-bg-elevated)] transition-colors"
              >
                <td className="px-5 py-3 font-medium text-[var(--color-text-primary)]">
                  <div className="flex items-center gap-2">
                    <Container className="w-3.5 h-3.5 text-[var(--color-accent-cyan)]" />
                    <span className="truncate max-w-[200px]">{c.name}</span>
                  </div>
                </td>
                <td className="px-5 py-3 text-[var(--color-text-secondary)]">
                  <span className="truncate max-w-[200px] block">
                    {c.image}
                  </span>
                </td>
                <td className="px-5 py-3">
                  <StatusBadge status={c.status} />
                </td>
                <td className="px-5 py-3 text-right text-[var(--color-text-primary)] tabular-nums">
                  {c.cpu.toFixed(1)}%
                </td>
                <td className="px-5 py-3 text-right text-[var(--color-text-primary)] tabular-nums">
                  {formatBytes(c.memory_used)}
                  <span className="text-[var(--color-text-muted)]">
                    {" "}
                    / {formatBytes(c.memory_limit)}
                  </span>
                </td>
                <td className="px-5 py-3 text-right text-[var(--color-text-secondary)] tabular-nums">
                  <span className="text-[var(--color-accent-green)]">
                    {formatBytes(c.network_rx)}
                  </span>
                  <span className="text-[var(--color-text-muted)]"> / </span>
                  <span className="text-[var(--color-accent-purple)]">
                    {formatBytes(c.network_tx)}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
