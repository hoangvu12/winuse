// Prints the docs' lines about limits, tokens, plans and errors, with a line of context.
(() => {
  const t = window.__docs;
  if (t == null) return 'pending';
  if (t.length < 200) return t;
  const lines = t.split('\n');
  const re = /max_tokens|token limit|tokens per|context (window|length)|32[,.]?768|32k|128k|rate limit|requests per|plan|tier|quota|max(imum)? (state|input|options)|too large|413|limit/i;
  const out = [];
  lines.forEach((l, i) => { if (re.test(l)) out.push((i + 1) + ': ' + l.slice(0, 220)); });
  return 'chars ' + t.length + '\n' + out.slice(0, 80).join('\n');
})()
