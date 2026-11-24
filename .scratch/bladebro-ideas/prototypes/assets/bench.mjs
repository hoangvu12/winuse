// Public-site benchmark: 6 sites x {none, fetch, urlpattern} x 3 cold runs (54 loads)
// plus one run per site of the deprecated `urls` wildcard form (6 loads). 60 total.
import fs from "node:fs";
import path from "node:path";
import { launch, CDP, sleep, ASSETS } from "./cdp.mjs";
import { loadOnce } from "./load.mjs";

export const SITES = {
  news: "https://www.theguardian.com/international",
  shop: "https://www.ikea.com/us/en/cat/sofas-fu003/",
  recipe: "https://pinchofyum.com/the-best-soft-chocolate-chip-cookies",
  youtube: "https://www.youtube.com/",
  wiki: "https://en.wikipedia.org/wiki/Bird",
  docs: "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array",
};
const MODES = ["none", "fetch", "urlpattern"];
const out = path.join(ASSETS, "results.jsonl");
const b = await launch({ name: "bench", port: 9342 });
const cdp = await CDP.connect(b.ver.webSocketDebuggerUrl);
let n = 0;
async function one(site, mode, run) {
  n++;
  let st;
  try {
    st = await loadOnce(cdp, SITES[site], mode, { timeout: 45000, settle: 2000 });
  } catch (e) {
    st = { error: String(e.message || e) };
  }
  st.site = site; st.mode = mode; st.run = run; st.n = n;
  fs.appendFileSync(out, JSON.stringify(st) + "\n");
  console.log(n, site, mode, run, st.status, st.dcl, st.load, st.wallLoad, st.bytesAtLoad, st.bytes, st.blocked, st.leakedInert, st.ax, st.axShown, st.navError || st.error || "", st.title);
  await sleep(2500);
}
try {
  for (let run = 1; run <= 3; run++) {
    for (const site of Object.keys(SITES)) {
      // rotate mode order per run so no mode always goes first
      const order = MODES.map((_, i) => MODES[(i + run - 1) % MODES.length]);
      for (const mode of order) await one(site, mode, run);
    }
  }
  for (const site of Object.keys(SITES)) await one(site, "urls", 1);
} finally {
  await b.close(cdp);
  cdp.close();
  console.log("done", n, "loads");
}
