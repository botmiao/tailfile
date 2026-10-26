import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import * as api from "./api";
import { PdfView } from "./PdfView";

type Folder = { node: string; path: string };
type Clip = { node: string; path: string; name: string; isDir: boolean };
type TreeRow = {
  key: string;
  depth: number;
  label: string;
  kind: "node" | "dir";
  node: string;
  path: string;
  readOnly?: boolean;
  self?: boolean;
};

const nodeKey = (id: string) => "n\u001f" + id;
const dirKey = (node: string, path: string) => "d\u001f" + node + "\u001f" + path;
const listKey = (node: string, path: string) => node + "\n" + path;

export function App() {
  const [base, setBase] = useState(api.loadBase);
  const [addr, setAddr] = useState(base);
  const [nodes, setNodes] = useState<api.NodeInfo[]>([]);
  const [cache, setCache] = useState<Record<string, api.Entry[]>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [folder, setFolder] = useState<Folder | null>(null);
  const [sel, setSel] = useState(-1);
  const [clip, setClip] = useState<Clip | null>(null);
  const [mode, setMode] = useState<"push" | "pull">("push");
  const [status, setStatus] = useState("Not connected");
  const [error, setError] = useState("");
  const [alive, setAlive] = useState(false);
  const [newName, setNewName] = useState("");
  const [renameTo, setRenameTo] = useState("");
  const [importText, setImportText] = useState("");
  const [preview, setPreview] = useState<api.Preview | null>(null);
  const [transfers, setTransfers] = useState<api.Transfer[]>([]);
  const [imports, setImports] = useState<api.Job[]>([]);
  const [dragNote, setDragNote] = useState("");
  const [dragPath, setDragPath] = useState("");
  const treeRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const auto = useRef(false);
  const folderRef = useRef<Folder | null>(null);
  const seenJob = useRef<Record<string, string>>({});
  folderRef.current = folder;

  const fail = (e: unknown) => setError(e instanceof Error ? e.message : String(e));

  const loadDir = useCallback(
    async (node: string, path: string) => {
      const key = listKey(node, path);
      try {
        const listing = await api.list(base, node, path);
        setCache((c) => ({ ...c, [key]: listing.entries ?? [] }));
        setError("");
      } catch (e) {
        fail(e);
        setCache((c) => ({ ...c, [key]: [] }));
      }
    },
    [base],
  );

  const refresh = useCallback(async () => {
    setStatus("Contacting the agent…");
    try {
      await api.health(base);
      const body = await api.nodes(base);
      const list = body.nodes ?? [];
      setNodes(list);
      setAlive(true);
      setError("");
      setCache({});
      setStatus(`${list.length} nodes`);
      setFolder((cur) => {
        if (cur) {
          void loadDir(cur.node, cur.path);
          return cur;
        }
        return cur;
      });
    } catch (e) {
      setAlive(false);
      setNodes([]);
      fail(e);
      setStatus("Agent is not running. Start tailfile-agent, or tailfile-agent -demo, then Connect.");
    }
  }, [base, loadDir]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (auto.current || folder || nodes.length === 0) return;
    const n = nodes.find((node) => (node.readRoots?.length ?? 0) > 0);
    const root = n?.readRoots?.[0];
    if (!n || !root) return;
    auto.current = true;
    setExpanded(new Set([nodeKey(n.id), dirKey(n.id, root)]));
    setFolder({ node: n.id, path: root });
    void loadDir(n.id, root);
  }, [nodes, folder, loadDir]);

  useEffect(() => {
    if (!folder) return;
    void api.setDropTarget(base, folder.node, folder.path).catch(() => {});
  }, [base, folder]);

  useEffect(() => {
    if (!alive) return;
    let stop = false;
    const tick = async () => {
      try {
        const [t, j] = await Promise.all([api.transfers(base), api.jobs(base)]);
        if (stop) return;
        const trs = t.transfers ?? [];
        const imps = (j.jobs ?? []).filter((job) => job.mode === "import");
        let reload = false;
        for (const tr of [...trs, ...imps.map((job) => ({ id: job.id, job }))]) {
          if (tr.job.status === "completed" && seenJob.current[tr.id] !== "completed") reload = true;
          seenJob.current[tr.id] = tr.job.status;
        }
        setTransfers(trs);
        setImports(imps);
        const open = folderRef.current;
        if (reload && open) void loadDir(open.node, open.path);
      } catch {
        /* the next tick reports a dead agent via Refresh */
      }
    };
    void tick();
    const id = setInterval(tick, 500);
    return () => {
      stop = true;
      clearInterval(id);
    };
  }, [alive, base]);

  const entries = folder ? (cache[listKey(folder.node, folder.path)] ?? []) : [];
  const file = sel >= 0 && sel < entries.length ? entries[sel] : null;

  useEffect(() => {
    if (!folder || !file || file.isDir) {
      setPreview(null);
      return;
    }
    let stop = false;
    api
      .preview(base, folder.node, file.path)
      .then((p) => {
        if (!stop) setPreview(p);
      })
      .catch((e) => {
        if (!stop) {
          setPreview(null);
          fail(e);
        }
      });
    return () => {
      stop = true;
    };
  }, [base, folder, file]);

  const rows = useMemo(() => flatten(nodes, expanded, cache), [nodes, expanded, cache]);

  const treeVirtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => treeRef.current,
    estimateSize: () => 28,
    overscan: 12,
  });
  const listVirtual = useVirtualizer({
    count: entries.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => 28,
    overscan: 16,
  });

  function toggle(key: string, node: string, path: string, kind: TreeRow["kind"]) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
    if (kind === "dir") {
      setFolder({ node, path });
      setSel(-1);
      if (!cache[listKey(node, path)]) void loadDir(node, path);
    } else if (!cache[listKey(node, path)] && path) {
      void loadDir(node, path);
    }
  }

  function openFolder(node: string, path: string) {
    setFolder({ node, path });
    setSel(-1);
    setExpanded((prev) => new Set(prev).add(nodeKey(node)).add(dirKey(node, path)));
    if (!cache[listKey(node, path)]) void loadDir(node, path);
  }

  function copySelected() {
    if (!folder || !file) {
      setStatus("Select a file or folder to copy");
      return;
    }
    setClip({ node: folder.node, path: file.path, name: file.name, isDir: file.isDir });
    setStatus("Copied " + file.name);
  }

  async function paste() {
    if (!clip) {
      setStatus("Clipboard is empty");
      return;
    }
    if (!folder) {
      setStatus("Open a destination folder");
      return;
    }
    try {
      const tr = await api.startTransfer(base, {
        mode,
        fromNode: clip.node,
        fromPath: clip.path,
        toNode: folder.node,
        toPath: api.joinRemote(folder.path, clip.name),
      });
      setError("");
      setStatus(`${mode} ${tr.fromNode} → ${tr.toNode}`);
    } catch (e) {
      fail(e);
      setStatus("Paste failed");
    }
  }

  async function createFolder() {
    if (!folder || !newName.trim()) {
      setStatus("Type a folder name first");
      return;
    }
    try {
      await api.mkdir(base, folder.node, api.joinRemote(folder.path, newName.trim()));
      setNewName("");
      setStatus("Created " + newName.trim());
      await loadDir(folder.node, folder.path);
    } catch (e) {
      fail(e);
    }
  }

  async function rename() {
    if (!folder || !file || !renameTo.trim()) {
      setStatus("Select an item and type the new name");
      return;
    }
    try {
      await api.rename(base, folder.node, file.path, api.joinRemote(folder.path, renameTo.trim()));
      setStatus("Renamed");
      setRenameTo("");
      await loadDir(folder.node, folder.path);
    } catch (e) {
      fail(e);
    }
  }

  async function importLocal() {
    if (!folder) {
      setStatus("Open a destination folder");
      return;
    }
    const paths = importText
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    if (paths.length === 0) {
      setStatus("Type a local path to import");
      return;
    }
    try {
      await api.importPaths(base, folder.node, folder.path, paths);
      setImportText("");
      setStatus(`Importing ${paths.length} item(s)`);
    } catch (e) {
      fail(e);
    }
  }

  async function cacheSelected() {
    if (!folder || !file || file.isDir) {
      setStatus("Select a file to cache for dragging out");
      return;
    }
    try {
      const res = await api.cacheFile(base, folder.node, file.path);
      setDragPath(res.localPath);
      setDragNote(
        res.cached
          ? `Cached ${file.name}. Wails cannot start an OS drag; copy this path or Reveal it.`
          : "This file is already on this machine. Wails cannot start an OS drag; Reveal opens it.",
      );
      setStatus("Cached " + file.name);
    } catch (e) {
      fail(e);
    }
  }

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      const target = ev.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return;
      const meta = ev.metaKey || ev.ctrlKey;
      if (!meta) return;
      if (ev.key.toLowerCase() === "c") {
        ev.preventDefault();
        copySelected();
      } else if (ev.key.toLowerCase() === "v") {
        ev.preventDefault();
        void paste();
      } else if (ev.key.toLowerCase() === "r") {
        ev.preventDefault();
        void refresh();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const readURL = folder && file && !file.isDir ? api.readURL(base, folder.node, file.path) : "";

  return (
    <div className="tf-app">
      <header className="tf-toolbar">
        <div className="tf-row">
          <span className="tf-brand">Tailfile</span>
          <span className="tf-tag">Wails</span>
          <input
            className="tf-grow"
            type="text"
            aria-label="Agent URL"
            value={addr}
            onChange={(e) => setAddr(e.target.value)}
          />
          <button
            onClick={() => {
              const next = addr.replace(/\/$/, "");
              api.saveBase(next);
              auto.current = false;
              setFolder(null);
              setBase(next);
            }}
          >
            Connect
          </button>
          <button onClick={() => void refresh()}>Refresh</button>
          <span className="tf-status">{status}</span>
        </div>
        <div className="tf-row">
          <button className={mode === "push" ? "seg-on" : ""} onClick={() => setMode("push")}>
            Push
          </button>
          <button className={mode === "pull" ? "seg-on" : ""} onClick={() => setMode("pull")}>
            Pull
          </button>
          <span className="tf-tag">A→B</span>
          <button onClick={copySelected}>Copy</button>
          <button onClick={() => void paste()}>Paste</button>
          <input
            type="text"
            aria-label="New folder"
            placeholder="New folder"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
          />
          <button onClick={() => void createFolder()}>Create</button>
          <input
            type="text"
            aria-label="Rename to"
            placeholder="Rename to"
            value={renameTo}
            onChange={(e) => setRenameTo(e.target.value)}
          />
          <button onClick={() => void rename()}>Rename</button>
          <button onClick={() => void cacheSelected()}>Cache for drag</button>
          <button
            disabled={!dragPath}
            onClick={() => {
              void api.reveal(base, dragPath).catch(fail);
            }}
          >
            Reveal
          </button>
        </div>
        <div className="tf-row">
          <input
            className="tf-grow"
            type="text"
            aria-label="Local path to import"
            placeholder="Local path to import (one per line is fine in a paste)"
            value={importText}
            onChange={(e) => setImportText(e.target.value)}
          />
          <button onClick={() => void importLocal()}>Import</button>
          <span className="tf-tag">Desktop builds also accept a drop onto the file list.</span>
        </div>
        {error && <p className="tf-danger">{error}</p>}
      </header>

      <div className="tf-split">
        <aside className="tf-tree">
          <p className="tf-pane-title">Nodes</p>
          {rows.length === 0 ? (
            <p className="tf-empty">No nodes yet.</p>
          ) : (
            <div className="tf-scroll" ref={treeRef}>
              <div style={{ height: treeVirtual.getTotalSize(), position: "relative" }}>
                {treeVirtual.getVirtualItems().map((v) => {
                  const row = rows[v.index];
                  const on = row.kind === "dir" && folder?.node === row.node && folder.path === row.path;
                  return (
                    <div
                      key={row.key}
                      className={on ? "tf-tree-row on" : "tf-tree-row"}
                      style={{ height: v.size, transform: `translateY(${v.start}px)` }}
                      onClick={() => {
                        if (row.kind === "dir") openFolder(row.node, row.path);
                        else toggle(row.key, row.node, row.path, row.kind);
                      }}
                    >
                      <span style={{ width: row.depth * 14 }} />
                      <button
                        className="tf-twist"
                        aria-label={expanded.has(row.key) ? "Collapse" : "Expand"}
                        onClick={(e) => {
                          e.stopPropagation();
                          toggle(row.key, row.node, row.path, row.kind);
                        }}
                      >
                        {expanded.has(row.key) ? "▾" : "▸"}
                      </button>
                      <span className="tf-tree-label">{row.label}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </aside>

        <section className="tf-right">
          <p className="tf-crumb">{folder ? folder.path : "Select a folder"}</p>
          <div
            className="tf-list"
            data-file-drop-target
            onDragOver={(e) => e.preventDefault()}
          >
            {!folder ? (
              <p className="tf-empty">Choose a node folder in the tree.</p>
            ) : (
              <>
                <div className="tf-cols">
                  <span>Name</span>
                  <span>Size</span>
                  <span className="tf-modified">Modified</span>
                </div>
                <div className="tf-scroll" ref={listRef}>
                  {entries.length === 0 ? (
                    <p className="tf-empty">This folder is empty. Drop files here in the desktop build, or import a local path.</p>
                  ) : (
                    <div style={{ height: listVirtual.getTotalSize(), position: "relative" }}>
                      {listVirtual.getVirtualItems().map((v) => {
                        const e = entries[v.index];
                        return (
                          <div
                            key={e.path}
                            className={v.index === sel ? "tf-file on" : "tf-file"}
                            style={{ height: v.size, transform: `translateY(${v.start}px)` }}
                            onClick={() => setSel(v.index)}
                            onDoubleClick={() => {
                              if (e.isDir && folder) openFolder(folder.node, e.path);
                            }}
                          >
                            <span className="tf-name">
                              {e.name}
                              {e.isDir ? "/" : ""}
                              {e.isSymlink ? " →" : ""}
                            </span>
                            <span className="tf-num">{api.formatSize(e.size, e.isDir)}</span>
                            <span className="tf-num tf-modified">{api.formatTime(e.modTime)}</span>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
          <div className="tf-preview">
            <p className="tf-pane-title">Preview</p>
            {preview?.kind === "image" && readURL && (
              <img src={readURL} alt={preview.name} />
            )}
            {preview?.kind === "pdf" && readURL && <PdfView url={readURL} />}
            {preview?.text && (
              <pre>
                {preview.text}
                {preview.truncated ? "\n…truncated" : ""}
              </pre>
            )}
            {preview && preview.kind !== "text" && preview.kind !== "image" && preview.kind !== "pdf" && (
              <p className="tf-muted">No preview for this file type.</p>
            )}
            {!preview && <p className="tf-muted">Select a file to preview text, an image, or a PDF.</p>}
            {dragNote && (
              <>
                <p className="tf-muted">{dragNote}</p>
                <input className="tf-path" readOnly value={dragPath} aria-label="Cached local path" />
              </>
            )}
          </div>
        </section>
      </div>

      <footer className="tf-transfers">
        <p className="tf-pane-title">Transfers</p>
        {transfers.length === 0 && imports.length === 0 && (
          <p className="tf-muted">
            Copy on one node, open a folder on another, paste. Bytes go directly between the two agents.
          </p>
        )}
        {transfers.map((tr) => (
          <div className="tf-job" key={tr.id}>
            <span>
              {tr.mode} {tr.fromNode} → {tr.toNode} {api.jobLine(tr.job)}
            </span>
            {tr.job.status === "running" && (
              <button onClick={() => void api.cancelTransfer(base, tr.id).catch(fail)}>Cancel</button>
            )}
            {(tr.job.status === "error" || tr.job.status === "canceled" || tr.job.status === "interrupted") && (
              <button onClick={() => void api.resumeTransfer(base, tr.id).catch(fail)}>Resume</button>
            )}
          </div>
        ))}
        {imports.map((job) => (
          <div key={job.id}>import {api.jobLine(job)}</div>
        ))}
      </footer>
    </div>
  );
}

function flatten(nodes: api.NodeInfo[], expanded: Set<string>, cache: Record<string, api.Entry[]>): TreeRow[] {
  const rows: TreeRow[] = [];
  for (const n of nodes) {
    const label = [
      n.name || n.id,
      n.readOnly ? "read-only" : "",
      n.self ? "this machine" : "",
      n.agentOK ? "" : "agent down",
    ]
      .filter(Boolean)
      .join("  ·  ");
    rows.push({
      key: nodeKey(n.id),
      depth: 0,
      label,
      kind: "node",
      node: n.id,
      path: "",
      readOnly: n.readOnly,
      self: n.self,
    });
    if (!expanded.has(nodeKey(n.id))) continue;
    for (const root of n.readRoots ?? []) {
      pushDir(rows, n.id, root, 1, expanded, cache);
    }
  }
  return rows;
}

function pushDir(
  rows: TreeRow[],
  node: string,
  path: string,
  depth: number,
  expanded: Set<string>,
  cache: Record<string, api.Entry[]>,
) {
  const key = dirKey(node, path);
  rows.push({
    key,
    depth,
    label: api.baseName(path) || path,
    kind: "dir",
    node,
    path,
  });
  if (!expanded.has(key)) return;
  for (const e of cache[listKey(node, path)] ?? []) {
    if (e.isDir) pushDir(rows, node, e.path, depth + 1, expanded, cache);
  }
}
