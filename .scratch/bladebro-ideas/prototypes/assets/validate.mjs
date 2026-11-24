// Local check: what each mode blocks on a page with every resource type.
import { launch, CDP } from "./cdp.mjs";
import { start } from "./server.mjs";
import { loadOnce } from "./load.mjs";

const s = await start(8765);
const b = await launch({ name: "val", port: 9341 });
const cdp = await CDP.connect(b.ver.webSocketDebuggerUrl);
try {
  for (const mode of ["none", "fetch", "urls", "urlpattern"]) {
    s.hits.length = 0;
    const st = await loadOnce(cdp, s.base + "/page.html", mode, { settle: 800 });
    const hit = s.hits.map((h) => h.url).sort();
    console.log(mode, JSON.stringify({ paused: st.paused, blockedByType: st.blockedByType, leakedInert: st.leakedInert, dcl: st.dcl, load: st.load, ax: st.ax, axShown: st.axShown }));
    console.log("   server hits:", hit.join(" "));
  }
} finally {
  cdp.close();
  await b.close();
  s.srv.close();
}
