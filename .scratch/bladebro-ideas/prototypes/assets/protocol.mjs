import { launch } from "./cdp.mjs";
const b = await launch({ name: "proto", port: 9340 });
try {
  console.log(b.ver.Browser, b.ver["Protocol-Version"]);
  const proto = await (await fetch(`http://127.0.0.1:${b.port}/json/protocol`)).json();
  const net = proto.domains.find((d) => d.domain === "Network");
  const cmd = net.commands.find((c) => c.name === "setBlockedURLs");
  console.log(JSON.stringify(cmd, null, 1));
  for (const t of ["BlockPattern"]) {
    const ty = net.types.find((x) => x.id === t);
    if (ty) console.log(JSON.stringify(ty, null, 1));
  }
  const f = proto.domains.find((d) => d.domain === "Fetch");
  console.log(JSON.stringify(f.types.find((x) => x.id === "RequestPattern"), null, 1));
  console.log(JSON.stringify(f.commands.find((x) => x.name === "enable").parameters.map((p) => p.name)));
} finally {
  await b.close();
}
