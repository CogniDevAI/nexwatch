import { useState } from "react";
import type { Check } from "@/types";
import pb from "@/lib/pocketbase";
import { Modal } from "@/components/ui/Modal";
import { Input, Select, Label, FieldCaption } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { Toggle } from "@/components/ui/Toggle";
import { TagInput } from "@/components/ui/TagInput";

interface CheckFormProps {
  check?: Check | null;
  onSave: () => void;
  onClose: () => void;
}

type CheckType = Check["type"];

const TYPE_OPTIONS: { value: CheckType; label: string }[] = [
  { value: "http", label: "HTTP" },
  { value: "tcp", label: "TCP" },
  { value: "icmp", label: "ICMP (ping)" },
];

const TARGET_PLACEHOLDER: Record<CheckType, string> = {
  http: "https://example.com/health",
  tcp: "10.0.0.5:5432",
  icmp: "10.0.0.5",
};

const TARGET_LABEL: Record<CheckType, string> = {
  http: "URL",
  tcp: "Host and port",
  icmp: "Hostname or IP",
};

const INTERVAL_PRESETS = [
  { label: "10 s", value: 10 },
  { label: "30 s", value: 30 },
  { label: "1 min", value: 60 },
  { label: "5 min", value: 300 },
];

const TIMEOUT_PRESETS = [
  { label: "3 s", value: 3 },
  { label: "5 s", value: 5 },
  { label: "10 s", value: 10 },
  { label: "30 s", value: 30 },
];

interface FieldErrors {
  name?: string;
  target?: string;
  interval?: string;
  timeout?: string;
  expectedStatus?: string;
  failuresBeforeDown?: string;
}

