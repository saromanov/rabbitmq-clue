import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  api,
  type ConnectionInfo,
  type TopologyBindingChange,
  type TopologyChangeKind,
  type TopologyPolicyChange,
  type TopologyQueueChange,
  type TopologyExchangeChange,
  type TopologyReport,
  type TopologySide,
  type TopologySnapshot,
} from "../api";

type SideState = {
  mode: "live" | "file";
  conn: string;
  vhost: string;
  snapshot: TopologySnapshot | null;
  fileName: string;
};

const emptySide = (): SideState => ({
  mode: "live",
  conn: "",
  vhost: "",
  snapshot: null,
  fileName: "",
});

const SNAPSHOT_FORMAT = "rabbitmq-clue.topology";

function kindStyle(kind: TopologyChangeKind): string {
  switch (kind) {
    case "added":
      return "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300";
    case "removed":
      return "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300";
    default:
      return "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300";
  }
}

function kindBadge(kind: TopologyChangeKind): string {
  switch (kind) {
    case "added":
      return "+";
    case "removed":
      return "-";
    default:
      return "~";
  }
}

function bindingLabel(c: TopologyBindingChange): string {
  const spec = c.from ?? c.to;
  if (!spec) return "?";
  return `${spec.source} → ${spec.destination} [${spec.routing_key || ""}]`;
}

function renderValue(v: unknown): string {
  if (v === null || v === undefined) return "(absent)";
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}

