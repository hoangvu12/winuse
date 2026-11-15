// Prints line 136 of the docs, then every line about context limits and long context, with 2 lines after each.
(() => {
  const lines = window.__docs.split('\n');
  const out = ['136: ' + lines[135]];
  const re = /context limit|long.context|262k|32k|32,768|32768|context window|anthropic-beta|x-typesafe-.*context|max_tokens_exceeded/i;
  lines.forEach((l, i) => {
    if (re.test(l)) out.push('--- ' + (i + 1) + ': ' + l.slice(0, 400), ...lines.slice(i + 1, i + 3).map(x => '    ' + x.slice(0, 300)));
  });
  return out.join('\n').slice(0, 9000);
})()
