// Follow-up: Log replay timestamps, main-world injected error listener, Network.enable timing repeats.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const PROFILE = path.join(HERE, 'profile');
const CHROME = 'C:/Program Files/Google/Chrome/Application/chrome.exe';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const reports = [];

const page = `<!doctype html><body><script>
const rep = (k, v) => fetch('/report?k=' + k, {method: 'POST', body: JSON.stringify(v)});
window.__loadedAt = Date.now();
console.error('T-error');
fetch('/missing-404');
// page-side detection attempts against a main-world listener
const det = {};
det.onerror = window.onerror === null;
det.ownKeys = Object.getOwnPropertyNames(window).filter((k) => /^__w/.test(k));
setTimeout(() => {
  // does a listener registered before us change event behaviour? check defaultPrevented / propagation
  window.addEventListener('error', (e) => { det.seenByPage = e.message; det.defaultPrevented = e.defaultPrevented; });
  setTimeout(() => { throw new Error('T-timeout-throw'); }, 50);
  setTimeout(() => rep('det', det), 300);
}, 0);
Promise.reject(new Error('T-unhandled-rejection'));
</script><script>throw new Error('T-script-throw');</script></body>`;

const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname === '/report') {
    let b = ''; req.on('data', (c) => (b += c)); req.on('end', () => { reports.push({ k: u.searchParams.get('k'), v: JSON.parse(b) }); res.end('ok'); });
    return;
  }
  if (u.pathname === '/p.html') { res.writeHead(200, { 'content-type': 'text/html' }); return res.end(page); }
  if (u.pathname === '/ok.txt') return res.end('ok');
  if (u.pathname === '/fetchloop.html') {
    res.writeHead(200, { 'content-type': 'text/html' });
    return res.end(`<!doctype html><script>(async()=>{const t0=performance.now();for(let i=0;i<300;i++){await (await fetch('/ok.txt?i='+i)).text();}fetch('/report?k=fl',{method:'POST',body:JSON.stringify(performance.now()-t0)});})()</script>`);
  }
  res.writeHead(404); res.end();
});

class CDP {
  constructor(ws) {
    this.ws = ws; this.id = 0; this.pending = new Map(); this.listeners = [];
    ws.addEventListener('message', (ev) => {
      const m = JSON.parse(ev.data);
      if (m.id && this.pending.has(m.id)) { const { res, rej } = this.pending.get(m.id); this.pending.delete(m.id); m.error ? rej(new Error(m.error.message)) : res(m.result); }
      else if (m.method) this.listeners.forEach((l) => l(m));
    });
  }
  send(method, params = {}, sessionId) { const id = ++this.id; this.ws.send(JSON.stringify({ id, method, params, sessionId })); return new Promise((res, rej) => this.pending.set(id, { res, rej })); }
  on(fn) { this.listeners.push(fn); return () => (this.listeners = this.listeners.filter((x) => x !== fn)); }
}

// Main-world collector: closure storage, forwarded to the isolated world via a one-off
// CustomEvent on a detached EventTarget is impossible across worlds, so use document events
// with an unguessable name; the isolated world listens and keeps the buffer.
const SECRET = 'wu' + Math.random().toString(36).slice(2);
const mainSrc = `(() => {
  const send = (o) => document.dispatchEvent(new CustomEvent(${JSON.stringify(SECRET)}, {detail: JSON.stringify(o)}));
  addEventListener('error', (e) => { if (e.error !== undefined || e.message) send({t: 'error', msg: e.message, src: e.filename, line: e.lineno, stack: e.error && e.error.stack ? String(e.error.stack).split('\\n').slice(0, 2).join(' | ') : null}); });
  addEventListener('unhandledrejection', (e) => send({t: 'rejection', reason: String(e.reason && e.reason.stack || e.reason).split('\\n')[0]}));
})()`;
const isoSrc = `(() => { const buf = globalThis.__wbuf = []; document.addEventListener(${JSON.stringify(SECRET)}, (e) => buf.push(JSON.parse(e.detail))); })()`;

