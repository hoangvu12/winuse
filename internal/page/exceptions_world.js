// winuse's exception buffer (.scratch/bladebro-ideas ticket 30), run in
// winuse's isolated world of each document, where the Page cannot reach
// it: it keeps the last __MAX__ records exceptions.js hands over as
// __NAME__ events, and globalThis.__KEY__() returns them as JSON and
// empties it. A record with hello says this document has the listener.
(() => {
  if (globalThis.__KEY__) return; // the world has it (a session's second install)
  const name = '__NAME__';
  let buf = [];
  let covered = false;
  let dropped = 0;
  document.addEventListener(name, (e) => {
    let r;
    try {
      r = JSON.parse(e.detail);
    } catch (_) {
      return;
    }
    if (!r || typeof r !== 'object') return;
    if (r.hello) {
      covered = true;
      return;
    }
    if (buf.length >= __MAX__) {
      buf.shift();
      dropped++;
    }
    buf.push(r);
  });
  globalThis.__KEY__ = () => {
    const out = JSON.stringify({ c: covered, d: dropped, r: buf });
    buf = [];
    dropped = 0;
    return out;
  };
  // A document loaded before this world's session has a listener that
  // answers.
  document.dispatchEvent(new CustomEvent(name + '?'));
})();
