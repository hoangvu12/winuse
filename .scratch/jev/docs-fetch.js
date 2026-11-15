// Starts fetching the docs' full text into window.__docs (winuse eval does not await).
window.__docs = null;
fetch('/llms-full.txt').then(r => r.ok ? r.text() : ('HTTP ' + r.status)).then(t => { window.__docs = t; }, e => { window.__docs = 'ERR ' + e; });
'started'
