const BASE = "/api";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init);
  const body = await res.text();
  if (!res.ok) {
    let msg = body;
    try {
      msg = JSON.parse(body).error ?? body;
    } catch {
      /* keep raw body */
    }
    throw new Error(msg || `HTTP ${res.status}`);
  }
  return (body ? JSON.parse(body) : null) as T;
}

function post<T>(path: string, payload: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export interface Scope {
  conn: string;
  vhost: string;
}

export interface ConnectionInfo {
  name: string;
  mgmt_url: string;
  default: boolean;
}

export interface RateDetails {
  rate: number;
}

export interface MessageStats {
  publish: number;
  publish_details?: RateDetails | null;
  deliver_get: number;
  deliver_details?: RateDetails | null;
  ack: number;
  ack_details?: RateDetails | null;
}

export interface Queue {
  name: string;
  vhost: string;
  type: string;
  durable: boolean;
  auto_delete: boolean;
  arguments: Record<string, unknown>;
  messages: number;
  messages_ready: number;
  messages_unacknowledged: number;
  consumers: number;
  message_stats?: MessageStats | null;
  state: string;
}

export interface QueueTotal {
  name: string;
  age_sec: number;
  head_time: string | null;
  source: string;
}

export interface DeathStep {
  queue: string;
  reason: string;
  time: string;
  exchange: string;
  count: number;
}

export interface PeekedMessage {
  payload: string;
  payload_encoding: string;
  payload_bytes: number;
  redelivered: boolean;
  exchange: string;
  routing_key: string;
  message_count: number;
  properties: {
    content_type?: string;
    timestamp?: { timestamp: number } | string;
    headers?: Record<string, unknown>;
    [key: string]: unknown;
  };
}

export interface Overview {
  rabbitmq_version: string;
  cluster_name: string;
  node: string;
  queues: number;
  queue_totals?: { messages: number; messages_ready: number; messages_unacknowledged: number };
  object_totals?: { consumers: number; connections: number; channels: number; exchanges: number };
}

export interface VHost {
  name: string;
}

export interface ReplaySpec {
  vhost: string;
  source_queue: string;
  target_queue?: string;
  target_exchange?: string;
  target_routing_key?: string;
  limit: number;
  dry_run: boolean;
  strip_x_death: boolean;
  filters?: { kind: string; value: string; header?: string }[];
}

export interface ReplayJob {
  id: string;
  spec: ReplaySpec;
  status: "running" | "completed" | "cancelled" | "failed";
  progress: { processed: number; succeeded: number; skipped: number; failed: number };
  started_at: string;
  ended_at?: string | null;
  errors?: string[];
}

export interface Exchange {
  name: string;
  vhost: string;
  type: string;
  durable: boolean;
  internal: boolean;
}

export interface Binding {
  source: string;
  destination: string;
  destination_type: string;
  routing_key: string;
  arguments: Record<string, unknown>;
}

export interface PublishSpec {
  conn: string;
  vhost: string;
  exchange: string;
  routing_key: string;
  body: string;
  content_type: string;
  headers: Record<string, string>;
  delivery_mode: number;
  priority: number;
  confirm: boolean;
}

export const api = {
  connections: () => request<ConnectionInfo[]>("/connections"),
  overview: (conn: string) => request<Overview>(`/overview?conn=${encodeURIComponent(conn)}`),
  vhosts: (conn: string) => request<VHost[]>(`/vhosts?conn=${encodeURIComponent(conn)}`),
  queues: (conn: string, vhost: string) =>
    request<Queue[]>(`/queues?conn=${encodeURIComponent(conn)}&vhost=${encodeURIComponent(vhost)}`),
  queue: (conn: string, vhost: string, name: string) =>
    request<Queue>(`/queue?conn=${encodeURIComponent(conn)}&vhost=${encodeURIComponent(vhost)}&name=${encodeURIComponent(name)}`),
  exchanges: (conn: string, vhost: string) =>
    request<Exchange[]>(`/exchanges?conn=${encodeURIComponent(conn)}&vhost=${encodeURIComponent(vhost)}`),
  bindings: (conn: string, vhost: string) =>
    request<Binding[]>(`/bindings?conn=${encodeURIComponent(conn)}&vhost=${encodeURIComponent(vhost)}`),
  peek: (conn: string, vhost: string, name: string, count: number) =>
    post<PeekedMessage[]>("/queues/peek", { conn, vhost, name, count }),
  ages: (conn: string, vhost: string, names: string[]) =>
    post<QueueTotal[]>("/queues/ages", { conn, vhost, names }),
  purge: (conn: string, vhost: string, name: string) =>
    post<{ status: string }>("/queues/purge", { conn, vhost, name }),
  replay: (spec: ReplaySpec & { conn: string }) =>
    post<{ job_id: string }>("/replay", spec),
  replayJob: (id: string) => request<ReplayJob>(`/replay/jobs/${id}`),
  replayJobs: () => request<ReplayJob[]>("/replay/jobs"),
  cancelReplay: (id: string) => post<{ status: string }>(`/replay/jobs/${id}/cancel`, {}),
  publish: (spec: PublishSpec) => post<{ status: string }>("/publish", spec),
};

export function extractDeathChain(msg: PeekedMessage): DeathStep[] {
  const raw = msg.properties?.headers?.["x-death"];
  if (!Array.isArray(raw)) return [];
  return raw as DeathStep[];
}

export function deathCount(msg: PeekedMessage): number {
  return extractDeathChain(msg).reduce((acc, s) => acc + (s.count ?? 0), 0);
}

export function originQueue(msg: PeekedMessage): string {
  const chain = extractDeathChain(msg);
  return chain.length ? chain[chain.length - 1].queue : "";
}