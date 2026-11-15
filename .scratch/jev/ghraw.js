// Start fetching a GitHub file from api.github.com inside the page; the
// text lands in window.__gh['__PATH__'] (winuse eval does not await a
// Promise, so ghfile.ps1 polls for it). ghfile.ps1 fills the placeholders.
(() => {
  window.__gh = window.__gh || {};
  const key = '__PATH__';
  window.__gh[key] = null;
  fetch('https://api.github.com/repos/__REPO__/contents/__PATH__?ref=__REF__',
    { headers: { Accept: 'application/vnd.github.raw' } })
    .then(r => r.ok ? r.text() : 'HTTP ' + r.status + ' for ' + key)
    .then(t => { window.__gh[key] = t; }, e => { window.__gh[key] = 'ERROR ' + e; });
  return 'started';
})()
