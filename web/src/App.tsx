import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type ConnectionInfo, type Scope } from "./api";
import Overview from "./pages/Overview";
import Queues from "./pages/Queues";
import Publish from "./pages/Publish";
import Topology from "./pages/Topology";

export type Page = "overview" | "queues" | "publish" | "topology";

const navItems: { id: Page; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "queues", label: "Queues" },
  { id: "publish", label: "Publish" },
  { id: "topology", label: "Topology" },
];

export default function App() {
  const [page, setPage] = useState<Page>("overview");
  const [scope, setScope] = useState<Scope>({ conn: "", vhost: "" });
  const [dark, setDark] = useState(() => localStorage.getItem("rmq-clue-theme") !== "light");
  const [selectedQueue, setSelectedQueue] = useState<string | null>(null);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    localStorage.setItem("rmq-clue-theme", dark ? "dark" : "light");
  }, [dark]);

  const { data: connections } = useQuery({
    queryKey: ["connections"],
    queryFn: api.connections,
  });

  const connName = scope.conn || connections?.find((c: ConnectionInfo) => c.default)?.name || connections?.[0]?.name || "";

  const { data: vhosts } = useQuery({
    queryKey: ["vhosts", connName],
    queryFn: () => api.vhosts(connName),
    enabled: connName !== "",
  });

  const activeVhost = scope.vhost || vhosts?.[0]?.name || "";

  return (
    <div className="min-h-screen bg-zinc-100 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-100">
      <header className="border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mx-auto flex max-w-7xl items-center gap-6 px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-lg font-bold tracking-tight text-orange-600 dark:text-orange-400">
              rabbitmq-clue
            </span>
          </div>
          <nav className="flex gap-1">
            {navItems.map((item) => (
              <button
                key={item.id}
                onClick={() => {
                  setPage(item.id);
                  if (item.id !== "queues") setSelectedQueue(null);
                }}
                className={`rounded-md px-3 py-1.5 text-sm font-medium ${
                  page === item.id
                    ? "bg-orange-600 text-white"
                    : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800"
                }`}
              >
                {item.label}
              </button>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <select
              value={connName}
              onChange={(e) => setScope({ conn: e.target.value, vhost: "" })}
              className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
              title="Connection"
            >
              {(connections ?? []).map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
            <select
              value={activeVhost}
              onChange={(e) => setScope({ conn: connName, vhost: e.target.value })}
              className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
              title="Virtual host"
            >
              {(vhosts ?? []).map((v) => (
                <option key={v.name} value={v.name}>
                  {v.name === "" ? "(empty vhost)" : v.name}
                </option>
              ))}
            </select>
            <button
              onClick={() => setDark(!dark)}
              className="rounded-md border border-zinc-300 px-2 py-1.5 dark:border-zinc-700"
              title="Toggle theme"
            >
              {dark ? "Light" : "Dark"}
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-4 py-6">
        {page === "overview" && <Overview scope={{ conn: connName, vhost: activeVhost }} onOpenQueues={() => setPage("queues")} />}
        {page === "queues" && (
          <Queues
            scope={{ conn: connName, vhost: activeVhost }}
            selectedQueue={selectedQueue}
            onSelectQueue={setSelectedQueue}
          />
        )}
        {page === "publish" && <Publish scope={{ conn: connName, vhost: activeVhost }} />}
        {page === "topology" && <Topology scope={{ conn: connName, vhost: activeVhost }} />}
      </main>
    </div>
  );
}