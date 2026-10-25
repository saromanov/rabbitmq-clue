import { useQuery } from "@tanstack/react-query";
import { api, type Scope } from "../api";

function Stat({ label, value, accent }: { label: string; value: string | number; accent?: boolean }) {
  return (
    <div className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="text-xs uppercase tracking-wide text-zinc-500">{label}</div>
      <div className={`mt-1 text-2xl font-semibold ${accent ? "text-orange-600 dark:text-orange-400" : ""}`}>
        {value}
      </div>
    </div>
  );
}

export default function Overview({ scope, onOpenQueues }: { scope: Scope; onOpenQueues: () => void }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["overview", scope.conn],
    queryFn: () => api.overview(scope.conn),
    enabled: scope.conn !== "",
    refetchInterval: 5000,
  });

  if (error) {
    return <div className="rounded-md bg-red-100 p-4 text-red-700 dark:bg-red-950 dark:text-red-300">{(error as Error).message}</div>;
  }
  if (isLoading || !data) {
    return <div className="text-zinc-500">Loading broker overview…</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-baseline gap-4">
        <h1 className="text-xl font-semibold">{data.cluster_name || "RabbitMQ cluster"}</h1>
        <span className="rounded bg-zinc-200 px-2 py-0.5 text-xs dark:bg-zinc-800">v{data.rabbitmq_version}</span>
        <span className="text-sm text-zinc-500">{data.node}</span>
      </div>
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4 lg:grid-cols-7">
        <Stat label="Queues" value={data.queues} accent />
        <Stat label="Messages" value={data.queue_totals?.messages ?? 0} />
        <Stat label="Ready" value={data.queue_totals?.messages_ready ?? 0} />
        <Stat label="Unacked" value={data.queue_totals?.messages_unacknowledged ?? 0} accent />
        <Stat label="Consumers" value={data.object_totals?.consumers ?? 0} />
        <Stat label="Connections" value={data.object_totals?.connections ?? 0} />
        <Stat label="Channels" value={data.object_totals?.channels ?? 0} />
      </div>
      <button
        onClick={onOpenQueues}
        className="rounded-md bg-orange-600 px-4 py-2 text-sm font-medium text-white hover:bg-orange-700"
      >
        Browse queues
      </button>
    </div>
  );
}