import { useState, useEffect, useCallback } from "react";
import pb from "@/lib/pocketbase";
import type { PublicStatusResponse, PublicStatusItem, PublicStatusDaily } from "@/types";
import "./StatusPage.css";

const REFRESH_INTERVAL_MS = 60_000;
const CLOCK_TICK_MS = 1_000;

type PageState = "loading" | "disabled" | "error" | "ready";

const OVERALL_COPY: Record<PublicStatusItem["status"], string> = {
  operational: "All systems operational",
  degraded: "Some systems degraded",
  down: "Some systems are down",
  unknown: "Status unknown",
};

const ITEM_STATUS_LABEL: Record<PublicStatusItem["status"], string> = {
  operational: "Operational",
  degraded: "Degraded",
  down: "Down",
  unknown: "Unknown",
};

/**
 * Public, unauthenticated status page — self-contained, no AppShell, no
 * auth. Reads GET /api/public/status directly (not through apiFetch,
 * which attaches an auth token and redirects to /login on 401 — neither
 * applies to an anonymous visitor here). See ui/DESIGN.md § Public status
 * page for the design rationale and ui/src/pages/StatusPage.css for its
 * own light/dark token pair, independent of the rest of this app's
 * dark-only palette.
 */
export function StatusPage() {
  const [state, setState] = useState<PageState>("loading");
  const [data, setData] = useState<PublicStatusResponse | null>(null);
  const [errorMessage, setErrorMessage] = useState("");
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    try {
      const url = new URL("/api/public/status", pb.baseUrl).toString();
      const response = await fetch(url);
      if (response.status === 404) {
        setState("disabled");
        return;
      }
      if (!response.ok) {
        throw new Error(`Request failed with status ${response.status}`);
      }
      const body = (await response.json()) as PublicStatusResponse;
      setData(body);
      setState("ready");
      setNow(Date.now());
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Failed to load status");
      setState("error");
    }
  }, []);

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), REFRESH_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [load]);

  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), CLOCK_TICK_MS);
    return () => clearInterval(tick);
  }, []);

  useEffect(() => {
    const previous = document.title;
    document.title = data?.title || "Service status";
    return () => {
      document.title = previous;
    };
  }, [data?.title]);

  return (
    <div className="status-page">
      <div className="status-page__wrap">
        {state === "loading" && <StatusPageSkeleton />}
        {state === "disabled" && <StatusPageDisabled />}
        {state === "error" && (
          <StatusPageError message={errorMessage} onRetry={() => void load()} />
        )}
        {state === "ready" && data && <StatusPageContent data={data} now={now} />}
      </div>
    </div>
  );
}

function StatusPageSkeleton() {
  return (
    <div className="status-page__skeleton" aria-busy="true" aria-label="Loading status">
      <div className="status-page__skeleton-block" style={{ width: "40%", height: "1.5rem" }} />
      <div className="status-page__skeleton-block" style={{ width: "70%" }} />
      <div className="status-page__skeleton-block" style={{ width: "100%", height: "3rem" }} />
      <div className="status-page__skeleton-block" style={{ width: "100%", height: "4rem" }} />
      <div className="status-page__skeleton-block" style={{ width: "100%", height: "4rem" }} />
    </div>
  );
}

function StatusPageDisabled() {
  return (
    <div className="status-page__state">
      <h1>This status page is not enabled</h1>
      <p>Check back later, or contact the site owner.</p>
    </div>
  );
}

function StatusPageError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="status-page__state">
      <h1>Couldn't load status</h1>
      <p>{message}</p>
      <button type="button" className="status-page__retry" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}

function StatusPageContent({ data, now }: { data: PublicStatusResponse; now: number }) {
  return (
    <>
      <h1 className="status-page__title">{data.title}</h1>
      {data.description && <p className="status-page__description">{data.description}</p>}

      <div className={`status-page__banner status-page__banner--${data.overall}`}>
        <span className="status-page__dot" aria-hidden="true" />
        {OVERALL_COPY[data.overall]}
      </div>

      {data.items.length === 0 ? (
        <p className="status-page__description">Nothing is being monitored yet.</p>
      ) : (
        data.items.map((item, i) => (
          <StatusPageItemRow key={`${item.type}-${item.label}-${i}`} item={item} />
        ))
      )}

      <div className="status-page__footer">Updated {relativeFromMs(now, data.updated_at)}</div>
    </>
  );
}

// relativeFromMs re-derives "N s ago" on every second-tick without
// re-fetching, by comparing the live clock (now) against the last fetch's
// updated_at — timeSince alone only recomputes when its input string
// changes, which only happens once per 60s poll.
function relativeFromMs(now: number, updatedAt: string): string {
  const seconds = Math.max(0, Math.floor((now - new Date(updatedAt).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  return `${Math.floor(seconds / 60)}m ago`;
}

function StatusPageItemRow({ item }: { item: PublicStatusItem }) {
  return (
    <div className="status-page__item">
      <div className="status-page__item-row">
        <div
          className="status-page__item-name"
          style={{ color: `var(--sp-${toneVar(item.status)})` }}
        >
          <span className="status-page__dot" aria-hidden="true" />
          <span style={{ color: "var(--sp-ink)" }}>{item.label}</span>
        </div>
        <div className="status-page__item-meta">
          <span
            className="status-page__item-status"
            style={{ color: `var(--sp-${toneVar(item.status)})` }}
          >
            {ITEM_STATUS_LABEL[item.status]}
          </span>
          <span>{item.uptime_30d.toFixed(2)}%</span>
        </div>
      </div>

      {item.daily.length > 0 && (
        <>
          <div
            className="status-page__bars"
            role="img"
            aria-label={`${item.daily.length}-day uptime history for ${item.label}`}
          >
            {item.daily.map((day) => (
              <DailyBar key={day.date} day={day} />
            ))}
          </div>
          <div className="status-page__bars-caption">
            <span>{item.daily.length} days ago</span>
            <span>Today</span>
          </div>
        </>
      )}
    </div>
  );
}

function DailyBar({ day }: { day: PublicStatusDaily }) {
  const tone = day.uptime >= 99.9 ? "operational" : day.uptime >= 95 ? "degraded" : "down";
  const title = `${day.date}: ${day.uptime.toFixed(2)}% uptime${day.incidents > 0 ? `, ${day.incidents} incident${day.incidents === 1 ? "" : "s"}` : ""}`;
  return (
    <div
      className="status-page__bar"
      data-tone={tone}
      style={{ opacity: 0.35 + (Math.min(100, Math.max(0, day.uptime)) / 100) * 0.65 }}
      title={title}
    />
  );
}

function toneVar(status: PublicStatusItem["status"]): string {
  switch (status) {
    case "operational":
      return "ok";
    case "degraded":
      return "warn";
    case "down":
      return "down";
    default:
      return "unknown";
  }
}
