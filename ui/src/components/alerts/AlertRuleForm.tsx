import { useState, useEffect, useMemo } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { AlertRule, Agent, Check, NotificationChannel } from "@/types";
import pb from "@/lib/pocketbase";
import { formatDuration } from "@/lib/time";
import { Modal } from "@/components/ui/Modal";
import { Input, Select, Label, FieldCaption } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { TagInput } from "@/components/ui/TagInput";

interface AlertRuleFormProps {
  rule?: AlertRule | null;
  onSave: () => void;
  onClose: () => void;
}

const RESOURCE_METRIC_TYPES = [
  { value: "cpu", label: "CPU" },
  { value: "memory", label: "Memory" },
  { value: "disk", label: "Disk" },
  { value: "network", label: "Network" },
  { value: "docker", label: "Docker" },
];

const SECURITY_METRIC_TYPES = [{ value: "cve_count", label: "CVE count" }];

const CVE_TARGET_OPTIONS = [
  { value: "", label: "Critical + high severity" },
  { value: "critical", label: "Critical only" },
];

const AVAILABILITY_METRIC_TYPES = [
  { value: "process_down", label: "Process down" },
  { value: "service_failed", label: "Service failed" },
  { value: "agent_offline", label: "Agent offline" },
  { value: "check_down", label: "Check down" },
  { value: "cert_expiry", label: "Certificate expiring" },
];

const AVAILABILITY_VALUES = new Set(AVAILABILITY_METRIC_TYPES.map((m) => m.value));

const LOGS_METRIC_TYPES = [{ value: "log_match", label: "Log pattern match" }];

const CHECK_RULE_VALUES = new Set(["check_down", "cert_expiry"]);

const CONDITIONS = [
  { value: "gt", label: "Greater than (>)" },
  { value: "lt", label: "Less than (<)" },
  { value: "eq", label: "Equal to (=)" },
];

const SEVERITIES = [
  { value: "warning", label: "Warning" },
  { value: "critical", label: "Critical" },
];

const DURATION_PRESETS = [
  { label: "30 s", value: 30 },
  { label: "1 min", value: 60 },
  { label: "5 min", value: 300 },
  { label: "15 min", value: 900 },
];

const ESCALATION_PRESETS = [
  { label: "Off", value: 0 },
  { label: "10 min", value: 600 },
  { label: "30 min", value: 1800 },
  { label: "1 h", value: 3600 },
];

type TargetingMode = "all" | "tags" | "agent";

function initialTargetingMode(rule?: AlertRule | null): TargetingMode {
  if (rule?.agent_id) return "agent";
  if (rule?.target_tags && rule.target_tags.length > 0) return "tags";
  return "all";
}

interface FieldErrors {
  name?: string;
  target?: string;
  targetTags?: string;
  agentId?: string;
  threshold?: string;
}

