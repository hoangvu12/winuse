// Experiment: console/error capture without Runtime.enable, and HTTP status on navigation.
// Usage: node exp.mjs  (writes results.json next to this file)
import http from 'node:http';
import net from 'node:net';
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const PROFILE = path.join(HERE, 'profile');
const CHROME = 'C:/Program Files/Google/Chrome/Application/chrome.exe';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---------- fixture server ----------
const reports = []; // page-side reports
const reqLog = []; // request headers seen by server
let PORT = 0;
let DEAD_PORT = 0;

const testPage = (q) => `<!doctype html><html><head><title>t</title></head><body>
<p id=x>test</p>
<script>
const RUN = ${JSON.stringify(q.get('run') || '')};
const PROBE = ${q.get('probe') === '1'};
const rep = (k, v) => fetch('/report?run=' + RUN + '&k=' + k, {method: 'POST', body: JSON.stringify(v)}).catch(()=>{});
console.log('T-log', {a: 1});
console.warn('T-warn');
console.error('T-error');
console.info('T-info');
console.debug('T-debug');
// deprecation candidates
try { const x = new XMLHttpRequest(); x.open('GET', '/ok.txt', false); x.send(); } catch (e) {}
try { window.addEventListener('unload', () => {}); } catch (e) {}
// CSP violation: img-src 'self' blocks this cross-origin image
const im = new Image(); im.src = 'http://localhost:${PORT}/img.png'; document.body.appendChild(im);
document.addEventListener('securitypolicyviolation', (e) => rep('cspEvent', e.violatedDirective));
fetch('/missing-404').catch(() => {});
fetch('http://127.0.0.1:${DEAD_PORT}/neterr').catch(() => {});
Promise.reject(new Error('T-unhandled-rejection'));
setTimeout(() => { throw new Error('T-timeout-throw'); }, 100);
// main-world visibility of injected stuff
setTimeout(() => rep('mainWorldSees', {buf: typeof window.__wbuf, binding: typeof window.__wb}), 400);
if (PROBE) {
  // detection probes, run repeatedly while domains are (maybe) on
  let n = 0;
  const tick = () => {
    const hits = {};
    const e = new Error('probe'); Object.defineProperty(e, 'stack', {get() { hits.errStack = true; return 'x'; }});
    const o = {}; Object.defineProperty(o, 'g', {get() { hits.objGetter = true; return 1; }, enumerable: true});
    const p = new Proxy({}, {ownKeys(t) { hits.proxyOwnKeys = true; return Reflect.ownKeys(t); }, get(t, k) { hits.proxyGet = String(k); return Reflect.get(t, k); }});
    const tp = {toString() { hits.toString = true; return 's'; }, [Symbol.toPrimitive]() { hits.toPrimitive = true; return 's'; }};
    console.debug('P-probe', e, o, p, tp);
    // timing: serialization cost of a big object
    const big = {}; for (let i = 0; i < 2000; i++) big['k' + i] = {i};
    const t0 = performance.now(); for (let i = 0; i < 50; i++) console.debug('P-big', big); const dt = performance.now() - t0;
    rep('probe', {n, hits, dt});
    if (++n < 6) setTimeout(tick, 400);
  };
  setTimeout(tick, 200);
}
</script>
<script>throw new Error('T-script-throw');</script>
</body></html>`;

