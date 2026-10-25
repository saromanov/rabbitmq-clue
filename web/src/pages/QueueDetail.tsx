import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  deathCount,
  extractDeathChain,
  originQueue,
  type PeekedMessage,
  type Scope,
} from "../api";

function pretty(msg: PeekedMessage): string {
  try {
    if (msg.payload_encoding === "base64") {
      return atob(msg.payload);
    }
    const parsed = JSON.parse(msg.payload);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return msg.payload;
  }
}

function timeOf(msg: PeekedMessage): string | null {
  const ts = msg.properties?.timestamp;
  if (typeof ts === "string") return ts;
  if (ts && typeof ts === "object" && "timestamp" in ts) {
    return new Date(Number(ts.timestamp) * 1000).toISOString();
  }
  const chain = extractDeathChain(msg);
  if (chain.length) {
    const last = chain.find((s) => s.time);
    return last?.time ?? null;
  }
  return null;
}

export default function QueueDetail({
  scope,
  queueName,
  onBack,
}: {
  scope: Scope;
  queueName: string;
  onBack: () => void;
}) {
  const qc = useQueryClient();
  const [peekCount, setPeekCount] = useState(20);
  const [selected, setSelected] = useState<number | null>(null);

  const [replayOpen, setReplayOpen] = useState(false);
  const [targetQueue, setTargetQueue] = useState("");
  const [limit, setLimit] = useState("0");
  const [dryRun, setDryRun] = useState(true);
  const [stripXDeath, setStripXDeath] = useState(false);
  const [filterKind, setFilterKind] = useState("none");
  const [filterHeader, setFilterHeader] = useState("");
  const [filterValue, setFilterValue] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);

  const { data: queue } = useQuery({
    queryKey: ["queue", scope.conn, scope.vhost, queueName],
    queryFn: () => api.queue(scope.conn, scope.vhost, queueName),
    refetchInterval: 3000,
  });

  const { data: messages, error: peekError, refetch: refetchPeek, isFetching } = useQuery({
    queryKey: ["peek", scope.conn, scope.vhost, queueName, peekCount],
    queryFn: () => api.peek(scope.conn, scope.vhost, queueName, peekCount),
    enabled: false,
  });

  const { data: job } = useQuery({
    queryKey: ["job", jobId],
    queryFn: () => api.replayJob(jobId!),
    enabled: jobId !== null,
    refetchInterval: (q) => (q.state.data?.status === "running" ? 500 : false),
  });

  const purge = useMutation({
    mutationFn: () => api.purge(scope.conn, scope.vhost, queueName),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["queues"] });
      qc.invalidateQueries({ queryKey: ["queue", scope.conn, scope.vhost, queueName] });
    },
  });

  const startReplay = useMutation({
    mutationFn: () => {
      const filters =
        filterKind === "none"
          ? undefined
          : filterKind === "header"
            ? [{ kind: "header", header: filterHeader, value: filterValue }]
            : [{ kind: "substring", value: filterValue }];
      return api.replay({
        conn: scope.conn,
        vhost: scope.vhost,
        source_queue: queueName,
        target_queue: targetQueue,
        limit: Number(limit) || 0,
        dry_run: dryRun,
        strip_x_death: stripXDeath,
        filters,
      });
    },
    onSuccess: (res) => setJobId(res.job_id),
  });

  const republish = useMutation({
    mutationFn: (msg: PeekedMessage) =>
      api.publish({
        conn: scope.conn,
        vhost: scope.vhost,
        exchange: msg.exchange,
        routing_key: msg.routing_key,
        body: pretty(msg),
        content_type: String(msg.properties?.content_type ?? ""),
        headers: {},
        delivery_mode: 2,
        priority: 0,
        confirm: true,
      }),
  });

  const firstMessage = messages?.[0];
  const detectedOrigin = firstMessage ? originQueue(firstMessage) : "";
  const suggestTarget = detectedOrigin && detectedOrigin !== queueName ? detectedOrigin : "";

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <button onClick={onBack} className="text-sm text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200">
          ← All queues
        </button>
        <h1 className="text-xl font-semibold">{queueName}</h1>
        <span className="rounded bg-zinc-200 px-2 py-0.5 text-xs dark:bg-zinc-800">{queue?.type || "classic"}</span>
        <div className="ml-auto flex gap-2">
          <button
            onClick={() => setReplayOpen(true)}
            className="rounded-md bg-orange-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-orange-700"
          >
            Replay / move…
          </button>
          <button
            onClick={() => {
              if (confirm(`Purge all messages from ${queueName}?`)) purge.mutate();
            }}
            className="rounded-md border border-red-300 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950"
          >
            Purge
          </button>
        </div>
      </div>

      {purge.isError && (
        <div className="rounded-md bg-red-100 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
          Purge failed: {(purge.error as Error).message}
        </div>
      )}

      <div className="grid grid-cols-3 gap-4 md:grid-cols-6">
        {[
          ["Ready", queue?.messages_ready ?? 0],
          ["Unacked", queue?.messages_unacknowledged ?? 0],
          ["Total", queue?.messages ?? 0],
          ["Consumers", queue?.consumers ?? 0],
          ["Published", queue?.message_stats?.publish ?? 0],
          ["Delivered", queue?.message_stats?.deliver_get ?? 0],
        ].map(([label, value]) => (
          <div key={label as string} className="rounded-lg border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900">
            <div className="text-xs uppercase tracking-wide text-zinc-500">{label}</div>
            <div className="text-xl font-semibold tabular-nums">{value}</div>
          </div>
        ))}
      </div>

      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="font-semibold">Messages</h2>
          <select
            value={peekCount}
            onChange={(e) => setPeekCount(Number(e.target.value))}
            className="rounded-md border border-zinc-300 bg-white px-2 py-1 text-sm dark:border-zinc-700 dark:bg-zinc-800"
          >
            {[10, 20, 50, 100].map((n) => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
          <button
            onClick={() => refetchPeek()}
            disabled={isFetching}
            className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            {isFetching ? "Peeking…" : "Peek"}
          </button>
          <span className="text-xs text-zinc-500">
            peek re-queues messages; broker may reorder them
          </span>
        </div>

        {suggestTarget && (
          <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
            This looks like a dead-letter queue: messages originally came from{" "}
            <strong>{suggestOriginText(suggestTarget)}</strong>. Click Replay / move… to send them back.
          </div>
        )}

        {peekError && (
          <div className="rounded-md bg-red-100 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
            {(peekError as Error).message}
          </div>
        )}

        {messages && (
          <div className="grid gap-4 lg:grid-cols-2">
            <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
              <table className="w-full text-sm">
                <thead className="bg-zinc-50 text-left text-xs uppercase text-zinc-500 dark:bg-zinc-900">
                  <tr>
                    <th className="px-3 py-2">#</th>
                    <th className="px-3 py-2">Routing key</th>
                    <th className="px-3 py-2 text-right">Deaths</th>
                    <th className="px-3 py-2 text-right">Bytes</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                  {messages.map((m, i) => (
                    <tr
                      key={i}
                      onClick={() => setSelected(i)}
                      className={`cursor-pointer ${selected === i ? "bg-orange-50 dark:bg-orange-950" : "hover:bg-zinc-50 dark:hover:bg-zinc-900"}`}
                    >
                      <td className="px-3 py-2 tabular-nums text-zinc-500">{i}</td>
                      <td className="px-3 py-2 font-mono text-xs">{m.routing_key}</td>
                      <td className="px-3 py-2 text-right tabular-nums">
                        {deathCount(m) > 0 ? (
                          <span className="rounded bg-red-100 px-1.5 text-red-700 dark:bg-red-950 dark:text-red-300">
                            {deathCount(m)}
                          </span>
                        ) : "—"}
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums text-zinc-500">{m.payload_bytes}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {selected !== null && messages[selected] && (
              <MessageView
                msg={messages[selected]}
                onRepublish={() => republish.mutate(messages[selected])}
                republishing={republish.isPending}
                republishError={republish.isError ? (republish.error as Error).message : null}
              />
            )}
          </div>
        )}
      </section>

      {replayOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={() => setReplayOpen(false)}>
          <div className="w-full max-w-lg space-y-4 rounded-xl bg-white p-6 dark:bg-zinc-900" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold">Replay messages</h2>
            <div className="text-sm text-zinc-500">
              from <span className="font-mono text-zinc-800 dark:text-zinc-200">{queueName}</span>
            </div>
            <label className="block text-sm">
              Target queue
              <div className="mt-1 flex gap-2">
                <input
                  value={targetQueue}
                  onChange={(e) => setTargetQueue(e.target.value)}
                  placeholder={suggestTarget || "work"}
                  className="w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
                />
                {suggestTarget && (
                  <button
                    onClick={() => setTargetQueue(suggestTarget)}
                    className="whitespace-nowrap rounded-md border border-zinc-300 px-2 text-xs dark:border-zinc-700"
                  >
                    use {suggestTarget}
                  </button>
                )}
              </div>
            </label>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <label>
                Limit (0 = all)
                <input
                  value={limit}
                  onChange={(e) => setLimit(e.target.value)}
                  className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
                />
              </label>
              <label>
                Filter
                <select
                  value={filterKind}
                  onChange={(e) => setFilterKind(e.target.value)}
                  className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
                >
                  <option value="none">all messages</option>
                  <option value="substring">payload contains</option>
                  <option value="header">header equals</option>
                </select>
              </label>
            </div>
            {filterKind !== "none" && (
              <div className="grid grid-cols-2 gap-3 text-sm">
                {filterKind === "header" && (
                  <label>
                    Header name
                    <input
                      value={filterHeader}
                      onChange={(e) => setFilterHeader(e.target.value)}
                      className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
                    />
                  </label>
                )}
                <label className={filterKind === "substring" ? "col-span-2" : ""}>
                  {filterKind === "header" ? "Header value" : "Substring"}
                  <input
                    value={filterValue}
                    onChange={(e) => setFilterValue(e.target.value)}
                    className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
                  />
                </label>
              </div>
            )}
            <div className="flex gap-4 text-sm">
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} />
                dry run
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={stripXDeath} onChange={(e) => setStripXDeath(e.target.checked)} />
                strip x-death history
              </label>
            </div>
            {dryRun && (
              <div className="rounded-md bg-zinc-100 p-2.5 text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">
                Dry run counts matching messages without replaying anything; the scan moves messages to the tail of the queue.
              </div>
            )}
            {job && (
              <div className="rounded-md border border-zinc-200 p-3 text-sm dark:border-zinc-700">
                <div className="flex items-center justify-between">
                  <span className="font-medium">
                    Job {job.status} {job.spec.dry_run ? "(dry run)" : ""}
                  </span>
                  {job.status === "running" ? (
                    <button onClick={() => api.cancelReplay(job.id)} className="text-xs text-red-600 dark:text-red-400">
                      cancel
                    </button>
                  ) : (
                    <button onClick={() => setJobId(null)} className="text-xs text-zinc-500">
                      dismiss
                    </button>
                  )}
                </div>
                <div className="mt-1 grid grid-cols-4 gap-2 text-xs tabular-nums text-zinc-500">
                  <span>processed {job.progress.processed}</span>
                  <span>succeeded {job.progress.succeeded}</span>
                  <span>skipped {job.progress.skipped}</span>
                  <span>failed {job.progress.failed}</span>
                </div>
                {job.errors && job.errors.length > 0 && (
                  <pre className="mt-2 overflow-x-auto rounded bg-red-50 p-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">
                    {job.errors.join("\n")}
                  </pre>
                )}
              </div>
            )}
            <div className="flex justify-end gap-2">
              <button onClick={() => setReplayOpen(false)} className="rounded-md border border-zinc-300 px-4 py-2 text-sm dark:border-zinc-700">
                Close
              </button>
              <button
                onClick={() => startReplay.mutate()}
                disabled={!targetQueue || startReplay.isPending}
                className="rounded-md bg-orange-600 px-4 py-2 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
              >
                {startReplay.isPending ? "Starting…" : dryRun ? "Dry run" : "Replay"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function suggestOriginText(name: string): string {
  return name;
}

function MessageView({
  msg,
  onRepublish,
  republishing,
  republishError,
}: {
  msg: PeekedMessage;
  onRepublish: () => void;
  republishing: boolean;
  republishError: string | null;
}) {
  const chain = extractDeathChain(msg);
  return (
    <div className="space-y-3 rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
      <div className="flex items-center justify-between">
        <div className="text-xs text-zinc-500">
          exchange <span className="font-mono">{msg.exchange || "(default)"}</span> · key{" "}
          <span className="font-mono">{msg.routing_key}</span> · {timeOf(msg) ?? "no timestamp"}
        </div>
        <button
          onClick={onRepublish}
          disabled={republishing}
          className="rounded-md border border-zinc-300 px-2.5 py-1 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
          title="Republish this message to its original exchange/routing key"
        >
          {republishing ? "Republishing…" : "Republish"}
        </button>
      </div>
      {republishError && <div className="text-xs text-red-600 dark:text-red-400">{republishError}</div>}
      {chain.length > 0 && (
        <div>
          <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Dead-letter history</div>
          <ol className="space-y-1 text-xs">
            {chain.map((s, i) => (
              <li key={i} className="rounded bg-zinc-100 px-2 py-1 dark:bg-zinc-800">
                <span className="font-mono">{s.queue}</span> · {s.reason} · ×{s.count} · {s.time}
              </li>
            ))}
          </ol>
        </div>
      )}
      <div>
        <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Properties</div>
        <pre className="overflow-x-auto rounded bg-zinc-100 p-2 text-xs dark:bg-zinc-800">
          {JSON.stringify(msg.properties, null, 2)}
        </pre>
      </div>
      <div>
        <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Payload</div>
        <pre className="max-h-72 overflow-auto rounded bg-zinc-100 p-2 text-xs dark:bg-zinc-800">{pretty(msg)}</pre>
      </div>
    </div>
  );
}