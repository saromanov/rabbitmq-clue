import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { api, type PublishSpec, type Scope } from "../api";

export default function Publish({ scope }: { scope: Scope }) {
  const [exchange, setExchange] = useState("");
  const [routingKey, setRoutingKey] = useState("");
  const [body, setBody] = useState("{\n  \"hello\": \"world\"\n}");
  const [contentType, setContentType] = useState("application/json");
  const [headersRaw, setHeadersRaw] = useState("{}");
  const [deliveryMode, setDeliveryMode] = useState("2");
  const [priority, setPriority] = useState("0");

  const { data: exchanges } = useQuery({
    queryKey: ["exchanges", scope.conn, scope.vhost],
    queryFn: () => api.exchanges(scope.conn, scope.vhost),
    enabled: scope.conn !== "",
  });

  const publish = useMutation({
    mutationFn: (spec: PublishSpec) => api.publish(spec),
  });

  let headersError: string | null = null;
  let headers: Record<string, string> = {};
  try {
    headers = JSON.parse(headersRaw);
  } catch (e) {
    headersError = (e as Error).message;
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <h1 className="text-xl font-semibold">Publish message</h1>
      <p className="text-sm text-zinc-500">
        Publishes with publisher confirms enabled and waits for the broker to accept the message.
      </p>
      <div className="grid grid-cols-2 gap-4">
        <label className="block text-sm">
          Exchange
          <select
            value={exchange}
            onChange={(e) => setExchange(e.target.value)}
            className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
          >
            <option value="">(default — routing key = queue name)</option>
            {(exchanges ?? []).filter((x) => !x.internal).map((x) => (
              <option key={x.name} value={x.name}>
                {x.name} ({x.type})
              </option>
            ))}
          </select>
        </label>
        <label className="block text-sm">
          Routing key
          <input
            value={routingKey}
            onChange={(e) => setRoutingKey(e.target.value)}
            className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
          />
        </label>
        <label className="block text-sm">
          Content type
          <input
            value={contentType}
            onChange={(e) => setContentType(e.target.value)}
            className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
          />
        </label>
        <div className="grid grid-cols-2 gap-4">
          <label className="block text-sm">
            Delivery mode
            <select
              value={deliveryMode}
              onChange={(e) => setDeliveryMode(e.target.value)}
              className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
            >
              <option value="1">transient</option>
              <option value="2">persistent</option>
            </select>
          </label>
          <label className="block text-sm">
            Priority
            <input
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 dark:border-zinc-700 dark:bg-zinc-800"
            />
          </label>
        </div>
      </div>
      <label className="block text-sm">
        Headers (JSON)
        <input
          value={headersRaw}
          onChange={(e) => setHeadersRaw(e.target.value)}
          className={`mt-1 w-full rounded-md border bg-white px-3 py-2 font-mono text-xs dark:bg-zinc-800 ${
            headersError ? "border-red-400" : "border-zinc-300 dark:border-zinc-700"
          }`}
        />
        {headersError && <span className="text-xs text-red-600 dark:text-red-400">invalid JSON: {headersError}</span>}
      </label>
      <label className="block text-sm">
        Payload
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={10}
          className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs dark:border-zinc-700 dark:bg-zinc-800"
        />
      </label>
      <div className="flex items-center gap-3">
        <button
          onClick={() =>
            publish.mutate({
              conn: scope.conn,
              vhost: scope.vhost,
              exchange,
              routing_key: routingKey,
              body,
              content_type: contentType,
              headers,
              delivery_mode: Number(deliveryMode),
              priority: Number(priority),
              confirm: true,
            })
          }
          disabled={!!headersError || publish.isPending}
          className="rounded-md bg-orange-600 px-4 py-2 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
        >
          {publish.isPending ? "Publishing…" : "Publish"}
        </button>
        {publish.isSuccess && (
          <span className="text-sm text-green-700 dark:text-green-400">Message confirmed by broker</span>
        )}
        {publish.isError && (
          <span className="text-sm text-red-600 dark:text-red-400">{(publish.error as Error).message}</span>
        )}
      </div>
    </div>
  );
}