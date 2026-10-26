import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  type CompileResponse,
  type SchemaFieldInfo,
} from "../api";

type ProtoFile = { name: string; content: string };

export function SchemaPicker({
  filterKind,
  filterName,
  selected,
  onSelect,
  allowImport,
}: {
  filterKind: "exchange" | "queue";
  filterName: string;
  selected: string;
  onSelect: (id: string) => void;
  allowImport: boolean;
}) {
  const qc = useQueryClient();
  const [importOpen, setImportOpen] = useState(false);

  const { data: schemas } = useQuery({
    queryKey: ["schemas", filterKind, filterName],
    queryFn: () =>
      api.schemas(filterName ? { [filterKind]: filterName } : undefined),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteSchema(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["schemas"] });
      if (selected) onSelect("");
    },
  });

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <select
          value={selected}
          onChange={(e) => onSelect(e.target.value)}
          className="min-w-52 rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
        >
          <option value="">(no schema)</option>
          {(schemas ?? []).map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} — {s.message_type}
            </option>
          ))}
        </select>
        {allowImport && (
          <button
            onClick={() => setImportOpen((v) => !v)}
            className="rounded-md border border-zinc-300 px-2.5 py-1.5 text-xs hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            {importOpen ? "Cancel import" : "Import schema…"}
          </button>
        )}
        {selected && (
          <button
            onClick={() => {
              if (confirm("Delete this saved schema?")) remove.mutate(selected);
            }}
            className="text-xs text-red-500 hover:text-red-600"
          >
            delete schema
          </button>
        )}
      </div>
      {importOpen && (
        <SchemaImport
          filterKind={filterKind}
          filterName={filterName}
          onSaved={(id) => {
            setImportOpen(false);
            onSelect(id);
          }}
        />
      )}
    </div>
  );
}

function filesToMap(files: ProtoFile[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const f of files) map[f.name] = f.content;
  return map;
}

