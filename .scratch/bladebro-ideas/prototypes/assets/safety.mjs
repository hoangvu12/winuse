// Safety tests on a local page: type-limited patterns, slow driver,
// and what happens to paused requests when the intercepting client goes away.
import { launch, CDP, sleep } from "./cdp.mjs";
import { start } from "./server.mjs";
import { loadOnce } from "./load.mjs";

const PATTERNS = ["Image", "Font", "Media"].map((resourceType) => ({ resourceType, requestStage: "Request" }));
const s = await start(8766);
const b = await launch({ name: "safety", port: 9343 });
const wsUrl = b.ver.webSocketDebuggerUrl;
const O = await CDP.connect(wsUrl); // observer
const log = (...a) => console.log(...a);
const evalIn = async (sid, expr) =>
  (await O.send("Runtime.evaluate", { expression: expr, returnByValue: true, awaitPromise: true }, sid)).result.value;

async function observedTab() {
  const { targetId } = await O.send("Target.createTarget", { url: "about:blank" });
  const { sessionId } = await O.send("Target.attachToTarget", { targetId, flatten: true });
  const reqs = new Map(); // requestId -> {url,type,state}
  let loadFired = 0;
  O.on((m, p, sid) => {
    if (sid !== sessionId) return;
    if (m === "Network.requestWillBeSent") reqs.set(p.requestId, { url: p.request.url.replace(s.base, ""), type: p.type, state: "pending" });
    else if (m === "Network.loadingFinished" && reqs.has(p.requestId)) reqs.get(p.requestId).state = "finished";
    else if (m === "Network.loadingFailed" && reqs.has(p.requestId)) reqs.get(p.requestId).state = "failed:" + p.errorText;
    else if (m === "Page.loadEventFired") loadFired++;
  });
  await O.send("Network.enable", {}, sessionId);
  await O.send("Page.enable", {}, sessionId);
  await O.send("Network.setCacheDisabled", { cacheDisabled: true }, sessionId);
  const summary = (filter = () => true) => {
    const c = {};
    for (const r of reqs.values()) if (filter(r)) c[r.state] = (c[r.state] || 0) + 1;
    return c;
  };
  return { targetId, sessionId, reqs, summary, loaded: () => loadFired };
}

async function interceptor(targetId) {
  const I = await CDP.connect(wsUrl);
  const { sessionId } = await I.send("Target.attachToTarget", { targetId, flatten: true });
  const paused = [];
  I.on((m, p, sid) => { if (m === "Fetch.requestPaused" && sid === sessionId) paused.push(p); });
  await I.send("Fetch.enable", { patterns: PATTERNS }, sessionId);
  return { I, sessionId, paused };
}

const inert = (r) => ["Image", "Font", "Media"].includes(r.type);
const live = (r) => !inert(r);

// ---------- A: which requests pause under type-limited patterns ----------
{
  log("\n== A: type-limited patterns, every pause answered with continueRequest");
  const tab = await observedTab();
  const { I, sessionId, paused } = await interceptor(tab.targetId);
  I.on((m, p, sid) => { if (m === "Fetch.requestPaused" && sid === sessionId) I.send("Fetch.continueRequest", { requestId: p.requestId }, sessionId).catch(() => {}); });
  s.hits.length = 0;
  await O.send("Page.navigate", { url: s.base + "/page.html" }, tab.sessionId);
  await sleep(1500);
  const types = {};
  for (const p of paused) types[p.resourceType] = (types[p.resourceType] || 0) + 1;
  log("paused by type:", JSON.stringify(types));
  log("non-inert requests and their state:", [...tab.reqs.values()].filter(live).map((r) => `${r.type}:${r.url}=${r.state}`).join(" "));
  log("page results:", JSON.stringify(await evalIn(tab.sessionId, "({app: window.__app, ...window.__r})")));
  I.close();
  await O.send("Target.closeTarget", { targetId: tab.targetId });
}

// ---------- B: slow driver (2 s before each failRequest) ----------
{
  log("\n== B: slow driver, fresh context each run, local page");
  for (const delayMs of [0, 2000]) {
    s.hits.length = 0;
    const st = await loadOnce(O, s.base + "/page.html", "fetch", { delayMs, settle: 3000, ax: false });
    const t0 = s.hits[0]?.t;
    const at = (u) => { const h = s.hits.find((x) => x.url.startsWith(u)); return h ? h.t - t0 : null; };
    log(`delay=${delayMs}ms dcl=${st.dcl} load=${st.load} wallLoad=${st.wallLoad} blocked=${st.blocked} paused=${JSON.stringify(st.paused)} serverHit(ms after html): app.js=${at("/app.js")} /api/data=${at("/api/data")} /api/xhr=${at("/api/xhr")}`);
  }
}

// ---------- C: interceptor goes away while requests are paused ----------
async function scenario(name, goAway, waitMs = 5000) {
  log(`\n== C/${name}`);
  const tab = await observedTab();
  const icp = await interceptor(tab.targetId);
  s.hits.length = 0;
  await O.send("Page.navigate", { url: s.base + "/page.html" }, tab.sessionId);
  await sleep(2000);
  log(`before: paused=${icp.paused.length} inert=${JSON.stringify(tab.summary(inert))} other=${JSON.stringify(tab.summary(live))} loadEvent=${tab.loaded()}`);
  const t = Date.now();
  await goAway(icp, tab);
  await sleep(waitMs);
  const imgHits = s.hits.filter((h) => h.url.startsWith("/img/") || h.url === "/font.woff2" || h.url === "/v.mp4").length;
  log(`after ${name} +${Date.now() - t}ms: inert=${JSON.stringify(tab.summary(inert))} loadEvent=${tab.loaded()} inertServerHits=${imgHits}`);
  // new requests in the same tab
  const nf = await Promise.race([
    evalIn(tab.sessionId, "fetch('/api/new').then(r=>r.text())"),
    sleep(4000).then(() => "HUNG(4s)"),
  ]);
  const ni = await Promise.race([
    evalIn(tab.sessionId, "new Promise(r=>{const i=new Image(); i.onload=()=>r('loaded'); i.onerror=()=>r('error'); i.src='/img/new-'+Date.now()+'.png'})"),
    sleep(4000).then(() => "HUNG(4s)"),
  ]);
  log(`new fetch: ${nf}; new image: ${ni}`);
  icp.I.close();
  await O.send("Target.closeTarget", { targetId: tab.targetId }).catch(() => {});
}

await scenario("C1 close interceptor websocket", async (icp) => icp.I.close());
await scenario("C2 Target.detachFromTarget (websocket stays open)", async (icp) =>
  icp.I.send("Target.detachFromTarget", { sessionId: icp.sessionId }));
await scenario("C3 Fetch.disable", async (icp) => icp.I.send("Fetch.disable", {}, icp.sessionId));
await scenario("C4 interceptor stays connected and never answers (60 s)", async () => {}, 60000);

await b.close(O);
O.close();
s.srv.close();