export function CheckForm({ check, onSave, onClose }: CheckFormProps) {
  const [name, setName] = useState(check?.name ?? "");
  const [type, setType] = useState<CheckType>(check?.type ?? "http");
  const [target, setTarget] = useState(check?.target ?? "");
  const [intervalSeconds, setIntervalSeconds] = useState(check?.interval_seconds || 60);
  const [timeoutSeconds, setTimeoutSeconds] = useState(check?.timeout_seconds || 5);
  const [method, setMethod] = useState<Check["method"]>(check?.method || "GET");
  const [expectedStatus, setExpectedStatus] = useState(check?.expected_status || 200);
  const [expectedBodyContains, setExpectedBodyContains] = useState(
    check?.expected_body_contains ?? "",
  );
  const [verifyTls, setVerifyTls] = useState(check ? check.verify_tls : true);
  const [tlsExpiryWarnDays, setTlsExpiryWarnDays] = useState(check?.tls_expiry_warn_days || 14);
  const [failuresBeforeDown, setFailuresBeforeDown] = useState(check?.failures_before_down || 2);
  const [enabled, setEnabled] = useState(check?.enabled ?? true);
  const [tags, setTags] = useState<string[]>(check?.tags ?? []);

  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [errors, setErrors] = useState<FieldErrors>({});

  const isHTTP = type === "http";

  function validate(): FieldErrors {
    const next: FieldErrors = {};
    if (!name.trim()) next.name = "Enter a name for this check.";
    if (!target.trim()) next.target = `Enter a ${TARGET_LABEL[type].toLowerCase()}.`;
    if (intervalSeconds < 10) next.interval = "Interval must be at least 10 seconds.";
    if (timeoutSeconds < 1) next.timeout = "Timeout must be at least 1 second.";
    if (isHTTP && (expectedStatus < 100 || expectedStatus > 599)) {
      next.expectedStatus = "Enter a valid HTTP status code (100–599).";
    }
    if (failuresBeforeDown < 1) next.failuresBeforeDown = "Must be at least 1.";
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
      type,
      target: target.trim(),
      interval_seconds: intervalSeconds,
      timeout_seconds: timeoutSeconds,
      method: isHTTP ? method : "",
      expected_status: isHTTP ? expectedStatus : 0,
      expected_body_contains: isHTTP ? expectedBodyContains.trim() : "",
      verify_tls: isHTTP ? verifyTls : false,
      tls_expiry_warn_days: isHTTP ? tlsExpiryWarnDays : 0,
      failures_before_down: failuresBeforeDown,
      enabled,
      tags,
    };

    try {
      if (check) {
        await pb.collection("checks").update(check.id, data);
      } else {
        await pb.collection("checks").create(data);
      }
      onSave();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save check");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title={check ? "Edit check" : "New check"} onClose={onClose}>
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
          <Label htmlFor="check-name">Name</Label>
          <Input
            id="check-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g., Billing API health"
          />
          {errors.name && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.name}</p>
          )}
        </div>

        <div>
          <FieldCaption>Type</FieldCaption>
          <div className="flex flex-wrap gap-4">
            {TYPE_OPTIONS.map((opt) => (
              <label key={opt.value} className="flex cursor-pointer items-center gap-1.5 text-sm">
                <input
                  type="radio"
                  name="check-type"
                  value={opt.value}
                  checked={type === opt.value}
                  onChange={() => setType(opt.value)}
                  className="border-[var(--color-line)] text-[var(--color-signal)] focus:ring-[var(--color-signal)]"
                />
                <span className="text-[var(--color-ink)]">{opt.label}</span>
              </label>
            ))}
          </div>
        </div>

        <div>
          <Label htmlFor="check-target">{TARGET_LABEL[type]}</Label>
          <Input
            id="check-target"
            type="text"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder={TARGET_PLACEHOLDER[type]}
          />
          {errors.target && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.target}</p>
          )}
        </div>

        {isHTTP && (
          <div>
            <Label htmlFor="check-method">Method</Label>
            <Select
              id="check-method"
              value={method}
              onChange={(e) => setMethod(e.target.value as Check["method"])}
              className="w-32"
            >
              <option value="GET">GET</option>
              <option value="HEAD">HEAD</option>
            </Select>
          </div>
        )}

        <div>
          <Label htmlFor="check-interval">Check interval</Label>
          <div className="mb-2 flex flex-wrap gap-1.5">
            {INTERVAL_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setIntervalSeconds(p.value)}
                className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                  intervalSeconds === p.value
                    ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
          <Input
            id="check-interval"
            type="number"
            min={10}
            value={intervalSeconds}
            onChange={(e) => setIntervalSeconds(parseInt(e.target.value) || 0)}
          />
          {errors.interval && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.interval}</p>
          )}
        </div>

        <div>
          <Label htmlFor="check-timeout">Timeout</Label>
          <div className="mb-2 flex flex-wrap gap-1.5">
            {TIMEOUT_PRESETS.map((p) => (
              <button
                key={p.value}
                type="button"
                onClick={() => setTimeoutSeconds(p.value)}
                className={`rounded-[var(--radius-chip)] border px-2.5 py-1 text-xs font-medium transition-colors ${
                  timeoutSeconds === p.value
                    ? "border-[var(--color-signal)]/40 bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
                    : "border-[var(--color-line)] text-[var(--color-ink-muted)] hover:border-[var(--color-ink-faint)] hover:text-[var(--color-ink)]"
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
          <Input
            id="check-timeout"
            type="number"
            min={1}
            value={timeoutSeconds}
            onChange={(e) => setTimeoutSeconds(parseInt(e.target.value) || 0)}
          />
          {errors.timeout && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.timeout}</p>
          )}
        </div>

        {isHTTP && (
          <>
            <div>
              <Label htmlFor="check-expected-status">Expected status code</Label>
              <Input
                id="check-expected-status"
                type="number"
                value={expectedStatus}
                onChange={(e) => setExpectedStatus(parseInt(e.target.value) || 0)}
                className="max-w-32"
              />
              {errors.expectedStatus && (
                <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.expectedStatus}</p>
              )}
            </div>

            <div>
              <Label htmlFor="check-body-contains">Response body contains (optional)</Label>
              <Input
                id="check-body-contains"
                type="text"
                value={expectedBodyContains}
                onChange={(e) => setExpectedBodyContains(e.target.value)}
                placeholder="e.g., “ok”"
              />
            </div>

            <div className="flex items-center gap-2">
              <Toggle checked={verifyTls} onChange={setVerifyTls} label="Verify TLS certificate" />
              <span className="text-sm text-[var(--color-ink-muted)]">Verify TLS certificate</span>
            </div>

            <div>
              <Label htmlFor="check-warn-days">Warn when certificate expires within (days)</Label>
              <Input
                id="check-warn-days"
                type="number"
                min={1}
                value={tlsExpiryWarnDays}
                onChange={(e) => setTlsExpiryWarnDays(parseInt(e.target.value) || 0)}
                className="max-w-32"
              />
            </div>
          </>
        )}

        <div>
          <Label htmlFor="check-failures-before-down">Failures before marked down</Label>
          <Input
            id="check-failures-before-down"
            type="number"
            min={1}
            value={failuresBeforeDown}
            onChange={(e) => setFailuresBeforeDown(parseInt(e.target.value) || 0)}
            className="max-w-32"
          />
          <p className="mt-1 text-xs text-[var(--color-ink-faint)]">
            Consecutive failed attempts required before this check flips to down.
          </p>
          {errors.failuresBeforeDown && (
            <p className="mt-1 text-xs text-[var(--color-critical)]">{errors.failuresBeforeDown}</p>
          )}
        </div>

        <div>
          <FieldCaption>Tags</FieldCaption>
          <TagInput value={tags} onChange={setTags} placeholder="Add a tag…" aria-label="Tags" />
        </div>

        <div className="flex items-center gap-2">
          <Toggle checked={enabled} onChange={setEnabled} label="Check enabled" />
          <span className="text-sm text-[var(--color-ink-muted)]">Enabled</span>
        </div>

        <div className="flex items-center justify-end gap-3 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Saving…" : check ? "Save changes" : "Create check"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