const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  reqLog.push({ url: req.url, headers: req.headers });
  if (u.pathname === '/report') {
    let b = '';
    req.on('data', (c) => (b += c));
    req.on('end', () => {
      reports.push({ run: u.searchParams.get('run'), k: u.searchParams.get('k'), v: JSON.parse(b || 'null') });
      res.end('ok');
    });
    return;
  }
  if (u.pathname === '/test.html') {
    res.writeHead(200, { 'content-type': 'text/html', 'content-security-policy': "img-src 'self'" });
    return res.end(testPage(u.searchParams));
  }
  if (u.pathname === '/ok.txt') return res.end('ok');
  if (u.pathname === '/blank.html') { res.writeHead(200, { 'content-type': 'text/html' }); return res.end('<!doctype html><p>blank</p>'); }
  if (u.pathname === '/fetchloop.html') {
    res.writeHead(200, { 'content-type': 'text/html' });
    return res.end(`<!doctype html><script>(async()=>{const t0=performance.now();for(let i=0;i<200;i++){await (await fetch('/ok.txt?i='+i)).text();}const dt=performance.now()-t0;fetch('/report?run=${u.searchParams.get('run')}&k=fetchloop',{method:'POST',body:JSON.stringify({dt,res:performance.getEntriesByType('resource').length,nav:performance.getEntriesByType('navigation')[0].toJSON()})});})()</script>`);
  }
  let m = u.pathname.match(/^\/s\/(\d+)(e?)$/);
  if (m) {
    const code = +m[1];
    const h = { 'content-type': 'text/html' };
    if (code === 429 || code === 503) h['retry-after'] = '30';
    res.writeHead(code, h);
    return res.end(m[2] ? '' : `<!doctype html><title>${code}</title><p>status ${code}</p>`);
  }
  if (u.pathname === '/redir/a') { res.writeHead(302, { location: '/redir/b' }); return res.end(); }
  if (u.pathname === '/redir/b') { res.writeHead(301, { location: '/s/404' }); return res.end(); }
  if (u.pathname === '/xredir/a') { res.writeHead(302, { location: `http://localhost:${PORT}/redir/b` }); return res.end(); }
  if (u.pathname === '/xredir/e') { res.writeHead(302, { location: '/s/404e' }); return res.end(); }
  res.writeHead(404, { 'content-type': 'text/plain' });
  res.end('nope');
});

// ---------- CDP client ----------
class CDP {
  constructor(ws) {
    this.ws = ws; this.id = 0; this.pending = new Map(); this.listeners = [];
    ws.addEventListener('message', (ev) => {
      const m = JSON.parse(ev.data);
      if (m.id && this.pending.has(m.id)) {
        const { res, rej } = this.pending.get(m.id); this.pending.delete(m.id);
        m.error ? rej(new Error(m.error.message)) : res(m.result);
      } else if (m.method) this.listeners.forEach((l) => l(m));
    });
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id;
    this.ws.send(JSON.stringify({ id, method, params, sessionId }));
    return new Promise((res, rej) => this.pending.set(id, { res, rej }));
  }
  on(fn) { this.listeners.push(fn); return () => (this.listeners = this.listeners.filter((x) => x !== fn)); }
}

function summarize(m) {
  const p = m.params;
  switch (m.method) {
    case 'Log.entryAdded': return { ev: 'Log.entryAdded', source: p.entry.source, level: p.entry.level, text: p.entry.text.slice(0, 140), url: p.entry.url, ts: p.entry.timestamp };
    case 'Runtime.consoleAPICalled': return { ev: 'Runtime.consoleAPICalled', type: p.type, text: (p.args || []).map((a) => a.value ?? a.description ?? a.type).join(' ').slice(0, 100), ts: p.timestamp };
    case 'Runtime.exceptionThrown': return { ev: 'Runtime.exceptionThrown', text: p.exceptionDetails.text, desc: (p.exceptionDetails.exception?.description || '').split('\n')[0], ts: p.timestamp };
    case 'Audits.issueAdded': return { ev: 'Audits.issueAdded', code: p.issue.code, details: JSON.stringify(p.issue.details).slice(0, 200) };
    case 'Runtime.bindingCalled': return { ev: 'Runtime.bindingCalled', name: p.name, payload: p.payload.slice(0, 200) };
    default: return null;
  }
}