export function AlertRuleForm({ rule, onSave, onClose }: AlertRuleFormProps) {
  const [name, setName] = useState(rule?.name ?? "");
  const [metricType, setMetricType] = useState(rule?.metric_type ?? "cpu");
  const [condition, setCondition] = useState(rule?.condition ?? "gt");
  const [threshold, setThreshold] = useState(rule?.threshold ?? 90);
  const [target, setTarget] = useState(rule?.target ?? "");
  const [duration, setDuration] = useState(rule?.duration ?? 300);
  const [severity, setSeverity] = useState(rule?.severity ?? "warning");
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [selectedChannels, setSelectedChannels] = useState<string[]>(
    rule?.notification_channels ?? [],
  );

  const [targetingMode, setTargetingMode] = useState<TargetingMode>(initialTargetingMode(rule));
  const [agentId, setAgentId] = useState(rule?.agent_id ?? "");
  const [targetTags, setTargetTags] = useState<string[]>(rule?.target_tags ?? []);

  const [escalationOpen, setEscalationOpen] = useState(
    (rule?.escalation_channels?.length ?? 0) > 0 || (rule?.escalation_after ?? 0) > 0,
  );
  const [escalationChannels, setEscalationChannels] = useState<string[]>(
    rule?.escalation_channels ?? [],
  );
  const [escalationAfter, setEscalationAfter] = useState(rule?.escalation_after ?? 0);

  const [agents, setAgents] = useState<Agent[]>([]);
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [checks, setChecks] = useState<Check[]>([]);
  const [checkId, setCheckId] = useState(rule?.check_id ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [errors, setErrors] = useState<FieldErrors>({});

  const isAvailability = AVAILABILITY_VALUES.has(metricType);
  // log_match is not boolean like the availability types above (it still
  // uses threshold/duration — a count of matching log lines in the last
  // `duration` seconds), so it shares showTarget's free-text Input for its
  // pattern rather than being folded into isAvailability.
  const isLogMatchRule = metricType === "log_match";
  const showTarget =
    metricType === "process_down" || metricType === "service_failed" || isLogMatchRule;
  // cve_count reuses the same "target" column as process_down/service_failed
  // (a free-text field on the record), but as a closed choice — "" means
  // critical+high, "critical" narrows to critical only — so it gets its
  // own Select rather than showTarget's free-text Input.
  const isCveRule = metricType === "cve_count";
  // check_down/cert_expiry target a "checks" record directly (via a check
  // selector, empty = every check) rather than the agent_id/target_tags
  // targeting model, which doesn't apply — a check isn't an agent.
  const isCheckRule = CHECK_RULE_VALUES.has(metricType);

  useEffect(() => {
    const loadData = async () => {
      try {
        const [agentRecords, channelRecords, checkRecords] = await Promise.all([
          pb.collection("agents").getFullList<Agent>({ sort: "hostname" }),
          pb.collection("notification_channels").getFullList<NotificationChannel>({
            sort: "name",
            filter: "enabled = true",
          }),
          pb.collection("checks").getFullList<Check>({ sort: "name" }),
        ]);
        setAgents(agentRecords);
        setChannels(channelRecords);
        setChecks(checkRecords ?? []);
      } catch {
        // Silently handle — dropdowns will be empty.
      }
    };
    void loadData();
  }, []);

  const tagSuggestions = useMemo(
    () => Array.from(new Set(agents.flatMap((a) => a.tags ?? []))).sort(),
    [agents],
  );

  function handleTargetingModeChange(mode: TargetingMode) {
    setTargetingMode(mode);
    if (mode === "all") {
      setAgentId("");
      setTargetTags([]);
    } else if (mode === "tags") {
      setAgentId("");
    } else {
      setTargetTags([]);
    }
  }

  function validate(): FieldErrors {
    const next: FieldErrors = {};
    if (!name.trim()) next.name = "Enter a name for this rule.";
    if (showTarget && !target.trim()) {
      next.target = isLogMatchRule
        ? "Enter a substring or /regex/ pattern to match."
        : metricType === "service_failed"
          ? "Enter a systemd service name to match."
          : "Enter a process name or command-line substring to match.";
    }
    if (!isCheckRule && targetingMode === "tags" && targetTags.length === 0) {
      next.targetTags = "Add at least one tag.";
    }
    if (!isCheckRule && targetingMode === "agent" && !agentId) {
      next.agentId = "Choose an agent.";
    }
    if (
      (!isAvailability || metricType === "cert_expiry") &&
      (threshold === null || Number.isNaN(threshold))
    ) {
      next.threshold =
        metricType === "cert_expiry" ? "Enter a number of days." : "Enter a threshold.";
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

    const data = {
      name: name.trim(),
      metric_type: metricType,
      condition,
      threshold,
      duration,
      severity,
      agent_id: !isCheckRule && targetingMode === "agent" ? agentId : "",
      target_tags: !isCheckRule && targetingMode === "tags" ? targetTags : [],
      target: showTarget || isCveRule ? target.trim() : "",
      check_id: isCheckRule ? checkId : "",
      enabled,
      notification_channels: selectedChannels,
      escalation_channels: escalationChannels,
      escalation_after: escalationAfter,
    };

    try {
      if (rule) {
        await pb.collection("alert_rules").update(rule.id, data);
      } else {
        await pb.collection("alert_rules").create(data);
      }
      onSave();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save alert rule");
    } finally {
      setSaving(false);
    }
  };

  const toggleChannel = (channelId: string) => {
    setSelectedChannels((prev) =>
      prev.includes(channelId) ? prev.filter((id) => id !== channelId) : [...prev, channelId],
    );
  };

  const toggleEscalationChannel = (channelId: string) => {
    setEscalationChannels((prev) =>
      prev.includes(channelId) ? prev.filter((id) => id !== channelId) : [...prev, channelId],
    );
  };

  return (
    <Modal title={rule ? "Edit alert rule" : "New alert rule"} onClose={onClose}>
      <form onSubmit={handleSubmit} className="max-h-[70vh] space-y-4 overflow-y-auto p-6">
        {error && (
          <div
            role="alert"
            className="rounded-[var(--radius-control)] border border-[var(--color-critical)]/25 bg-[var(--color-critical)]/10 px-4 py-2 text-sm text-[var(--color-critical)]"
          >
            {error}
          </div>
        )}

        {/* Name */}
        <div>
          <Label htmlFor="alert-rule-name">Rule name</Label>
          <Input
            id="alert-rule-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g., High CPU alert"
          />
          {errors.name && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.name}</p>
          )}
        </div>

        {/* Metric Type */}
        <div>
          <Label htmlFor="alert-metric-type">Metric type</Label>
          <Select
            id="alert-metric-type"
            value={metricType}
            onChange={(e) => setMetricType(e.target.value as AlertRule["metric_type"])}
          >
            <optgroup label="Resource metrics">
              {RESOURCE_METRIC_TYPES.map((mt) => (
                <option key={mt.value} value={mt.value}>
                  {mt.label}
                </option>
              ))}
            </optgroup>
            <optgroup label="Security">
              {SECURITY_METRIC_TYPES.map((mt) => (
                <option key={mt.value} value={mt.value}>
                  {mt.label}
                </option>
              ))}
            </optgroup>
            <optgroup label="Availability">
              {AVAILABILITY_METRIC_TYPES.map((mt) => (
                <option key={mt.value} value={mt.value}>
                  {mt.label}
                </option>
              ))}
            </optgroup>
            <optgroup label="Logs">
              {LOGS_METRIC_TYPES.map((mt) => (
                <option key={mt.value} value={mt.value}>
                  {mt.label}
                </option>
              ))}
            </optgroup>
          </Select>
        </div>

        {/* CVE count scope — cve_count reuses the "target" field as a
            closed choice (all severities vs. critical only) rather than
            the free-text process/service name showTarget renders below. */}
        {isCveRule && (
          <div>
            <Label htmlFor="alert-cve-target">Count</Label>
            <Select
              id="alert-cve-target"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            >
              {CVE_TARGET_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </Select>
          </div>
        )}

        {/* Target — process/service name for process_down/service_failed, or
            a substring/regex pattern for log_match. */}
        {showTarget && (
          <div>
            <Label htmlFor="alert-target">
              {isLogMatchRule
                ? "Log pattern"
                : metricType === "service_failed"
                  ? "Service name"
                  : "Process name"}
            </Label>
            <Input
              id="alert-target"
              type="text"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder={
                isLogMatchRule
                  ? "out of memory, or /error code [45]\\d\\d/"
                  : metricType === "service_failed"
                    ? "postgresql.service"
                    : "nginx"
              }
            />
            {isLogMatchRule && (
              <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
                A case-insensitive substring, or a /regex/ pattern delimited by slashes.
              </p>
            )}
            {errors.target && (
              <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.target}</p>
            )}
          </div>
        )}

        {/* Certificate expiry warning window — cert_expiry reuses the
            threshold field as "warn within N days" rather than a metric
            comparison, so it gets its own labeled input instead of the
            generic Condition + Threshold pair below. */}
        {metricType === "cert_expiry" && (
          <div>
            <Label htmlFor="alert-cert-days">Warn when certificate expires within (days)</Label>
            <Input
              id="alert-cert-days"
              type="number"
              min={1}
              step="any"
              value={threshold}
              onChange={(e) => setThreshold(parseFloat(e.target.value) || 0)}
              className="max-w-32"
            />
            {errors.threshold && (
              <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.threshold}</p>
            )}
          </div>
        )}

        {/* Condition + Threshold — hidden for boolean availability rules.
            log_match keeps Threshold (a match count) but hides Condition,
            since it's always semantically "count >= threshold". */}
        {!isAvailability && (
          <div className={isLogMatchRule ? "" : "grid grid-cols-2 gap-3"}>
            {!isLogMatchRule && (
              <div>
                <Label htmlFor="alert-condition">Condition</Label>
                <Select
                  id="alert-condition"
                  value={condition}
                  onChange={(e) => setCondition(e.target.value as AlertRule["condition"])}
                >
                  {CONDITIONS.map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </Select>
              </div>
            )}
            <div>
              <Label htmlFor="alert-threshold">
                {isLogMatchRule ? "Match count threshold" : "Threshold"}
              </Label>
              <Input
                id="alert-threshold"
                type="number"
                step="any"
                value={threshold}
                onChange={(e) => setThreshold(parseFloat(e.target.value) || 0)}
              />
              {isLogMatchRule && (
                <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
                  Fires once at least this many log lines match the pattern within the duration
                  window below.
                </p>
              )}
              {errors.threshold && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.threshold}</p>
              )}
            </div>
          </div>
        )}

        {/* Duration — quick presets plus an exact numeric field, always shown */}
        <div>
          <Label htmlFor="alert-duration">Duration</Label>
          <div className="mb-2 flex flex-wrap gap-1.5">
            {DURATION_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setDuration(p.value)}
                className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                  duration === p.value
                    ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
          <Input
            id="alert-duration"
            type="number"
            value={duration}
            onChange={(e) => setDuration(parseInt(e.target.value) || 0)}
            min={0}
          />
          <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
            {isLogMatchRule
              ? `Also the log-matching window: fires once at least the threshold count of matching lines land in the trailing ${formatDuration(duration)}.`
              : `How long the condition must hold before firing: ${formatDuration(duration)}.`}
          </p>
        </div>

        {/* Severity */}
        <div>
          <Label htmlFor="alert-severity">Severity</Label>
          <Select
            id="alert-severity"
            value={severity}
            onChange={(e) => setSeverity(e.target.value as AlertRule["severity"])}
          >
            {SEVERITIES.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </Select>
        </div>

        {/* Targeting — agent_id/target_tags don't apply to check_down/
            cert_expiry rules, which target a "checks" record directly via
            the check selector below instead. */}
        {isCheckRule ? (
          <div>
            <Label htmlFor="alert-check">Check</Label>
            <Select id="alert-check" value={checkId} onChange={(e) => setCheckId(e.target.value)}>
              <option value="">All checks</option>
              {checks.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </Select>
            <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
              Leave as "All checks" to fire one alert per breaching check.
            </p>
          </div>
        ) : (
          <div>
            <FieldCaption>Applies to</FieldCaption>
            <div className="mb-2 flex flex-wrap gap-4">
              {(
                [
                  { value: "all", label: "All agents" },
                  { value: "tags", label: "Agents with tags" },
                  { value: "agent", label: "One agent" },
                ] as const
              ).map((opt) => (
                <label key={opt.value} className="flex cursor-pointer items-center gap-1.5 text-sm">
                  <input
                    type="radio"
                    name="alert-targeting-mode"
                    value={opt.value}
                    checked={targetingMode === opt.value}
                    onChange={() => handleTargetingModeChange(opt.value)}
                    className="border-[var(--color-line)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                  />
                  <span className="text-[var(--color-ink)]">{opt.label}</span>
                </label>
              ))}
            </div>

            {targetingMode === "tags" && (
              <div>
                <TagInput
                  value={targetTags}
                  onChange={setTargetTags}
                  suggestions={tagSuggestions}
                  placeholder="Add a tag…"
                  aria-label="Target tags"
                />
                <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
                  Applies to every agent with any of these tags.
                </p>
                {errors.targetTags && (
                  <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.targetTags}</p>
                )}
              </div>
            )}

            {targetingMode === "agent" && (
              <div>
                <Select
                  aria-label="Target agent"
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
          </div>
        )}

        {/* Notification Channels */}
        <div>
          <FieldCaption>Notification channels</FieldCaption>
          {channels.length === 0 ? (
            <p className="text-xs text-[var(--color-ink-faint)]">
              No enabled channels available. Configure them in Settings.
            </p>
          ) : (
            <div className="space-y-2">
              {channels.map((ch) => (
                <label key={ch.id} className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={selectedChannels.includes(ch.id)}
                    onChange={() => toggleChannel(ch.id)}
                    className="rounded border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                  />
                  <span className="text-sm text-[var(--color-ink)]">{ch.name}</span>
                  <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-xs text-[var(--color-ink-faint)]">
                    {ch.type}
                  </span>
                </label>
              ))}
            </div>
          )}
        </div>

        {/* Escalation — collapsible, since most rules don't need it */}
        <div className="rounded-[var(--radius-control)] border border-[var(--color-line)]">
          <button
            type="button"
            onClick={() => setEscalationOpen((o) => !o)}
            aria-expanded={escalationOpen}
            className="flex w-full items-center justify-between px-3 py-2.5 text-left text-sm font-medium text-[var(--color-ink)]"
          >
            <span className="flex items-center gap-2">
              Escalation
              {escalationAfter > 0 && (
                <span className="rounded-[var(--radius-chip)] bg-[var(--color-panel-raised)] px-1.5 py-0.5 text-xs font-normal text-[var(--color-ink-muted)]">
                  After {formatDuration(escalationAfter)}
                </span>
              )}
            </span>
            {escalationOpen ? (
              <ChevronDown className="h-4 w-4 text-[var(--color-ink-faint)]" aria-hidden="true" />
            ) : (
              <ChevronRight className="h-4 w-4 text-[var(--color-ink-faint)]" aria-hidden="true" />
            )}
          </button>

          {escalationOpen && (
            <div className="space-y-3 border-t border-[var(--color-line)] p-3">
              <div>
                <FieldCaption>Escalate after</FieldCaption>
                <div className="flex flex-wrap gap-1.5">
                  {ESCALATION_PRESETS.map((p) => (
                    <button
                      key={p.value}
                      type="button"
                      onClick={() => setEscalationAfter(p.value)}
                      className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                        escalationAfter === p.value
                          ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                          : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                      }`}
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
                <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
                  Dispatches once to the channels below if the alert is still firing,
                  unacknowledged, and unsilenced after this long.
                </p>
              </div>

              {escalationAfter > 0 && (
                <div>
                  <FieldCaption>Escalation channels</FieldCaption>
                  {channels.length === 0 ? (
                    <p className="text-xs text-[var(--color-ink-faint)]">
                      No enabled channels available.
                    </p>
                  ) : (
                    <div className="space-y-2">
                      {channels.map((ch) => (
                        <label key={ch.id} className="flex cursor-pointer items-center gap-2">
                          <input
                            type="checkbox"
                            checked={escalationChannels.includes(ch.id)}
                            onChange={() => toggleEscalationChannel(ch.id)}
                            className="rounded border-[var(--color-line)] bg-[var(--color-void)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                          />
                          <span className="text-sm text-[var(--color-ink)]">{ch.name}</span>
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Enabled Toggle */}
        <div className="flex items-center gap-2">
          <Toggle checked={enabled} onChange={setEnabled} label="Rule enabled" />
          <span className="text-sm text-[var(--color-ink-muted)]">Enabled</span>
        </div>

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Saving…" : rule ? "Save changes" : "Create rule"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
