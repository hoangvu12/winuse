// Prints docs lines 19585 to 19700 (models table, context length, rate limits).
window.__docs.split('\n').slice(19584, 19700).map((l, i) => (19585 + i) + ': ' + l.slice(0, 260)).join('\n')
