import { Fragment, useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Pencil, Play, Plus, Radio, Trash2 } from "lucide-react";
import type { Check } from "@/types";
import pb from "@/lib/pocketbase";
import { apiFetch } from "@/lib/api";
import { timeSince } from "@/lib/time";
import { checkStatus, formatLatency, formatUptime, summarizeChecks } from "@/lib/checks";
import { useChecksStore } from "@/stores/checksStore";
import { useChecksSummary } from "@/hooks/useChecksSummary";
import { useAuthStore } from "@/stores/authStore";
import { CheckForm } from "@/components/checks/CheckForm";
import { CheckDetailDrawer } from "@/components/checks/CheckDetailDrawer";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { StatusIndicator } from "@/components/ui/StatusIndicator";
import { Button, IconButton } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { usePageTitle } from "@/hooks/usePageTitle";

export function Checks() {
  usePageTitle("Checks");

  const canManage = useAuthStore((s) => s.hasRole("operator"));
  // Fetched and subscribed once by AppShell — this just reads the shared
  // store for check identity/config. See DESIGN.md §10.
  const { checks, loading, error, fetchChecks } = useChecksStore();
  const { summary, refetch: refetchSummary } = useChecksSummary();

  const [showForm, setShowForm] = useState(false);
  const [editingCheck, setEditingCheck] = useState<Check | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [runningId, setRunningId] = useState<string | null>(null);

  const summaryById = useMemo(() => new Map(summary.map((s) => [s.id, s])), [summary]);
  const counts = useMemo(() => summarizeChecks(summary), [summary]);

  const handleRunNow = async (checkId: string) => {
    setRunningId(checkId);
    try {
      await apiFetch(`/api/custom/checks/${checkId}/run`, { method: "POST" });
      await refetchSummary();
    } catch {
      // Result is supplementary — the row simply doesn't update this cycle.
    } finally {
      setRunningId(null);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await pb.collection("checks").delete(id);
      setDeleteConfirm(null);
    } catch {
      // Handle silently — the row stays as-is if the delete fails.
    }
  };

  const handleFormSave = () => {
    setShowForm(false);
    setEditingCheck(null);
    void fetchChecks();
    void refetchSummary();
  };

  return (
    <div>
      <PageHeader
        title="Checks"
        description="HTTP, TCP, and ICMP monitors run directly from the hub."
        actions={
          canManage && (
            <Button variant="primary" onClick={() => setShowForm(true)}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              New check
            </Button>
          )
        }
      />

      {!loading && !error && checks.length > 0 && (
        <Panel className="mb-6 p-5">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
            <StatusIndicator status="ok" label={`${counts.up} up`} />
            <StatusIndicator status="critical" label={`${counts.down} down`} />
            {counts.expiringSoon > 0 && (
              <StatusIndicator
                status="warning"
                label={`${counts.expiringSoon} certificate${counts.expiringSoon === 1 ? "" : "s"} expiring soon`}
              />
            )}
          </div>
        </Panel>
      )}

      <Panel>
        {loading ? (
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load checks"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={fetchChecks}>
                Try again
              </Button>
            }
          />
        ) : checks.length === 0 ? (
          <EmptyState
            icon={Radio}
            title="No checks configured"
            description="Add an HTTP, TCP, or ICMP check to start monitoring an endpoint from the hub."
          />
        ) : (
          <Table>
            <thead>
              <tr className="border-b border-[var(--color-line)]">
                <Th></Th>
                <Th>Name</Th>
                <Th>Type</Th>
                <Th>Target</Th>
                <Th align="right">Latency</Th>
                <Th align="right">Uptime 24h</Th>
                <Th align="right">Uptime 7d</Th>
                <Th>Last checked</Th>
                <Th align="right">Actions</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {checks.map((check, idx) => {
                const row = summaryById.get(check.id);
                const status = row ? checkStatus(row) : "offline";
                const expanded = expandedId === check.id;
                const hasRun = Boolean(row?.last_checked_at);

                return (
                  <Fragment key={check.id}>
                    <tr key={check.id} className={rowClass(idx)}>
                      <Td>
                        <button
                          type="button"
                          aria-label={expanded ? "Collapse details" : "Expand details"}
                          aria-expanded={expanded}
                          onClick={() => setExpandedId(expanded ? null : check.id)}
                          className="text-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                        >
                          {expanded ? (
                            <ChevronDown className="h-4 w-4" aria-hidden="true" />
                          ) : (
                            <ChevronRight className="h-4 w-4" aria-hidden="true" />
                          )}
                        </button>
                      </Td>
                      <Td className="font-medium">
                        <div className="flex items-center gap-2">
                          <StatusIndicator status={status} dotOnly />
                          {check.name}
                          {!check.enabled && (
                            <span className="text-2xs rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-[var(--color-ink-faint)]">
                              Disabled
                            </span>
                          )}
                          {row?.cert_expiring_soon && (
                            <span className="text-2xs rounded-[var(--radius-chip)] border border-[var(--color-warn)]/30 bg-[var(--color-warn)]/10 px-1.5 py-0.5 font-medium text-[var(--color-warn)]">
                              Cert expiring soon
                            </span>
                          )}
                        </div>
                      </Td>
                      <Td className="text-[var(--color-ink-muted)] uppercase">{check.type}</Td>
                      <Td
                        className="max-w-xs truncate font-mono text-xs text-[var(--color-ink-muted)]"
                        title={check.target}
                      >
                        {check.target}
                      </Td>
                      <Td align="right" className="font-mono tabular-nums">
                        {row && hasRun ? formatLatency(row.latency_ms) : "—"}
                      </Td>
                      <Td align="right" className="font-mono tabular-nums">
                        {row && hasRun ? formatUptime(row.uptime_24h) : "—"}
                      </Td>
                      <Td align="right" className="font-mono tabular-nums">
                        {row && hasRun ? formatUptime(row.uptime_7d) : "—"}
                      </Td>
                      <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                        {hasRun ? timeSince(row!.last_checked_at!) : "Never"}
                      </Td>
                      <Td align="right">
                        <div className="flex items-center justify-end gap-1">
                          {canManage && (
                            <IconButton
                              aria-label={`Run ${check.name} now`}
                              title="Run now"
                              disabled={runningId === check.id}
                              onClick={() => void handleRunNow(check.id)}
                            >
                              <Play className="h-4 w-4" aria-hidden="true" />
                            </IconButton>
                          )}
                          {canManage && (
                            <IconButton
                              aria-label={`Edit ${check.name}`}
                              title="Edit"
                              onClick={() => setEditingCheck(check)}
                            >
                              <Pencil className="h-4 w-4" aria-hidden="true" />
                            </IconButton>
                          )}
                          {canManage &&
                            (deleteConfirm === check.id ? (
                              <div className="flex items-center gap-1">
                                <Button
                                  size="sm"
                                  variant="danger"
                                  onClick={() => void handleDelete(check.id)}
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
                                aria-label={`Delete ${check.name}`}
                                title="Delete"
                                onClick={() => setDeleteConfirm(check.id)}
                                className="hover:!bg-[var(--color-critical)]/10 hover:!text-[var(--color-critical)]"
                              >
                                <Trash2 className="h-4 w-4" aria-hidden="true" />
                              </IconButton>
                            ))}
                        </div>
                      </Td>
                    </tr>
                    {expanded && (
                      <tr>
                        <td colSpan={9} className="p-0">
                          <CheckDetailDrawer check={check} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </Table>
        )}
      </Panel>

      {(showForm || editingCheck) && (
        <CheckForm
          check={editingCheck}
          onSave={handleFormSave}
          onClose={() => {
            setShowForm(false);
            setEditingCheck(null);
          }}
        />
      )}
    </div>
  );
}