export default function Topology({ scope }: { scope: { conn: string; vhost: string } }) {
  const [from, setFrom] = useState<SideState>(emptySide);
  const [to, setTo] = useState<SideState>(emptySide);
  const [report, setReport] = useState<TopologyReport | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data: connections } = useQuery({ queryKey: ["connections"], queryFn: api.connections });

  const fromConn = from.conn || connections?.[0]?.name || "";
  const toConn = to.conn || connections?.[0]?.name || "";

  const { data: fromVhosts } = useQuery({
    queryKey: ["vhosts", fromConn],
    queryFn: () => api.vhosts(fromConn),
    enabled: from.mode === "live" && fromConn !== "",
  });
  const { data: toVhosts } = useQuery({
    queryKey: ["vhosts", toConn],
    queryFn: () => api.vhosts(toConn),
    enabled: to.mode === "live" && toConn !== "",
  });

  const fromVhost = from.mode === "live" ? from.vhost || fromVhosts?.[0]?.name || scope.vhost : "";
  const toVhost = to.mode === "live" ? to.vhost || toVhosts?.[0]?.name || scope.vhost : "";

  const sideReady = (s: SideState): boolean =>
    s.mode === "file" ? s.snapshot !== null : (s.conn || connections?.[0]?.name) !== "";

  async function loadFile(side: "from" | "to", file: File) {
    setError(null);
    try {
      const parsed = JSON.parse(await file.text()) as TopologySnapshot;
      if (parsed.format !== SNAPSHOT_FORMAT || typeof parsed.version !== "number") {
        throw new Error(`not a ${SNAPSHOT_FORMAT} document`);
      }
      const patch = { mode: "file" as const, snapshot: parsed, fileName: file.name };
      if (side === "from") setFrom((s) => ({ ...s, ...patch }));
      else setTo((s) => ({ ...s, ...patch }));
    } catch (e) {
      setError(`from file ${(e as Error).message}`);
    }
  }

  async function exportSnapshot(side: "from" | "to") {
    setError(null);
    const conn = side === "from" ? fromConn : toConn;
    const vhost = side === "from" ? fromVhost : toVhost;
    try {
      const snap = await api.topologySnapshot(conn, vhost);
      const name = `topology_${conn}_${vhost.replace(/\//g, "") || "default"}.json`;
      const url = URL.createObjectURL(new Blob([JSON.stringify(snap, null, 2)], { type: "application/json" }));
      const a = document.createElement("a");
      a.href = url;
      a.download = name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function runDiff() {
    setRunning(true);
    setError(null);
    try {
      const toSide = (s: SideState, conn: string, vhost: string): TopologySide =>
        s.mode === "file" && s.snapshot
          ? { snapshot: s.snapshot }
          : { conn: s.conn || conn, vhost: s.vhost || vhost };
      const result = await api.topologyDiff(toSide(from, fromConn, fromVhost), toSide(to, toConn, toVhost));
      setReport(result);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRunning(false);
    }
  }

  function SideEditor({ side, state, setState, vhosts, conn }: {
    side: "from" | "to";
    state: SideState;
    setState: (fn: (s: SideState) => SideState) => void;
    vhosts?: { name: string }[];
    conn: string;
  }) {
    return (
      <div className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold uppercase tracking-wide text-zinc-500">
            {side === "from" ? "From (A)" : "To (B)"}
          </h3>
          <div className="flex gap-1 text-xs">
            {(["live", "file"] as const).map((m) => (
              <button
                key={m}
                onClick={() => setState((s) => ({ ...s, mode: m }))}
                className={`rounded px-2 py-1 ${state.mode === m
                  ? "bg-orange-600 text-white"
                  : "text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800"}`}
              >
                {m === "live" ? "Live broker" : "Snapshot file"}
              </button>
            ))}
          </div>
        </div>
        {state.mode === "live" ? (
          <div className="flex flex-wrap items-center gap-2">
            <select
              value={conn}
              onChange={(e) => setState((s) => ({ ...s, conn: e.target.value, vhost: "" }))}
              className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 text-sm dark:border-zinc-700 dark:bg-zinc-800"
              title="Connection"
            >
              {(connections ?? []).map((c: ConnectionInfo) => (
                <option key={c.name} value={c.name}>{c.name}</option>
              ))}
            </select>
            <select
              value={state.vhost || vhosts?.[0]?.name || ""}
              onChange={(e) => setState((s) => ({ ...s, vhost: e.target.value }))}
              className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 text-sm dark:border-zinc-700 dark:bg-zinc-800"
              title="Virtual host"
            >
              {(vhosts ?? []).map((v) => (
                <option key={v.name} value={v.name}>{v.name === "" ? "(empty vhost)" : v.name}</option>
              ))}
            </select>
            <button
              onClick={() => exportSnapshot(side)}
              className="rounded-md border border-zinc-300 px-2 py-1.5 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
              title="Download the topology snapshot as JSON"
            >
              Export snapshot
            </button>
          </div>
        ) : (
          <div className="space-y-2 text-sm">
            <input
              type="file"
              accept="application/json,.json"
              onChange={(e) => e.target.files?.[0] && loadFile(side, e.target.files[0])}
              className="block w-full text-sm"
            />
            {state.snapshot && (
              <div className="text-xs text-zinc-500">
                {state.fileName}: source “{state.snapshot.source}”, vhost “{state.snapshot.vhost}”,
                taken {new Date(state.snapshot.taken_at).toLocaleString()}
                <button onClick={() => setState((s) => ({ ...s, snapshot: null, fileName: "" }))} className="ml-2 text-orange-600 hover:underline">
                  remove
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Topology diff</h1>
        <button
          onClick={runDiff}
          disabled={running || !sideReady(from) || !sideReady(to)}
          className="rounded-md bg-orange-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
        >
          {running ? "Diffing…" : "Run diff"}
        </button>
        <button
          onClick={() => { setReport(null); setError(null); }}
          className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          Clear
        </button>
      </div>

      {error && (
        <div className="rounded-md bg-red-100 p-4 text-red-700 dark:bg-red-950 dark:text-red-300">{error}</div>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <SideEditor side="from" state={from} setState={setFrom} vhosts={fromVhosts} conn={fromConn} />
        <SideEditor side="to" state={to} setState={setTo} vhosts={toVhosts} conn={toConn} />
      </div>

      {report && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-zinc-200 p-4 text-sm dark:border-zinc-800">
            <span className="font-medium">{report.from} → {report.to}</span>
            <span className="text-zinc-500">[vhost {report.vhost}]</span>
            {report.summary.identical ? (
              <span className="rounded bg-green-100 px-2 py-0.5 text-green-800 dark:bg-green-950 dark:text-green-300">identical</span>
            ) : (
              <>
                <span className="rounded bg-green-100 px-2 py-0.5 text-green-800 dark:bg-green-950 dark:text-green-300">{report.summary.added} added</span>
                <span className="rounded bg-red-100 px-2 py-0.5 text-red-800 dark:bg-red-950 dark:text-red-300">{report.summary.removed} removed</span>
                <span className="rounded bg-amber-100 px-2 py-0.5 text-amber-800 dark:bg-amber-950 dark:text-amber-300">{report.summary.changed} changed</span>
              </>
            )}
          </div>

          {section("Queues", report.queues, (c) => c.name, (c) => c.fields)}
          {section("Exchanges", report.exchanges, (c) => c.name, (c) => c.fields)}
          {sectionBindings(report.bindings)}
          {sectionPolicies(report.policies)}
        </div>
      )}
    </div>
  );
}

function section(
  title: string,
  changes: TopologyQueueChange[] | TopologyExchangeChange[] | null | undefined,
  label: (c: TopologyQueueChange | TopologyExchangeChange) => string,
  fields: (c: TopologyQueueChange | TopologyExchangeChange) => { field: string; from: unknown; to: unknown }[] | null | undefined,
) {
  if (!changes?.length) return null;
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 dark:border-zinc-800">
      <div className="bg-zinc-50 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-zinc-900">
        {title} ({changes.length})
      </div>
      <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
        {changes.map((c, i) => (
          <div key={i} className="px-4 py-2 text-sm">
            <div className="flex items-center gap-2">
              <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${kindStyle(c.kind)}`}>{kindBadge(c.kind)}</span>
              <span className="font-medium">{label(c)}</span>
            </div>
            {(fields(c) ?? []).map((f, j) => (
              <div key={j} className="ml-8 mt-1 font-mono text-xs text-zinc-500">
                {f.field}: <span className="text-red-600 dark:text-red-400">{renderValue(f.from)}</span>
                {" → "}
                <span className="text-green-600 dark:text-green-400">{renderValue(f.to)}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function sectionBindings(changes: TopologyBindingChange[] | null | undefined) {
  if (!changes?.length) return null;
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 dark:border-zinc-800">
      <div className="bg-zinc-50 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-zinc-900">
        Bindings ({changes.length})
      </div>
      <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
        {changes.map((c, i) => (
          <div key={i} className="px-4 py-2 text-sm">
            <div className="flex items-center gap-2">
              <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${kindStyle(c.kind)}`}>{kindBadge(c.kind)}</span>
              <span className="font-medium">{bindingLabel(c)}</span>
            </div>
            {(c.fields ?? []).map((f, j) => (
              <div key={j} className="ml-8 mt-1 font-mono text-xs text-zinc-500">
                {f.field}: <span className="text-red-600 dark:text-red-400">{renderValue(f.from)}</span>
                {" → "}
                <span className="text-green-600 dark:text-green-400">{renderValue(f.to)}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function sectionPolicies(changes: TopologyPolicyChange[] | null | undefined) {
  if (!changes?.length) return null;
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 dark:border-zinc-800">
      <div className="bg-zinc-50 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-zinc-900">
        Policies ({changes.length})
      </div>
      <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
        {changes.map((c, i) => (
          <div key={i} className="px-4 py-2 text-sm">
            <div className="flex items-center gap-2">
              <span className={`rounded px-1.5 py-0.5 font-mono text-xs ${kindStyle(c.kind)}`}>{kindBadge(c.kind)}</span>
              <span className="font-medium">{c.name}</span>
            </div>
            {(c.fields ?? []).map((f, j) => (
              <div key={j} className="ml-8 mt-1 font-mono text-xs text-zinc-500">
                {f.field}: <span className="text-red-600 dark:text-red-400">{renderValue(f.from)}</span>
                {" → "}
                <span className="text-green-600 dark:text-green-400">{renderValue(f.to)}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}