async function main() {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const BASE = `http://127.0.0.1:${server.address().port}`;
  fs.rmSync(PROFILE, { recursive: true, force: true }); fs.mkdirSync(PROFILE, { recursive: true });
  const chrome = spawn(CHROME, ['--headless=new', `--user-data-dir=${PROFILE}`, '--remote-debugging-port=0', '--no-first-run', 'about:blank'], { stdio: 'ignore' });
  const out = {};
  let ws;
  try {
    const pf = path.join(PROFILE, 'DevToolsActivePort');
    for (let i = 0; i < 100 && !fs.existsSync(pf); i++) await sleep(100);
    const [dport, dpath] = fs.readFileSync(pf, 'utf8').trim().split('\n');
    ws = new WebSocket(`ws://127.0.0.1:${dport}${dpath}`);
    await new Promise((r) => ws.addEventListener('open', r));
    const c = new CDP(ws);
    const attach = async (url) => { const { targetId } = await c.send('Target.createTarget', { url }); const { sessionId } = await c.send('Target.attachToTarget', { targetId, flatten: true }); return { targetId, sessionId }; };

    // 1. Log replay timestamps: load, wait 3s, then Log.enable
    {
      const { targetId } = await c.send('Target.createTarget', { url: BASE + '/p.html' });
      await sleep(3000);
      const { sessionId } = await c.send('Target.attachToTarget', { targetId, flatten: true });
      const loadedAt = (await c.send('Runtime.evaluate', { expression: 'window.__loadedAt', returnByValue: true }, sessionId)).result.value;
      const evs = []; const off = c.on((m) => { if (m.sessionId === sessionId && m.method === 'Log.entryAdded') evs.push(m.params.entry); });
      const enableAt = Date.now();
      await c.send('Log.enable', {}, sessionId); await sleep(500); off();
      out.replay = { loadedAt, enableAt, entries: evs.map((e) => ({ source: e.source, level: e.level, text: e.text.slice(0, 80), ts: Math.round(e.timestamp), msAfterLoad: Math.round(e.timestamp - loadedAt) })) };
      // Log.clear then disable/enable: is the store cleared?
      await c.send('Log.clear', {}, sessionId); await c.send('Log.disable', {}, sessionId);
      const evs2 = []; const off2 = c.on((m) => { if (m.sessionId === sessionId && m.method === 'Log.entryAdded') evs2.push(m.params.entry.text.slice(0, 60)); });
      await c.send('Log.enable', {}, sessionId); await sleep(300); off2();
      out.replayAfterClear = evs2;
      await c.send('Target.closeTarget', { targetId });
    }

    // 2. Main-world listener + isolated bridge, no Runtime.enable / Log.enable
    {
      const { targetId, sessionId: s } = await attach('about:blank');
      await c.send('Page.enable', {}, s);
      await c.send('Page.addScriptToEvaluateOnNewDocument', { source: isoSrc, worldName: 'wu_iso', runImmediately: true }, s);
      await c.send('Page.addScriptToEvaluateOnNewDocument', { source: mainSrc }, s);
      reports.length = 0;
      await c.send('Page.navigate', { url: BASE + '/p.html' }, s);
      await sleep(1500);
      const tree = await c.send('Page.getFrameTree', {}, s);
      const iw = await c.send('Page.createIsolatedWorld', { frameId: tree.frameTree.frame.id, worldName: 'wu_iso' }, s);
      const buf = (await c.send('Runtime.evaluate', { expression: 'JSON.stringify(globalThis.__wbuf)', contextId: iw.executionContextId, returnByValue: true }, s)).result.value;
      out.mainWorldListener = { buf: JSON.parse(buf || 'null'), pageDetection: reports.find((r) => r.k === 'det')?.v };
      await c.send('Target.closeTarget', { targetId });
    }

    // 3. Network.enable cost: alternate 4 runs each, 300 sequential fetches
    out.fetchloop = { nonet: [], net: [] };
    for (let i = 0; i < 4; i++) {
      for (const withNet of [false, true]) {
        const { targetId, sessionId: s } = await attach('about:blank');
        await c.send('Page.enable', {}, s);
        if (withNet) await c.send('Network.enable', {}, s);
        reports.length = 0;
        await c.send('Page.navigate', { url: BASE + '/fetchloop.html' }, s);
        for (let j = 0; j < 100 && !reports.find((r) => r.k === 'fl'); j++) await sleep(100);
        out.fetchloop[withNet ? 'net' : 'nonet'].push(Math.round(reports.find((r) => r.k === 'fl')?.v));
        await c.send('Target.closeTarget', { targetId });
      }
    }
    await c.send('Browser.close').catch(() => {});
  } finally {
    try { ws && ws.close(); } catch {}
    await sleep(800); try { chrome.kill(); } catch {}
    server.close();
    fs.writeFileSync(path.join(HERE, 'results2.json'), JSON.stringify(out, null, 1));
    console.log(JSON.stringify(out, null, 1));
  }
}
main().catch((e) => { console.error(e); process.exitCode = 1; });
