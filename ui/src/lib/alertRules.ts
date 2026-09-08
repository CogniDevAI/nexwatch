import type { AlertRule, Agent, Check } from "@/types";
import { formatDuration } from "@/lib/time";

const CONDITION_SYMBOLS: Record<AlertRule["condition"], string> = { gt: ">", lt: "<", eq: "=" };

const AVAILABILITY_LABELS: Record<string, string> = {
  process_down: "Process down",
  service_failed: "Service failed",
  agent_offline: "Agent offline",
  check_down: "Check down",
};

/** One-line summary of what a rule watches and, for process/service rules,
 *  its target: "cpu > 90", "Process down: nginx", "Agent offline",
 *  "Certificate expiring within 14d". */
export function ruleSummary(rule: AlertRule): string {
  if (rule.metric_type === "cert_expiry") {
    return `Certificate expiring within ${rule.threshold}d`;
  }
  if (rule.metric_type === "log_match") {
    return `Log match: "${rule.target}" (≥ ${rule.threshold})`;
  }
  const label = AVAILABILITY_LABELS[rule.metric_type];
  if (label) {
    return rule.target ? `${label}: ${rule.target}` : label;
  }
  const symbol = CONDITION_SYMBOLS[rule.condition] ?? rule.condition;
  return `${rule.metric_type} ${symbol} ${rule.threshold}`;
}

/** "All agents" / "Tags: web, db" / a hostname / "All checks" / a check
 *  name — which agents (or checks) a rule applies to. checksById is only
 *  needed for check_down/cert_expiry rules; omit it for pages that don't
 *  otherwise fetch checks. */
export function ruleTargetingSummary(
  rule: AlertRule,
  agentsById: Map<string, Agent>,
  checksById: Map<string, Check> = new Map(),
): string {
  if (rule.metric_type === "check_down" || rule.metric_type === "cert_expiry") {
    if (rule.check_id) {
      const check = checksById.get(rule.check_id);
      return check?.name || rule.check_id;
    }
    return "All checks";
  }
  if (rule.agent_id) {
    const agent = agentsById.get(rule.agent_id);
    return agent?.hostname || agent?.name || rule.agent_id;
  }
  if (rule.target_tags && rule.target_tags.length > 0) {
    return `Tags: ${rule.target_tags.join(", ")}`;
  }
  return "All agents";
}

/** "Off" or "After 30m (2 channels)". */
export function ruleEscalationSummary(rule: AlertRule): string {
  if (!rule.escalation_after) return "Off";
  const count = rule.escalation_channels?.length ?? 0;
  return `After ${formatDuration(rule.escalation_after)} (${count} channel${count === 1 ? "" : "s"})`;
}
