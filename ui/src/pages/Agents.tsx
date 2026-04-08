import { useState, useEffect, useCallback } from "react";
import {
  Plus,
  Trash2,
  Copy,
  Check,
  X,
  Server,
  Wifi,
  WifiOff,
  Activity,
} from "lucide-react";
import type { Agent } from "@/types";
import pb from "@/lib/pocketbase";
import { agentStatus } from "@/lib/agent";

function generateToken(length = 32): string {
  const chars =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return Array.from(array, (b) => chars[b % chars.length]).join("");
}

function formatDate(dateStr: string): string {
  if (!dateStr) return "Never";
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

function timeSince(dateStr: string): string {
  if (!dateStr) return "Never";
  const seconds = Math.floor(
    (Date.now() - new Date(dateStr).getTime()) / 1000,
  );
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

export function Agents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [newAgent, setNewAgent] = useState<{
    token: string;
    id: string;
  } | null>(null);
  const [addingName, setAddingName] = useState("");
  const [creating, setCreating] = useState(false);
  const [copied, setCopied] = useState(false);

  const fetchAgents = useCallback(async () => {
    setLoading(true);
    try {
      const records = await pb.collection("agents").getFullList<Agent>({
        sort: "-last_seen",
      });
      setAgents(records);
    } catch {
      // Handle silently.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  // Real-time subscription for live status updates.
  useEffect(() => {
    const unsubPromise = pb
      .collection("agents")
      .subscribe<Agent>("*", (event) => {
        switch (event.action) {
          case "create":
            setAgents((prev) => [event.record, ...prev]);
            break;
          case "update":
            setAgents((prev) =>
              prev.map((a) =>
                a.id === event.record.id ? event.record : a,
              ),
            );
            break;
          case "delete":
            setAgents((prev) =>
              prev.filter((a) => a.id !== event.record.id),
            );
            break;
        }
      });

    return () => {
      unsubPromise.then((unsub) => unsub());
    };
  }, []);

  const handleAddAgent = async () => {
    if (!addingName.trim()) return;
    setCreating(true);
    try {
      const token = generateToken();
      const record = await pb.collection("agents").create<Agent>({
        name: addingName.trim(),
        hostname: addingName.trim(),
        os: "",
        ip: "",
        version: "",
        status: "offline",
        token,
        last_seen: "",
      });
      setNewAgent({ token, id: record.id });
    } catch {
      // Handle silently.
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("agents").delete(id);
      setAgents((prev) => prev.filter((a) => a.id !== id));
      setDeleteConfirm(null);
    } catch {
      // Handle silently.
    }
  };

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback: select text.
    }
  };

  const handleCloseAddModal = () => {
    setShowAddModal(false);
    setNewAgent(null);
    setAddingName("");
    setCopied(false);
  };

  // Build hub WebSocket URL from current page location.
  // http → ws, https → wss
  const hubWsUrl = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/ws/agent`;

  const installCommand = newAgent
    ? `curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- --hub ${hubWsUrl} --token ${newAgent.token}`
    : "";

  const installCommandOracle = newAgent
    ? `curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- --hub ${hubWsUrl} --token ${newAgent.token} --mode oracle --oracle-home /u01/app/oracle/product/19.3.0/dbhome1 --oracle-sid SIDNAME`
    : "";

  const onlineCount = agents.filter((a) => agentStatus(a) === "online").length;
  const offlineCount = agents.filter((a) => agentStatus(a) === "offline").length;

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold">Agents</h2>
        <button
          onClick={() => setShowAddModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity"
        >
          <Plus className="w-4 h-4" />
          Add Agent
        </button>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4">
          <div className="flex items-center gap-3">
            <div
              className="w-9 h-9 rounded-lg flex items-center justify-center"
              style={{ backgroundColor: "var(--color-accent-cyan)15" }}
            >
              <Server className="w-4 h-4 text-[var(--color-accent-cyan)]" />
            </div>
            <div>
              <p className="text-xl font-bold text-[var(--color-text-primary)]">
                {agents.length}
              </p>
              <p className="text-xs text-[var(--color-text-secondary)]">
                Total Agents
              </p>
            </div>
          </div>
        </div>
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4">
          <div className="flex items-center gap-3">
            <div
              className="w-9 h-9 rounded-lg flex items-center justify-center"
              style={{ backgroundColor: "var(--color-accent-green)15" }}
            >
              <Wifi className="w-4 h-4 text-[var(--color-accent-green)]" />
            </div>
            <div>
              <p className="text-xl font-bold text-[var(--color-text-primary)]">
                {onlineCount}
              </p>
              <p className="text-xs text-[var(--color-text-secondary)]">
                Online
              </p>
            </div>
          </div>
        </div>
        <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4">
          <div className="flex items-center gap-3">
            <div
              className="w-9 h-9 rounded-lg flex items-center justify-center"
              style={{ backgroundColor: "var(--color-accent-red)15" }}
            >
              <WifiOff className="w-4 h-4 text-[var(--color-accent-red)]" />
            </div>
            <div>
              <p className="text-xl font-bold text-[var(--color-text-primary)]">
                {offlineCount}
              </p>
              <p className="text-xs text-[var(--color-text-secondary)]">
                Offline
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* Agents Table */}
      <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] overflow-hidden">
        {loading ? (
          <div className="p-6 flex items-center justify-center">
            <Activity className="w-5 h-5 text-[var(--color-accent-cyan)] animate-pulse" />
            <span className="ml-3 text-sm text-[var(--color-text-secondary)]">
              Loading agents...
            </span>
          </div>
        ) : agents.length === 0 ? (
          <div className="p-10 text-center">
            <Server className="w-12 h-12 text-[var(--color-text-muted)] mx-auto mb-4" />
            <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
              No agents registered
            </h3>
            <p className="text-sm text-[var(--color-text-secondary)] max-w-md mx-auto mb-4">
              Add your first agent to start monitoring. Click "Add Agent" to
              generate an install command.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border-default)] text-[var(--color-text-muted)]">
                  <th className="px-6 py-3 text-left font-medium">Status</th>
                  <th className="px-6 py-3 text-left font-medium">
                    Hostname
                  </th>
                  <th className="px-6 py-3 text-left font-medium">IP</th>
                  <th className="px-6 py-3 text-left font-medium">OS</th>
                  <th className="px-6 py-3 text-left font-medium">Version</th>
                  <th className="px-6 py-3 text-left font-medium">
                    Last Seen
                  </th>
                  <th className="px-6 py-3 text-right font-medium">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {agents.map((agent) => (
                  <tr
                    key={agent.id}
                    className="border-b border-[var(--color-border-muted)] hover:bg-[var(--color-bg-elevated)]/50"
                  >
                    <td className="px-6 py-3">
                      <span
                        className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium ${
                          agentStatus(agent) === "online"
                            ? "bg-[var(--color-accent-green)]/10 text-[var(--color-accent-green)]"
                            : "bg-[var(--color-text-muted)]/10 text-[var(--color-text-muted)]"
                        }`}
                      >
                        <span
                          className={`w-1.5 h-1.5 rounded-full ${
                            agentStatus(agent) === "online"
                              ? "bg-[var(--color-accent-green)]"
                              : "bg-[var(--color-text-muted)]"
                          }`}
                        />
                        {agentStatus(agent)}
                      </span>
                    </td>
                    <td className="px-6 py-3 font-medium text-[var(--color-text-primary)]">
                      {agent.hostname || agent.name || "Pending..."}
                    </td>
                    <td className="px-6 py-3 text-[var(--color-text-secondary)] font-mono text-xs">
                      {agent.ip || "—"}
                    </td>
                    <td className="px-6 py-3 text-[var(--color-text-secondary)]">
                      {agent.os || "—"}
                    </td>
                    <td className="px-6 py-3">
                      {agent.version ? (
                        <span className="px-2 py-0.5 rounded bg-[var(--color-bg-elevated)] text-[var(--color-text-secondary)] text-xs font-mono">
                          {agent.version}
                        </span>
                      ) : (
                        <span className="text-[var(--color-text-muted)]">
                          —
                        </span>
                      )}
                    </td>
                    <td className="px-6 py-3 text-[var(--color-text-muted)] whitespace-nowrap">
                      <span title={formatDate(agent.last_seen)}>
                        {timeSince(agent.last_seen)}
                      </span>
                    </td>
                    <td className="px-6 py-3 text-right">
                      {deleteConfirm === agent.id ? (
                        <div className="flex items-center justify-end gap-1">
                          <button
                            onClick={() => handleDelete(agent.id)}
                            className="px-2 py-1 text-xs rounded bg-[var(--color-accent-red)] text-white"
                          >
                            Confirm
                          </button>
                          <button
                            onClick={() => setDeleteConfirm(null)}
                            className="px-2 py-1 text-xs rounded text-[var(--color-text-muted)] hover:bg-[var(--color-bg-elevated)]"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <button
                          onClick={() => setDeleteConfirm(agent.id)}
                          className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-accent-red)] hover:bg-[var(--color-accent-red)]/10"
                          title="Remove agent"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Add Agent Modal */}
      {showAddModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-lg mx-4 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] shadow-2xl">
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-[var(--color-border-default)]">
              <h3 className="text-lg font-semibold">
                {newAgent ? "Agent Created" : "Add New Agent"}
              </h3>
              <button
                onClick={handleCloseAddModal}
                className="p-1.5 rounded-lg text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)]"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6">
              {!newAgent ? (
                /* Step 1: Enter agent name */
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium text-[var(--color-text-secondary)] mb-1">
                      Agent Name
                    </label>
                    <input
                      type="text"
                      value={addingName}
                      onChange={(e) => setAddingName(e.target.value)}
                      placeholder="e.g., production-web-01"
                      className="w-full px-3 py-2 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-sm focus:outline-none focus:border-[var(--color-accent-cyan)]"
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleAddAgent();
                      }}
                    />
                  </div>
                  <div className="flex justify-end gap-3">
                    <button
                      onClick={handleCloseAddModal}
                      className="px-4 py-2 text-sm font-medium text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] rounded-lg hover:bg-[var(--color-bg-elevated)]"
                    >
                      Cancel
                    </button>
                    <button
                      onClick={handleAddAgent}
                      disabled={!addingName.trim() || creating}
                      className="px-4 py-2 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
                    >
                      {creating ? "Creating..." : "Generate Token"}
                    </button>
                  </div>
                </div>
              ) : (
                /* Step 2: Show install command */
                <div className="space-y-4">
                  <p className="text-sm text-[var(--color-accent-green)]">
                    Agent token generated successfully. Copy the command for your server type:
                  </p>

                  {/* Standard */}
                  <div>
                    <p className="text-xs font-semibold text-[var(--color-text-secondary)] mb-1.5">Standard (Linux)</p>
                    <div className="relative">
                      <pre className="p-4 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all">
                        {installCommand}
                      </pre>
                      <button
                        onClick={() => handleCopy(installCommand)}
                        className="absolute top-2 right-2 p-1.5 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] text-[var(--color-text-muted)] hover:text-[var(--color-accent-cyan)] transition-colors"
                        title="Copy"
                      >
                        {copied ? <Check className="w-4 h-4 text-[var(--color-accent-green)]" /> : <Copy className="w-4 h-4" />}
                      </button>
                    </div>
                  </div>

                  {/* Oracle */}
                  <div>
                    <p className="text-xs font-semibold text-[var(--color-text-secondary)] mb-1.5">Oracle DB (edit <code className="text-[var(--color-accent-yellow)]">--oracle-sid</code>)</p>
                    <div className="relative">
                      <pre className="p-4 rounded-lg bg-[var(--color-bg-primary)] border border-[var(--color-border-default)] text-[var(--color-text-primary)] text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all">
                        {installCommandOracle}
                      </pre>
                      <button
                        onClick={() => handleCopy(installCommandOracle)}
                        className="absolute top-2 right-2 p-1.5 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] text-[var(--color-text-muted)] hover:text-[var(--color-accent-cyan)] transition-colors"
                        title="Copy"
                      >
                        <Copy className="w-4 h-4" />
                      </button>
                    </div>
                  </div>

                  <div className="rounded-lg bg-[var(--color-bg-elevated)] border border-[var(--color-border-muted)] p-3">
                    <p className="text-xs text-[var(--color-text-muted)]">
                      This token will only be shown once.
                    </p>
                  </div>

                  <div className="flex justify-end">
                    <button
                      onClick={handleCloseAddModal}
                      className="px-4 py-2 bg-[var(--color-accent-cyan)] text-[var(--color-bg-primary)] text-sm font-medium rounded-lg hover:opacity-90 transition-opacity"
                    >
                      Done
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
