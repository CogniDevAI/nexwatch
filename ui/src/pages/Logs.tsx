import { LogsView } from "@/components/logs/LogsView";
import { PageHeader } from "@/components/ui/PageHeader";
import { usePageTitle } from "@/hooks/usePageTitle";

export function Logs() {
  usePageTitle("Logs");

  return (
    <div>
      <PageHeader
        title="Logs"
        description="Search and live-tail journald and file logs shipped from every agent."
      />
      <LogsView />
    </div>
  );
}
