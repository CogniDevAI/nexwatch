import { beforeEach, describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { useActiveAlerts } from "@/hooks/useActiveAlerts";
import { useAlertsStore } from "@/stores/alertsStore";
import { useAgentStore } from "@/stores/agentStore";
import { useChecksStore } from "@/stores/checksStore";
import type { Alert, AlertRule } from "@/types";

function makeAlert(overrides: Partial<Alert> = {}): Alert {
  return {
    id: "al1",
    rule_id: "r1",
    agent_id: "",
    check_id: "",
    status: "firing",
    value: 1,
    message: "m",
    fired_at: "2026-09-05 10:00:00.000Z",
    resolved_at: "",
    silenced: false,
    acknowledged_at: "",
    acknowledged_by: "",
    escalated_at: "",
    collectionId: "c1",
    collectionName: "alerts",
    created: "",
    updated: "",
    ...overrides,
  };
}

const rule: AlertRule = {
  id: "r1",
  name: "check-down-rule",
  metric_type: "check_down",
  condition: "",
  threshold: 0,
  target: "",
  target_tags: [],
  agent_id: "",
  check_id: "",
  duration: 0,
  severity: "critical",
  enabled: true,
  notification_channels: [],
  escalation_channels: [],
  escalation_after: 0,
  collectionId: "c2",
  collectionName: "alert_rules",
  created: "",
  updated: "",
};

beforeEach(() => {
  useAlertsStore.setState({ firingAlerts: [], rules: {}, loading: false, error: null });
  useAgentStore.setState({ agents: [] });
  useChecksStore.setState({ checks: [], loading: false, error: null });
});

describe("useActiveAlerts — check-based alerts", () => {
  it("groups two different breaching checks under the same rule into two rows, not one", () => {
    useAlertsStore.setState({
      firingAlerts: [
        makeAlert({ id: "al1", check_id: "check-a" }),
        makeAlert({ id: "al2", check_id: "check-b" }),
      ],
      rules: { r1: rule },
    });

    const { result } = renderHook(() => useActiveAlerts());

    expect(result.current.alerts).toHaveLength(2);
    const checkIds = result.current.alerts.map((a) => a.checkId).sort();
    expect(checkIds).toEqual(["check-a", "check-b"]);
  });

  it("resolves the check's name/target and leaves agentName empty for a check-based alert", () => {
    useChecksStore.setState({
      checks: [
        {
          id: "check-a",
          name: "billing-api",
          type: "http",
          target: "https://billing.internal/health",
          interval_seconds: 30,
          timeout_seconds: 5,
          method: "GET",
          expected_status: 200,
          expected_body_contains: "",
          verify_tls: true,
          tls_expiry_warn_days: 14,
          failures_before_down: 2,
          enabled: true,
          tags: [],
          collectionId: "c3",
          collectionName: "checks",
          created: "",
          updated: "",
        },
      ],
      loading: false,
      error: null,
    });
    useAlertsStore.setState({
      firingAlerts: [makeAlert({ check_id: "check-a" })],
      rules: { r1: rule },
    });

    const { result } = renderHook(() => useActiveAlerts());

    expect(result.current.alerts).toHaveLength(1);
    const [alert] = result.current.alerts;
    expect(alert.checkName).toBe("billing-api");
    expect(alert.checkTarget).toBe("https://billing.internal/health");
    expect(alert.agentId).toBe("");
    expect(alert.agentName).toBe("");
  });

  it("still groups duplicate records for the exact same check into one row with a count", () => {
    useAlertsStore.setState({
      firingAlerts: [
        makeAlert({ id: "al1", check_id: "check-a", fired_at: "2026-09-05 10:00:00.000Z" }),
        makeAlert({ id: "al2", check_id: "check-a", fired_at: "2026-09-05 10:05:00.000Z" }),
      ],
      rules: { r1: rule },
    });

    const { result } = renderHook(() => useActiveAlerts());

    expect(result.current.alerts).toHaveLength(1);
    expect(result.current.alerts[0].count).toBe(2);
    // The most-recently-fired record's id is the representative one.
    expect(result.current.alerts[0].id).toBe("al2");
  });
});
