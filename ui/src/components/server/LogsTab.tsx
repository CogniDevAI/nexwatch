import { LogsView } from "@/components/logs/LogsView";

interface LogsTabProps {
  agentId: string;
}

/** Host detail's "Logs" tab: the same log search/live-tail view as the
 *  standalone Logs page, scoped to this one agent. */
export function LogsTab({ agentId }: LogsTabProps) {
  return <LogsView agentId={agentId} />;
}
