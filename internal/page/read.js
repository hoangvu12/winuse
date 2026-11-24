// read.js - a Page's main content as Markdown, for `winuse read`.
//
// Derived from bladebro src/page/js/markdown.js (bladebro 4.8.3,
// https://github.com/dondai44423/bladebro at d50ee9e), Copyright the
// bladebro authors, licensed under the Apache License 2.0; a copy is in
// third_party/bladebro/LICENSE.
//
// Changes from the original (winuse, 2026, from .scratch/bladebro-ideas
// tickets 12 and 25):
//   - the site-specific branches (product page, Reddit post, GitHub repo)
//     and the template slots are removed; it is one function expression
//     that returns {markdown, main, scoped, block, title, url};
//   - the main content is ranked (every element scored by its text, links
//     counting half, navigation, headers, footers and asides counting
//     nothing; main, article and role=main are preferred) instead of taking
//     the first <main> over 200 characters, and it never renders empty;
//   - links go back whole between marks, and read.go prints them by
//     page.SiteURL's rule: paths on the same site, a shortened domain plus
//     path elsewhere (opts.urls "full" keeps them whole); images keep
//     their alt text and lose their URLs;
//   - the page's h1 is kept when it sits in a <header>, and [edit] links and
//     heading permalinks are dropped;
//   - data tables render as Markdown tables with their header row and every
//     row (winuse discloses its own cut); layout tables render as lines;
//   - block elements break lines; lists indent by depth;
//   - a short page with a challenge or block sign is named (block) by
//     block.js, the port of bladebro's detect_block.js that read.go puts
//     in here;
//   - no budget here: winuse cuts the Markdown and says where.
//
// It runs in an isolated world, through Runtime.callFunctionOn: on an
// Element (`this`) that subtree is rendered as it is; otherwise the main
// content is picked.
function (opts) {
  opts = opts || {};
  var FULL = opts.urls === 'full';
  var SKIP = {SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, SVG: 1, TEMPLATE: 1, META: 1, LINK: 1, BUTTON: 1, INPUT: 1, SELECT: 1, TEXTAREA: 1, CANVAS: 1, OBJECT: 1, EMBED: 1, VIDEO: 1, AUDIO: 1, MAP: 1, HEAD: 1, TITLE: 1};
  var STRUCTURE = {UL: 1, OL: 1, DL: 1, TABLE: 1, THEAD: 1, TBODY: 1, TFOOT: 1, TR: 1, SELECT: 1};
  var NOISE = {NAV: 1, FOOTER: 1, HEADER: 1, ASIDE: 1};
  var NOISE_ROLES = {navigation: 1, banner: 1, contentinfo: 1, complementary: 1, search: 1};
  var MAIN_SELECTORS = 'main,[role="main"],article,#content,.content,#main-content,.main-content,#main,.post,.article,.entry-content,.post-body,.article-body,.story-body,#article-body';
  var win = (typeof window !== 'undefined') ? window : null;
  var doc = (this && this.ownerDocument) || document;
  var here = doc.location || {};

  function upper(n) { return (n.tagName || '').toUpperCase(); }

  function hidden(n) {
    if (n.hidden) return true;
    if (n.getAttribute && n.getAttribute('aria-hidden') === 'true') return true;
    var st;
    try { st = (n.ownerDocument.defaultView || win).getComputedStyle(n); } catch (e) { return false; }
    return !!st && (st.display === 'none' || st.visibility === 'hidden' || st.visibility === 'collapse');
  }

  function display(n) {
    try { return (n.ownerDocument.defaultView || win).getComputedStyle(n).display || ''; } catch (e) { return ''; }
  }

  function isNoise(n) {
    if (NOISE[upper(n)]) return true;
    var role = n.getAttribute && n.getAttribute('role');
    return !!(role && NOISE_ROLES[role]);
  }

  function isAd(n) {
    var cl = (typeof n.className === 'string' ? n.className : '').toLowerCase();
    var id = (n.id || '').toLowerCase();
    if (/(^|[\s_-])(dfp|advert|advertisement|sponsored|ad-container|ad-wrapper|ad-slot|ad-banner|ad-feedback|adbanner|adsense|adblock|ad-label|ads-label|ads-container|mol-ads|promoted|adsbygoogle|google-ad|doubleclick|adfeedback)/.test(cl)) return true;
    if (/(^|[\s_-])(dfp|advert|sponsored|google_ads|doubleclick)/.test(id)) return true;
    if (n.hasAttribute('data-ad') || n.hasAttribute('data-ad-slot') || n.hasAttribute('data-ad-client') || n.hasAttribute('data-google-query-id')) return true;
    var al = (n.getAttribute('aria-label') || '').toLowerCase();
    return /^advertisement/.test(al);
  }

  function isEditLink(n) {
    var cl = (typeof n.className === 'string' ? n.className : '');
    if (/(^|\s)(mw-editsection|editsection|headerlink|anchor-link|heading-anchor|hash-link|anchorjs-link)(\s|$)/.test(cl)) return true;
    if (upper(n) === 'A') {
      var t = (n.textContent || '').trim();
      if (/^\[?\s*(edit|edit source)\s*\]?$/i.test(t)) return true;
      if (/^[#¶§🔗]$/.test(t)) return true;
    }
    return false;
  }

  // ---- links ----

  // A link goes back whole, between marks: \x01, its text, \x1f, its
  // absolute URL, \x02. read.go prints it, shortened by page.SiteURL, the
  // rule the Page lines' LinkURL shares (it needs the Public Suffix List),
  // so there is no URL rule here. The marks are dropped from the Page's
  // text (see MARKS).
  function absURL(href) {
    try { return new URL(href, here.href).href; } catch (e) { return href; }
  }
  var MARKS = /[\x01\x02\x1f]/g;
  function linkMark(text, href) {
    return '\x01' + text + '\x1f' + absURL(href).replace(MARKS, '') + '\x02';
  }

  // ---- the main content ----

  // score is every element's text length with links counting half and the
  // noise (navigation, header, footer, aside, ads, hidden) counting nothing.
  var scores = typeof Map !== 'undefined' ? new Map() : null;
  function score(n) {
    if (n.nodeType === 3) return {t: n.textContent.replace(/\s+/g, ' ').trim().length, l: 0};
    if (n.nodeType !== 1) return {t: 0, l: 0};
    var tag = upper(n);
    if (SKIP[tag] || hidden(n) || isNoise(n) || isAd(n)) { scores.set(n, 0); return {t: 0, l: 0}; }
    var t = 0, l = 0;
    var cs = n.childNodes;
    for (var i = 0; i < cs.length; i++) { var r = score(cs[i]); t += r.t; l += r.l; }
    if (n.shadowRoot) { var sc = n.shadowRoot.childNodes; for (var j = 0; j < sc.length; j++) { var r2 = score(sc[j]); t += r2.t; l += r2.l; } }
    if (tag === 'A') l = t;
    scores.set(n, t - l / 2);
    return {t: t, l: l};
  }

  function semantic(n) {
    var tag = upper(n);
    return tag === 'MAIN' || tag === 'ARTICLE' || (n.getAttribute && n.getAttribute('role') === 'main');
  }

  function findMain(d) {
    var body = d.body;
    if (!body) return d.documentElement;
    score(body);
    var total = scores.get(body) || 0;
    if (total <= 0) return body;
    // A semantic candidate that holds most of the content wins: the
    // highest-scoring main, article or role=main over 40% of the page, or a
    // common content container over 60%.
    var best = null, bestScore = 0;
    var cands = d.querySelectorAll(MAIN_SELECTORS);
    for (var i = 0; i < cands.length; i++) {
      var c = cands[i], s = scores.get(c) || 0;
      var need = semantic(c) ? 0.4 : 0.6;
      if (s >= need * total && s > bestScore) { best = c; bestScore = s; }
    }
    var node = best || body;
    // Down through wrappers: while one child holds 85% of the content, the
    // content is in that child.
    for (;;) {
      var kids = node.children, top = null, topScore = 0;
      for (var k = 0; k < kids.length; k++) {
        var ks = scores.get(kids[k]) || 0;
        if (ks > topScore) { top = kids[k]; topScore = ks; }
      }
      var nodeScore = scores.get(node) || 0;
      if (!top || topScore < 0.85 * nodeScore) break;
      if (semantic(node) && !semantic(top)) break; // a main or article is kept whole
      node = top;
    }
    return node;
  }

  // The page's h1: the first shown one outside navigation and footers.
  function pageH1(d) {
    var hs = d.querySelectorAll('h1');
    for (var i = 0; i < hs.length; i++) {
      var h = hs[i];
      if (hidden(h) || !(h.textContent || '').trim()) continue;
      var bad = false;
      for (var p = h.parentElement; p; p = p.parentElement) {
        var tg = upper(p);
        if (tg === 'NAV' || tg === 'FOOTER' || tg === 'ASIDE') { bad = true; break; }
      }
      if (!bad) return h;
    }
    return null;
  }

  // ---- Markdown ----

  function toMd(root, h1) {
    var out = [];
    var counters = []; // the open lists, innermost last
    function add(s) { out.push(s); }
    function nl() { add('\n'); }
    function sub(n) {
      var cs = n.childNodes;
      for (var i = 0; i < cs.length; i++) walk(cs[i]);
      if (n.shadowRoot) { var s2 = n.shadowRoot.childNodes; for (var j = 0; j < s2.length; j++) walk(s2[j]); }
    }
    function inline(n) {
      var save = out; out = [];
      sub(n);
      var s = out.join('');
      out = save;
      return s.replace(/\s+/g, ' ').trim();
    }
    function headingText(n) {
      var save = out; out = [];
      sub(n);
      var s = out.join('');
      out = save;
      // A heading keeps its links (a search result's title is one); its
      // bold is dropped, and edit and permalink links never reach it.
      return s.replace(/\*\*/g, '').replace(/\s+/g, ' ').trim();
    }
    function cellText(n) {
      return inline(n).replace(/\|/g, '\\|');
    }
    function table(n) {
      var rows = [];
      var trs = n.rows || n.querySelectorAll('tr');
      for (var i = 0; i < trs.length; i++) {
        if (trs[i].closest && trs[i].closest('table') !== n) continue;
        if (hidden(trs[i])) continue;
        var cells = [], heads = 0;
        var cs = trs[i].cells || [];
        for (var j = 0; j < cs.length; j++) {
          if (upper(cs[j]) === 'TH') heads++;
          cells.push(cellText(cs[j]));
        }
        if (cells.length) rows.push({cells: cells, head: heads === cells.length || (trs[i].parentElement && upper(trs[i].parentElement) === 'THEAD')});
      }
      if (!rows.length) return;
      var width = 0;
      for (var r = 0; r < rows.length; r++) width = Math.max(width, rows[r].cells.length);
      // The header: the last of the leading header rows; else the first row.
      var h = 0;
      while (h + 1 < rows.length && rows[h].head && rows[h + 1].head) h++;
      var lines = [];
      function line(cells) {
        var c = cells.slice();
        while (c.length < width) c.push('');
        return '| ' + c.join(' | ') + ' |';
      }
      var cap = n.caption ? inline(n.caption) : '';
      if (cap) lines.push('**' + cap + '**', '');
      lines.push(line(rows[h].cells));
      var sep = [];
      for (var w = 0; w < width; w++) sep.push('---');
      lines.push('|' + sep.join('|') + '|');
      for (var q = h + 1; q < rows.length; q++) lines.push(line(rows[q].cells));
      add('\n\n' + lines.join('\n') + '\n\n');
    }
    function dataTable(n) {
      if (n.getAttribute('role') === 'presentation') return false;
      if (n.querySelector('table')) return false; // nested tables are layout
      if (n.querySelector('th') || n.tHead || n.caption) return true;
      return false;
    }
    function walk(n, isRoot) {
      if (n.nodeType === 3) {
        var t = n.textContent.replace(/\s+/g, ' ').replace(MARKS, '');
        // The white space between list items, rows and cells is markup.
        if (t === ' ' && n.parentElement && STRUCTURE[upper(n.parentElement)]) return;
        if (t) add(t);
        return;
      }
      if (n.nodeType !== 1) return;
      var tag = upper(n);
      if (!isRoot) {
        if (SKIP[tag]) return;
        if (hidden(n)) return;
        if (isAd(n) || isEditLink(n)) return;
        if (isNoise(n)) {
          // The page's h1 often sits in the header: keep it alone.
          if (h1 && n.contains(h1)) { add('\n\n# ' + headingText(h1) + '\n\n'); }
          return;
        }
      }
      switch (tag) {
        case 'H1': case 'H2': case 'H3': case 'H4': case 'H5': case 'H6':
          var ht = headingText(n);
          if (ht) add('\n\n' + '######'.slice(0, +tag.charAt(1)) + ' ' + ht + '\n\n');
          return;
        case 'P':
          add('\n\n'); sub(n); add('\n\n');
          return;
        case 'A':
          var tx = inline(n);
          var raw = n.getAttribute('href');
          if (!tx) {
            var img = n.querySelector && n.querySelector('img[alt]');
            tx = img ? img.getAttribute('alt').trim() : '';
          }
          if (!tx) return;
          if (!raw || raw === '#' || /^\s*javascript:/i.test(raw)) { add(tx); return; }
          // read.go prints it, once when its text is its URL.
          add(linkMark(tx, n.href || raw));
          return;
        case 'IMG':
          var al = (n.getAttribute('alt') || '').trim();
          if (al) add(FULL && n.src ? '![' + al + '](' + n.src + ')' : '![' + al + ']');
          return;
        case 'LI':
          var counter = counters.length ? counters[counters.length - 1] : null;
          var mark = '- ';
          if (counter && counter.ordered) mark = (counter.next++) + '. ';
          add('\n' + '  '.repeat(Math.max(0, counters.length - 1)) + mark);
          sub(n);
          return;
        case 'UL': case 'OL':
          // A top-level list stands apart; a nested one goes on under its item.
          var top = !counters.length;
          counters.push({ordered: tag === 'OL', next: tag === 'OL' && n.start ? n.start : 1});
          if (top) add('\n');
          sub(n);
          if (top) add('\n\n');
          counters.pop();
          return;
        case 'DT':
          add('\n**'); sub(n); add('**\n');
          return;
        case 'DD':
          add('\n: '); sub(n); add('\n');
          return;
        case 'CODE': case 'KBD': case 'SAMP':
          if (n.parentElement && upper(n.parentElement) === 'PRE') { sub(n); return; }
          var code = (n.textContent || '').replace(/\s+/g, ' ');
          if (code.trim()) add('`' + code + '`');
          return;
        case 'PRE':
          add('\n\n```\n' + (n.innerText || n.textContent || '').replace(/\s+$/, '') + '\n```\n\n');
          return;
        case 'BLOCKQUOTE':
          var save = out; out = [];
          sub(n);
          var q = clean(out.join(''));
          out = save;
          if (q) add('\n\n> ' + q.replace(/\n/g, '\n> ') + '\n\n');
          return;
        case 'TABLE':
          if (dataTable(n)) { table(n); return; }
          add('\n'); sub(n); add('\n');
          return;
        case 'TR':
          add('\n'); sub(n); add('\n');
          return;
        case 'TD': case 'TH':
          sub(n); add(' ');
          return;
        case 'IFRAME':
          try { var fd = n.contentDocument; if (fd && fd.body) walk(fd.body); } catch (e) {}
          return;
        case 'BR':
          add('\n');
          return;
        case 'HR':
          add('\n\n---\n\n');
          return;
        case 'STRONG': case 'B':
          var bt = inline(n);
          if (bt) add('**' + bt + '**');
          return;
        case 'EM': case 'I':
          var it = inline(n);
          if (it) add('*' + it + '*');
          return;
      }
      var d = display(n);
      var block = d && d.indexOf('inline') !== 0 && d !== 'contents';
      if (block) add('\n');
      sub(n);
      if (block) add('\n');
    }
    if (h1 && !root.contains(h1) && !(h1.contains(root))) add('# ' + headingText(h1) + '\n\n');
    walk(root, true);
    return clean(out.join(''));
  }

  // clean trims the lines (a list item keeps its indent) and the blank
  // runs, outside code blocks.
  function clean(s) {
    var parts = s.split(/(```\n[\s\S]*?\n```)/);
    for (var i = 0; i < parts.length; i += 2) {
      parts[i] = parts[i].split('\n').map(function (l) {
        l = l.replace(/[ \t ]+$/, '').replace(/(\S)[ \t ]{2,}/g, '$1 ');
        if (/^ *(- |\d+\. )/.test(l)) return l.replace(/^( *(- |\d+\. )) +/, '$1');
        return l.replace(/^[ \t ]+/, '');
      }).join('\n').replace(/\n{3,}/g, '\n\n');
    }
    return parts.join('').replace(/\n{3,}/g, '\n\n').trim();
  }

  function desc(el) {
    if (!el || !el.tagName) return '';
    var s = el.tagName.toLowerCase();
    if (el.id) return s + '#' + el.id;
    var cl = (typeof el.className === 'string' && el.className.trim()) ? el.className.trim().split(/\s+/)[0] : '';
    return cl ? s + '.' + cl : s;
  }

  // ---- a block or challenge page ----

  // blockKind is block.js, the one wall check navigate's Note uses too;
  // read.go puts it in place of the marker.
  var blockKind = /* winuse:block.js */ null;

  var scoped = (typeof Element !== 'undefined') && (this instanceof Element);
  if (!scores) scores = {set: function () {}, get: function () { return 0; }};
  var h1 = scoped ? null : pageH1(doc);
  var main = scoped ? this : findMain(doc);
  var md = main ? toMd(main, h1) : '';
  if (!md && !scoped && doc.body && main !== doc.body) {
    main = doc.body;
    md = toMd(main, h1);
  }
  return {markdown: md, main: desc(main), scoped: scoped, block: scoped ? '' : blockKind(doc), title: doc.title || '', url: here.href || ''};
}
