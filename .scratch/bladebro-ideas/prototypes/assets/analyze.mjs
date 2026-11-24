// Medians per site and mode from results.jsonl, plus extension-pattern coverage.
import fs from "node:fs";
import path from "node:path";
import { ASSETS } from "./cdp.mjs";
import { EXTS } from "./load.mjs";

const rows = fs.readFileSync(path.join(ASSETS, "results.jsonl"), "utf8").trim().split("\n").map((l) => JSON.parse(l));
const med = (a) => {
  a = a.filter((x) => typeof x === "number").sort((x, y) => x - y);
  if (!a.length) return null;
  const m = Math.floor(a.length / 2);
  return a.length % 2 ? a[m] : Math.round((a[m - 1] + a[m]) / 2);
};
const sites = [...new Set(rows.map((r) => r.site))];
const kb = (x) => (x == null ? "-" : Math.round(x / 1024));
console.log("site | mode | n | DCL ms | load ms | KB@load | KB@load+2s | AX nodes | AX non-ignored | innerText | blocked | inert done | errors");
for (const site of sites) {
  for (const mode of ["none", "fetch", "urlpattern", "urls"]) {
    const rs = rows.filter((r) => r.site === site && r.mode === mode);
    if (!rs.length) continue;
    const g = (k) => med(rs.map((r) => r[k]));
    console.log(
      [site, mode, rs.length, g("dcl"), g("load"), kb(g("bytesAtLoad")), kb(g("bytes")), g("ax"), g("axShown"), g("textLen"), g("blocked"), g("leakedInert"),
        rs.filter((r) => r.error || r.navError || r.load == null).length].join(" | "),
    );
  }
}
// Fetch mode: which resource types paused on real sites
const pausedTypes = {};
for (const r of rows.filter((r) => r.mode === "fetch")) for (const [t, n] of Object.entries(r.paused || {})) pausedTypes[t] = (pausedTypes[t] || 0) + n;
console.log("\nresource types that paused in fetch mode, all sites:", JSON.stringify(pausedTypes));

// Coverage: share of Image/Font/Media URLs (no-blocking runs) the extension patterns match.
const pats = EXTS.map((e) => new URLPattern(`*://*:*/*.${e}`));
const glob = EXTS.map((e) => new RegExp("^.*\\." + e + "$", "i"));
console.log("\ncoverage of inert URLs by extension patterns (no-blocking runs, unique URLs):");
for (const site of sites) {
  const urls = new Set(rows.filter((r) => r.site === site && r.mode === "none").flatMap((r) => r.inertUrls || []));
  let up = 0, gl = 0;
  const miss = [];
  for (const u of urls) {
    if (pats.some((p) => p.test(u))) up++; else miss.push(u);
    if (glob.some((g) => g.test(u))) gl++;
  }
  console.log(`${site}: ${urls.size} inert URLs; URLPattern form matches ${up} (${Math.round((100 * up) / urls.size)}%); strict '*.ext' end-of-URL match ${gl}; sample misses: ${miss.slice(0, 3).map((u) => u.slice(0, 90)).join("  ")}`);
}