function SchemaImport({
  filterKind,
  filterName,
  onSaved,
}: {
  filterKind: "exchange" | "queue";
  filterName: string;
  onSaved: (id: string) => void;
}) {
  const qc = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<ProtoFile[]>([
    { name: "schema.proto", content: "" },
  ]);
  const [active, setActive] = useState(0);
  const [name, setName] = useState("");
  const [compiled, setCompiled] = useState<CompileResponse | null>(null);
  const [messageType, setMessageType] = useState("");
  const [compileError, setCompileError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const compile = useMutation({
    mutationFn: (type: string) => api.compileSchema(filesToMap(files), type),
    onSuccess: (res) => {
      setCompiled(res);
      setCompileError(null);
      if (!messageType && res.types.length === 1) {
        setMessageType(res.types[0]);
      }
    },
    onError: (e) => {
      setCompileError((e as Error).message);
      setCompiled(null);
    },
  });

  useEffect(() => {
    if (compiled && messageType) {
      compile.mutate(messageType);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messageType]);

  const save = useMutation({
    mutationFn: () => {
      const bindings =
        filterName && (filterKind === "exchange" || filterKind === "queue")
          ? [{ kind: filterKind, name: filterName }]
          : [];
      return api.saveSchema({
        name: name || messageType,
        message_type: messageType,
        bindings,
        files: filesToMap(files),
      });
    },
    onSuccess: (meta) => {
      qc.invalidateQueries({ queryKey: ["schemas"] });
      onSaved(meta.id);
    },
    onError: (e) => setSaveError((e as Error).message),
  });

  const updateFile = (idx: number, patch: Partial<ProtoFile>) => {
    setFiles((fs) => fs.map((f, i) => (i === idx ? { ...f, ...patch } : f)));
    setCompiled(null);
  };

  const onUpload = async (fl: FileList | null) => {
    if (!fl) return;
    const loaded: ProtoFile[] = [];
    for (const f of fl) {
      loaded.push({ name: f.name, content: await f.text() });
    }
    setFiles(loaded);
    setActive(0);
    setCompiled(null);
  };

  return (
    <div className="space-y-3 rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <input
          ref={fileInput}
          type="file"
          accept=".proto"
          multiple
          onChange={(e) => onUpload(e.target.files)}
          className="hidden"
        />
        <button
          onClick={() => fileInput.current?.click()}
          className="rounded-md border border-zinc-300 px-2.5 py-1.5 text-xs hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          Upload .proto files
        </button>
        {files.map((f, i) => (
          <span key={i} className="flex items-center gap-1">
            <button
              onClick={() => setActive(i)}
              className={`rounded px-2 py-1 text-xs font-mono ${
                active === i
                  ? "bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-300"
                  : "bg-zinc-100 dark:bg-zinc-800"
              }`}
            >
              {f.name}
            </button>
            {files.length > 1 && (
              <button
                onClick={() => {
                  setFiles((fs) => fs.filter((_, j) => j !== i));
                  setActive(0);
                  setCompiled(null);
                }}
                className="text-xs text-zinc-400 hover:text-red-500"
                title="remove file"
              >
                ×
              </button>
            )}
          </span>
        ))}
        <button
          onClick={() => {
            setFiles((fs) => [...fs, { name: `file${fs.length + 1}.proto`, content: "" }]);
            setActive(files.length);
            setCompiled(null);
          }}
          className="rounded-md border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700"
        >
          + file
        </button>
      </div>
      <textarea
        value={files[active]?.content ?? ""}
        onChange={(e) => updateFile(active, { content: e.target.value })}
        rows={10}
        spellCheck={false}
        className="w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-xs dark:border-zinc-700 dark:bg-zinc-800"
        placeholder="syntax = &quot;proto3&quot;; ..."
      />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <button
          onClick={() => compile.mutate("")}
          disabled={compile.isPending || !files.some((f) => f.content.trim())}
          className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          {compile.isPending ? "Compiling…" : "Compile"}
        </button>
        {compiled && (
          <>
            <select
              value={messageType}
              onChange={(e) => setMessageType(e.target.value)}
              className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
            >
              <option value="">choose message type…</option>
              {compiled.types.map((t) => (
                <option key={t} value={t}>{t}</option>
              ))}
            </select>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="schema name"
              className="w-44 rounded-md border border-zinc-300 bg-white px-2 py-1.5 dark:border-zinc-700 dark:bg-zinc-800"
            />
            <button
              onClick={() => save.mutate()}
              disabled={!messageType || save.isPending}
              className="rounded-md bg-orange-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
            >
              {save.isPending ? "Saving…" : "Save schema"}
            </button>
          </>
        )}
      </div>
      {compileError && <div className="text-xs text-red-600 dark:text-red-400">{compileError}</div>}
      {saveError && <div className="text-xs text-red-600 dark:text-red-400">{saveError}</div>}
      {compiled && messageType && <FieldList fields={compiled.fields ?? []} />}
    </div>
  );
}

export function SchemaPayloadEditor({
  schemaId,
  type,
  onType,
  onSkeleton,
}: {
  schemaId: string;
  type: string;
  onType: (t: string) => void;
  onSkeleton: (json: string) => void;
}) {
  const { data: full, error: loadError } = useQuery({
    queryKey: ["schema-full", schemaId],
    queryFn: () => api.schema(schemaId),
  });

  const messageType = type || full?.message_type || "";
  const files = full?.files ?? {};

  const { data: compiled, error: compileError } = useQuery({
    queryKey: ["schema-compile", schemaId, messageType],
    queryFn: () => api.compileSchema(files, messageType),
    enabled: !!schemaId && !!messageType && Object.keys(files).length > 0,
  });

  if (!full) {
    return loadError ? (
      <div className="text-xs text-red-600 dark:text-red-400">{(loadError as Error).message}</div>
    ) : null;
  }

  return (
    <div className="space-y-2 rounded-lg border border-zinc-200 p-3 dark:border-zinc-800">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
          {full.name}
        </span>
        <select
          value={messageType}
          onChange={(e) => onType(e.target.value)}
          className="rounded-md border border-zinc-300 bg-white px-2 py-1.5 text-xs dark:border-zinc-700 dark:bg-zinc-800"
        >
          {[full.message_type, ...(compiled?.types ?? [])]
            .filter((t, i, a) => t && a.indexOf(t) === i)
            .map((t) => (
              <option key={t} value={t}>{t}</option>
            ))}
        </select>
        <button
          onClick={() => compiled?.skeleton && onSkeleton(compiled.skeleton)}
          disabled={!compiled?.skeleton}
          className="rounded-md border border-zinc-300 px-2.5 py-1.5 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          Load skeleton into payload
        </button>
      </div>
      {compileError && (
        <div className="text-xs text-red-600 dark:text-red-400">{(compileError as Error).message}</div>
      )}
      {compiled?.fields && <FieldList fields={compiled.fields} />}
    </div>
  );
}

export function FieldList({ fields }: { fields: SchemaFieldInfo[] }) {
  if (!fields.length) return null;
  return (
    <div className="max-h-64 overflow-auto rounded-md border border-zinc-200 p-2 text-xs dark:border-zinc-800">
      <div className="mb-1 font-semibold uppercase tracking-wide text-zinc-500">Fields</div>
      <ul className="space-y-0.5 font-mono">
        {fields.map((f) => (
          <li key={f.path} className="text-zinc-600 dark:text-zinc-300">
            {f.path}
            <span className="ml-2 rounded bg-zinc-100 px-1 text-zinc-500 dark:bg-zinc-800">
              {f.repeated ? "[]" : f.map ? "map" : f.kind}
              {f.type_name ? ` ${f.type_name}` : ""}
            </span>
            {f.oneof && <span className="ml-1 text-amber-600 dark:text-amber-400">oneof</span>}
            {f.enum_values && f.enum_values.length > 0 && (
              <span className="ml-1 text-zinc-400">[{f.enum_values.join("|")}]</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
