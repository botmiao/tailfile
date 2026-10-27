export type Entry = {
  name: string;
  path: string;
  isDir: boolean;
  isSymlink?: boolean;
  size: number;
  modTime: string;
};

export type NodeInfo = {
  id: string;
  name: string;
  os?: string;
  online: boolean;
  agentOK: boolean;
  self: boolean;
  readOnly: boolean;
  readRoots?: string[];
};

export type FileConflict = {
  rel: string;
  path: string;
  size?: number;
};

export type Job = {
  id: string;
  mode: string;
  status: string;
  bytesTotal: number;
  bytesDone: number;
  filesTotal: number;
  filesDone: number;
  current?: string;
  destPath: string;
  error?: string;
  conflict?: FileConflict;
};

export type Settings = {
  net: string;
  netRequested?: string;
  authKeyConfigured: boolean;
};

export type AuthKeyStatus = {
  configured: boolean;
  restartRequired: boolean;
};

export type Transfer = {
  id: string;
  mode: string;
  fromNode: string;
  fromPath: string;
  toNode: string;
  toPath: string;
  job: Job;
};

export type Preview = {
  kind: string;
  name: string;
  size: number;
  mime: string;
  path: string;
  text?: string;
  truncated?: boolean;
};

const storageKey = "tailfile.agent";
const previewKey = "tailfile.preview";

declare global {
  interface Window {
    TAILFILE_TOKEN?: string;
  }
}

export function loadBase(): string {
  return localStorage.getItem(storageKey) || "http://127.0.0.1:8645";
}

export function loadPreviewOn(): boolean {
  return localStorage.getItem(previewKey) !== "0";
}

export function savePreviewOn(on: boolean): void {
  localStorage.setItem(previewKey, on ? "1" : "0");
}

function authHeaders(init?: HeadersInit): Headers {
  const headers = new Headers(init);
  const token = window.TAILFILE_TOKEN;
  if (token) headers.set("Authorization", "Bearer " + token);
  return headers;
}

export function saveBase(base: string): void {
  localStorage.setItem(storageKey, base.replace(/\/$/, ""));
}

