// Experiment driver: launches its own Chrome (temp profile under ./profile),
// loads each page once, runs read_markdown.js / extract_list.js through
// Runtime.evaluate (and Runtime.callFunctionOn for scoped runs), measures
// innerText and the AX tree, and writes everything under ./out/<slug>/.
//
//   node run.mjs [--headful] [--only hn,npm] [--settle 3000]
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CHROME = 'C:/Program Files/Google/Chrome/Application/chrome.exe';
const argv = process.argv.slice(2);
const HEADFUL = argv.includes('--headful');
const onlyIdx = argv.indexOf('--only');
const ONLY = onlyIdx >= 0 ? argv[onlyIdx + 1].split(',') : null;
const settleIdx = argv.indexOf('--settle');
const SETTLE = settleIdx >= 0 ? +argv[settleIdx + 1] : 3500;
const READ_SRC = fs.readFileSync(path.join(HERE, 'js/read_markdown.js'), 'utf8');
const EXTRACT_SRC = fs.readFileSync(path.join(HERE, 'js/extract_list.js'), 'utf8');
const PAGES = JSON.parse(fs.readFileSync(path.join(HERE, 'pages.json'), 'utf8'));

const sleep = ms => new Promise(r => setTimeout(r, ms));

// ---- launch ----
const profile = path.join(HERE, 'profile', 'run-' + Date.now());
fs.mkdirSync(profile, { recursive: true });
const chromeArgs = [
  HEADFUL ? null : '--headless=new',
  `--user-data-dir=${profile}`,
  '--remote-debugging-port=0',
  '--no-first-run', '--no-default-browser-check', '--disable-sync',
  '--window-size=1366,900', 'about:blank',
].filter(Boolean);
const proc = spawn(CHROME, chromeArgs, { stdio: 'ignore' });
console.error('chrome pid', proc.pid, 'profile', profile);
let portFile = path.join(profile, 'DevToolsActivePort');
for (let i = 0; i < 100 && !fs.existsSync(portFile); i++) await sleep(100);
await sleep(200);
const [port, wsPath] = fs.readFileSync(portFile, 'utf8').trim().split('\n');
const ws = new WebSocket(`ws://127.0.0.1:${port}${wsPath}`);
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });

let nextId = 1;
const pending = new Map();
const listeners = [];
ws.onmessage = ev => {
  const m = JSON.parse(ev.data);
  if (m.id && pending.has(m.id)) { const { res, rej } = pending.get(m.id); pending.delete(m.id); m.error ? rej(new Error(m.error.message)) : res(m.result); }
  else if (m.method) for (const l of listeners) l(m);
};
function send(method, params = {}, sessionId, timeout = 60000) {
  const id = nextId++;
  ws.send(JSON.stringify({ id, method, params, sessionId }));
  return new Promise((res, rej) => {
    pending.set(id, { res, rej });
    setTimeout(() => { if (pending.has(id)) { pending.delete(id); rej(new Error(method + ' timed out')); } }, timeout);
  });
}
function waitEvent(name, sessionId, timeout) {
  return new Promise(res => {
    const l = m => { if (m.method === name && m.sessionId === sessionId) { listeners.splice(listeners.indexOf(l), 1); res(true); } };
    listeners.push(l);
    setTimeout(() => { const i = listeners.indexOf(l); if (i >= 0) { listeners.splice(i, 1); res(false); } }, timeout);
  });
}

async function shutdown() {
  try { await send('Browser.close', {}, undefined, 5000); } catch {}
  await sleep(1500);
  if (proc.exitCode === null) { try { process.kill(proc.pid); } catch {} }
}
process.on('SIGINT', async () => { await shutdown(); process.exit(1); });

