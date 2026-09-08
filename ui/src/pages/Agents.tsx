import { useState, useMemo } from "react";
import {
  Plus,
  Trash2,
  RefreshCw,
  Copy,
  Check,
  Server,
  Tag as TagIcon,
  ArrowUpCircle,
} from "lucide-react";
import type { Agent } from "@/types";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { timeSince, formatDateTime } from "@/lib/time";
import { updateAvailable } from "@/lib/agentUpdates";
import { useAuthStore } from "@/stores/authStore";
import { useAgentStore } from "@/stores/agentStore";
import { useFleetHealth } from "@/hooks/useFleetHealth";
import { useAgentUpdateInfo } from "@/hooks/useAgentUpdateInfo";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel, PanelBody } from "@/components/ui/Panel";
import { FleetStrip } from "@/components/ui/FleetStrip";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { PlatformIcon } from "@/components/ui/PlatformIcon";
import { Tabs } from "@/components/ui/Tabs";
import { Modal } from "@/components/ui/Modal";
import { Input, Label } from "@/components/ui/Field";
import { TagInput } from "@/components/ui/TagInput";
import { TagChips, TagFilterBar } from "@/components/ui/TagChips";
import { Button, IconButton } from "@/components/ui/Button";
import { Menu } from "@/components/ui/Menu";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { usePageTitle } from "@/hooks/usePageTitle";
import { UpdateAvailableBadge, UpdateStatusChip } from "@/components/agents/UpdateBadges";
import { UpdateAgentModal } from "@/components/agents/UpdateAgentModal";
import { UpdateAllModal } from "@/components/agents/UpdateAllModal";
import type { TabItem } from "@/components/ui/Tabs";

type InstallOS = "linux" | "windows";

// Thin per-OS wrappers so PlatformIcon (which takes a "platform" prop, not
// just "className") fits Tabs' generic `icon: ComponentType<{className}>`
// shape.
function LinuxTabIcon({ className }: { className?: string }) {
  return <PlatformIcon platform="linux" className={className} />;
}
function WindowsTabIcon({ className }: { className?: string }) {
  return <PlatformIcon platform="windows" className={className} />;
}

const OS_TABS: TabItem<InstallOS>[] = [
  { key: "linux", label: "Linux", icon: LinuxTabIcon },
  { key: "windows", label: "Windows", icon: WindowsTabIcon },
];

interface AgentActionsProps {
  agent: Agent;
  canManage: boolean;
  compareVersion: string;
  deleteConfirm: string | null;
  regeneratingId: string | null;
  onUpdate: (agent: Agent) => void;
  onEditTags: (agent: Agent) => void;
  onRegenerateToken: (agentId: string) => void;
  onDeleteRequest: (agentId: string) => void;
  onDeleteConfirm: (agentId: string) => void;
  onDeleteCancel: () => void;
}

/**
 * Row actions shared by the Agents desktop table cell and mobile card
 * footer. Defined at module scope (not nested inside `Agents()`, as it
 * originally was) so React keeps the same component identity across
 * re-renders — a component recreated inline on every parent render gets a
 * fresh function-type identity each time, which React treats as a type
 * change and remounts, silently discarding any state owned by a descendant
 * like `Menu`'s open/closed flag. That bug was invisible before the R2
 * overflow-menu fix (the row's only other transient state, `deleteConfirm`,
 * already lived in the parent), but became a real, visible defect — the
 * "More actions" menu would appear to close itself — once `Menu` owned its
 * own local state. See DESIGN.md §7/"Row action overflow".
 *
 * Four labeled ghost buttons don't fit a row at desktop table widths or on
 * a 390px mobile card (R2 fix). Update and Edit tags stay direct, visible
 * actions — they're the ones reached most often — while Regenerate token
 * and Delete move into a "More actions" overflow menu. This one action set
 * is shared by both layouts, so the fix applies to both at once.
 */
