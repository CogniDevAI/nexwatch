import { useNavigate } from "react-router-dom";
import { StatusIndicator } from "./StatusIndicator";
import { STATUS_META, type Status } from "./status";
import type { FleetAgent } from "@/hooks/useFleetHealth";

interface FleetStripProps {
  agents: FleetAgent[];
  /** "lg" is the hero strip (Dashboard/Agents top bar); "sm" is the sidebar rail. */
  size?: "lg" | "sm";
  emptyLabel?: string;
  /** Highlights the tick for the currently viewed host, e.g. `location.pathname`. */
  currentPath?: string;
}

// Each segment is painted solidly in its status color (not just outlined),
// so the glyph/hostname on top need an explicit contrasting color rather
// than StatusIndicator's default status-tinted text — see DESIGN.md §4.
const SEGMENT_BG: Record<Status, string> = {
  ok: "bg-[var(--color-ok)]",
  warning: "bg-[var(--color-warn)]",
  critical: "bg-[var(--color-critical)]",
  offline: "bg-[var(--color-offline)]",
};

const SEGMENT_TONE: Record<Status, string> = {
  ok: "text-[var(--color-void)]",
  warning: "text-[var(--color-void)]",
  critical: "text-white",
  offline: "text-white",
};

function summarize(agents: FleetAgent[]): string {
  const total = agents.length;
  const counts: Record<Status, number> = { ok: 0, warning: 0, critical: 0, offline: 0 };
  for (const a of agents) counts[a.status] += 1;

  const extras: string[] = [];
  if (counts.critical) extras.push(`${counts.critical} critical`);
  if (counts.warning) extras.push(`${counts.warning} warning`);
  if (counts.offline) extras.push(`${counts.offline} offline`);

  return `${counts.ok} of ${total} operational${extras.length ? `, ${extras.join(", ")}` : ""}`;
}

/**
 * The memorable thing (DESIGN.md §4): one tick per agent, colored and shaped
 * by real health (connectivity plus any firing alert), clickable, with a
 * plain-language summary underneath. Used as the hero strip on Dashboard and
 * Agents ("lg") and as the always-visible sidebar rail ("sm").
 *
 * Every agent is a segment that fills 1/N of the row — a fleet of one agent
 * renders as one full-width segment, not a small icon in an empty bar.
 */
export function FleetStrip({
  agents,
  size = "lg",
  emptyLabel = "No agents connected",
  currentPath,
}: FleetStripProps) {
  const navigate = useNavigate();

  if (agents.length === 0) {
    return <p className="text-sm text-[var(--color-ink-faint)]">{emptyLabel}</p>;
  }

  return (
    <div>
      <ul
        className={`flex gap-1 ${size === "lg" ? "w-full" : "flex-wrap"}`}
        aria-label="Agent status"
      >
        {agents.map((agent) => {
          const isActive = currentPath === `/servers/${agent.id}`;
          const pulse = agent.status === "critical" || agent.status === "offline";
          const tone = SEGMENT_TONE[agent.status];

          return (
            <li key={agent.id} className={size === "lg" ? "min-w-9 flex-1" : undefined}>
              <button
                type="button"
                onClick={() => navigate(`/servers/${agent.id}`)}
                title={agent.name}
                className={`flex w-full items-center overflow-hidden rounded-[var(--radius-chip)] transition-opacity hover:opacity-90 ${SEGMENT_BG[agent.status]} ${
                  size === "lg" ? "h-10 gap-1.5 px-2.5" : "h-6 w-6 justify-center"
                } ${isActive ? "ring-2 ring-[var(--color-signal)] ring-offset-2 ring-offset-[var(--color-panel)]" : ""}`}
              >
                <StatusIndicator
                  status={agent.status}
                  dotOnly
                  pulse={pulse}
                  toneClassName={tone}
                  label={`${agent.name}: ${STATUS_META[agent.status].label}`}
                />
                {size === "lg" && (
                  <span
                    className={`min-w-0 truncate text-xs font-medium ${tone}`}
                    aria-hidden="true"
                  >
                    {agent.name}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
      <p className="mt-2 text-sm text-[var(--color-ink-muted)]">{summarize(agents)}</p>
    </div>
  );
}
