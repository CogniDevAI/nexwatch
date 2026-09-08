import { useState, useEffect, useCallback, useMemo } from "react";
import { Plus, Trash2, BellOff } from "lucide-react";
import type { Agent, Check, ActiveSilence } from "@/types";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { formatDateTime } from "@/lib/time";
import { useAuthStore } from "@/stores/authStore";
import { useSilences, silenceBucket, type SilenceBucket } from "@/hooks/useSilences";
import { SilenceForm } from "@/components/silences/SilenceForm";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { Select } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { usePageTitle } from "@/hooks/usePageTitle";

type BucketFilter = "all" | SilenceBucket;

const BUCKET_META: Record<SilenceBucket, { label: string; classes: string }> = {
  active: {
    label: "Active",
    classes:
      "border-[var(--color-signal)]/30 bg-[var(--color-signal)]/10 text-[var(--color-signal)]",
  },
  scheduled: {
    label: "Scheduled",
    classes:
      "border-[var(--color-line)] bg-[var(--color-panel-raised)] text-[var(--color-ink-muted)]",
  },
  expired: {
    label: "Expired",
    classes: "border-[var(--color-line)] bg-transparent text-[var(--color-ink-faint)]",
  },
};

function SilenceStatusBadge({ bucket }: { bucket: SilenceBucket }) {
  const meta = BUCKET_META[bucket];
  return (
    <span
      className={`inline-flex items-center rounded-[var(--radius-chip)] border px-2 py-0.5 text-xs font-medium ${meta.classes}`}
    >
      {meta.label}
    </span>
  );
}

