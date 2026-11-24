// Minimal CDP client + Chrome launcher (Node 24, global WebSocket).
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

export const ASSETS = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"));
export const CHROME = "C:/Program Files/Google/Chrome/Application/chrome.exe";
export const CHROMIUM = "C:/Users/ADMIN/AppData/Local/ms-playwright/chromium-1255/chrome-win64/chrome.exe";

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export async function launch({ exe = CHROME, port = 9333, name = "p", extra = [] } = {}) {
  const dir = path.join(ASSETS, "profile", `${name}-${Date.now()}`);
  fs.mkdirSync(dir, { recursive: true });
  const args = [
    "--headless=new",
    `--user-data-dir=${dir}`,
    `--remote-debugging-port=${port}`,
    "--remote-debugging-address=127.0.0.1",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    "--disable-component-update",
    "--disable-sync",
    "--window-size=1366,900",
    ...extra,
    "about:blank",
  ];
  const proc = spawn(exe, args, { stdio: ["ignore", "ignore", "pipe"] });
  let stderr = "";
  proc.stderr.on("data", (d) => (stderr += d));
  let ver;
  for (let i = 0; i < 100; i++) {
    try {
      ver = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
      break;
    } catch {
      await sleep(100);
    }
  }
  if (!ver) throw new Error("chrome did not start: " + stderr.slice(-500));
  const close = async (cdp) => {
    try { if (cdp) await Promise.race([cdp.send("Browser.close"), sleep(3000)]); } catch {}
    await sleep(500);
    if (proc.exitCode === null) { try { proc.kill(); } catch {} }
    await sleep(500);
    try { fs.rmSync(dir, { recursive: true, force: true }); } catch {}
  };
  return { proc, ver, port, dir, close };
}

export class CDP {
  constructor(url) {
    this.url = url;
    this.id = 0;
    this.pending = new Map();
    this.listeners = new Set();
  }
  static async connect(url) {
    const c = new CDP(url);
    await c.open();
    return c;
  }
  open() {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(this.url);
      this.ws = ws;
      ws.onopen = () => resolve();
      ws.onerror = (e) => reject(new Error("ws error " + (e.message || "")));
      ws.onclose = () => {
        for (const [, p] of this.pending) p.reject(new Error("closed"));
        this.pending.clear();
        this.closed = true;
      };
      ws.onmessage = (ev) => {
        const m = JSON.parse(ev.data);
        if (m.id !== undefined) {
          const p = this.pending.get(m.id);
          if (!p) return;
          this.pending.delete(m.id);
          if (m.error) p.reject(Object.assign(new Error(m.error.message), { cdp: m.error }));
          else p.resolve(m.result);
        } else {
          for (const l of this.listeners) l(m.method, m.params || {}, m.sessionId);
        }
      };
    });
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id;
    const msg = { id, method, params };
    if (sessionId) msg.sessionId = sessionId;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify(msg));
    });
  }
  on(fn) {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }
  waitFor(method, sessionId, pred = () => true, timeout = 30000) {
    return new Promise((resolve, reject) => {
      const t = setTimeout(() => { off(); reject(new Error("timeout waiting " + method)); }, timeout);
      const off = this.on((m, p, s) => {
        if (m === method && (!sessionId || s === sessionId) && pred(p)) {
          clearTimeout(t); off(); resolve(p);
        }
      });
    });
  }
  close() { try { this.ws.close(); } catch {} }
}