async function call<T>(base: string, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(base + path, {
    method,
    headers: authHeaders(body === undefined ? undefined : { "Content-Type": "application/json" }),
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const message = data && typeof data.error === "string" ? data.error : res.statusText;
    throw new Error(message || `HTTP ${res.status}`);
  }
  return data as T;
}

export function health(base: string) {
  return call<{ ok: boolean; version: string; name: string }>(base, "GET", "/v1/health");
}

export function nodes(base: string) {
  return call<{ nodes: NodeInfo[] | null }>(base, "GET", "/v1/nodes");
}

export function list(base: string, node: string, path: string) {
  const q = new URLSearchParams({ node, path });
  return call<{ path: string; entries: Entry[] | null }>(base, "GET", "/v1/fs/list?" + q);
}

export function preview(base: string, node: string, path: string) {
  const q = new URLSearchParams({ node, path });
  return call<Preview>(base, "GET", "/v1/fs/preview?" + q);
}

export function readURL(base: string, node: string, path: string) {
  const q = new URLSearchParams({ node, path });
  return base.replace(/\/$/, "") + "/v1/fs/read?" + q;
}

async function readResponse(base: string, node: string, path: string): Promise<Response> {
  const res = await fetch(readURL(base, node, path), { headers: authHeaders() });
  if (!res.ok) {
    const text = await res.text();
    let message = res.statusText;
    try {
      const data = JSON.parse(text) as { error?: string };
      if (data.error) message = data.error;
    } catch {
      if (text) message = text;
    }
    throw new Error(message || `HTTP ${res.status}`);
  }
  return res;
}

export async function readBlob(base: string, node: string, path: string): Promise<Blob> {
  const res = await readResponse(base, node, path);
  return res.blob();
}

export async function readBytes(base: string, node: string, path: string): Promise<Uint8Array> {
  const res = await readResponse(base, node, path);
  return new Uint8Array(await res.arrayBuffer());
}

export function mkdir(base: string, node: string, path: string) {
  return call<void>(base, "POST", "/v1/fs/mkdir", { node, path });
}

export function rename(base: string, node: string, from: string, to: string) {
  return call<void>(base, "POST", "/v1/fs/rename", { node, from, to });
}

export function startTransfer(
  base: string,
  req: { mode: string; fromNode: string; fromPath: string; toNode: string; toPath: string },
) {
  return call<Transfer>(base, "POST", "/v1/transfers", req);
}

export function transfers(base: string) {
  return call<{ transfers: Transfer[] | null }>(base, "GET", "/v1/transfers");
}

export function cancelTransfer(base: string, id: string) {
  return call<Transfer>(base, "POST", `/v1/transfers/${encodeURIComponent(id)}/cancel`);
}

export function resumeTransfer(base: string, id: string) {
  return call<Transfer>(base, "POST", `/v1/transfers/${encodeURIComponent(id)}/resume`);
}

export function answerTransferConflict(base: string, id: string, action: "overwrite" | "skip", applyToRest: boolean) {
  return call<Transfer>(base, "POST", `/v1/transfers/${encodeURIComponent(id)}/conflict`, {
    action,
    applyToRest,
  });
}

export function answerJobConflict(base: string, id: string, action: "overwrite" | "skip", applyToRest: boolean) {
  return call<Job>(base, "POST", `/v1/jobs/${encodeURIComponent(id)}/conflict`, {
    action,
    applyToRest,
  });
}

export function settings(base: string) {
  return call<Settings>(base, "GET", "/v1/local/settings");
}

export function setAuthKey(base: string, authKey: string) {
  return call<AuthKeyStatus>(base, "PUT", "/v1/local/settings/authkey", { authKey });
}

export function clearAuthKey(base: string) {
  return call<AuthKeyStatus>(base, "DELETE", "/v1/local/settings/authkey");
}

export function jobs(base: string) {
  return call<{ jobs: Job[] | null }>(base, "GET", "/v1/jobs");
}

export function setDropTarget(base: string, node: string, destDir: string) {
  return call<void>(base, "POST", "/v1/local/drop-target", { node, destDir });
}

export function importPaths(base: string, node: string, destDir: string, paths: string[]) {
  return call<Job>(base, "POST", "/v1/local/import", { node, destDir, paths });
}

export function cacheFile(base: string, node: string, path: string) {
  return call<{ localPath: string; cached: boolean }>(base, "POST", "/v1/local/cache", { node, path });
}

export function reveal(base: string, path: string) {
  return call<void>(base, "POST", "/v1/local/reveal", { path });
}

export function joinRemote(dir: string, name: string): string {
  const cleanDir = dir.replaceAll("\\", "/").replace(/\/+$/, "");
  const cleanName = name.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
  if (!cleanName) return cleanDir || "/";
  if (!cleanDir) return cleanName;
  if (cleanDir.endsWith(":")) return cleanDir + "/" + cleanName;
  return cleanDir + "/" + cleanName;
}

export function baseName(p: string): string {
  const s = p.replaceAll("\\", "/").replace(/\/+$/, "");
  const i = s.lastIndexOf("/");
  return i >= 0 ? s.slice(i + 1) : s;
}

export function formatSize(n: number, dir: boolean): string {
  if (dir) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export function formatTime(s: string): string {
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function jobLine(j: Job): string {
  const pct = j.bytesTotal > 0 ? ` ${Math.floor((j.bytesDone * 100) / j.bytesTotal)}%` : "";
  const name = baseName(j.current || j.destPath || "");
  let msg = `${j.status}${pct}  ${name}`.trim();
  if (j.error && j.status !== "completed") msg += "  " + j.error;
  return msg;
}