function AgentActions({
  agent,
  canManage,
  compareVersion,
  deleteConfirm,
  regeneratingId,
  onUpdate,
  onEditTags,
  onRegenerateToken,
  onDeleteRequest,
  onDeleteConfirm,
  onDeleteCancel,
}: AgentActionsProps) {
  if (!canManage) return <span className="text-xs text-[var(--color-ink-faint)]">—</span>;

  if (deleteConfirm === agent.id) {
    return (
      <div className="flex items-center justify-end gap-1">
        <Button size="sm" variant="danger" onClick={() => onDeleteConfirm(agent.id)}>
          Confirm
        </Button>
        <Button size="sm" variant="ghost" onClick={onDeleteCancel}>
          Cancel
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center justify-end gap-1">
      {updateAvailable(agent, compareVersion) && agent.status === "online" && (
        <Button size="sm" variant="ghost" onClick={() => onUpdate(agent)}>
          <ArrowUpCircle className="h-3.5 w-3.5" />
          <span>Update</span>
        </Button>
      )}
      <Button size="sm" variant="ghost" onClick={() => onEditTags(agent)}>
        <TagIcon className="h-3.5 w-3.5" />
        <span>Edit tags</span>
      </Button>
      <Menu
        aria-label={`More actions for ${agent.hostname || agent.name || "this agent"}`}
        items={[
          {
            label: regeneratingId === agent.id ? "Regenerating…" : "Regenerate token",
            icon: RefreshCw,
            onSelect: () => onRegenerateToken(agent.id),
            disabled: regeneratingId === agent.id,
          },
          {
            label: "Delete",
            icon: Trash2,
            onSelect: () => onDeleteRequest(agent.id),
            danger: true,
          },
        ]}
      />
    </div>
  );
}

export function Agents() {
  usePageTitle("Agents");

  const canManage = useAuthStore((s) => s.hasRole("operator"));
  const canManageAll = useAuthStore((s) => s.hasRole("admin"));
  // Agents are fetched and subscribed once by AppShell — this just reads the
  // shared store, and useFleetHealth derives the same alert-aware status
  // used everywhere else. See DESIGN.md §10 and §3 ("one status source of
  // truth"). Mutations (create/delete/regenerate) still call pb directly;
  // the shared subscription picks up the resulting realtime event.
  const { agents, loading, error, fetchAgents } = useAgentStore();
  const { fleet, statusByAgentId } = useFleetHealth();
  const { compareVersion } = useAgentUpdateInfo();
  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [newAgent, setNewAgent] = useState<{
    token: string;
    id: string;
  } | null>(null);
  const [tokenFlow, setTokenFlow] = useState<"create" | "regenerate">("create");
  const [addingName, setAddingName] = useState("");
  const [creating, setCreating] = useState(false);
  const [regeneratingId, setRegeneratingId] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [installOS, setInstallOS] = useState<InstallOS>("linux");
  const [editingTagsAgent, setEditingTagsAgent] = useState<Agent | null>(null);
  const [tagsDraft, setTagsDraft] = useState<string[]>([]);
  const [savingTags, setSavingTags] = useState(false);
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set());
  const [updatingAgent, setUpdatingAgent] = useState<Agent | null>(null);
  const [showUpdateAllModal, setShowUpdateAllModal] = useState(false);

  const outdatedAgents = useMemo(
    () => agents.filter((a) => a.status === "online" && updateAvailable(a, compareVersion)),
    [agents, compareVersion],
  );

  const allTags = useMemo(
    () => Array.from(new Set(agents.flatMap((a) => a.tags ?? []))).sort(),
    [agents],
  );

  const toggleTag = (tag: string) => {
    setSelectedTags((prev) => {
      const next = new Set(prev);
      if (next.has(tag)) next.delete(tag);
      else next.add(tag);
      return next;
    });
  };

  const filteredAgents = useMemo(
    () =>
      selectedTags.size === 0
        ? agents
        : agents.filter((a) => (a.tags ?? []).some((t) => selectedTags.has(t))),
    [agents, selectedTags],
  );
  const filteredAgentIds = useMemo(
    () => new Set(filteredAgents.map((a) => a.id)),
    [filteredAgents],
  );
  const filteredFleet = useMemo(
    () => fleet.filter((f) => filteredAgentIds.has(f.id)),
    [fleet, filteredAgentIds],
  );

  const handleOpenTagEditor = (agent: Agent) => {
    setEditingTagsAgent(agent);
    setTagsDraft(agent.tags ?? []);
  };

  const handleSaveTags = async () => {
    if (!editingTagsAgent) return;
    setSavingTags(true);
    try {
      await pb.collection("agents").update(editingTagsAgent.id, { tags: tagsDraft });
      setEditingTagsAgent(null);
    } catch {
      // Handle silently — the modal stays open so the operator can retry.
    } finally {
      setSavingTags(false);
    }
  };

  // Requests a new (or rotated) token for an agent from the hub and returns
  // the plaintext value. The hub never returns the token again after this.
  const mintToken = async (agentId: string): Promise<string> => {
    const res = await apiFetch(`/api/custom/agents/${agentId}/token`, {
      method: "POST",
    });
    if (!res.ok) {
      throw new Error("failed to generate token");
    }
    const data = (await res.json()) as { token: string };
    return data.token;
  };

  const handleAddAgent = async () => {
    if (!addingName.trim()) return;
    setCreating(true);
    try {
      const record = await pb.collection("agents").create<Agent>({
        name: addingName.trim(),
        hostname: addingName.trim(),
        os: "",
        ip: "",
        version: "",
        status: "offline",
        last_seen: "",
      });
      const token = await mintToken(record.id);
      setTokenFlow("create");
      setNewAgent({ token, id: record.id });
    } catch {
      // Handle silently.
    } finally {
      setCreating(false);
    }
  };

  const handleRegenerateToken = async (agentId: string) => {
    setRegeneratingId(agentId);
    try {
      const token = await mintToken(agentId);
      setTokenFlow("regenerate");
      setNewAgent({ token, id: agentId });
      setShowAddModal(true);
    } catch {
      // Handle silently.
    } finally {
      setRegeneratingId(null);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("agents").delete(id);
      // No local state to patch — the shared subscription (owned by
      // AppShell) picks up the delete event and updates the store.
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
    setInstallOS("linux");
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

  // Two-step download-then-run (Invoke-WebRequest, then dot-invoke the
  // saved script) rather than a one-line `irm ... | iex` — an agent
  // install needs an elevated (Administrator) PowerShell prompt, and
  // piping straight into iex runs whatever the URL currently serves with
  // no chance to read it first. install-agent.ps1's own header comment
  // documents this same reasoning.
  const installCommandWindows = newAgent
    ? `Invoke-WebRequest -Uri https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.ps1 -OutFile install-agent.ps1\n.\\install-agent.ps1 -Hub ${hubWsUrl} -Token ${newAgent.token}`
    : "";

  return (
    <div>
      <PageHeader
        title="Agents"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {canManageAll && outdatedAgents.length > 0 && (
              <Button variant="secondary" onClick={() => setShowUpdateAllModal(true)}>
                <ArrowUpCircle className="h-4 w-4" aria-hidden="true" />
                Update all outdated ({outdatedAgents.length})
              </Button>
            )}
            {canManage && (
              <Button
                variant="primary"
                onClick={() => {
                  setTokenFlow("create");
                  setShowAddModal(true);
                }}
              >
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add agent
              </Button>
            )}
          </div>
        }
      />

      {/* Fleet health — same hero strip as the Dashboard. Skipped on error or
          once we know there are zero agents, so the message below isn't
          duplicated. Scoped by the tag filter below, same as Dashboard. */}
      {(loading || (!error && agents.length > 0)) && (
        <Panel className="mb-6 p-5">
          {loading && agents.length === 0 ? (
            <Skeleton className="h-9 w-full" />
          ) : (
            <FleetStrip agents={filteredFleet} size="lg" />
          )}
        </Panel>
      )}

      {!loading && !error && agents.length > 0 && (
        <TagFilterBar tags={allTags} selected={selectedTags} onToggle={toggleTag} />
      )}

      {/* Agents Table (desktop) / Cards (mobile) */}
      {loading ? (
        <Panel>
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        </Panel>
      ) : error ? (
        <Panel>
          <ErrorState
            title="Couldn't load agents"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={() => void fetchAgents()}>
                Try again
              </Button>
            }
          />
        </Panel>
      ) : agents.length === 0 ? (
        <Panel>
          <EmptyState
            icon={Server}
            title="No agents registered"
            description="Add your first agent to start monitoring. Choose Add agent to generate an install command."
          />
        </Panel>
      ) : filteredAgents.length === 0 ? (
        <Panel>
          <EmptyState
            icon={Server}
            title="No agents match the selected tags"
            description="Clear a tag filter above to see more agents."
          />
        </Panel>
      ) : (
        <>
          {/* Desktop table */}
          <div className="hidden md:block">
            <Table>
              <thead>
                <tr className="border-b border-[var(--color-line)]">
                  <Th>Status</Th>
                  <Th>Hostname</Th>
                  <Th>Tags</Th>
                  <Th>IP</Th>
                  <Th>OS</Th>
                  <Th className="whitespace-nowrap">Version</Th>
                  <Th className="whitespace-nowrap">Last seen</Th>
                  <Th align="right" className="whitespace-nowrap">
                    Actions
                  </Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {filteredAgents.map((agent, idx) => (
                  <tr key={agent.id} className={rowClass(idx)}>
                    <Td>
                      <StatusIndicator status={statusByAgentId.get(agent.id) ?? "offline"} />
                    </Td>
                    <Td className="font-medium">{agent.hostname || agent.name || "Pending…"}</Td>
                    <Td>
                      {(agent.tags ?? []).length > 0 ? (
                        <TagChips tags={agent.tags ?? []} />
                      ) : (
                        <span className="text-[var(--color-ink-faint)]">—</span>
                      )}
                    </Td>
                    <Td className="font-mono text-xs text-[var(--color-ink-muted)]">
                      {agent.ip || "—"}
                    </Td>
                    <Td className="text-[var(--color-ink-muted)]">
                      {agent.os ? (
                        <span className="flex items-center gap-1.5">
                          <PlatformIcon platform={agent.platform} />
                          {agent.os}
                        </span>
                      ) : (
                        "—"
                      )}
                    </Td>
                    <Td>
                      <div className="flex flex-wrap items-center gap-1.5">
                        {agent.version ? (
                          <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-2 py-0.5 font-mono text-xs text-[var(--color-ink-muted)]">
                            {agent.version}
                          </span>
                        ) : (
                          <span className="text-[var(--color-ink-faint)]">—</span>
                        )}
                        {updateAvailable(agent, compareVersion) && (
                          <UpdateAvailableBadge targetVersion={compareVersion} />
                        )}
                        <UpdateStatusChip
                          status={agent.update_status}
                          error={agent.update_error}
                          onRetry={canManage ? () => setUpdatingAgent(agent) : undefined}
                        />
                      </div>
                    </Td>
                    <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                      <span title={formatDateTime(agent.last_seen)}>
                        {timeSince(agent.last_seen)}
                      </span>
                    </Td>
                    <Td align="right">
                      <AgentActions
                        agent={agent}
                        canManage={canManage}
                        compareVersion={compareVersion}
                        deleteConfirm={deleteConfirm}
                        regeneratingId={regeneratingId}
                        onUpdate={setUpdatingAgent}
                        onEditTags={handleOpenTagEditor}
                        onRegenerateToken={(id) => void handleRegenerateToken(id)}
                        onDeleteRequest={setDeleteConfirm}
                        onDeleteConfirm={(id) => void handleDelete(id)}
                        onDeleteCancel={() => setDeleteConfirm(null)}
                      />
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>

          {/* Mobile cards */}
          <div className="space-y-3 md:hidden">
            {filteredAgents.map((agent) => (
              <Panel key={agent.id}>
                <PanelBody className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <span className="font-medium text-[var(--color-ink)]">
                      {agent.hostname || agent.name || "Pending…"}
                    </span>
                    <StatusIndicator status={statusByAgentId.get(agent.id) ?? "offline"} />
                  </div>
                  {(agent.tags ?? []).length > 0 && <TagChips tags={agent.tags ?? []} />}
                  <dl className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm">
                    <dt className="text-[var(--color-ink-faint)]">IP</dt>
                    <dd className="text-right font-mono text-xs text-[var(--color-ink-muted)]">
                      {agent.ip || "—"}
                    </dd>
                    <dt className="text-[var(--color-ink-faint)]">Version</dt>
                    <dd className="text-right font-mono text-xs text-[var(--color-ink-muted)]">
                      {agent.version || "—"}
                    </dd>
                    <dt className="text-[var(--color-ink-faint)]">Last seen</dt>
                    <dd
                      className="text-right text-xs text-[var(--color-ink-muted)]"
                      title={formatDateTime(agent.last_seen)}
                    >
                      {timeSince(agent.last_seen)}
                    </dd>
                  </dl>
                  {(updateAvailable(agent, compareVersion) || agent.update_status) && (
                    <div className="flex flex-wrap items-center gap-1.5">
                      {updateAvailable(agent, compareVersion) && (
                        <UpdateAvailableBadge targetVersion={compareVersion} />
                      )}
                      <UpdateStatusChip
                        status={agent.update_status}
                        error={agent.update_error}
                        onRetry={canManage ? () => setUpdatingAgent(agent) : undefined}
                      />
                    </div>
                  )}
                  <div className="border-t border-[var(--color-line-soft)] pt-3">
                    <AgentActions
                      agent={agent}
                      canManage={canManage}
                      compareVersion={compareVersion}
                      deleteConfirm={deleteConfirm}
                      regeneratingId={regeneratingId}
                      onUpdate={setUpdatingAgent}
                      onEditTags={handleOpenTagEditor}
                      onRegenerateToken={(id) => void handleRegenerateToken(id)}
                      onDeleteRequest={setDeleteConfirm}
                      onDeleteConfirm={(id) => void handleDelete(id)}
                      onDeleteCancel={() => setDeleteConfirm(null)}
                    />
                  </div>
                </PanelBody>
              </Panel>
            ))}
          </div>
        </>
      )}

      {/* Add Agent / Token Modal */}
      {showAddModal && (
        <Modal
          title={
            newAgent
              ? tokenFlow === "regenerate"
                ? "Token regenerated"
                : "Agent created"
              : "Add new agent"
          }
          onClose={handleCloseAddModal}
        >
          <div className="p-6">
            {!newAgent ? (
              /* Step 1: Enter agent name */
              <div className="space-y-4">
                <div>
                  <Label htmlFor="new-agent-name">Agent name</Label>
                  <Input
                    id="new-agent-name"
                    type="text"
                    value={addingName}
                    onChange={(e) => setAddingName(e.target.value)}
                    placeholder="e.g., production-web-01"
                    onKeyDown={(e) => {
                      if (e.key === "Enter") void handleAddAgent();
                    }}
                  />
                </div>
                <div className="flex justify-end gap-3">
                  <Button variant="ghost" onClick={handleCloseAddModal}>
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    onClick={handleAddAgent}
                    disabled={!addingName.trim() || creating}
                  >
                    {creating ? "Creating…" : "Generate token"}
                  </Button>
                </div>
              </div>
            ) : (
              /* Step 2: Show install command */
              <div className="space-y-4">
                <p className="flex items-center gap-2 text-sm text-[var(--color-ok)]">
                  <Check className="h-4 w-4 flex-shrink-0" aria-hidden="true" />
                  Agent token generated. Copy the command for your server type.
                </p>

                {/* OS tabs — same underline style/shared component as the
                    host detail page's tab bar (DESIGN.md), not pills. */}
                <Tabs
                  items={OS_TABS}
                  activeKey={installOS}
                  onChange={setInstallOS}
                  aria-label="Install command operating system"
                  className="border-b border-[var(--color-line)]"
                />

                {installOS === "linux" ? (
                  <>
                    {/* Standard */}
                    <div>
                      <p className="mb-1.5 text-xs font-semibold text-[var(--color-ink-muted)]">
                        Standard (Linux)
                      </p>
                      <div className="relative">
                        <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-4 font-mono text-xs break-all whitespace-pre-wrap text-[var(--color-ink)]">
                          {installCommand}
                        </pre>
                        <IconButton
                          aria-label="Copy standard install command"
                          onClick={() => handleCopy(installCommand)}
                          className="absolute top-2 right-2 border border-[var(--color-line)] bg-[var(--color-panel)]"
                        >
                          {copied ? (
                            <Check className="h-4 w-4 text-[var(--color-ok)]" />
                          ) : (
                            <Copy className="h-4 w-4" />
                          )}
                        </IconButton>
                      </div>
                    </div>

                    {/* Oracle */}
                    <div>
                      <p className="mb-1.5 text-xs font-semibold text-[var(--color-ink-muted)]">
                        Oracle DB (edit{" "}
                        <code className="text-[var(--color-warn)]">--oracle-sid</code>)
                      </p>
                      <div className="relative">
                        <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-4 font-mono text-xs break-all whitespace-pre-wrap text-[var(--color-ink)]">
                          {installCommandOracle}
                        </pre>
                        <IconButton
                          aria-label="Copy Oracle install command"
                          onClick={() => handleCopy(installCommandOracle)}
                          className="absolute top-2 right-2 border border-[var(--color-line)] bg-[var(--color-panel)]"
                        >
                          <Copy className="h-4 w-4" />
                        </IconButton>
                      </div>
                    </div>
                  </>
                ) : (
                  /* Windows: PowerShell, elevated (Administrator) prompt.
                     Two-step download-then-run rather than `irm | iex` —
                     see installCommandWindows's own comment above. */
                  <div>
                    <p className="mb-1.5 text-xs font-semibold text-[var(--color-ink-muted)]">
                      PowerShell (run as Administrator)
                    </p>
                    <div className="relative">
                      <pre className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] p-4 font-mono text-xs break-all whitespace-pre-wrap text-[var(--color-ink)]">
                        {installCommandWindows}
                      </pre>
                      <IconButton
                        aria-label="Copy Windows install command"
                        onClick={() => handleCopy(installCommandWindows)}
                        className="absolute top-2 right-2 border border-[var(--color-line)] bg-[var(--color-panel)]"
                      >
                        <Copy className="h-4 w-4" />
                      </IconButton>
                    </div>
                  </div>
                )}

                <div className="rounded-[var(--radius-control)] border border-[var(--color-line-soft)] bg-[var(--color-panel-raised)] p-3">
                  <p className="text-xs text-[var(--color-ink-faint)]">
                    This token will only be shown once.
                  </p>
                </div>

                <div className="flex justify-end">
                  <Button variant="primary" onClick={handleCloseAddModal}>
                    Done
                  </Button>
                </div>
              </div>
            )}
          </div>
        </Modal>
      )}

      {/* Edit tags modal */}
      {editingTagsAgent && (
        <Modal
          title={`Edit tags — ${editingTagsAgent.hostname || editingTagsAgent.name}`}
          onClose={() => setEditingTagsAgent(null)}
        >
          <div className="space-y-4 p-6">
            <div>
              <Label htmlFor="agent-tags-input">Tags</Label>
              <TagInput
                id="agent-tags-input"
                value={tagsDraft}
                onChange={setTagsDraft}
                suggestions={allTags}
                placeholder="Add a tag…"
              />
            </div>
            <div className="flex justify-end gap-3">
              <Button variant="ghost" onClick={() => setEditingTagsAgent(null)}>
                Cancel
              </Button>
              <Button variant="primary" onClick={() => void handleSaveTags()} disabled={savingTags}>
                {savingTags ? "Saving…" : "Save tags"}
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Update one agent */}
      {updatingAgent && (
        <UpdateAgentModal
          agent={updatingAgent}
          defaultVersion={compareVersion}
          onClose={() => setUpdatingAgent(null)}
        />
      )}

      {/* Update all outdated agents (admin) */}
      {showUpdateAllModal && (
        <UpdateAllModal
          affectedAgents={outdatedAgents}
          defaultVersion={compareVersion}
          onClose={() => setShowUpdateAllModal(false)}
        />
      )}
    </div>
  );
}
