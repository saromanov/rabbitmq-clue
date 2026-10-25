import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Queue, type QueueTotal, type Scope } from "../api";
import QueueDetail from "./QueueDetail";

function rate(stats?: { deliver_details?: { rate: number } | null; publish_details?: { rate: number } | null } | null) {
  return stats ?? null;
}

function fmtAge(sec: number): string {
  if (sec < 60) return `${Math.round(sec)}s`;
  if (sec < 3600) return `${Math.round(sec / 60)}m`;
  if (sec < 86400) return `${Math.round(sec / 3600)}h`;
  return `${Math.round(sec / 86400)}d`;
}

type SortKey = "name" | "messages" | "messages_unacknowledged" | "consumers";

export default function Queues({
  scope,
  selectedQueue,
  onSelectQueue,
}: {
  scope: Scope;
  selectedQueue: string | null;
  onSelectQueue: (name: string | null) => void;
}) {
  const [sortKey, setSortKey] = useState<SortKey>("messages");
  const [showEmpty, setShowEmpty] = useState(true);
  const [ages, setAges] = useState<Record<string, QueueTotal>>({});
  const [agesLoading, setAgesLoading] = useState(false);

  const { data: queues, isLoading, error, refetch } = useQuery({
    queryKey: ["queues", scope.conn, scope.vhost],
    queryFn: () => api.queues(scope.conn, scope.vhost),
    enabled: scope.conn !== "",
    refetchInterval: 5000,
  });

  const visible = useMemo(() => {
    const list = (queues ?? []).filter((q) => showEmpty || q.messages > 0);
    return [...list].sort((a, b) => {
      if (sortKey === "name") return a.name.localeCompare(b.name);
      return (b[sortKey] as number) - (a[sortKey] as number);
    });
  }, [queues, showEmpty, sortKey]);

  async function estimateAges() {
    if (!queues?.length) return;
    setAgesLoading(true);
    try {
      const targets = visible.filter((q) => q.messages > 0).map((q) => q.name);
      const totals = await api.ages(scope.conn, scope.vhost, targets);
      const map: Record<string, QueueTotal> = {};
      for (const t of totals) map[t.name] = t;
      setAges(map);
    } finally {
      setAgesLoading(false);
    }
  }

  if (selectedQueue) {
    return <QueueDetail scope={scope} queueName={selectedQueue} onBack={() => onSelectQueue(null)} />;
  }

  if (error) {
    return <div className="rounded-md bg-red-100 p-4 text-red-700 dark:bg-red-950 dark:text-red-300">{(error as Error).message}</div>;
  }
  if (isLoading) {
    return <div className="text-zinc-500">Loading queues…</div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Queues</h1>
        <label className="flex items-center gap-1.5 text-sm text-zinc-600 dark:text-zinc-300">
          <input type="checkbox" checked={showEmpty} onChange={(e) => setShowEmpty(e.target.checked)} />
          show empty
        </label>
        <div className="ml-auto flex items-center gap-2">
          <button
            onClick={estimateAges}
            disabled={agesLoading}
            className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
            title="Peek the head message of each queue to estimate how old its backlog is"
          >
            {agesLoading ? "Estimating…" : "Estimate backlog age"}
          </button>
          <button
            onClick={() => refetch()}
            className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            Refresh
          </button>
        </div>
      </div>
      <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
        <table className="w-full text-sm">
          <thead className="bg-zinc-50 text-left text-xs uppercase tracking-wide text-zinc-500 dark:bg-zinc-900">
            <tr>
              <th className="cursor-pointer px-4 py-2.5" onClick={() => setSortKey("name")}>Queue</th>
              <th className="px-4 py-2.5">Type</th>
              <th className="cursor-pointer px-4 py-2.5 text-right" onClick={() => setSortKey("messages")}>Ready</th>
              <th className="cursor-pointer px-4 py-2.5 text-right" onClick={() => setSortKey("messages_unacknowledged")}>Unacked</th>
              <th className="cursor-pointer px-4 py-2.5 text-right" onClick={() => setSortKey("consumers")}>Consumers</th>
              <th className="px-4 py-2.5 text-right">Rates pub/del</th>
              <th className="px-4 py-2.5 text-right">Backlog age</th>
              <th className="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {visible.map((q: Queue) => {
              const rs = rate(q.message_stats);
              const age = ages[q.name];
              return (
                <tr key={q.name} className="hover:bg-zinc-50 dark:hover:bg-zinc-900">
                  <td className="px-4 py-2.5 font-medium">{q.name}</td>
                  <td className="px-4 py-2.5 text-zinc-500">{q.type || "classic"}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums">{q.messages_ready}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums">{q.messages_unacknowledged}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums">{q.consumers}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums text-zinc-500">
                    {rs?.publish_details?.rate ?? 0} / {rs?.deliver_details?.rate ?? 0}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {q.messages === 0 ? "—" : age ? fmtAge(age.age_sec) : ""}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      onClick={() => onSelectQueue(q.name)}
                      className="rounded-md bg-orange-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-orange-700"
                    >
                      Inspect
                    </button>
                  </td>
                </tr>
              );
            })}
            {visible.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-zinc-500">
                  No queues in this virtual host.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}