export function Silences() {
  usePageTitle("Silences");

  const canManage = useAuthStore((s) => s.hasRole("operator"));
  // Fetched and subscribed once by AppShell, same as agents/alerts — this
  // just reads the shared store. See DESIGN.md §10.
  const { silences, loading, error, refetch } = useSilences();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [checks, setChecks] = useState<Check[]>([]);
  const [activeAgentCoverage, setActiveAgentCoverage] = useState<Map<string, number>>(new Map());
  const [activeCheckCoverage, setActiveCheckCoverage] = useState<Map<string, number>>(new Map());
  const [bucketFilter, setBucketFilter] = useState<BucketFilter>("all");
  const [showForm, setShowForm] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  useEffect(() => {
    const loadAgents = async () => {
      try {
        const records = await pb.collection("agents").getFullList<Agent>({ sort: "hostname" });
        setAgents(records);
      } catch {
        // Scope summaries fall back to raw ids — non-fatal.
      }
    };
    void loadAgents();
  }, []);

  useEffect(() => {
    const loadChecks = async () => {
      try {
        const records = await pb.collection("checks").getFullList<Check>({ sort: "name" });
        setChecks(records);
      } catch {
        // Scope summaries fall back to raw ids — non-fatal.
      }
    };
    void loadChecks();
  }, []);

  // Covered-agent/check counts come only from the custom active endpoint
  // (the authoritative agent_id/tag-overlap/check_ids match the hub
  // already computes) — recomputing that matching logic client-side would
  // risk drifting from the backend's actual rule.
  const fetchActiveCoverage = useCallback(async () => {
    try {
      const res = await apiFetch("/api/custom/silences/active");
      if (!res.ok) return;
      const data = (await res.json()) as { silences: ActiveSilence[] };
      setActiveAgentCoverage(new Map(data.silences.map((s) => [s.id, s.covered_agent_ids.length])));
      setActiveCheckCoverage(new Map(data.silences.map((s) => [s.id, s.covered_check_ids.length])));
    } catch {
      // Coverage counts are supplementary — the table still renders without them.
    }
  }, []);

  useEffect(() => {
    void fetchActiveCoverage();
    const interval = setInterval(fetchActiveCoverage, 30_000);
    return () => clearInterval(interval);
  }, [fetchActiveCoverage]);

  const agentsById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const checksById = useMemo(() => new Map(checks.map((c) => [c.id, c])), [checks]);

  const bucketed = useMemo(
    () => silences.map((s) => ({ silence: s, bucket: silenceBucket(s) })),
    [silences],
  );

  const filtered =
    bucketFilter === "all" ? bucketed : bucketed.filter((b) => b.bucket === bucketFilter);

  function scopeSummary(agentId: string, tags: string[], checkIds: string[]): string {
    if (agentId) {
      const agent = agentsById.get(agentId);
      return agent?.hostname || agent?.name || agentId;
    }
    const [checkId] = checkIds;
    if (checkId && checkIds.length === 1) {
      const check = checksById.get(checkId);
      return check?.name || checkId;
    }
    if (tags.length > 0) return `Tags: ${tags.join(", ")}`;
    return "All agents";
  }

  const handleEndNow = async (id: string) => {
    try {
      await pb.collection("silences").update(id, { ends_at: new Date().toISOString() });
      void fetchActiveCoverage();
    } catch {
      // Handle silently — the row stays as-is if the update fails.
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("silences").delete(id);
      setDeleteConfirm(null);
    } catch {
      // Handle silently.
    }
  };

  const handleFormSave = () => {
    setShowForm(false);
    void refetch();
    void fetchActiveCoverage();
  };

  return (
    <div>
      <PageHeader
        title="Silences"
        description="Maintenance windows that suppress alert notifications without losing the incident."
        actions={
          canManage && (
            <Button variant="primary" onClick={() => setShowForm(true)}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              New silence
            </Button>
          )
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Select
          value={bucketFilter}
          onChange={(e) => setBucketFilter(e.target.value as BucketFilter)}
          aria-label="Filter by status"
          className="w-auto"
        >
          <option value="all">All statuses</option>
          <option value="active">Active</option>
          <option value="scheduled">Scheduled</option>
          <option value="expired">Expired</option>
        </Select>

        {!loading && !error && (
          <span className="text-sm text-[var(--color-ink-faint)]">
            {filtered.length} {filtered.length === 1 ? "result" : "results"}
          </span>
        )}
      </div>

      <Panel>
        {loading ? (
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load silences"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={refetch}>
                Try again
              </Button>
            }
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={BellOff}
            title="No silences configured"
            description="Create a silence to suppress notifications during planned maintenance."
          />
        ) : (
          <Table>
            <thead>
              <tr className="border-b border-[var(--color-line)]">
                <Th>Name</Th>
                <Th>Scope</Th>
                <Th>Window</Th>
                <Th>Created by</Th>
                <Th>Covered agents</Th>
                <Th>Covered checks</Th>
                <Th>Status</Th>
                <Th align="right">Actions</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {filtered.map(({ silence, bucket }, idx) => (
                <tr key={silence.id} className={rowClass(idx)}>
                  <Td className="font-medium">
                    {silence.name}
                    {silence.reason && (
                      <p
                        className="mt-0.5 max-w-xs truncate text-xs text-[var(--color-ink-faint)]"
                        title={silence.reason}
                      >
                        {silence.reason}
                      </p>
                    )}
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">
                    {scopeSummary(silence.agent_id, silence.tags ?? [], silence.check_ids ?? [])}
                  </Td>
                  <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                    {formatDateTime(silence.starts_at)} → {formatDateTime(silence.ends_at)}
                  </Td>
                  <Td className="text-[var(--color-ink-muted)]">{silence.created_by || "—"}</Td>
                  <Td className="font-mono text-[var(--color-ink-muted)] tabular-nums">
                    {bucket === "active" ? (activeAgentCoverage.get(silence.id) ?? "—") : "—"}
                  </Td>
                  <Td className="font-mono text-[var(--color-ink-muted)] tabular-nums">
                    {bucket === "active" ? (activeCheckCoverage.get(silence.id) ?? "—") : "—"}
                  </Td>
                  <Td>
                    <SilenceStatusBadge bucket={bucket} />
                  </Td>
                  <Td align="right">
                    {!canManage ? (
                      <span className="text-xs text-[var(--color-ink-faint)]">—</span>
                    ) : (
                      <div className="flex items-center justify-end gap-1">
                        {bucket === "active" && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => void handleEndNow(silence.id)}
                          >
                            End now
                          </Button>
                        )}
                        {bucket !== "active" &&
                          (deleteConfirm === silence.id ? (
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="danger"
                                onClick={() => void handleDelete(silence.id)}
                              >
                                Confirm
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => setDeleteConfirm(null)}
                              >
                                Cancel
                              </Button>
                            </div>
                          ) : (
                            <IconButton
                              aria-label={`Delete silence ${silence.name}`}
                              onClick={() => setDeleteConfirm(silence.id)}
                              className="hover:!bg-[var(--color-critical)]/10 hover:!text-[var(--color-critical)]"
                            >
                              <Trash2 className="h-4 w-4" />
                            </IconButton>
                          ))}
                      </div>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      {showForm && <SilenceForm onSave={handleFormSave} onClose={() => setShowForm(false)} />}
    </div>
  );
}
