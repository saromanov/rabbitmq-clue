import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, type PublishSpec, type Scope } from "../api";
import { SchemaPayloadEditor, SchemaPicker } from "../components/SchemaPicker";

export default function Publish({ scope }: { scope: Scope }) {
  const [exchange, setExchange] = useState("");
  const [routingKey, setRoutingKey] = useState("");
  const [body, setBody] = useState("{\n  \"hello\": \"world\"\n}");
  const [contentType, setContentType] = useState("application/json");
  const [headersRaw, setHeadersRaw] = useState("{}");
  const [deliveryMode, setDeliveryMode] = useState("2");
  const [priority, setPriority] = useState("0");

  const [schemaId, setSchemaId] = useState("");
  const [schemaType, setSchemaType] = useState("");
  const [encoding, setEncoding] = useState("binary");
  const [wrapAny, setWrapAny] = useState(false);
  const [jsonProtoNames, setJsonProtoNames] = useState(false);
  const [payloadError, setPayloadError] = useState<string | null>(null);

  const { data: exchanges } = useQuery({
    queryKey: ["exchanges", scope.conn, scope.vhost],
    queryFn: () => api.exchanges(scope.conn, scope.vhost),
    enabled: scope.conn !== "",
  });

  const publish = useMutation({
    mutationFn: (spec: PublishSpec) => api.publish(spec),
  });

  const publishProto = useMutation({
    mutationFn: (payload: unknown) =>
      api.publishProto({
        conn: scope.conn,
        vhost: scope.vhost,
        schema_id: schemaId,
        message_type: schemaType,
        payload,
        encoding,
        wrap_any: wrapAny,
        json_proto_names: jsonProtoNames,
        exchange,
        routing_key: routingKey,
        headers: safeHeaders(headersRaw),
        delivery_mode: Number(deliveryMode),
        priority: Number(priority),
      }),
  });

  let headersError: string | null = null;
  try {
    safeHeaders(headersRaw);
  } catch (e) {
    headersError = (e as Error).message;
  }

  const publishDisabled =
    !!headersError || publish.isPending || publishProto.isPending ||
    (schemaId !== "" && !schemaType);

  const onPublish = () => {
    if (schemaId) {
      try {
        const payload = JSON.parse(body);
        setPayloadError(null);
        publishProto.mutate(payload);
      } catch (e) {
        setPayloadError((e as Error).message);
      }
      return;
    }
    publish.mutate({
      conn: scope.conn,
      vhost: scope.vhost,
      exchange,
      routing_key: routingKey,
      body,
      content_type: contentType,
      headers: safeHeaders(headersRaw),
      delivery_mode: Number(deliveryMode),
      priority: Number(priority),
      confirm: true,
    });
  };

  const lastError = publishProto.isError
    ? (publishProto.error as Error).message
    : publish.isError
      ? (publish.error as Error).message
      : payloadError;
  const succeeded = publish.isSuccess || publishProto.isSuccess;
  const pending = publish.isPending || publishProto.isPending;

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
            onChange={(e) => {
              setExchange(e.target.value);
              setSchemaId("");
              setSchemaType("");
            }}
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
            disabled={schemaId !== ""}
            className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-800"
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

      <div className="space-y-2 rounded-lg border border-zinc-200 p-3 dark:border-zinc-800">
        <div className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
          Protobuf schema{exchange ? ` for ${exchange}` : ""}
        </div>
        <SchemaPicker
          filterKind="exchange"
          filterName={exchange}
          selected={schemaId}
          onSelect={(id) => {
            setSchemaId(id);
            setSchemaType("");
          }}
          allowImport
        />
        {schemaId && (
          <>
            <SchemaPayloadEditor
              schemaId={schemaId}
              type={schemaType}
              onType={setSchemaType}
              onSkeleton={(sk) => setBody(sk)}
            />
            <div className="flex flex-wrap items-center gap-4 text-sm">
              <label className="flex items-center gap-2">
                Encoding
                <select
                  value={encoding}
                  onChange={(e) => setEncoding(e.target.value)}
                  className="rounded-md border border-zinc-300 bg-white px-2 py-1 dark:border-zinc-700 dark:bg-zinc-800"
                >
                  <option value="binary">binary proto</option>
                  <option value="json">JSON (protojson)</option>
                </select>
              </label>
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={wrapAny} onChange={(e) => setWrapAny(e.target.checked)} />
                wrap in google.protobuf.Any
              </label>
              {encoding === "json" && (
                <label className="flex items-center gap-2">
                  <input type="checkbox" checked={jsonProtoNames} onChange={(e) => setJsonProtoNames(e.target.checked)} />
                  snake_case names
                </label>
              )}
            </div>
          </>
        )}
      </div>

      <label className="block text-sm">
        {schemaId ? "Payload (protojson)" : "Payload"}
        <textarea
          value={body}
          onChange={(e) => {
            setBody(e.target.value);
            setPayloadError(null);
          }}
          rows={10}
          spellCheck={false}
          className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs dark:border-zinc-700 dark:bg-zinc-800"
        />
      </label>
      <div className="flex items-center gap-3">
        <button
          onClick={onPublish}
          disabled={publishDisabled}
          className="rounded-md bg-orange-600 px-4 py-2 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
        >
          {pending ? "Publishing…" : "Publish"}
        </button>
        {succeeded && (
          <span className="text-sm text-green-700 dark:text-green-400">Message confirmed by broker</span>
        )}
        {lastError && !pending && (
          <span className="text-sm text-red-600 dark:text-red-400">{lastError}</span>
        )}
      </div>
    </div>
  );
}

function safeHeaders(raw: string): Record<string, string> {
  const parsed = JSON.parse(raw);
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    return parsed as Record<string, string>;
  }
  throw new Error("headers must be a JSON object");
}