async function main() {
  // dead port
  const tmp = net.createServer(); await new Promise((r) => tmp.listen(0, '127.0.0.1', r));
  DEAD_PORT = tmp.address().port; await new Promise((r) => tmp.close(r));
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  PORT = server.address().port;
  const BASE = `http://127.0.0.1:${PORT}`;

  fs.rmSync(PROFILE, { recursive: true, force: true });
  fs.mkdirSync(PROFILE, { recursive: true });
  const chrome = spawn(CHROME, ['--headless=new', `--user-data-dir=${PROFILE}`, '--remote-debugging-port=0', '--no-first-run', '--no-default-browser-check', 'about:blank'], { stdio: 'ignore' });
  const out = { chromePid: chrome.pid };
  let ws;
  try {
    const portFile = path.join(PROFILE, 'DevToolsActivePort');
    for (let i = 0; i < 100 && !fs.existsSync(portFile); i++) await sleep(100);
    const [dport, dpath] = fs.readFileSync(portFile, 'utf8').trim().split('\n');
    ws = new WebSocket(`ws://127.0.0.1:${dport}${dpath}`);
    await new Promise((r, j) => { ws.addEventListener('open', r); ws.addEventListener('error', j); });
    const c = new CDP(ws);
    out.version = await c.send('Browser.getVersion');

    async function newTab(url = 'about:blank') {
      const { targetId } = await c.send('Target.createTarget', { url });
      const { sessionId } = await c.send('Target.attachToTarget', { targetId, flatten: true });
      return { targetId, sessionId };
    }
    async function navWait(s, url) {
      let loaded;
      const p = new Promise((r) => (loaded = r));
      const off = c.on((m) => { if (m.sessionId === s && m.method === 'Page.loadEventFired') loaded(); });
      const nav = await c.send('Page.navigate', { url }, s);
      await Promise.race([p, sleep(5000)]); off();
      return nav;
    }
    const DOMAINS = { log: ['Log.enable'], runtime: ['Runtime.enable'], both: ['Log.enable', 'Runtime.enable'], logaudits: ['Log.enable', 'Audits.enable'], none: [] };

    // ---------- Question A ----------
    out.A = {};
    for (const mode of Object.keys(DOMAINS)) {
      for (const timing of ['after', 'before']) {
        if (mode === 'none' && timing === 'before') continue;
        const run = `${mode}-${timing}`;
        const evs = [];
        let tab;
        if (timing === 'after') {
          tab = await newTab();
          const off = c.on((m) => { if (m.sessionId === tab.sessionId) { const s = summarize(m); if (s) evs.push(s); } });
          await c.send('Page.enable', {}, tab.sessionId);
          for (const d of DOMAINS[mode]) await c.send(d, {}, tab.sessionId);
          await navWait(tab.sessionId, `${BASE}/test.html?run=${run}&probe=1`);
          await sleep(3500);
          off();
        } else {
          // load the page in a fresh target with no session, let everything fire, then attach + enable
          const { targetId } = await c.send('Target.createTarget', { url: `${BASE}/test.html?run=${run}&probe=0` });
          await sleep(2000);
          const { sessionId } = await c.send('Target.attachToTarget', { targetId, flatten: true });
          tab = { targetId, sessionId };
          const off = c.on((m) => { if (m.sessionId === sessionId) { const s = summarize(m); if (s) evs.push({ ...s, afterEnable: true }); } });
          for (const d of DOMAINS[mode]) await c.send(d, {}, sessionId);
          await sleep(1500);
          off();
        }
        out.A[run] = { events: evs.filter((e) => !/P-big/.test(e.text || '')), probeBigCount: evs.filter((e) => /P-big/.test(e.text || '')).length, reports: reports.filter((r) => r.run === run) };
        await c.send('Target.closeTarget', { targetId: tab.targetId });
      }
    }

    // ---------- Injected isolated-world listener (no Runtime/Log) ----------
    {
      const run = 'inject';
      const tab = await newTab();
      const s = tab.sessionId;
      const evs = [];
      const off = c.on((m) => { if (m.sessionId === s) { const x = summarize(m); if (x) evs.push(x); } });
      await c.send('Page.enable', {}, s);
      const src = `(() => {
        const buf = globalThis.__wbuf = [];
        const push = (o) => { buf.push(o); try { globalThis.__wb && globalThis.__wb(JSON.stringify(o)); } catch (e) {} };
        window.addEventListener('error', (e) => push({t: 'error', msg: e.message, file: e.filename, line: e.lineno, hasErrorObj: e.error != null, target: e.target && e.target.tagName}), true);
        window.addEventListener('unhandledrejection', (e) => push({t: 'unhandledrejection', reasonVisible: e.reason !== undefined, reason: String(e.reason && e.reason.message || e.reason)}));
        document.addEventListener('securitypolicyviolation', (e) => push({t: 'csp', dir: e.violatedDirective, blocked: e.blockedURI}));
        push({t: 'installed', hasConsoleSameAsMain: false});
      })()`;
      const add = await c.send('Page.addScriptToEvaluateOnNewDocument', { source: src, worldName: 'wu_probe' }, s);
      let bindingErr = null;
      try { await c.send('Runtime.addBinding', { name: '__wb', executionContextName: 'wu_probe' }, s); } catch (e) { bindingErr = e.message; }
      await navWait(s, `${BASE}/test.html?run=${run}&probe=0`);
      await sleep(1500);
      const tree = await c.send('Page.getFrameTree', {}, s);
      const frameId = tree.frameTree.frame.id;
      const iw = await c.send('Page.createIsolatedWorld', { frameId, worldName: 'wu_probe' }, s);
      const r1 = await c.send('Runtime.evaluate', { expression: 'JSON.stringify(globalThis.__wbuf || null)', contextId: iw.executionContextId, returnByValue: true }, s);
      const iw2 = await c.send('Page.createIsolatedWorld', { frameId, worldName: 'wu_probe' }, s);
      off();
      out.A.inject = { addScriptId: add.identifier, bindingErr, contextIds: [iw.executionContextId, iw2.executionContextId], buf: JSON.parse(r1.result.value), cdpEvents: evs, reports: reports.filter((r) => r.run === run) };
      await c.send('Target.closeTarget', { targetId: tab.targetId });
    }

    // ---------- Question B: HTTP status ----------
    out.B = {};
    const paths = ['/s/200', '/s/404', '/s/404e', '/s/429', '/s/429e', '/s/503', '/s/503e', '/redir/a', '/xredir/a', '/xredir/e'];
    const navExpr = `(() => { const e = performance.getEntriesByType('navigation')[0]; return JSON.stringify(e ? {responseStatus: e.responseStatus, redirectCount: e.redirectCount, name: e.name, type: e.type, transferSize: e.transferSize} : null) + ' | href=' + location.href + ' | title=' + document.title + ' | body=' + (document.body ? document.body.innerText.slice(0, 120).replace(/\\n+/g, ' / ') : ''); })()`;
    for (const withNet of [false, true]) {
      const tab = await newTab();
      const s = tab.sessionId;
      await c.send('Page.enable', {}, s);
      const evs = [];
      const off = c.on((m) => {
        if (m.sessionId !== s) return;
        const p = m.params;
        if (m.method === 'Network.responseReceived' && p.type === 'Document') evs.push({ ev: 'responseReceived', url: p.response.url, status: p.response.status, statusText: p.response.statusText, retryAfter: p.response.headers['retry-after'] ?? p.response.headers['Retry-After'], loaderId: p.loaderId });
        if (m.method === 'Network.requestWillBeSent' && p.type === 'Document') evs.push({ ev: 'requestWillBeSent', url: p.request.url, redirectStatus: p.redirectResponse?.status, loaderId: p.loaderId });
        if (m.method === 'Network.loadingFailed' && p.type === 'Document') evs.push({ ev: 'loadingFailed', errorText: p.errorText });
      });
      if (withNet) await c.send('Network.enable', {}, s);
      for (const pth of paths) {
        evs.length = 0;
        const nav = await navWait(s, BASE + pth);
        await sleep(300);
        const ev = await c.send('Runtime.evaluate', { expression: navExpr, returnByValue: true }, s);
        const tree = await c.send('Page.getFrameTree', {}, s);
        const fr = tree.frameTree.frame;
        let iso = null;
        try {
          const iw = await c.send('Page.createIsolatedWorld', { frameId: fr.id, worldName: 'wu_nav' }, s);
          iso = (await c.send('Runtime.evaluate', { expression: "performance.getEntriesByType('navigation')[0]?.responseStatus", contextId: iw.executionContextId, returnByValue: true }, s)).result.value;
        } catch (e) { iso = 'ERR ' + e.message; }
        out.B[`${withNet ? 'net' : 'nonet'} ${pth}`] = { navigate: { errorText: nav.errorText, loaderId: !!nav.loaderId }, frameUrl: fr.url, unreachableUrl: fr.unreachableUrl, page: ev.result.value, isolatedResponseStatus: iso, network: withNet ? [...evs] : undefined };
      }
      off();
      // Network.enable side effects: server-seen headers and fetch loop timing
      const run = `fetchloop-${withNet ? 'net' : 'nonet'}`;
      const before = reqLog.length;
      await navWait(s, `${BASE}/fetchloop.html?run=${run}`);
      for (let i = 0; i < 50 && !reports.find((r) => r.run === run); i++) await sleep(100);
      out.B[run] = { report: reports.find((r) => r.run === run)?.v, sampleHeaders: reqLog.slice(before).find((r) => r.url.startsWith('/ok.txt'))?.headers, docHeaders: reqLog.slice(before).find((r) => r.url.startsWith('/fetchloop'))?.headers };
      await c.send('Target.closeTarget', { targetId: tab.targetId });
    }
    await c.send('Browser.close').catch(() => {});
  } finally {
    try { ws && ws.close(); } catch {}
    await sleep(800);
    try { chrome.kill(); } catch {}
    server.close();
    fs.writeFileSync(path.join(HERE, 'results.json'), JSON.stringify(out, null, 1));
    console.log('done');
  }
}
main().catch((e) => { console.error(e); process.exitCode = 1; });
