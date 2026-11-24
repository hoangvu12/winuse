// One cold page load in a fresh browser context under a blocking mode.
import { sleep } from "./cdp.mjs";

export const EXTS = ["png", "jpg", "jpeg", "gif", "webp", "avif", "svg", "woff", "woff2", "ttf", "mp4", "webm"];
export const UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36";

export async function applyMode(cdp, sid, mode, st, { delayMs = 0 } = {}) {
  if (mode === "fetch") {
    cdp.on((m, p, s) => {
      if (m !== "Fetch.requestPaused" || s !== sid) return;
      st.paused[p.resourceType] = (st.paused[p.resourceType] || 0) + 1;
      const go = () =>
        cdp.send("Fetch.failRequest", { requestId: p.requestId, errorReason: "BlockedByClient" }, sid).catch(() => {});
      delayMs ? setTimeout(go, delayMs) : go();
    });
    await cdp.send(
      "Fetch.enable",
      { patterns: ["Image", "Font", "Media"].map((resourceType) => ({ resourceType, requestStage: "Request" })) },
      sid,
    );
  } else if (mode === "urls") {
    await cdp.send("Network.setBlockedURLs", { urls: EXTS.map((e) => `*.${e}`) }, sid);
  } else if (mode === "urlpattern") {
    await cdp.send(
      "Network.setBlockedURLs",
      { urlPatterns: EXTS.map((e) => ({ urlPattern: `*://*:*/*.${e}`, block: true })) },
      sid,
    );
  } else if (mode !== "none") throw new Error("mode " + mode);
}

export async function loadOnce(cdp, url, mode, { timeout = 45000, settle = 2000, ax = true, delayMs = 0 } = {}) {
  const { browserContextId } = await cdp.send("Target.createBrowserContext", { disposeOnDetach: true });
  const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank", browserContextId });
  const { sessionId: sid } = await cdp.send("Target.attachToTarget", { targetId, flatten: true });
  const st = {
    url, mode, paused: {}, bytes: 0, bytesAtLoad: null, reqs: 0, blocked: 0, failed: 0,
    byType: {}, blockedByType: {}, leakedInert: 0, dcl: null, load: null, wallLoad: null, ax: null, axShown: null,
  };
  const types = new Map();
  const off = cdp.on((m, p, s) => {
    if (s !== sid) return;
    if (m === "Network.requestWillBeSent") {
      st.reqs++;
      types.set(p.requestId, p.type);
      if (["Image", "Font", "Media"].includes(p.type) && !p.request.url.startsWith("data:")) (st.inertUrls ||= []).push(p.request.url);
    } else if (m === "Network.loadingFinished") {
      st.bytes += p.encodedDataLength || 0;
      const t = types.get(p.requestId);
      st.byType[t] = (st.byType[t] || 0) + (p.encodedDataLength || 0);
      if (["Image", "Font", "Media"].includes(t)) st.leakedInert++;
    } else if (m === "Network.loadingFailed") {
      if (p.blockedReason || /BLOCKED_BY_CLIENT/.test(p.errorText || "")) {
        st.blocked++;
        st.blockedByType[p.type] = (st.blockedByType[p.type] || 0) + 1;
      } else st.failed++;
    }
  });
  try {
    await cdp.send("Network.enable", {}, sid);
    await cdp.send("Network.clearBrowserCache", {}, sid);
    await cdp.send("Network.setUserAgentOverride", { userAgent: UA }, sid);
    await cdp.send("Page.enable", {}, sid);
    await applyMode(cdp, sid, mode, st, { delayMs });
    const loadP = cdp.waitFor("Page.loadEventFired", sid, () => true, timeout).then(() => Date.now()).catch(() => null);
    const t0 = Date.now();
    const nav = await cdp.send("Page.navigate", { url }, sid);
    if (nav.errorText) st.navError = nav.errorText;
    const tl = await loadP;
    st.wallLoad = tl ? tl - t0 : null;
    st.bytesAtLoad = st.bytes;
    const nt = await cdp.send(
      "Runtime.evaluate",
      {
        expression: `(() => { const n = performance.getEntriesByType('navigation')[0]; return n ? { dcl: n.domContentLoadedEventStart, load: n.loadEventStart, status: n.responseStatus, title: document.title } : null })()`,
        returnByValue: true,
      },
      sid,
    );
    const v = nt.result.value || {};
    st.dcl = v.dcl ? Math.round(v.dcl) : null;
    st.load = v.load ? Math.round(v.load) : null;
    st.status = v.status;
    st.title = (v.title || "").slice(0, 60);
    await sleep(settle);
    if (ax) {
      const tree = await cdp.send("Accessibility.getFullAXTree", {}, sid);
      st.ax = tree.nodes.length;
      st.axShown = tree.nodes.filter((n) => !n.ignored).length;
      const txt = await cdp.send("Runtime.evaluate", { expression: "document.body ? document.body.innerText.length : 0", returnByValue: true }, sid);
      st.textLen = txt.result.value;
    }
  } finally {
    off();
    await cdp.send("Target.disposeBrowserContext", { browserContextId }).catch(() => {});
  }
  return st;
}
