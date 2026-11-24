// Does Fetch.enable / Fetch.requestPaused work through chrome.debugger?
// Loads ./testext unpacked into a throwaway headless Chromium (temp profile).
import path from "node:path";
import { launch, CDP, sleep, ASSETS, CHROMIUM, CHROME } from "./cdp.mjs";
import { start } from "./server.mjs";

const exe = process.argv[2] === "chrome" ? CHROME : CHROMIUM;
const ext = path.join(ASSETS, "testext");
const s = await start(8767);
const b = await launch({
  exe, name: "ext", port: 9344,
  extra: [`--load-extension=${ext}`, `--disable-extensions-except=${ext}`],
});
const O = await CDP.connect(b.ver.webSocketDebuggerUrl);
const log = (...a) => console.log(...a);
try {
  log("browser:", b.ver.Browser);
  let sw;
  for (let i = 0; i < 50 && !sw; i++) {
    const { targetInfos } = await O.send("Target.getTargets");
    sw = targetInfos.find((t) => t.type === "service_worker" && t.url.startsWith("chrome-extension://") && t.url.endsWith("/sw.js"));
    if (!sw && i === 49) log("service workers seen:", targetInfos.filter((t) => t.type === "service_worker").map((t) => t.url).join(" "));
    if (!sw) await sleep(200);
  }
  if (!sw) throw new Error("extension service worker not found (extension not loaded)");
  log("extension worker:", sw.url);
  const { sessionId: W } = await O.send("Target.attachToTarget", { targetId: sw.targetId, flatten: true });
  const inSW = async (expr) => {
    const r = await O.send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true }, W);
    if (r.exceptionDetails) return "EXCEPTION " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text);
    return r.result.value;
  };

  const { targetId } = await O.send("Target.createTarget", { url: "about:blank" });
  const { sessionId: P } = await O.send("Target.attachToTarget", { targetId, flatten: true });
  const reqs = new Map();
  O.on((m, p, sid) => {
    if (sid !== P) return;
    if (m === "Network.requestWillBeSent") reqs.set(p.requestId, { type: p.type, url: p.request.url, st: "pending" });
    else if (m === "Network.loadingFinished" && reqs.has(p.requestId)) reqs.get(p.requestId).st = "finished";
    else if (m === "Network.loadingFailed" && reqs.has(p.requestId)) reqs.get(p.requestId).st = "failed:" + p.errorText;
  });
  await O.send("Network.enable", {}, P);
  await O.send("Network.setCacheDisabled", { cacheDisabled: true }, P);
  const inertSummary = () => {
    const c = {};
    for (const r of reqs.values()) if (["Image", "Font", "Media"].includes(r.type)) c[r.st] = (c[r.st] || 0) + 1;
    return JSON.stringify(c);
  };

  log("attach:", await inSW(`chrome.debugger.attach({targetId:${JSON.stringify(targetId)}}, "1.3").then(()=>"ok", e=>"ERR "+e.message)`));
  log("Fetch.enable:", await inSW(`chrome.debugger.sendCommand({targetId:${JSON.stringify(targetId)}}, "Fetch.enable", {patterns:["Image","Font","Media"].map(t=>({resourceType:t,requestStage:"Request"}))}).then(r=>"ok "+JSON.stringify(r), e=>"ERR "+e.message)`));

  // Phase 1: worker answers every pause with failRequest.
  reqs.clear(); s.hits.length = 0;
  await O.send("Page.navigate", { url: s.base + "/page.html" }, P);
  await sleep(2500);
  log("\nphase 1 (worker answers):");
  log("  requestPaused seen by worker:", JSON.stringify(await inSW("state.events")));
  log("  worker errors:", JSON.stringify(await inSW("state.errors")));
  log("  inert outcomes:", inertSummary(), " server hits:", s.hits.map((h) => h.url).join(" "));

  // Phase 2: worker stops answering (what happens if winuse's connection closes:
  // the extension keeps the debugger attached until its 5 min idle detach).
  await inSW("state.answer = false; state.events = []; true");
  reqs.clear(); s.hits.length = 0;
  await O.send("Page.navigate", { url: s.base + "/page.html?2" }, P);
  await sleep(3000);
  log("\nphase 2 (worker stops answering):");
  log("  paused:", (await inSW("state.events")).length, " inert outcomes after 3 s:", inertSummary());
  log("  detach:", await inSW(`chrome.debugger.detach({targetId:${JSON.stringify(targetId)}}).then(()=>"ok", e=>"ERR "+e.message)`));
  await sleep(2000);
  log("  inert outcomes 2 s after chrome.debugger.detach:", inertSummary(), " inert server hits:", s.hits.filter((h) => /img|font|mp4/.test(h.url)).length);

  // Phase 3: Network.setBlockedURLs (both forms) through chrome.debugger.
  log("\nphase 3 (Network.setBlockedURLs via chrome.debugger):");
  log("  attach:", await inSW(`chrome.debugger.attach({targetId:${JSON.stringify(targetId)}}, "1.3").then(()=>"ok", e=>"ERR "+e.message)`));
  log("  Network.enable:", await inSW(`chrome.debugger.sendCommand({targetId:${JSON.stringify(targetId)}}, "Network.enable", {}).then(()=>"ok", e=>"ERR "+e.message)`));
  log("  setBlockedURLs urlPatterns:", await inSW(`chrome.debugger.sendCommand({targetId:${JSON.stringify(targetId)}}, "Network.setBlockedURLs", {urlPatterns:["png","jpg","svg","woff2","mp4"].map(e=>({urlPattern:"*://*:*/*."+e, block:true}))}).then(()=>"ok", e=>"ERR "+e.message)`));
  reqs.clear(); s.hits.length = 0;
  await O.send("Page.navigate", { url: s.base + "/page.html?3" }, P);
  await sleep(2000);
  log("  inert outcomes:", inertSummary(), " server hits:", s.hits.map((h) => h.url).join(" "));
  log("  detach:", await inSW(`chrome.debugger.detach({targetId:${JSON.stringify(targetId)}}).then(()=>"ok", e=>"ERR "+e.message)`));
  log("  onDetach reasons:", JSON.stringify(await inSW("state.detaches")));
} catch (e) {
  log("FAILED:", e.message);
} finally {
  await b.close(O);
  O.close();
  s.srv.close();
}
