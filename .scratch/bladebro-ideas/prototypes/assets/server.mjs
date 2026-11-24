// Local test server: one page that pulls every resource type.
import http from "node:http";

const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
  "base64",
);

export function page(n = 5) {
  const imgs = Array.from({ length: n }, (_, i) => `<img alt="pic ${i}" src="/img/${i}.png">`).join("\n");
  return `<!doctype html><html><head><title>t</title>
<link rel="stylesheet" href="/style.css">
<script src="/app.js"></script>
</head><body>
<h1>Test page</h1>
${imgs}
<img alt="query" src="/img/q.jpg?w=100">
<img alt="noext" src="/img/noext">
<img alt="svg" src="/img/icon.svg">
<video src="/v.mp4" preload="auto"></video>
<p class="f">font text</p>
<script>
window.__r = {};
fetch('/api/data').then(r => r.text()).then(t => window.__r.fetch = t).catch(e => window.__r.fetch = 'ERR ' + e);
const x = new XMLHttpRequest(); x.open('GET', '/api/xhr');
x.onload = () => window.__r.xhr = x.responseText; x.onerror = () => window.__r.xhr = 'ERR';
x.send();
</script>
</body></html>`;
}

export function start(port = 8765) {
  const hits = [];
  const srv = http.createServer((req, res) => {
    hits.push({ t: Date.now(), url: req.url });
    const u = req.url.split("?")[0];
    if (u === "/" || u.startsWith("/page")) {
      res.writeHead(200, { "content-type": "text/html", "cache-control": "no-store" });
      return res.end(page());
    }
    if (u === "/blank") { res.writeHead(200, { "content-type": "text/html" }); return res.end("<!doctype html><p>blank</p>"); }
    if (u === "/style.css") {
      res.writeHead(200, { "content-type": "text/css" });
      return res.end("@font-face{font-family:F;src:url(/font.woff2) format('woff2')} .f{font-family:F}");
    }
    if (u === "/app.js") { res.writeHead(200, { "content-type": "text/javascript" }); return res.end("window.__app=1;"); }
    if (u.startsWith("/api/")) { res.writeHead(200, { "content-type": "text/plain" }); return res.end("ok " + u); }
    if (u.startsWith("/img/icon.svg")) {
      res.writeHead(200, { "content-type": "image/svg+xml" });
      return res.end('<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>');
    }
    if (u.startsWith("/img/")) { res.writeHead(200, { "content-type": "image/png" }); return res.end(PNG); }
    if (u === "/font.woff2") { res.writeHead(200, { "content-type": "font/woff2" }); return res.end(Buffer.alloc(2000)); }
    if (u === "/v.mp4") { res.writeHead(200, { "content-type": "video/mp4" }); return res.end(Buffer.alloc(5000)); }
    res.writeHead(404); res.end("nf");
  });
  return new Promise((r) => srv.listen(port, "127.0.0.1", () => r({ srv, hits, base: `http://127.0.0.1:${port}` })));
}