const evalIn = async (sid, expression) => {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sid);
  if (r.exceptionDetails) throw new Error('eval: ' + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
  return r.result.value;
};
const callOn = async (sid, objectId, src, arg) => {
  const r = await send('Runtime.callFunctionOn', { functionDeclaration: src, objectId, arguments: [{ value: arg }], returnByValue: true }, sid);
  if (r.exceptionDetails) throw new Error('call: ' + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
  return r.result.value;
};
const timed = async f => { const t = Date.now(); const v = await f(); return [v, Date.now() - t]; };

// AX tree: node counts plus a rough "role name" render (non-ignored nodes,
// wrapper roles skipped, StaticText that repeats its parent's name skipped).
function axStats(nodes) {
  const byId = new Map(nodes.map(n => [n.nodeId, n]));
  const skipRoles = new Set(['generic', 'none', 'InlineTextBox', 'LineBreak', 'RootWebArea', 'paragraph', 'Section', 'div']);
  let nonIgnored = 0, lines = 0, chars = 0;
  for (const n of nodes) {
    if (n.ignored) continue;
    nonIgnored++;
    const role = n.role?.value || '';
    const name = (n.name?.value || '').replace(/\s+/g, ' ').trim();
    if (skipRoles.has(role) && !name) continue;
    if (role === 'InlineTextBox') continue;
    if (role === 'StaticText') {
      const p = byId.get(n.parentId);
      if (p && (p.name?.value || '').replace(/\s+/g, ' ').trim() === name) continue;
      if (!name) continue;
    }
    lines++;
    chars += 2 + role.length + (name ? name.length + 3 : 0) + 6; // indent + role + "name" + ref
  }
  return { ax_nodes: nodes.length, ax_non_ignored: nonIgnored, ax_render_lines: lines, ax_render_chars_est: chars };
}

const tok = s => Math.round((typeof s === 'string' ? s.length : JSON.stringify(s).length) / 4);

const summary = [];
for (const pg of PAGES) {
  if (ONLY && !ONLY.includes(pg.slug)) continue;
  const outDir = path.join(HERE, 'out', pg.slug);
  fs.mkdirSync(outDir, { recursive: true });
  const rec = { slug: pg.slug, url: pg.url };
  let targetId;
  try {
    ({ targetId } = await send('Target.createTarget', { url: 'about:blank' }));
    const { sessionId: sid } = await send('Target.attachToTarget', { targetId, flatten: true });
    await send('Page.enable', {}, sid);
    const loaded = waitEvent('Page.loadEventFired', sid, 30000);
    await send('Page.navigate', { url: pg.url }, sid);
    rec.load_event = await loaded;
    await sleep(pg.settle || SETTLE);
    const meta = await evalIn(sid, `({title: document.title, url: location.href, inner: (document.body && document.body.innerText) || '', truth: ${pg.truth ? `[...document.querySelectorAll(${JSON.stringify(pg.truth)})].filter(e=>e.offsetParent!==null||e.getClientRects().length>0).length` : 'null'}})`);
    rec.title = meta.title; rec.final_url = meta.url; rec.truth_selector = pg.truth || null; rec.truth_count = meta.truth;
    rec.innertext_chars = meta.inner.length; rec.innertext_tok = tok(meta.inner);
    fs.writeFileSync(path.join(outDir, 'innertext.txt'), meta.inner);
    const shot = await send('Page.captureScreenshot', { format: 'jpeg', quality: 45 }, sid);
    fs.writeFileSync(path.join(outDir, 'screen.jpg'), Buffer.from(shot.data, 'base64'));

    const [rd, rdMs] = await timed(() => evalIn(sid, `(${READ_SRC}).call(null, {budget: 200000})`));
    rec.read_main = rd.main; rec.read_chars = rd.markdown.length; rec.read_tok = tok(rd.markdown); rec.read_ms = rdMs;
    rec.read_chars_in_8000_budget = Math.min(rd.markdown.length, 8000);
    fs.writeFileSync(path.join(outDir, 'read.md'), rd.markdown);

    const [ex, exMs] = await timed(() => evalIn(sid, `(${EXTRACT_SRC}).call(null, {limit: 100, debug: true})`));
    rec.extract_ms = exMs;
    const exNoDebug = { ...ex }; delete exNoDebug.ranked;
    rec.extract = { container: ex.container_desc || ex.container, count: ex.count, error: ex.error, page_total: ex.page_total, page_claim_text: ex.page_claim_text, partial: ex.partial, paged: ex.paged, confidence: ex.confidence, dupes: ex.duplicates_removed, ranked: ex.ranked };
    rec.extract_json_chars = JSON.stringify(exNoDebug).length; rec.extract_tok = tok(exNoDebug);
    const fields = {};
    for (const it of ex.items || []) for (const k of Object.keys(it)) fields[k] = (fields[k] || 0) + 1;
    rec.extract_fields = fields;
    fs.writeFileSync(path.join(outDir, 'extract.json'), JSON.stringify(ex, null, 1));

    const tb = await evalIn(sid, `(${EXTRACT_SRC}).call(null, {limit: 100, mode: 'table'})`);
    rec.table = tb.error ? { error: tb.error } : { table: tb.table, caption: tb.caption, columns: tb.columns, count: tb.count, rows_total: tb.rows_total, tables_found: tb.tables_found };
    fs.writeFileSync(path.join(outDir, 'table.json'), JSON.stringify(tb, null, 1));
    const ld = await evalIn(sid, `(${EXTRACT_SRC}).call(null, {mode: 'jsonld'})`);
    rec.jsonld = ld.error ? { error: ld.error } : { container: ld.container, title: ld.items[0]?.title, keys: Object.keys(ld.items[0] || {}) };
    fs.writeFileSync(path.join(outDir, 'jsonld.json'), JSON.stringify(ld, null, 1));

    // Scoped runs: the same scripts with a root Element as `this`.
    rec.scoped = [];
    for (const sel of pg.scopes || []) {
      const r = await send('Runtime.evaluate', { expression: `document.querySelector(${JSON.stringify(sel)})` }, sid);
      if (!r.result.objectId) { rec.scoped.push({ sel, error: 'no element' }); continue; }
      const sx = await callOn(sid, r.result.objectId, EXTRACT_SRC, { limit: 100, debug: true });
      const sr = await callOn(sid, r.result.objectId, READ_SRC, { budget: 200000 });
      rec.scoped.push({ sel, extract_container: sx.container_desc, extract_count: sx.count, extract_error: sx.error, first: sx.items?.[0] && { title: sx.items[0].title, url: sx.items[0].url, cells: sx.items[0].cells }, read_chars: sr.markdown.length });
      fs.writeFileSync(path.join(outDir, 'scoped-' + sel.replace(/[^a-z0-9]+/gi, '_').slice(0, 40) + '.json'), JSON.stringify({ sel, extract: sx, read: sr.markdown.slice(0, 3000) }, null, 1));
    }

    await send('DOM.enable', {}, sid);
    await send('Accessibility.enable', {}, sid);
    const [ax, axMs] = await timed(() => send('Accessibility.getFullAXTree', {}, sid));
    Object.assign(rec, axStats(ax.nodes)); rec.ax_ms = axMs;
    rec.ax_tok_est = Math.round(rec.ax_render_chars_est / 4);

    // Short excerpts for the record.
    const ex3 = (ex.items || []).slice(0, 3);
    fs.writeFileSync(path.join(outDir, 'excerpt.txt'),
      `URL: ${meta.url}\nTITLE: ${meta.title}\n\n--- read (main=${rd.main}, ${rd.markdown.length} chars) first 1500 ---\n${rd.markdown.slice(0, 1500)}\n\n--- read last 600 ---\n${rd.markdown.slice(-600)}\n\n--- extract (${ex.container_desc}, count=${ex.count}) first 3 ---\n${JSON.stringify(ex3, null, 1).slice(0, 2500)}\n`);
    console.error(`${pg.slug}: title="${meta.title.slice(0, 60)}" inner=${rec.innertext_chars} read=${rec.read_chars}(${rd.main}) extract=${ex.count ?? ex.error}@${ex.container_desc} ax=${rec.ax_nodes}`);
  } catch (e) {
    rec.error = String(e.message || e);
    console.error(pg.slug, 'ERROR', rec.error);
  }
  try { if (targetId) await send('Target.closeTarget', { targetId }); } catch {}
  fs.writeFileSync(path.join(outDir, 'metrics.json'), JSON.stringify(rec, null, 1));
  summary.push(rec);
}
fs.writeFileSync(path.join(HERE, 'out', `summary-${HEADFUL ? 'headful' : 'headless'}-${Date.now()}.json`), JSON.stringify(summary, null, 1));
await shutdown();
console.error('done; chrome exit', proc.exitCode);
process.exit(0);
