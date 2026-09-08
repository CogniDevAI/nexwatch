import { Fragment, useCallback, useEffect, useState } from "react";
import { Navigate } from "react-router-dom";
import { ChevronDown, ChevronRight, ScrollText } from "lucide-react";
import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import type { AuditLogEntry } from "@/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { Input } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { usePageTitle } from "@/hooks/usePageTitle";

const PER_PAGE = 50;
// Debounce free-text filter inputs so every keystroke doesn't issue its own
// request — the same tradeoff a live search box always makes.
const FILTER_DEBOUNCE_MS = 300;

function formatDate(dateStr: string): string {
  if (!dateStr) return "—";
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

/** Builds a PocketBase filter expression from the two free-text filters.
 *  "~" is PocketBase's case-insensitive contains/like operator — this reads
 *  as "action contains this text" rather than a strict prefix match, which
 *  in practice covers the documented "filter by action prefix" need (typing
 *  "docker" surfaces every docker.* entry) without depending on any
 *  wildcard-escaping specifics. */
function buildFilter(actionQuery: string, actorQuery: string): string {
  const parts: string[] = [];
  if (actionQuery.trim()) {
    parts.push(`action ~ "${actionQuery.trim().replace(/"/g, '\\"')}"`);
  }
  if (actorQuery.trim()) {
    const q = actorQuery.trim().replace(/"/g, '\\"');
    parts.push(`(actor_email ~ "${q}" || actor_id ~ "${q}")`);
  }
  return parts.join(" && ");
}

function ResultBadge({ result }: { result: AuditLogEntry["result"] }) {
  const isSuccess = result === "success";
  return (
    <span
      className={`text-2xs inline-flex items-center rounded-full px-2 py-0.5 font-medium ${
        isSuccess
          ? "bg-[var(--color-ok)]/10 text-[var(--color-ok)]"
          : "bg-[var(--color-critical)]/10 text-[var(--color-critical)]"
      }`}
    >
      {isSuccess ? "Success" : "Failure"}
    </span>
  );
}

function DetailsCell({ details }: { details: string }) {
  if (!details) return <span className="text-[var(--color-ink-faint)]">—</span>;
  let pretty = details;
  try {
    pretty = JSON.stringify(JSON.parse(details), null, 2);
  } catch {
    // Leave as-is if it's somehow not valid JSON.
  }
  return (
    <pre className="text-2xs max-w-prose overflow-x-auto rounded-[var(--radius-control)] bg-[var(--color-void)] p-3 font-mono whitespace-pre-wrap text-[var(--color-ink-muted)]">
      {pretty}
    </pre>
  );
}

export function AuditLog() {
  usePageTitle("Audit log");

  const hasRole = useAuthStore((s) => s.hasRole);

  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actionQuery, setActionQuery] = useState("");
  const [actorQuery, setActorQuery] = useState("");
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const fetchPage = useCallback(async (targetPage: number, filter: string, append: boolean) => {
    if (append) {
      setLoadingMore(true);
    } else {
      setLoading(true);
      setError(null);
    }
    try {
      const result = await pb.collection("audit_log").getList<AuditLogEntry>(targetPage, PER_PAGE, {
        sort: "-created",
        filter,
      });
      setEntries((prev) => (append ? [...prev, ...result.items] : result.items));
      setPage(result.page);
      setTotalPages(result.totalPages);
    } catch (err) {
      if (!append) {
        setError(err instanceof Error ? err.message : "Failed to load the audit log");
      }
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  // Debounced refetch from page 1 whenever a filter changes.
  useEffect(() => {
    const handle = setTimeout(() => {
      void fetchPage(1, buildFilter(actionQuery, actorQuery), false);
    }, FILTER_DEBOUNCE_MS);
    return () => clearTimeout(handle);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fetchPage identity is stable (useCallback, no deps)
  }, [actionQuery, actorQuery]);

  // Only operator+ can read audit_log (see the collection's ListRule) —
  // anyone else is redirected back to Settings, matching Users.tsx's
  // admin-only gate for the same reason.
  if (!hasRole("operator")) {
    return <Navigate to="/settings" replace />;
  }

  const handleLoadMore = () => {
    void fetchPage(page + 1, buildFilter(actionQuery, actorQuery), true);
  };

  return (
    <div>
      <PageHeader title="Audit log" description="Who did what, across the whole hub." />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Input
          value={actionQuery}
          onChange={(e) => setActionQuery(e.target.value)}
          placeholder="Filter by action (e.g. docker, alert_rule)"
          aria-label="Filter by action"
          className="w-64"
        />
        <Input
          value={actorQuery}
          onChange={(e) => setActorQuery(e.target.value)}
          placeholder="Filter by actor (email or id)"
          aria-label="Filter by actor"
          className="w-64"
        />
        {!loading && !error && (
          <span className="text-sm text-[var(--color-ink-faint)]">
            {entries.length} {entries.length === 1 ? "entry" : "entries"} loaded
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
            title="Couldn't load the audit log"
            description={error}
            action={
              <Button
                variant="primary"
                size="sm"
                onClick={() => void fetchPage(1, buildFilter(actionQuery, actorQuery), false)}
              >
                Try again
              </Button>
            }
          />
        ) : entries.length === 0 ? (
          <EmptyState
            icon={ScrollText}
            title="No audit entries yet"
            description="Mutating actions across the hub — Docker actions, rule changes, user management — will show up here as they happen."
          />
        ) : (
          <>
            <Table>
              <thead>
                <tr className="border-b border-[var(--color-line)]">
                  <Th></Th>
                  <Th>Time</Th>
                  <Th>Actor</Th>
                  <Th>Action</Th>
                  <Th>Target</Th>
                  <Th>Agent</Th>
                  <Th>Result</Th>
                  <Th>Request ID</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-line-soft)]">
                {entries.map((entry, idx) => {
                  const isExpanded = expandedId === entry.id;
                  return (
                    <Fragment key={entry.id}>
                      <tr className={rowClass(idx)}>
                        <Td>
                          <IconButton
                            aria-label={isExpanded ? "Hide details" : "Show details"}
                            aria-expanded={isExpanded}
                            onClick={() => setExpandedId(isExpanded ? null : entry.id)}
                          >
                            {isExpanded ? (
                              <ChevronDown className="h-4 w-4" />
                            ) : (
                              <ChevronRight className="h-4 w-4" />
                            )}
                          </IconButton>
                        </Td>
                        <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                          {formatDate(entry.created)}
                        </Td>
                        <Td className="text-[var(--color-ink-muted)]">
                          {entry.actor_email || entry.actor_id || "—"}
                        </Td>
                        <Td className="font-mono text-xs font-medium">{entry.action}</Td>
                        <Td className="text-[var(--color-ink-muted)]">
                          {entry.target_type
                            ? `${entry.target_type}${entry.target_id ? ` · ${entry.target_id}` : ""}`
                            : "—"}
                        </Td>
                        <Td className="text-[var(--color-ink-muted)]">{entry.agent_id || "—"}</Td>
                        <Td>
                          <ResultBadge result={entry.result} />
                        </Td>
                        <Td className="font-mono text-xs text-[var(--color-ink-faint)]">
                          {entry.request_id || "—"}
                        </Td>
                      </tr>
                      {isExpanded && (
                        <tr className="bg-[var(--color-panel-raised)]">
                          <Td colSpan={8}>
                            <DetailsCell details={entry.details} />
                          </Td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </Table>
            {page < totalPages && (
              <div className="flex justify-center border-t border-[var(--color-line)] p-4">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleLoadMore}
                  disabled={loadingMore}
                >
                  {loadingMore ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </>
        )}
      </Panel>
    </div>
  );
}
