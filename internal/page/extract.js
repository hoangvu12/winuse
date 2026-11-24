// extract.js: a Page's repeated items as JSON rows, for `winuse extract`
// (ticket 26). It runs in winuse's isolated world, never the Page's own:
// Runtime.evaluate "(" + src + ").call(null, opts)" for the whole Page, or
// Runtime.callFunctionOn on the --root node.
//
// Derived from bladebro 4.8.3 (https://github.com/dondai44423/bladebro),
// Copyright the bladebro authors, licensed under the Apache License 2.0
// (third_party/bladebro/LICENSE, NOTICE beside it):
//   - src/mcp/server/js/auto_extract.js  (list detection, item fields, the
//     redirect unwrapping and the page's result count)
//   - src/adapters/js/jsonld.js          (schema.org JSON-LD)
//   - src/page/js/detect_block.js        (the generic block-wall check,
//                                         block.js, put in by extract.go)
// Changed for winuse (ticket 12's findings): the site adapters and site
// branches are gone; items group loosely (same tag and class, or most
// children shared); sibling rows of one record merge (two rows per story);
// only the title link dedupes, and table rows never; off-site links get
// no bonus, and navs, footers, asides and prose never win; a weak list
// loses to JSON-LD, and an article or doc page has no list; a --root that
// is one item gathers its similar siblings; data tables print their cells
// by header; "10.3k", "24,134,885 questions" and "1-30 of 208" counts are
// read; a star count is stars, and vote, answer, view and comment counts
// are fields, not description; bot walls are named.
//
// opts: {as: 'auto'|'list'|'table'|'jsonld', limit: N}.
// Returns {kind, container, label, rows, found, at_least, page_says, wall};
// kind '' means no list found, and at_least says found stopped at
// MAX_ITEMS with more items left.
function (opts) {
'use strict';
opts = opts || {};
const LIMIT = opts.limit > 0 ? opts.limit : 50;
const MODE = opts.as || 'auto';
const ROOT = (this && this.nodeType === 1) ? this : null;
const SCOPE = ROOT || document.body || document.documentElement;
const MAX_ITEMS = 2000; // rows counted past the limit, at most (then found is at_least)

// ---- text and visibility ----
const textCache = new WeakMap();
function text(el) {
  if (!el) return '';
  let v = textCache.get(el);
  if (v === undefined) {
    v = (el.innerText || '').replace(/\s+/g, ' ').trim();
    textCache.set(el, v);
  }
  return v;
}
function shown(el) {
  if (!el || el.nodeType !== 1) return false;
  if (typeof el.checkVisibility === 'function' && !el.checkVisibility({ visibilityProperty: true })) return false;
  return el.getClientRects().length > 0;
}
// A record is one item: one element, or the sibling rows of one record.
const recText = rec => rec.map(text).filter(Boolean).join(' ');
function recAll(rec, sel) {
  const out = [];
  for (const el of rec) {
    if (el.matches(sel)) out.push(el);
    for (const x of el.querySelectorAll(sel)) out.push(x);
  }
  return out;
}
const recOne = (rec, sel) => recAll(rec, sel)[0] || null;

// ---- numbers ----
// num reads "1,234", "10.3k", "2.1M" as a whole number.
function num(s) {
  s = String(s || '').trim().replace(/[  ]/g, ' ');
  const m = s.match(/^(\d[\d.,]*)\s*([kKmM])?$/);
  if (!m) return NaN;
  const d = m[1];
  if (m[2]) return Math.round(parseFloat(d.replace(/,/g, '')) * (/k/i.test(m[2]) ? 1e3 : 1e6));
  if (/^\d{1,3}([.,]\d{3})+$/.test(d)) return parseInt(d.replace(/[.,]/g, ''), 10);
  if (/^\d+$/.test(d)) return parseInt(d, 10);
  return NaN;
}
const NUM = '(\\d[\\d.,]*(?:[kKmM]\\b)?)';

// ---- the block wall ----
// wall is block.js (detect_block.js, its generic part), the one wall check
// navigate's Note and read use too; extract.go puts it in place of the
// marker. Called with no document, it checks this one.
const wall = /* winuse:block.js */ null;

// ---- what the page says it holds ----
const NOUNS = 'results?|items?|listings?|products?|entries|matches|hits|questions|repositories|repos|packages|articles|posts|jobs|stories|videos|users|issues|pull requests|recipes|threads|topics|courses|books|homes|properties|companies|people';
function pageSays() {
  const t = ((document.body && document.body.innerText) || '').slice(0, 60000);
  const pats = [
    [new RegExp('\\b\\d[\\d.,]*\\s*(?:-|–|—|to)\\s*\\d[\\d.,]*\\s+of\\s+(?:about\\s+|over\\s+)?' + NUM + '(?:\\s+(' + NOUNS + '))?', 'i'), 'results'],
    [new RegExp('(?:about|over|more than|of)\\s+' + NUM + '\\s+(' + NOUNS + ')\\b', 'i'), ''],
    [new RegExp('(?:^|[^\\w.,])' + NUM + '\\s+(' + NOUNS + ')\\b', 'i'), ''],
  ];
  for (const [re, def] of pats) {
    const m = t.match(re);
    if (!m) continue;
    const v = num(m[1]);
    if (!isNaN(v) && v > 0 && v < 1e12) return { total: v, noun: (m[2] || def).toLowerCase() };
  }
  return null;
}

// ---- JSON-LD (jsonld.js) ----
function jsonld(withArticles) {
  const entities = [];
  const collect = node => {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) { for (const n of node) collect(n); return; }
    if (Array.isArray(node['@graph'])) collect(node['@graph']);
    if (node['@type']) entities.push(node);
  };
  for (const s of [...document.querySelectorAll('script[type="application/ld+json"]')].slice(0, 20)) {
    try { collect(JSON.parse(s.textContent || '')); } catch (e) {}
  }
  const typeOf = e => { const t = e['@type']; const arr = Array.isArray(t) ? t : [t]; return arr.filter(x => typeof x === 'string').map(x => x.replace(/^https?:\/\/schema\.org\//i, '')); };
  const is = (e, names) => typeOf(e).some(x => names.includes(x));
  const str = v => (typeof v === 'string' ? v.replace(/\s+/g, ' ').trim() : (typeof v === 'number' ? String(v) : ''));
  const nameOf = v => { if (!v) return ''; if (typeof v === 'string') return str(v); if (Array.isArray(v)) return v.map(nameOf).filter(Boolean).join(', '); return str(v.name || v.title || ''); };
  const here = location.href.split('#')[0];
  const ratingOf = (e, o) => {
    const ar = e.aggregateRating;
    if (!ar || ar.ratingValue === undefined) return;
    const rv = parseFloat(ar.ratingValue); if (!isNaN(rv)) o.rating = rv;
    const rc = ar.reviewCount !== undefined ? ar.reviewCount : ar.ratingCount;
    if (rc !== undefined) { const n = parseInt(String(rc), 10); if (!isNaN(n)) o.reviews = n; }
  };
  const row = e => {
    const o = {};
    const title = str(e.name) || str(e.headline) || str(e.title);
    if (title) o.title = title.slice(0, 200);
    o.url = str(e.url) || here;
    if (is(e, ['Recipe'])) {
      const au = nameOf(e.author); if (au) o.author = au;
      for (const [k, f] of [['prepTime', 'prep_time'], ['cookTime', 'cook_time'], ['totalTime', 'total_time']]) if (str(e[k])) o[f] = str(e[k]);
      if (e.recipeYield) o.yield = nameOf(e.recipeYield);
      if (Array.isArray(e.recipeIngredient)) o.ingredients = e.recipeIngredient.map(str).filter(Boolean).slice(0, 60);
      if (Array.isArray(e.recipeInstructions)) o.steps = e.recipeInstructions.map(s => typeof s === 'string' ? str(s) : str(s.text || s.name)).filter(Boolean).slice(0, 60);
      ratingOf(e, o);
    } else if (is(e, ['Product'])) {
      const brand = nameOf(e.brand); if (brand) o.brand = brand;
      if (str(e.sku)) o.sku = str(e.sku);
      const offers = Array.isArray(e.offers) ? e.offers[0] : e.offers;
      if (offers && offers.price !== undefined) {
        const cur = str(offers.priceCurrency); o.price = (cur ? cur + ' ' : '') + String(offers.price);
        if (str(offers.availability)) o.availability = str(offers.availability).replace(/^https?:\/\/schema\.org\//i, '');
      }
      ratingOf(e, o);
    } else if (is(e, ['JobPosting'])) {
      const co = nameOf(e.hiringOrganization); if (co) o.company = co.slice(0, 120);
      const loc = e.jobLocation && (Array.isArray(e.jobLocation) ? e.jobLocation[0] : e.jobLocation);
      const addr = loc && loc.address;
      if (addr) { const parts = [str(addr.addressLocality), str(addr.addressRegion), str(addr.addressCountry)].filter(Boolean); if (parts.length) o.location = parts.join(', '); }
      if (str(e.employmentType)) o.employment_type = str(e.employmentType);
      if (str(e.datePosted)) o.date_posted = str(e.datePosted);
    } else if (is(e, ['Event'])) {
      if (str(e.startDate)) o.start_date = str(e.startDate);
      if (str(e.endDate)) o.end_date = str(e.endDate);
      const loc = nameOf(e.location); if (loc) o.location = loc;
    } else {
      const au = nameOf(e.author); if (au) o.author = au.slice(0, 140);
      if (str(e.datePublished)) o.published = str(e.datePublished);
    }
    const d = str(e.description).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    if (d) o.description = d.slice(0, 600);
    return o;
  };
  const MAIN = ['Recipe', 'Product', 'JobPosting', 'Event'];
  const ARTICLES = ['NewsArticle', 'Article', 'BlogPosting', 'TechArticle', 'ScholarlyArticle', 'Report'];
  const article = entities.some(e => is(e, ARTICLES));
  // An ItemList is a list of its own.
  const list = entities.find(e => is(e, ['ItemList']) && Array.isArray(e.itemListElement) && e.itemListElement.length);
  if (list) {
    const rows = list.itemListElement.map(it => {
      const item = it.item && typeof it.item === 'object' ? it.item : it;
      const o = row(item);
      if (!o.title && str(it.name)) o.title = str(it.name);
      if (str(it.url)) o.url = str(it.url);
      return o;
    }).filter(o => o.title);
    if (rows.length) return { type: 'ItemList', label: str(list.name), rows, article };
  }
  const kinds = withArticles ? MAIN.concat(ARTICLES) : MAIN;
  for (const k of kinds) {
    const all = entities.filter(e => is(e, [k]) && (str(e.name) || str(e.headline)));
    if (!all.length) continue;
    const rows = all.map(row);
    return { type: k, label: rows.length === 1 ? (rows[0].title || '') : '', rows, article };
  }
  return { type: '', label: '', rows: [], article };
}

// ---- containers ----
function firstClass(el) {
  const c = el.getAttribute('class');
  if (!c) return '';
  return c.trim().split(/\s+/)[0] || '';
}
function describe(el) {
  const tag = el.tagName.toLowerCase();
  const id = el.getAttribute('id');
  if (id && id.length <= 40 && !/\d{4,}/.test(id)) return '<' + tag + ' id="' + id + '">';
  const c = firstClass(el);
  if (c && c.length <= 40) return '<' + tag + ' class="' + c + '">';
  return '<' + tag + '>';
}
function labelOf(el) {
  const clip = s => s.replace(/\s+/g, ' ').trim().slice(0, 80);
  const al = el.getAttribute('aria-label');
  if (al && al.trim()) return clip(al);
  const lb = el.getAttribute('aria-labelledby');
  if (lb) {
    const t = lb.split(/\s+/).map(id => document.getElementById(id)).filter(Boolean).map(text).join(' ');
    if (t) return clip(t);
  }
  if (el.tagName === 'TABLE' && el.caption && text(el.caption)) return clip(text(el.caption).replace(/\[\w+\]/g, ''));
  // The nearest heading before it: a previous sibling of it or of one of
  // its two nearest ancestors.
  for (let n = el, up = 0; n && up < 3; n = n.parentElement, up++) {
    for (let p = n.previousElementSibling, k = 0; p && k < 4; p = p.previousElementSibling, k++) {
      if (/^H[1-6]$/.test(p.tagName) || p.getAttribute('role') === 'heading') return clip(text(p));
      const h = p.querySelector('h1,h2,h3,h4,h5,h6');
      if (h && text(h) && text(p).length < 200) return clip(text(h));
    }
  }
  return '';
}

// ---- grouping ----
const SKIP_TAGS = new Set(['STYLE', 'SCRIPT', 'HEAD', 'NOSCRIPT', 'SVG', 'TEMPLATE', 'LINK', 'META', 'BR', 'HR', 'PATH', 'IFRAME', 'INPUT', 'SELECT', 'OPTION', 'BUTTON', 'LABEL', 'IMG', 'PICTURE', 'SOURCE', 'COL', 'COLGROUP', 'THEAD']);
const SEMANTIC = new Set(['UL', 'OL', 'TBODY', 'TABLE', 'DL']);
const PROSE_TAGS = new Set(['P', 'BLOCKQUOTE', 'PRE', 'FIGURE', 'FIGCAPTION', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'SPAN', 'A', 'B', 'I', 'EM', 'STRONG', 'CODE', 'SMALL']);
const LANDMARK = 'nav,footer,aside,header,[role="navigation"],[role="contentinfo"],[role="complementary"],[role="banner"]';
function baseKey(el) {
  const dt = el.getAttribute('data-testid') || el.getAttribute('data-component-type');
  if (dt) return el.tagName + '@' + dt.replace(/[-_]?\d+$/, '');
  const c = firstClass(el);
  if (c) return el.tagName + '.' + c.replace(/\d+/g, '#');
  const r = el.getAttribute('role');
  if (r) return el.tagName + ':' + r;
  return el.tagName;
}
function shape(el) {
  const s = new Set();
  for (const k of el.children) s.add(k.tagName + (firstClass(k) ? '.' + firstClass(k) : ''));
  return s;
}
function similar(a, b) {
  if (!a.size && !b.size) return true;
  let i = 0;
  for (const x of a) if (b.has(x)) i++;
  return i / Math.max(a.size, b.size) >= 0.5;
}
// groupsOf sorts a container's shown children into groups of alike items:
// the same tag and first class (or test id, or role); children with no
// class group by tag and a mostly shared set of children, except in a
// list, table or feed, where every child of a tag is one group.
function groupsOf(c) {
  const kids = [...c.children].filter(k => !SKIP_TAGS.has(k.tagName) && shown(k));
  const semantic = SEMANTIC.has(c.tagName) || /^(list|feed|grid|rowgroup|table|listbox|tree)$/.test(c.getAttribute('role') || '');
  const groups = [];
  const of = new Map();
  for (const k of kids) {
    const key = baseKey(k);
    let g = null;
    if (key === k.tagName && !semantic) {
      const sh = shape(k);
      g = groups.find(x => x.base === key && similar(x.shape, sh));
      if (!g) { g = { base: key, key: key + '~' + groups.length, shape: sh, els: [] }; groups.push(g); }
    } else {
      g = groups.find(x => x.key === key);
      if (!g) { g = { base: key, key, shape: shape(k), els: [] }; groups.push(g); }
    }
    g.els.push(k);
    of.set(k, g);
  }
  return { kids, groups, of };
}
// records finds sibling rows that make one record (a story's title row and
// its subtext row): a pattern of 2 to 4 children of different tags or
// shapes that repeats at least 3 times. Rows that only alternate a class
// (striped rows) are no pattern.
function records(kids, of) {
  const seq = kids.map(k => of.get(k).key);
  let best = null;
  for (let p = 2; p <= 4 && !best; p++) {
    for (let s = 0; s + p * 3 <= seq.length; s++) {
      const pat = seq.slice(s, s + p);
      if (new Set(pat).size !== p) continue;
      let distinct = false;
      for (let j = 1; j < p; j++) {
        const a = kids[s], b = kids[s + j];
        if (a.tagName !== b.tagName || !similar(shape(a), shape(b))) distinct = true;
      }
      if (!distinct) continue;
      let n = 0;
      for (let i = s; i + p <= seq.length; i += p) {
        let ok = true;
        for (let j = 0; j < p; j++) if (seq[i + j] !== pat[j]) { ok = false; break; }
        if (!ok) break;
        n++;
      }
      if (n >= 3 && (!best || n * p > best.n * best.p)) best = { s, p, n };
    }
  }
  if (!best) return null;
  const recs = [];
  for (let i = 0; i < best.n; i++) recs.push(kids.slice(best.s + i * best.p, best.s + (i + 1) * best.p));
  return { recs, keys: new Set(seq.slice(best.s, best.s + best.p)) };
}

// ---- items ----
const PRICE = /([$€£¥₹]\s?\d[\d,]*(?:[.,]\d{1,2})?)/;
const DATE = /(\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}\/\d{1,2}\/\d{2,4}\b|\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.?\s+\d{1,2},?\s*\d{4}\b|\b\d+\s+(?:second|minute|hour|day|week|month|year)s?\s+ago\b)/i;
const ACT_TEXT = /^(vote|upvote|downvote|comment|comments|discuss|reply|replies|share|save|hide|flag|report|favorite|fav|like|dislike|follow|subscribe|pin|unpin|more|less|edit|delete|remove|add|new|open|show|expand|collapse|permalink|embed|cite|parent|context|full story|read more|continue reading|view|all|next|prev|previous|add to cart|buy now)$/i;
const ACT_HREF = /\/vote\b|\/comment|\/reply|\/action|^javascript:|#comment|#respond|#reply/i;
const COUNT_WORDS = 'points?|votes?|answers?|views?|comments?|replies|forks?|downloads?';
function actionLink(a) {
  const t = text(a).toLowerCase();
  if (t.split(/\s+/).length <= 3 && ACT_TEXT.test(t)) return true;
  if (ACT_HREF.test(a.getAttribute('href') || '')) return true;
  if (new RegExp('^' + NUM + '\\s*(?:' + COUNT_WORDS + '|stars?)$', 'i').test(t)) return true;
  if (/\/stargazers$/.test(a.getAttribute('href') || '')) return true;
  return false;
}
const isItem = rec => { const t = recText(rec); return t.length >= 12 || !!recOne(rec, 'img[src]') || recAll(rec, 'a[href]').length > 0 || PRICE.test(t); };
const norm = s => s.toLowerCase().replace(/[^\p{L}\p{N} ]/gu, '').replace(/\s+/g, ' ').trim();

// The title link: the link in the heading, else the link whose text is
// the title, else the longest link that is no action.
function titleLink(rec, heading, title) {
  if (heading) {
    const a = heading.closest('a[href]') || heading.querySelector('a[href]');
    if (a) return a;
  }
  const lnks = recAll(rec, 'a[href]').filter(a => !actionLink(a) && shown(a) && text(a));
  if (!lnks.length) return null;
  if (title) {
    const tn = norm(title);
    const m = lnks.find(a => { const an = norm(text(a)); return an.length > 3 && (tn === an || tn.includes(an)); });
    if (m) return m;
  }
  return lnks.reduce((b, a) => text(a).length > text(b).length ? a : b, lnks[0]);
}
const REDIRECT_PATH = /\/(?:sspa|gp)\/click|\/ck\/a\/?$|\/l\/?$|\/aclk\/?$|\/clk\/?$|\/url\/?$|\/r\/?$|\/redirect(?:or)?\/?$|\/linkout|\/l\.php$|\/away\/?$|\/out\/?$|\/goto\/?$|\/go\/?$|servlet\/redirect/i;
const REDIRECT_PARAMS = ['url', 'u', 'q', 'uddg', 'target', 'to', 'redirect', 'redirect_uri', 'link', 'dest', 'destination', 'href'];
function canonicalURL(href) {
  try {
    let u = new URL(href, location.href);
    for (let i = 0; i < 2 && REDIRECT_PATH.test(u.pathname); i++) {
      let next = null;
      for (const p of REDIRECT_PARAMS) {
        const v = u.searchParams.get(p);
        if (v && /^https?:\/\//i.test(v)) { next = v; break; }
      }
      if (!next) break;
      u = new URL(next);
    }
    return u.href;
  } catch (e) { return href; }
}
// counts reads "123 points", "3 answers", "1.2k views" into fields.
function counts(t, o) {
  const re = new RegExp('(?:^|[^\\w.,])' + NUM + '\\s*(' + COUNT_WORDS + ')\\b', 'gi');
  let m;
  while ((m = re.exec(t))) {
    const v = num(m[1]);
    if (isNaN(v)) continue;
    let w = m[2].toLowerCase();
    w = w === 'replies' ? 'replies' : w.replace(/s$/, '') + 's';
    if (!(w in o)) o[w] = v;
  }
}
// stars is a star count (GitHub's), never a rating.
function stars(rec, t) {
  const a = recAll(rec, 'a[href$="/stargazers"]')[0];
  if (a) { const v = num(text(a).replace(/[^\d.,kKmM]/g, '')); if (!isNaN(v)) return v; }
  const m = t.match(/[★⭐]\s*(\d[\d.,]*(?:[kKmM]\b)?)/) || t.match(new RegExp(NUM + '\\s*stars?\\b(?!\\s*(?:out of|rating))', 'i'));
  if (m) { const v = num(m[1]); if (!isNaN(v) && (v > 5 || !/[.,]/.test(m[1]))) return v; }
  return null;
}
function rating(rec, t) {
  for (const el of recAll(rec, '[aria-label]')) {
    const al = el.getAttribute('aria-label') || '';
    const m = al.match(/(\d+(?:\.\d+)?)\s*(?:out of|\/)\s*(?:5|10)\b/i) || (/rat(?:ed|ing)/i.test(al) && al.match(/(\d+(?:\.\d+)?)/));
    if (m) return parseFloat(m[1]);
  }
  const m = t.match(/(\d(?:\.\d+)?)\s*(?:out of\s*5|\/\s*5\b)(?:\s*stars?)?/i);
  return m ? parseFloat(m[1]) : null;
}
function reviews(t) {
  const m = t.match(/(\d[\d,]*)\s*(?:global\s+)?(?:ratings?|reviews?)\b/i);
  return m ? parseInt(m[1].replace(/,/g, ''), 10) : null;
}
function availability(t) {
  const l = t.toLowerCase();
  if (/out of stock|sold out|currently unavailable/.test(l)) return 'out of stock';
  if (/in stock/.test(l)) return 'in stock';
  const ol = l.match(/only\s+(\d+)\s+left/); if (ol) return 'only ' + ol[1] + ' left';
  if (/pre-?order/.test(l)) return 'pre-order';
  return null;
}
// priceOf is the first element's text that is a price alone (a price in a
// sentence is no field), other than the struck-out one.
function priceOf(rec, original) {
  const only = new RegExp('^(?:now\\s+|price:?\\s*)?' + PRICE.source + '$', 'i');
  for (const el of recAll(rec, '*')) {
    if (el.children.length > 2 || el.closest('del,s')) continue;
    const m = text(el).match(only);
    if (m) { const p = m[1].replace(/\s+/g, ''); if (p !== original) return p; }
  }
  return null;
}
function originalPrice(rec, t) {
  const d = recOne(rec, 'del,s,[class*="was-price"],[class*="list-price"],[class*="original-price"]');
  if (d) { const m = (d.innerText || d.textContent || '').match(PRICE); if (m) return m[0].replace(/\s+/g, ''); }
  const m = t.match(/was\s+([$€£¥₹]\s?\d[\d,]*(?:[.,]\d{1,2})?)/i);
  return m ? m[1].replace(/\s+/g, '') : null;
}
// description: the item's own text outside its links and headings, with
// the counts, prices, ratings, dates and separators taken out.
function description(rec, title, o) {
  const parts = [];
  let scanned = 0;
  const walk = n => {
    if (++scanned > 5000) return;
    if (n.nodeType === 3) { parts.push(n.textContent); return; }
    if (n.nodeType !== 1) return;
    if (n.matches('a,h1,h2,h3,h4,h5,h6,[role="heading"],script,style,noscript,svg,button,del,s,cite,[hidden],[aria-hidden="true"]')) return;
    if (n.children.length <= 2 && PRICE.test(text(n)) && new RegExp('^(?:now\\s+|was\\s+|price:?\\s*)?' + PRICE.source + '$', 'i').test(text(n))) return;
    if (n.tagName !== 'BR' && !shown(n)) return;
    parts.push(' ');
    for (const ch of n.childNodes) walk(ch);
    parts.push(' ');
  };
  for (const el of rec) walk(el);
  let d = parts.join('').replace(/\s+/g, ' ');
  d = d.replace(new RegExp('(?:^|\\s)' + NUM + '\\s*(?:' + COUNT_WORDS + '|stars?)\\b', 'gi'), ' ');
  d = d.replace(/[★⭐]\s*\d[\d.,]*(?:[kKmM]\b)?/g, ' ');
  d = d.replace(/(\d(?:\.\d+)?)\s*(?:out of\s*5|\/\s*5\b)(?:\s*stars?)?/gi, ' ');
  d = d.replace(/\(?\b\d[\d,]*\s*(?:global\s+)?(?:ratings?|reviews?)\)?/gi, ' ');
  if (o.date) d = d.split(o.date).join(' ');
  if (o.availability) d = d.replace(new RegExp(o.availability.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'), ' ');
  d = d.replace(/\(\s*[\w-]+(?:\.[\w-]+)*\.[a-z]{2,}\s*\)/gi, ' '); // a site bit: (example.com)
  d = d.replace(/^\s*\d+\.\s+/, ' ');                                 // a rank: "1."
  d = d.replace(/\s*[|·•]\s*/g, ' | ').replace(/(?:\s*\|\s*)+/g, ' | ');
  d = d.replace(/^[\s|·•\-–]+|[\s|·•\-–]+$/g, '').replace(/\b(?:by|sponsored|asked|answered|modified|posted|updated|published)$/i, '').replace(/\s+/g, ' ').trim();
  d = d.replace(/^[\s|·•\-–]+|[\s|·•\-–]+$/g, '');
  if (d.length <= 15) return '';
  const dn = norm(d), tn = norm(title || '');
  if (!dn || (tn && (tn.includes(dn) || dn === tn))) return '';
  return d.slice(0, 300);
}
// fields reads one list item's row; key dedupes by its title link.
function fields(rec) {
  const o = {};
  const full = recText(rec);
  const h = recOne(rec, 'h1,h2,h3,h4,h5,h6,[role="heading"]');
  let title = h ? text(h) : '';
  const link = titleLink(rec, h, title);
  if (link && (!title || title.length < 4)) title = text(link);
  if (!title) {
    const first = rec.map(text).find(Boolean) || '';
    const sentence = first.split(/[.!?](?:\s|$)/)[0];
    title = sentence && sentence.length >= 10 ? sentence : first;
  }
  if (title) { const pi = title.search(PRICE); if (pi >= 10) title = title.slice(0, pi).trim(); }
  if (title) o.title = title.slice(0, 200);
  if (link) o.url = canonicalURL(link.href);
  const op = originalPrice(rec, full);
  const pr = priceOf(rec, op); if (pr) o.price = pr;
  if (op && pr && op !== pr) o.original_price = op;
  const rt = rating(rec, full); if (rt !== null) o.rating = rt;
  const rv = reviews(full); if (rv !== null) o.reviews = rv;
  const st = stars(rec, full); if (st !== null) o.stars = st;
  counts(full, o);
  const av = availability(full); if (av) o.availability = av;
  const rest = title ? full.split(title).join(' ') : full;
  const dt = rest.match(DATE); if (dt) o.date = dt[0];
  if (/\bsponsored\b/i.test(full) || recOne(rec, '[data-testid*="sponsored"],[aria-label*="ponsored"]')) o.sponsored = true;
  const d = description(rec, title, o); if (d) o.description = d;
  return { row: o, key: link ? canonicalURL(link.href).split('#')[0] + '|' + norm(title) : '' };
}

// ---- data tables ----
function tableHeaders(table) {
  let hr = table.tHead && table.tHead.rows.length ? table.tHead.rows[table.tHead.rows.length - 1] : null;
  if (!hr) { const r0 = table.rows[0]; if (r0 && r0.cells.length && [...r0.cells].every(c => c.tagName === 'TH')) hr = r0; }
  if (!hr) return null;
  const hs = [], used = {};
  for (const c of hr.cells) {
    let h = text(c).replace(/\[\w+\]/g, '').trim() || ('col' + (hs.length + 1));
    if (used[h]) { used[h]++; h = h + '_' + used[h]; } else used[h] = 1;
    hs.push(h);
    for (let s = 1; s < (c.colSpan || 1); s++) hs.push(h + '_' + (s + 1));
  }
  return { cells: hs, row: hr };
}
function tableRow(tr, hdr) {
  const o = {};
  let i = 0, url = '';
  for (const c of tr.cells) {
    const k = hdr ? (hdr.cells[i] || ('col' + (i + 1))) : ('col' + (i + 1));
    const v = text(c).replace(/\[\w+\]/g, '').trim();
    if (v) o[k] = v.slice(0, 300);
    if (!url) { const a = [...c.querySelectorAll('a[href]')].find(x => !/^#/.test(x.getAttribute('href') || '') && text(x)); if (a) url = canonicalURL(a.href); }
    i += c.colSpan || 1;
  }
  if (url && Object.keys(o).length) o.url = url;
  return o;
}
function tableRows(table) {
  const hdr = tableHeaders(table);
  return [...table.rows].filter(r => (!hdr || r !== hdr.row) && !(r.parentElement && r.parentElement.tagName === 'THEAD') && shown(r) && text(r))
    .map(r => tableRow(r, hdr)).filter(o => Object.keys(o).length);
}
const dataTable = t => !!t && t.tagName === 'TABLE' && !!tableHeaders(t) && t.rows.length >= 3;

// ---- candidates ----
// landmarked: inside a nav, header, footer or aside, unless --root asked
// for it.
function landmarked(c) {
  const lm = c.closest(LANDMARK);
  if (!lm) return false;
  if (!ROOT) return true;
  return lm !== ROOT && ROOT.contains(lm);
}
// NOTES names a list of references, footnotes or citations, by the class,
// id or role of the container or its two nearest ancestors.
const NOTES = /\b(?:ref(?:erence)?s?|reflist|footnotes?|citations?|endnotes?|doc-endnotes|doc-bibliography|bibliography)\b/i;
function notesName(c) {
  let s = '';
  for (let n = c, up = 0; n && up < 3; n = n.parentElement, up++) s += ' ' + (n.getAttribute('class') || '') + ' ' + (n.id || '') + ' ' + (n.getAttribute('role') || '');
  return s;
}
// score rates one group of items in container c, 0 when it is no list.
function score(c, items) {
  const count = items.length;
  if (count < 3) return 0;
  let tt = 0, lt = 0, hc = 0, ic = 0, lc = 0, single = 0, prose = 0;
  for (const rec of items) {
    const t = recText(rec);
    tt += t.length;
    const ls = recAll(rec, 'a[href]').filter(a => !actionLink(a));
    let ltext = 0; for (const a of ls) ltext += text(a).length;
    lt += Math.min(ltext, t.length);
    if (recOne(rec, 'h1,h2,h3,h4,h5,h6,[role="heading"]')) hc++;
    if (recOne(rec, 'img[src]')) ic++;
    if (ls.length) lc++;
    if (ls.length === 1 && norm(text(ls[0])) === norm(t)) single++;
    if (rec.every(el => PROSE_TAGS.has(el.tagName))) prose++;
  }
  const avg = tt / count;
  if (avg < 12) return 0;
  if (!ROOT) {
    if (prose / count > 0.5) return 0;                              // paragraphs, not items
    if (avg > 150 && lt / tt < 0.1 && hc / count < 0.5) return 0;   // blocks of prose
  }
  const tf = Math.min(Math.max(avg / 50, 0.5), 4);
  let s = count * tf * (1 + hc / count + (ic / count) * 0.5 + (lc / count) * 0.5);
  if (single / count > 0.8 && avg < 40) s *= 0.3;                   // a menu of bare links
  if (dataTable(c.tagName === 'TBODY' ? c.closest('table') : c)) s *= 3; // a table with headers says it is data
  if (NOTES.test(notesName(c))) s *= 0.2;                            // references and footnotes
  return s;
}
function bestIn(scope) {
  let best = null;
  for (const c of [scope, ...scope.querySelectorAll('*')]) {
    if (SKIP_TAGS.has(c.tagName) || c.children.length < 3 || landmarked(c)) continue;
    const { kids, groups, of } = groupsOf(c);
    if (kids.length < 3) continue;
    const rec = records(kids, of);
    const options = [];
    if (rec) options.push(rec.recs.filter(isItem));
    for (const g of groups) {
      if (g.els.length < 3 || (rec && rec.keys.has(g.key))) continue;
      options.push(g.els.map(e => [e]).filter(isItem));
    }
    for (const items of options) {
      const s = score(c, items);
      if (s > 0 && (!best || s > best.score)) best = { c, items, score: s };
    }
  }
  return best;
}
// exampleOf treats --root as one item: its nearest ancestor-or-self that
// has at least two alike siblings names the list.
function exampleOf(el) {
  for (let n = el; n && n !== document.body && n.parentElement; n = n.parentElement) {
    const { kids, of } = groupsOf(n.parentElement);
    const g = of.get(n);
    if (!g) continue;
    const rec = records(kids, of);
    let items = null;
    if (rec && rec.keys.has(g.key)) items = rec.recs.filter(isItem);
    else items = g.els.map(e => [e]).filter(isItem);
    if (items.length >= 3) return { c: n.parentElement, items, score: score(n.parentElement, items) };
  }
  return null;
}

// proseChars is the page's paragraph text outside lists, tables and
// landmarks: what makes an article or a doc page.
function proseChars() {
  let n = 0;
  for (const p of SCOPE.querySelectorAll('p')) {
    if (p.closest(LANDMARK) || p.closest('li,td,dd')) continue;
    const t = text(p);
    if (t.length >= 60) n += t.length;
  }
  return n;
}

// ---- the answer ----
function answer(kind, container, label, rows, atLeast) {
  return { kind, container, label, rows: rows.slice(0, LIMIT), found: rows.length, at_least: !!atLeast, page_says: kind ? pageSays() : null, wall: '' };
}
const none = () => answer('', '', '', []);
function listAnswer(best) {
  const c = best.c;
  const tbl = c.tagName === 'TBODY' ? c.closest('table') : (c.tagName === 'TABLE' ? c : null);
  if (tbl && dataTable(tbl) && best.items.every(r => r.length === 1 && r[0].tagName === 'TR')) {
    const hdr = tableHeaders(tbl);
    return answer('table', describe(tbl), labelOf(tbl), best.items.map(r => tableRow(r[0], hdr)).filter(o => Object.keys(o).length));
  }
  const rows = [];
  const seen = new Set();
  let more = false;
  for (const rec of best.items) {
    if (rows.length >= MAX_ITEMS) { more = true; break; }
    const f = fields(rec);
    if (!Object.keys(f.row).length) continue;
    if (f.key) { if (seen.has(f.key)) continue; seen.add(f.key); }
    rows.push(f.row);
  }
  const box = (c.tagName === 'TBODY' && c.parentElement) ? c.parentElement : c;
  return answer('list', describe(box), labelOf(box), rows, more);
}
function tableAnswer() {
  let t = ROOT ? ROOT.closest('table') : null;
  if (!dataTable(t)) {
    const all = [...SCOPE.querySelectorAll('table')].filter(x => shown(x) && dataTable(x) && !landmarked(x));
    t = all.reduce((a, b) => (!a || b.rows.length > a.rows.length) ? b : a, null);
  }
  if (!t && ROOT) t = ROOT.closest('table');
  if (!t) return none();
  return answer('table', describe(t), labelOf(t), tableRows(t));
}
function jsonldAnswer(withArticles) {
  const ld = jsonld(withArticles);
  return ld.rows.length ? answer('jsonld', ld.type, ld.label, ld.rows) : null;
}

const w = wall();
if (w) return { kind: '', container: '', label: '', rows: [], found: 0, at_least: false, page_says: null, wall: w };
if (MODE === 'jsonld') return jsonldAnswer(true) || none();
if (MODE === 'table') return tableAnswer();

let best;
if (ROOT) {
  // --root is the list or an ancestor of it, or one item of the list. A
  // list inside it wins when the root is its container: the list's own
  // element, or one whose text is mostly the list's. Else the root may be
  // one item, and its alike siblings win when there are more of them.
  const inside = bestIn(ROOT);
  const ex = exampleOf(ROOT);
  const holds = !!inside && (inside.c === ROOT ||
    inside.items.reduce((n, r) => n + recText(r).length, 0) * 2 >= text(ROOT).length);
  best = ex && !holds && (!inside || ex.items.length > inside.items.length) ? ex : inside;
} else {
  best = bestIn(SCOPE);
}
if (MODE === 'list' || ROOT) return best ? listAnswer(best) : none();

// auto on the whole page: a strong list wins, else the page's JSON-LD;
// an article or doc page has no list.
const ld = jsonld(false);
const weak = !best || best.items.length < 4 || best.score < 12 ||
  best.items.every(r => !recOne(r, 'a[href]') && !recOne(r, 'h1,h2,h3,h4,h5,h6'));
if (weak && ld.rows.length) return jsonldAnswer(false);
if (best) {
  const prose = proseChars();
  if (ld.article || prose >= 1500) {
    const listChars = best.items.reduce((n, r) => n + recText(r).length, 0);
    if (listChars < prose * 0.5 || (ld.article && !dataTable(best.c.closest('table')))) return none();
  }
}
return best ? listAnswer(best) : none();
}
