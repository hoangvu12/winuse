// extract_list.js - a Page's repeated list items as JSON records.
//
// Derived from bladebro 4.8.3 (https://github.com/dondai44423/bladebro),
// Copyright the bladebro authors, licensed under the Apache License 2.0
// (copy in ./LICENSE.bladebro):
//   - src/mcp/server/js/auto_extract.js  (list detection + field extraction)
//   - src/adapters/js/jsonld.js          (schema.org JSON-LD fallback)
// Changes from the original: removed the site adapters (__SITE_ADAPTERS__),
// the Reddit/GitHub field branches and the GitHub-repo / product-detail
// paths; replaced the __LIMIT__ slot with opts.limit; wrapped as one
// function expression; added an optional root Element (`this`) that scopes
// the candidate scan; added header-keyed `cells` for rows of data tables and
// an opts.mode='table' that returns a data table's rows (both new code).
//
// Use:
//   Runtime.evaluate       "(" + src + ").call(null, {limit: 100})"
//   Runtime.callFunctionOn functionDeclaration: src, objectId: <Element>,
//                          arguments: [{value: {limit: 100}}]
// Returns a JSON-able object (not a string).
function (opts) {
opts = opts || {};
const LIMIT = opts.limit || 100;
const ROOT = (typeof Element !== 'undefined' && this instanceof Element) ? this : null;
const SCOPE = ROOT || document;
// Price: currency symbol required (no bare decimal numbers - false positives).
const PRICE=/([$€£¥₹]\s?\d[\d,]*(?:[.,]\d{1,2})?)/;
const DATE=/(\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}[\/]\d{1,2}[\/]\d{2,4}\b|\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\s+\d{1,2},?\s*\d{2,4}\b|\b\d+\s+(?:second|minute|hour|day|week|month|year)s?\s+ago\b)/i;
const HOST=(location.hostname||'').toLowerCase();
const ACT_PAT=/^(vote|upvote|downvote|comment|comments|discuss|reply|replies|share|save|hide|flag|report|favorite|fav|like|dislike|follow|Subscribe|Pin|Unpin|More|less|edit|delete|remove|add|new|open|show|expand|collapse|permalink|embed|cite|parent|context|full story|read more|continue reading|view|all|next|prev|previous)$/i;
const ACT_HREF=/\/vote|\/comment|\/reply|\/action|javascript:|#comment|#respond|#reply/i;
const _vtxtCache=new WeakMap();
function sig(el){const k=[...el.children].map(c=>c.tagName).join(',');return el.tagName+'['+k+']';}
function txt(el){if(!el)return '';return(el.innerText||'').replace(/\s+/g,' ').trim();}
function vtxt(el){if(!el)return '';if(typeof el==='object'){const c=_vtxtCache.get(el);if(c!==undefined)return c;}const v=(el.innerText||'').replace(/\s+/g,' ').trim();if(typeof el==='object')_vtxtCache.set(el,v);return v;}
function isShown(el){if(!el)return false;if(el.offsetParent!==null&&el.offsetParent!==undefined)return true;if(typeof el.getClientRects==='function')return el.getClientRects().length>0;return false;}
function links(el){const r=[...el.querySelectorAll('a[href]')];if(el.matches&&el.matches('a[href]'))r.unshift(el);return r;}
function extLink(el){return links(el).find(a=>a.hostname&&a.hostname!==HOST);}
function norm(s){return s.toLowerCase().replace(/[^a-z0-9 ]/g,'').replace(/\s+/g,' ').trim();}
function isActionLink(a){
const t=txt(a).toLowerCase();
if(t.split(/\s+/).length<=3&&ACT_PAT.test(t))return true;
if(ACT_HREF.test(a.href))return true;
if(/\d+\s+(point|comment|vote|reply|reaction)/i.test(t))return true;
return false;
}
function bestLink(item,title){
const lnks=links(item).filter(a=>typeof a.href==='string'&&!isActionLink(a)&&isShown(a));
if(lnks.length===0)return null;
const ext=lnks.find(a=>a.hostname&&a.hostname!==HOST);
if(ext)return ext;
if(title){
const tn=norm(title);
if(tn){const match=lnks.find(a=>{const an=norm(txt(a));return an&&tn.includes(an)&&an.length>3});if(match)return match;}
}
return lnks.reduce((best,a)=>{const at=txt(a).length,bt=txt(best).length;return at>bt?a:best;},lnks[0]);
}
const REDIRECT_PATH=/\/(?:sspa|gp)\/click|\/ck\/a\/?$|\/l\/?$|\/aclk\/?$|\/clk\/?$|\/url\/?$|\/redirect(?:or)?\/?$|\/linkout|\/l\.php$|\/away\/?$|\/out\/?$|\/goto\/?$|\/go\/?$|servlet\/redirect/i;
const REDIRECT_PARAMS=['url','u','q','uddg','target','to','redirect','redirect_uri','link','dest','destination','href'];
function decodeBingU(v){if(!v||!/^a1[A-Za-z0-9_-]{8,}$/.test(v))return null;try{const b64=v.slice(2).replace(/-/g,'+').replace(/_/g,'/');const pad=b64+'='.repeat((4-b64.length%4)%4);const decoded=atob(pad);return /^https?:\/\//i.test(decoded)?decoded:null;}catch(e){return null;}}
function canonicalURL(href){try{let u=new URL(href,location.href);for(let i=0;i<2&&REDIRECT_PATH.test(u.pathname);i++){let next=null;if(/\/ck\/a\/?$/i.test(u.pathname)){const bu=decodeBingU(u.searchParams.get('u'));if(bu){next=bu;}}if(!next){for(const p of REDIRECT_PARAMS){const v=u.searchParams.get(p);if(!v)continue;let d=v;if(!/^https?:\/\//i.test(d)&&d.charAt(0)!=='/'){try{d=decodeURIComponent(v);}catch(e){}}if(/^https?:\/\//i.test(d)){next=d;break;}if(d.charAt(0)==='/'){next=new URL(d,u).href;break;}}}if(!next)break;try{u=new URL(next);}catch(e){break;}}return u.href;}catch(e){return href;}}
function wrappedURL(href){try{const u=new URL(href,location.href);return /^https?:$/.test(u.protocol)&&REDIRECT_PATH.test(u.pathname)&&REDIRECT_PARAMS.some(p=>!!u.searchParams.get(p));}catch(e){return false;}}
// How many results the page TEXT claims vs. how many are in the DOM.
function pageClaim(){const t=(document.body&&document.body.innerText?document.body.innerText:'').slice(0,60000);
const pats=[[/showing\s+[\d.,]+\s*[-–—]\s*[\d.,]+\s+of\s+([\d.,]+)/i,true],[/[\d.,]+\s+of\s+([\d.,]+)\s+(?:results?|items?|listings|products|entries|matches|hits)/i,true],[/(?:about|over|more than)\s+([\d.,]+)\s+(?:results?|items?|listings|products|entries|matches|hits)/i,false],[/([\d.,]+)\s+(?:results?|listings|products|entries|matches|hits)\b/i,false]];
for(const pp of pats){const m=t.match(pp[0]);if(m){const v=parseInt(m[1].replace(/[.,]/g,''),10);if(v>=20&&v<1e9)return {total:v,paged:pp[1],matched:m[0].slice(0,80)};}}
return null;}
// Shopping fields (generic).
function rating(el){const a=el.querySelector('[aria-label*="star"],[aria-label*="rating"]');if(a){const al=a.getAttribute('aria-label')||'';const m=al.match(/(\d+\.?\d*)\s*out of\s*\d+/i)||al.match(/(\d+\.?\d*)/);if(m)return parseFloat(m[1]);}const r=el.querySelector('[data-testid*="rating"],[class*="rating"],[class*="star"],[data-hook*="rating"],i[class*="a-icon-star"],span[class*="a-icon-alt"]');if(r){const m=(r.innerText||r.textContent||'').match(/(\d+\.?\d*)/);if(m)return parseFloat(m[1]);}const t=txt(el);const tm=t.match(/(\d+\.?\d*)\s*(?:out of\s*5|\/5|stars?)/i);if(tm)return parseFloat(tm[1]);return null;}
function reviews(el){const t=txt(el);let m=t.match(/(\d[\d,]*)\s*(?:global\s+)?(?:ratings?|reviews?)/i);if(m)return parseInt(m[1].replace(/,/g,''),10);m=t.match(/(\d[\d,]*)\+?\s*(?:bought|purchased|sold)/i);if(m)return parseInt(m[1].replace(/,/g,''),10);return null;}
function avail(el){const t=txt(el).toLowerCase();if(/in stock|in-store only/.test(t))return 'in stock';if(/out of stock|currently unavailable/.test(t))return 'out of stock';const ol=t.match(/only\s+(\d+)\s+left/i);if(ol)return 'only '+ol[1]+' left';const sh=t.match(/usually ships[^.]{0,50}/i);if(sh)return sh[0].trim();if(/pre-order/i.test(t))return 'pre-order';return null;}
function origPrice(el){const d=el.querySelector('del,s,[data-testid*="original"],[class*="was-price"],[class*="list-price"],[class*="original-price"]');if(d){const m=(d.innerText||d.textContent||'').match(PRICE);if(m)return m[0];}const t=txt(el);const m=t.match(/was\s+([$€£¥₹]\s?\d[\d,]*(?:[.,]\d{1,2})?)/i);if(m)return m[1];return null;}
function isSponsored(el){const t=txt(el).toLowerCase();if(t.includes('sponsored')||t.includes('sponsored ad'))return true;const b=el.querySelector('[data-testid*="sponsored"],[class*="sponsored"],[aria-label*="sponsored"]');return!!b;}
function desc(el){if(!el||!el.tagName)return '';let s=el.tagName.toLowerCase();try{const id=el.getAttribute&&el.getAttribute('id');if(id&&id.length<=30)return s+'#'+id;const cl=(typeof el.className==='string'&&el.className.trim())?el.className.trim().split(/\s+/)[0]:'';if(cl&&cl.length<=24)s+='.'+cl;}catch(e){}return s;}

// ---- NEW: data tables (not in bladebro) ----
function tableHeaders(table){
  // Header row: the last <thead> row, else the first row if it is all <th>.
  const head=table.tHead&&table.tHead.rows.length?table.tHead.rows[table.tHead.rows.length-1]:null;
  let hr=head;
  if(!hr){const r0=table.rows[0];if(r0&&r0.cells.length&&[...r0.cells].every(c=>c.tagName==='TH'))hr=r0;}
  if(!hr)return null;
  const hs=[];const used={};
  for(const c of hr.cells){let h=txt(c).replace(/\[\w+\]/g,'').trim()||('col'+(hs.length+1));if(used[h]){used[h]++;h=h+'_'+used[h];}else used[h]=1;const span=c.colSpan||1;hs.push(h);for(let s=1;s<span;s++)hs.push(h+'_'+(s+1));}
  return {cells:hs,row:hr};
}
function rowCells(tr,hdr){const o={};let i=0;for(const c of tr.cells){const k=hdr.cells[i]||('col'+(i+1));const v=txt(c).replace(/\[\w+\]/g,'').trim();if(v)o[k]=v.slice(0,200);i+=c.colSpan||1;}return o;}
function tableRecords(table,limit){
  const hdr=tableHeaders(table);if(!hdr)return null;
  const body=[...table.rows].filter(r=>r!==hdr.row&&!(r.parentElement&&r.parentElement.tagName==='THEAD')&&isShown(r));
  const items=[];
  for(const tr of body){if(items.length>=limit)break;const o=rowCells(tr,hdr);if(Object.keys(o).length===0)continue;const a=[...tr.querySelectorAll('a[href]')].find(x=>typeof x.href==='string'&&!x.href.includes('#cite'));if(a)o._url=canonicalURL(a.href);items.push(o);}
  return {container:'table',table:desc(table),caption:table.caption?txt(table.caption).slice(0,120):undefined,columns:hdr.cells,count:items.length,rows_total:body.length,items:items};
}
function dataTables(scope){const ts=[...(scope.matches&&scope.matches('table')?[scope]:[]),...scope.querySelectorAll('table')].filter(t=>isShown(t)&&tableHeaders(t)&&t.rows.length>=3);return ts;}
if(opts.mode==='table'){
  const ts=dataTables(SCOPE);
  if(!ts.length)return {error:'no data table (a table with a header row) found',items:[]};
  const big=ts.reduce((a,b)=>b.rows.length>a.rows.length?b:a,ts[0]);
  const r=tableRecords(big,LIMIT);r.tables_found=ts.length;return r;
}
// ---- end NEW ----

// schema.org JSON-LD fallback (bladebro src/adapters/js/jsonld.js, unchanged).
function extractJsonLd() {
  const scripts = [...document.querySelectorAll('script[type="application/ld+json"]')];
  if (!scripts.length) return null;
  const entities = [];
  const collect = node => {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) { for (const n of node) collect(n); return; }
    if (Array.isArray(node['@graph'])) collect(node['@graph']);
    if (node['@type']) entities.push(node);
  };
  for (const s of scripts.slice(0, 12)) { try { collect(JSON.parse(s.textContent || '')); } catch (e) {} }
  const hasType = (e, names) => { const t = e['@type']; const arr = Array.isArray(t) ? t : [t]; return arr.some(x => typeof x === 'string' && names.includes(x.replace(/^https?:\/\/schema\.org\//i, ''))); };
  const str = v => (typeof v === 'string' ? v.replace(/\s+/g, ' ').trim() : '');
  const nameOf = v => { if (!v) return ''; if (typeof v === 'string') return str(v); if (Array.isArray(v)) return v.map(nameOf).filter(Boolean).join(', '); return str(v.name || v.title || ''); };
  const job = entities.find(e => hasType(e, ['JobPosting']));
  if (job && str(job.title)) {
    const o = { title: str(job.title).slice(0, 200), url: str(job.url) || location.href.split('#')[0] };
    const co = nameOf(job.hiringOrganization); if (co) o.company = co.slice(0, 120);
    const loc = job.jobLocation && (Array.isArray(job.jobLocation) ? job.jobLocation[0] : job.jobLocation);
    const addr = loc && loc.address;
    if (addr) { const parts = [str(addr.addressLocality), str(addr.addressRegion), str(addr.addressCountry)].filter(Boolean); if (parts.length) o.location = parts.join(', '); }
    if (str(job.jobLocationType)) o.location_type = str(job.jobLocationType);
    if (str(job.employmentType)) o.employment_type = str(job.employmentType);
    if (str(job.datePosted)) o.date_posted = str(job.datePosted);
    if (str(job.validThrough)) o.valid_through = str(job.validThrough);
    if (job.baseSalary && job.baseSalary.value) { const sv = job.baseSalary.value; const cur = str(job.baseSalary.currency);
      if (sv.minValue || sv.maxValue) o.salary = [cur, sv.minValue, sv.maxValue ? '-' + sv.maxValue : '', str(sv.unitText)].filter(Boolean).join(' ');
      else if (sv.value) o.salary = [cur, sv.value, str(sv.unitText)].filter(Boolean).join(' '); }
    const d = str(job.description).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim(); if (d) o.description = d.slice(0, 1200);
    return { container: 'schema-job', detail: true, count: 1, items: [o] };
  }
  const recipe = entities.find(e => hasType(e, ['Recipe']));
  if (recipe && str(recipe.name)) {
    const o = { title: str(recipe.name).slice(0, 200), url: str(recipe.url) || location.href.split('#')[0] };
    const au = nameOf(recipe.author); if (au) o.author = au;
    if (str(recipe.prepTime)) o.prep_time = str(recipe.prepTime);
    if (str(recipe.cookTime)) o.cook_time = str(recipe.cookTime);
    if (str(recipe.totalTime)) o.total_time = str(recipe.totalTime);
    if (recipe.recipeYield) o.yield = nameOf(recipe.recipeYield);
    if (Array.isArray(recipe.recipeIngredient)) o.ingredients = recipe.recipeIngredient.map(str).filter(Boolean).slice(0, 40);
    if (recipe.aggregateRating && recipe.aggregateRating.ratingValue !== undefined) { const rv = parseFloat(recipe.aggregateRating.ratingValue); if (!isNaN(rv)) o.rating = rv; }
    return { container: 'schema-recipe', detail: true, count: 1, items: [o] };
  }
  const ev = entities.find(e => hasType(e, ['Event']));
  if (ev && str(ev.name)) {
    const o = { title: str(ev.name).slice(0, 200), url: str(ev.url) || location.href.split('#')[0] };
    if (str(ev.startDate)) o.start_date = str(ev.startDate);
    if (str(ev.endDate)) o.end_date = str(ev.endDate);
    const loc = nameOf(ev.location); if (loc) o.location = loc;
    const d = str(ev.description); if (d) o.description = d.slice(0, 600);
    return { container: 'schema-event', detail: true, count: 1, items: [o] };
  }
  const prod = entities.find(e => hasType(e, ['Product']));
  if (prod && str(prod.name)) {
    const o = { title: str(prod.name).slice(0, 200), url: str(prod.url) || location.href.split('#')[0] };
    const brand = nameOf(prod.brand); if (brand) o.brand = brand;
    if (str(prod.sku)) o.sku = str(prod.sku);
    const offers = Array.isArray(prod.offers) ? prod.offers[0] : prod.offers;
    if (offers && offers.price !== undefined) { const cur = str(offers.priceCurrency); o.price = (cur ? cur + ' ' : '') + String(offers.price);
      if (str(offers.availability)) o.availability = str(offers.availability).replace(/^https?:\/\/schema\.org\//i, ''); }
    if (prod.aggregateRating && prod.aggregateRating.ratingValue !== undefined) { const rv = parseFloat(prod.aggregateRating.ratingValue); if (!isNaN(rv)) o.rating = rv;
      const rc = prod.aggregateRating.reviewCount ?? prod.aggregateRating.ratingCount;
      if (rc !== undefined) { const rcn = parseInt(String(rc), 10); if (!isNaN(rcn)) o.reviews = rcn; } }
    const d = str(prod.description); if (d) o.description = d.slice(0, 600);
    return { container: 'schema-product', detail: true, count: 1, items: [o] };
  }
  const art = entities.find(e => hasType(e, ['NewsArticle', 'Article', 'BlogPosting', 'TechArticle', 'ScholarlyArticle']));
  if (art && (str(art.headline) || str(art.name))) {
    const o = { title: (str(art.headline) || str(art.name)).slice(0, 240), url: str(art.url) || location.href.split('#')[0] };
    const au = nameOf(art.author); if (au) o.author = au.slice(0, 140);
    const pub = nameOf(art.publisher); if (pub) o.publisher = pub.slice(0, 120);
    if (str(art.datePublished)) o.published = str(art.datePublished);
    if (str(art.dateModified) && str(art.dateModified) !== o.published) o.modified = str(art.dateModified);
    const d = str(art.description); if (d) o.description = d.slice(0, 600);
    return { container: 'schema-article', detail: true, count: 1, items: [o] };
  }
  return null;
}
// NEW: opts.mode='jsonld' runs only the JSON-LD reader (bladebro reaches it
// only when no repeated list is found).
if(opts.mode==='jsonld'){return extractJsonLd()||{error:'no usable JSON-LD',items:[]};}

const SKIP_TAGS=new Set(['STYLE','SCRIPT','HEAD','NOSCRIPT','SVG','TEMPLATE','LINK','META','BR','HR','PATH','DEFS','USE','G','RECT','CIRCLE','LINE','POLYGON','POLYLINE']);
const SEMANTIC_LIST_CONTAINERS=new Set(['UL','OL','TBODY']);
function groupKey(parent,child){
const pRole=parent.getAttribute&&parent.getAttribute('role');
const cRole=child.getAttribute&&child.getAttribute('role');
const dt=child.getAttribute&&(child.getAttribute('data-testid')||child.getAttribute('data-component-type'));
if(dt)return child.tagName+'@'+dt;
if(cRole==='listitem'||cRole==='article'||child.tagName==='ARTICLE')return child.tagName+':'+(cRole||'article');
if(SEMANTIC_LIST_CONTAINERS.has(parent.tagName)||pRole==='list'||pRole==='feed')return child.tagName+'*';
return sig(child);
}
// Quality gate: a real list item is VISIBLE and carries substance.
const isItem=it=>{const t=vtxt(it);return t.length>=12||it.querySelector('img[src]')||links(it).length>0||PRICE.test(t);};
let best=null,bestScore=0,bestKey='';
const candidates=ROOT?[ROOT,...ROOT.querySelectorAll('*')]:document.querySelectorAll('*');
const ranked=[];
for(const c of candidates){
if(SKIP_TAGS.has(c.tagName))continue;
const kids=[...c.children].filter(k=>k.nodeType===1&&isShown(k));
if(kids.length<3)continue;
const groups={};
for(const k of kids){const s=groupKey(c,k);if(!groups[s])groups[s]=[];groups[s].push(k);}
for(const s in groups){
let items=groups[s];
if(items.length<3)continue;
if(items[0]&&SKIP_TAGS.has(items[0].tagName))continue;
items=items.filter(isItem);
if(items.length<3)continue;
let totalText=0,extCount=0,hCount=0,imgCount=0,linkCount=0;
for(const it of items){totalText+=vtxt(it).length;if(extLink(it))extCount++;if(it.querySelector('h1,h2,h3,h4,h5,h6,[role="heading"]'))hCount++;if(it.querySelector('img[src]'))imgCount++;if(links(it).length>0)linkCount++;}
const count=items.length;const avgText=totalText/count;
if(avgText<12)continue;
const tf=Math.min(Math.max(avgText/50,0.5),4);
const score=count*tf*(1+(extCount/count)*2+(hCount/count)+(imgCount/count)*0.5+(linkCount/count)*0.3);
if(opts.debug)ranked.push({container:desc(c),key:s,count:count,score:Math.round(score)});
if(score>bestScore){bestScore=score;best=c;bestKey=s;}
}
}
if(!best){
if(!ROOT){const ld=extractJsonLd();if(ld)return ld;}
return {error:'no repeated list found',items:[]};
}
const unwrappedSet=new WeakSet();
const items=[...best.children].filter(k=>k.nodeType===1&&isShown(k)&&groupKey(best,k)===bestKey&&isItem(k)).map(item=>{
const fullText=vtxt(item);const o={};
// Title: heading -> longest link text -> first sentence.
const h=item.querySelector('h1,h2,h3,h4,h5,h6,[role="heading"]');
let title=h?vtxt(h):'';
const link=bestLink(item,title);
if(link){const lt=vtxt(link);if(lt&&(!title||(!h&&title.length<10)))title=lt.slice(0,200);}
if(!title){const sentence=fullText.split(/\.|!|\?/)[0];title=(sentence&&sentence.length>10?sentence:fullText).slice(0,200);}
if(title&&/^\s*[\d.,]+\s*(units?|beds?|baths?|ba|bd|mi|miles?|sq\.?\s?ft|sqft|acres?)?\s*$/i.test(title)){const lt2=fullText.replace(/\s+/g,' ').trim();if(lt2.length>title.length+6)title=lt2.slice(0,200);}
if(title){const pi=title.search(PRICE);if(pi>=10)title=title.slice(0,pi).trim();}
if(title)o.title=title.slice(0,200);
if(link){const cu=canonicalURL(link.href);if(cu!==link.href)unwrappedSet.add(o);o.url=cu;if(wrappedURL(cu))o.url_wrapped=true;}
const img=item.querySelector('img[src]');
if(img){o.image=img.src;if(img.alt)o.image_alt=img.alt.slice(0,100);}
const pr=(fullText.match(PRICE)||[])[0];
if(pr)o.price=pr;
const nonTitle=fullText.slice((title||'').length);
const dt=(nonTitle.match(DATE)||[])[0];
if(dt)o.date=dt;
const parts=[];let scanned=0;
const readDesc=n=>{if(++scanned>20000)return;if(n.nodeType===3){parts.push(n.textContent);return;}if(n.nodeType!==1)return;if(n.matches('a,h1,h2,h3,h4,h5,h6,script,style,noscript,svg,[hidden]'))return;const cs=n.ownerDocument.defaultView.getComputedStyle(n);if(cs.display==='none'||cs.visibility==='hidden'||cs.opacity==='0')return;for(const ch of n.childNodes)readDesc(ch);if(n.shadowRoot)for(const ch of n.shadowRoot.childNodes)readDesc(ch);};
readDesc(item);
const description=parts.join(' ').replace(/\s+/g,' ').trim();
if(description&&description.length>15){const dn=norm(description),tn=norm(title||'');if(dn&&!tn.includes(dn)&&!dn.includes(tn))o.description=description.slice(0,300);}
const rt=rating(item);if(rt!==null)o.rating=rt;const rv=reviews(item);if(rv!==null)o.reviews=rv;const av=avail(item);if(av)o.availability=av;const op=origPrice(item);if(op)o.original_price=op;if(isSponsored(item))o.sponsored=true;
// NEW: a row of a data table also carries its cells keyed by column header.
if(item.tagName==='TR'){const tb=item.closest('table');const hdr=tb&&tableHeaders(tb);if(hdr&&item!==hdr.row)o.cells=rowCells(item,hdr);}
return o;
}).filter(o=>Object.keys(o).length>0);
const uniq=[];const seen=new Set();let dupes=0,keyless=0;
for(const it of items){const key=it.url?it.url.split('#')[0]:(it.title?'t:'+norm(it.title):'i:'+(keyless++));if(key&&seen.has(key)){dupes++;continue;}if(uniq.length>=LIMIT)break;if(key)seen.add(key);uniq.push(it);}
let sponsored=0,unwrapped=0,unresolved=0;
for(const it of uniq){if(it.sponsored)sponsored++;if(unwrappedSet.has(it))unwrapped++;if(it.url_wrapped)unresolved++;}
const lowConf=uniq.length>0&&uniq.every(o=>!o.url)&&(uniq.reduce((a,o)=>a+(o.title||'').length,0)/uniq.length)<25;
const out={container:best.tagName.toLowerCase(),container_desc:desc(best),count:uniq.length,items:uniq};
if(sponsored>0)out.sponsored=sponsored;
if(dupes>0)out.duplicates_removed=dupes;
if(unwrapped>0)out.redirects_unwrapped=unwrapped;if(unresolved>0){out.redirects_unresolved=unresolved;out.note=unresolved+' unresolved redirect URL(s); destination is opaque - follow the link and read its final URL';}
const claim=pageClaim();
if(claim){out.page_total=claim.total;out.page_claim_text=claim.matched;if(claim.total>uniq.length){out.partial=true;if(claim.paged)out.paged=true;}}
if(lowConf){out.confidence='low';out.note='no clear list found - items may be page fragments; verify before relying on them';}
if(opts.debug)out.ranked=ranked.sort((a,b)=>b.score-a.score).slice(0,6);
return out;
}
