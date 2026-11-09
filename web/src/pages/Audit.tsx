import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type AuditEntry, type AuditQuery } from "../api";

function statusBadge(status: AuditEntry["status"]): string {
  switch (status) {
    case "completed":
      return "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300";
    case "failed":
      return "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300";
    case "cancelled":
      return "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300";
    default:
      return "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300";
  }
}

function operationBadge(operation: AuditEntry["operation"]): string {
  switch (operation) {
    case "replay":
      return "bg-purple-100 text-purple-800 dark:bg-purple-950 dark:text-purple-300";
    case "purge":
      return "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300";
    default:
      return "bg-zinc-100 text-zinc-800 dark:bg-zinc-800 dark:text-zinc-300";
  }
}

function targetLabel(e: AuditEntry): string {
  if (e.target_queue) return e.target_queue;
  if (e.target_exchange) {
    const key = e.target_routing_key ? ` [${e.target_routing_key}]` : "";
    return `${e.target_exchange}${key}`;
  }
  return "";
}

function filterChips(e: AuditEntry): string[] {
  return (e.filters ?? []).map((f) => (f.kind === "header" ? `header ${f.header}=${f.value}` : `${f.kind} "${f.value}"`));
}

function countSummary(e: AuditEntry): string {
  const parts: string[] = [];
  if (e.operation === "replay") {
    if (e.messages) parts.push(`${e.messages} processed`);
    if (e.succeeded) parts.push(`${e.succeeded} ok`);
    if (e.skipped) parts.push(`${e.skipped} skipped`);
    if (e.failed) parts.push(`${e.failed} failed`);
  }
  if (!parts.length && e.messages) parts.push(`${e.messages}`);
  return parts.join(", ");
}

export default function Audit() {
  const [operation, setOperation] = useState("");
  const [queue, setQueue] = useState("");
  const [user, setUser] = useState("");

  const query: AuditQuery = {};
  if (operation) query.operation = operation;
  if (queue) query.queue = queue;
  if (user) query.user = user;

  const { data: entries, isLoading, error } = useQuery({
    queryKey: ["audit", query],
    queryFn: () => api.audit(query),
    refetchInterval: 3000,
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-xl font-semibold">Audit log</h1>
        <span className="text-sm text-zinc-500">who ran which replay, purge or publish, and how many messages were affected</span>
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <select
          value={operation}
          onChange={(e) => setOperation(e.target.value)}
          className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
          title="Operation"
        >
          <option value="">All operations</option>
          <option value="replay">Replay</option>
          <option value="purge">Purge</option>
          <option value="publish">Publish</option>
        </select>
        <input
          value={queue}
          onChange={(e) => setQueue(e.target.value)}
          placeholder="Queue"
          className="w-40 rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
        />
        <input
          value={user}
          onChange={(e) => setUser(e.target.value)}
          placeholder="User"
          className="w-40 rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
        />
        {(operation || queue || user) && (
          <button
            onClick={() => {
              setOperation("");
              setQueue("");
              setUser("");
            }}
            className="rounded-md border border-zinc-300 px-2 py-1.5 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            Reset
          </button>
        )}
      </div>

      {error && (
        <div className="rounded-md bg-red-100 p-4 text-red-700 dark:bg-red-950 dark:text-red-300">
          {(error as Error).message}
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
        <table className="min-w-full divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
          <thead className="bg-zinc-50 text-left text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-zinc-900">
            <tr>
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">User</th>
              <th className="px-3 py-2">Operation</th>
              <th className="px-3 py-2">Queue</th>
              <th className="px-3 py-2">Target</th>
              <th className="px-3 py-2">Filters</th>
              <th className="px-3 py-2">Messages</th>
              <th className="px-3 py-2">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {isLoading && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-zinc-500">
                  Loading…
                </td>
              </tr>
            )}
            {!isLoading && !entries?.length && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-zinc-500">
                  No operations recorded yet. Replays, purges and publishes will appear here.
                </td>
              </tr>
            )}
            {(entries ?? []).map((e) => (
              <tr key={e.id} className="align-top" title={e.error ?? undefined}>
                <td className="whitespace-nowrap px-3 py-2 text-zinc-500">
                  {new Date(e.time).toLocaleString()}
                </td>
                <td className="px-3 py-2">
                  <div className="font-medium">{e.user}</div>
                  {e.conn && <div className="text-xs text-zinc-500">{e.conn}/“{e.vhost}”</div>}
                </td>
                <td className="px-3 py-2">
                  <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${operationBadge(e.operation)}`}>
                    {e.operation}
                  </span>
                  {e.dry_run && (
                    <span className="ml-1 rounded bg-zinc-100 px-1.5 py-0.5 text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">
                      dry-run
                    </span>
                  )}
                  {e.job_id && <div className="mt-1 font-mono text-xs text-zinc-400">{e.job_id}</div>}
                </td>
                <td className="px-3 py-2 font-mono text-xs">{e.queue || "—"}</td>
                <td className="px-3 py-2 font-mono text-xs">{targetLabel(e) || "—"}</td>
                <td className="px-3 py-2">
                  <div className="flex flex-wrap gap-1">
                    {filterChips(e).map((chip, i) => (
                      <span
                        key={i}
                        className="rounded bg-zinc-100 px-1.5 py-0.5 font-mono text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300"
                      >
                        {chip}
                      </span>
                    ))}
                  </div>
                </td>
                <td className="whitespace-nowrap px-3 py-2">{countSummary(e) || "—"}</td>
                <td className="whitespace-nowrap px-3 py-2">
                  <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${statusBadge(e.status)}`}>
                    {e.status}
                  </span>
                  {e.status === "failed" && e.error && (
                    <div className="mt-1 max-w-xs truncate font-mono text-xs text-red-600 dark:text-red-400">
                      {e.error}
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
