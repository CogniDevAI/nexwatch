import { useState, useEffect, useMemo } from "react";
import type { Agent, Check } from "@/types";
import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import { Modal } from "@/components/ui/Modal";
import { Input, Select, Label, FieldCaption, Textarea } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { TagInput } from "@/components/ui/TagInput";

interface SilenceFormProps {
  /** Prefills scope="agent" for the "Silence this host" quick action. */
  initialAgentId?: string;
  initialAgentHostname?: string;
  onSave: () => void;
  onClose: () => void;
}

// "tags" and "check-tags" both ultimately write the same silences.tags
// field — the hub matches it against both agent tags and check tags (see
// alerts.SilenceMatchesAgent/SilenceMatchesCheck) — so the two scopes exist
// only to show the right tag-suggestion list and caption for what the
// operator is actually trying to silence.
type Scope = "all" | "tags" | "agent" | "check-tags" | "check";

const END_PRESETS = [
  { label: "30 min", value: 1800 },
  { label: "1 h", value: 3600 },
  { label: "4 h", value: 14400 },
  { label: "24 h", value: 86400 },
];

/** Local "YYYY-MM-DDTHH:mm" string for a <input type="datetime-local"> value. */
function toDatetimeLocalValue(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

interface FieldErrors {
  name?: string;
  tags?: string;
  agentId?: string;
  checkId?: string;
  startAt?: string;
  endAt?: string;
}

/** Create form for a maintenance-window silence. Reused by the Silences page
 *  and by "Silence this host" on the host detail header (prefilled with that
 *  agent and a default 1h window). Editing an existing silence isn't
 *  supported — only "End now" and delete, both simple field patches the
 *  Silences page issues directly. See README "Silences (maintenance windows)". */
export function SilenceForm({
  initialAgentId,
  initialAgentHostname,
  onSave,
  onClose,
}: SilenceFormProps) {
  const user = useAuthStore((s) => s.user);

  const [name, setName] = useState(
    initialAgentHostname ? `Maintenance: ${initialAgentHostname}` : "",
  );
  const [reason, setReason] = useState("");
  const [scope, setScope] = useState<Scope>(initialAgentId ? "agent" : "all");
  const [agentId, setAgentId] = useState(initialAgentId ?? "");
  const [tags, setTags] = useState<string[]>([]);
  const [checkId, setCheckId] = useState("");

  const [startMode, setStartMode] = useState<"now" | "at">("now");
  const [startAt, setStartAt] = useState(() => toDatetimeLocalValue(new Date()));

  const [endMode, setEndMode] = useState<"preset" | "custom">("preset");
  const [endPreset, setEndPreset] = useState(3600);
  const [endAt, setEndAt] = useState(() => toDatetimeLocalValue(new Date(Date.now() + 3600_000)));

  const [agents, setAgents] = useState<Agent[]>([]);
  const [checks, setChecks] = useState<Check[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [errors, setErrors] = useState<FieldErrors>({});

  useEffect(() => {
    const loadAgents = async () => {
      try {
        const records = await pb.collection("agents").getFullList<Agent>({ sort: "hostname" });
        setAgents(records);
      } catch {
        // Dropdown/suggestions will be empty.
      }
    };
    void loadAgents();
  }, []);

  // Fetched independently rather than read from checksStore, matching
  // AlertRuleForm's check_down/cert_expiry check selector (DESIGN.md §11):
  // this form can be opened from the host detail page's "Silence this
  // host" action too, where checksStore may not have been populated
  // recently, and a stale dropdown here would be worse than one extra
  // fetch.
  useEffect(() => {
    const loadChecks = async () => {
      try {
        const records = await pb.collection("checks").getFullList<Check>({ sort: "name" });
        setChecks(records);
      } catch {
        // Dropdown/suggestions will be empty.
      }
    };
    void loadChecks();
  }, []);

  const tagSuggestions = useMemo(
    () => Array.from(new Set(agents.flatMap((a) => a.tags ?? []))).sort(),
    [agents],
  );

  const checkTagSuggestions = useMemo(
    () => Array.from(new Set(checks.flatMap((c) => c.tags ?? []))).sort(),
    [checks],
  );

  function computeWindow(): { startMs: number; endMs: number } {
    const startMs = startMode === "now" ? Date.now() : new Date(startAt).getTime();
    const endMs = endMode === "preset" ? startMs + endPreset * 1000 : new Date(endAt).getTime();
    return { startMs, endMs };
  }

  function validate(): FieldErrors {
    const next: FieldErrors = {};
    if (!name.trim()) next.name = "Enter a name for this silence.";
    if ((scope === "tags" || scope === "check-tags") && tags.length === 0) {
      next.tags = "Add at least one tag.";
    }
    if (scope === "agent" && !agentId) next.agentId = "Choose an agent.";
    if (scope === "check" && !checkId) next.checkId = "Choose a check.";

    const { startMs, endMs } = computeWindow();
    if (startMode === "at" && (!startAt || Number.isNaN(startMs))) {
      next.startAt = "Choose a start time.";
    }
    if (endMode === "custom" && (!endAt || Number.isNaN(endMs))) {
      next.endAt = "Choose an end time.";
    }
    if (!Number.isNaN(startMs) && !Number.isNaN(endMs) && endMs <= startMs) {
      next.endAt = "End must be after start.";
    }
    return next;
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    const fieldErrors = validate();
    setErrors(fieldErrors);
    if (Object.keys(fieldErrors).length > 0) return;

    setSaving(true);
    const { startMs, endMs } = computeWindow();

    const data = {
      name: name.trim(),
      reason: reason.trim(),
      agent_id: scope === "agent" ? agentId : "",
      tags: scope === "tags" || scope === "check-tags" ? tags : [],
      check_ids: scope === "check" ? [checkId] : [],
      starts_at: new Date(startMs).toISOString(),
      ends_at: new Date(endMs).toISOString(),
      // The hub does not stamp this itself — it's set here to the creating
      // user's email so "who created it" is readable without a users join.
      created_by: user?.email || user?.id || "",
    };

    try {
      await pb.collection("silences").create(data);
      onSave();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create silence");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title="New silence" onClose={onClose}>
      <form onSubmit={handleSubmit} className="max-h-[70vh] space-y-4 overflow-y-auto p-6">
        {error && (
          <div
            role="alert"
            className="rounded-[var(--radius-control)] border border-[var(--color-critical)]/25 bg-[var(--color-critical)]/10 px-4 py-2 text-sm text-[var(--color-critical)]"
          >
            {error}
          </div>
        )}

        <div>
          <Label htmlFor="silence-name">Name</Label>
          <Input
            id="silence-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g., Nightly patch window"
          />
          {errors.name && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.name}</p>
          )}
        </div>

        <div>
          <Label htmlFor="silence-reason">Reason</Label>
          <Textarea
            id="silence-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="e.g., Scheduled OS patching"
            rows={2}
          />
        </div>

        <div>
          <FieldCaption>Scope</FieldCaption>
          <div className="mb-2 flex flex-wrap gap-4">
            {(
              [
                { value: "all", label: "All agents" },
                { value: "tags", label: "Tags" },
                { value: "agent", label: "One agent" },
                { value: "check-tags", label: "Checks with tags" },
                { value: "check", label: "One check" },
              ] as const
            ).map((opt) => (
              <label key={opt.value} className="flex cursor-pointer items-center gap-1.5 text-sm">
                <input
                  type="radio"
                  name="silence-scope"
                  value={opt.value}
                  checked={scope === opt.value}
                  onChange={() => {
                    setScope(opt.value);
                    if (opt.value !== "tags" && opt.value !== "check-tags") setTags([]);
                    if (opt.value !== "agent") setAgentId("");
                    if (opt.value !== "check") setCheckId("");
                  }}
                  className="border-[var(--color-line)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                />
                <span className="text-[var(--color-ink)]">{opt.label}</span>
              </label>
            ))}
          </div>

          {scope === "tags" && (
            <div>
              <TagInput
                value={tags}
                onChange={setTags}
                suggestions={tagSuggestions}
                placeholder="Add a tag…"
                aria-label="Silence tags"
              />
              {errors.tags && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.tags}</p>
              )}
            </div>
          )}

          {scope === "agent" && (
            <div>
              <Select
                aria-label="Silenced agent"
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
              >
                <option value="">Choose an agent…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.hostname || a.id}
                  </option>
                ))}
              </Select>
              {errors.agentId && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.agentId}</p>
              )}
            </div>
          )}

          {scope === "check-tags" && (
            <div>
              <TagInput
                value={tags}
                onChange={setTags}
                suggestions={checkTagSuggestions}
                placeholder="Add a tag…"
                aria-label="Silence check tags"
              />
              <FieldCaption>
                Matches by tag overlap, the same as the Tags scope — this is just a shortcut for
                picking from checks' own tags. Any check sharing a tag here is silenced.
              </FieldCaption>
              {errors.tags && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.tags}</p>
              )}
            </div>
          )}

          {scope === "check" && (
            <div>
              <Select
                aria-label="Silenced check"
                value={checkId}
                onChange={(e) => setCheckId(e.target.value)}
              >
                <option value="">Choose a check…</option>
                {checks.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name || c.id}
                  </option>
                ))}
              </Select>
              {errors.checkId && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.checkId}</p>
              )}
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <FieldCaption>Starts</FieldCaption>
            <div className="mb-2 flex flex-wrap gap-4">
              <label className="flex cursor-pointer items-center gap-1.5 text-sm">
                <input
                  type="radio"
                  name="silence-start-mode"
                  checked={startMode === "now"}
                  onChange={() => setStartMode("now")}
                  className="border-[var(--color-line)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                />
                <span className="text-[var(--color-ink)]">Now</span>
              </label>
              <label className="flex cursor-pointer items-center gap-1.5 text-sm">
                <input
                  type="radio"
                  name="silence-start-mode"
                  checked={startMode === "at"}
                  onChange={() => setStartMode("at")}
                  className="border-[var(--color-line)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                />
                <span className="text-[var(--color-ink)]">At a time</span>
              </label>
            </div>
            {startMode === "at" && (
              <Input
                type="datetime-local"
                value={startAt}
                onChange={(e) => setStartAt(e.target.value)}
                aria-label="Start time"
              />
            )}
            {errors.startAt && (
              <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.startAt}</p>
            )}
          </div>

          <div>
            <FieldCaption>Ends</FieldCaption>
            <div className="mb-2 flex flex-wrap gap-1.5">
              {END_PRESETS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  onClick={() => {
                    setEndMode("preset");
                    setEndPreset(p.value);
                  }}
                  className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                    endMode === "preset" && endPreset === p.value
                      ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                      : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                  }`}
                >
                  {p.label}
                </button>
              ))}
              <button
                type="button"
                onClick={() => setEndMode("custom")}
                className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                  endMode === "custom"
                    ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                }`}
              >
                Custom…
              </button>
            </div>
            {endMode === "custom" && (
              <Input
                type="datetime-local"
                value={endAt}
                onChange={(e) => setEndAt(e.target.value)}
                aria-label="End time"
              />
            )}
            {errors.endAt && (
              <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.endAt}</p>
            )}
          </div>
        </div>

        <div className="flex items-center justify-end gap-3 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Creating…" : "Create silence"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
