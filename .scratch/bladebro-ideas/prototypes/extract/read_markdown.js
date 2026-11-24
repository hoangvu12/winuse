// read_markdown.js - a Page's main content as Markdown.
//
// Derived from bladebro src/page/js/markdown.js (bladebro 4.8.3,
// https://github.com/dondai44423/bladebro), Copyright the bladebro authors,
// licensed under the Apache License 2.0 (copy in ./LICENSE.bladebro).
// Changes from the original: removed the site-specific branches (product
// page, Reddit post, GitHub repo) and the __SCOPE_*__ / __BUDGET__ template
// slots; wrapped as one function expression so it runs standalone; returns
// {markdown, main, truncated, total_chars} instead of a bare string.
// findMain, isAd and toMd are otherwise unchanged.
//
// Use:
//   Runtime.evaluate       "(" + src + ").call(null, {budget: 20000})"
//   Runtime.callFunctionOn functionDeclaration: src, objectId: <Element>,
//                          arguments: [{value: {budget: 20000}}]
//   With an Element as `this`, that subtree is rendered (no findMain).
function (opts) {
  opts = opts || {};
  var MAX = opts.budget || 20000;
  var SKIP = ['SCRIPT','STYLE','NOSCRIPT','SVG','TEMPLATE','META','LINK','BUTTON','INPUT','SELECT','TEXTAREA'];
  var NOISE = ['NAV','FOOTER','HEADER','ASIDE'];

  function findMain(d){var m=d.querySelector('main,[role="main"]');if(m&&(m.innerText||'').length>200)return m;var arts=d.querySelectorAll('article');if(arts&&arts.length===1&&(arts[0].innerText||'').length>200)return arts[0];if(arts&&arts.length>1){var big=[];for(var ai=0;ai<arts.length;ai++){if((arts[ai].innerText||'').length>200)big.push(arts[ai]);}if(big.length===1)return big[0];}var sels=['#content','.content','#main-content','.main-content','.post','.article','.entry-content','.post-body','.article-body','.story-body','#article-body'];for(var i=0;i<sels.length;i++){var el=d.querySelector(sels[i]);if(el&&(el.innerText||'').length>200)return el;}var best=null,bs=0;var c=d.querySelectorAll('div,section,table');for(var j=0;j<c.length;j++){var el=c[j];var t=(el.innerText||'');if(t.length<200)continue;var l=el.querySelectorAll('a');var lt=0;for(var k=0;k<l.length;k++)lt+=(l[k].innerText||'').length;var sc=t.length-lt*2;if(sc>bs){bs=sc;best=el;}}return best||d.body;}

  function isAd(n){var cl=(typeof n.className==='string'?n.className:'').toLowerCase();var id=(n.id||'').toLowerCase();if(/dfp|advert|sponsored|ad-container|ad-wrapper|ad-slot|ad-banner|ad-feedback|adbanner|adsense|adblock|ad-label|ads-label|ads-container|mol-ads|promoted|adsbygoogle|google-ad|doubleclick|adfeedback/.test(cl))return true;if(/dfp|advert|sponsored|google_ads|doubleclick/.test(id))return true;if(n.hasAttribute('data-ad')||n.hasAttribute('data-ad-slot')||n.hasAttribute('data-ad-client')||n.hasAttribute('data-google-query-id'))return true;if(n.tagName==='INS'&&/adsbygoogle/.test(cl))return true;var al=(n.getAttribute('aria-label')||'').toLowerCase();if(/advertisement/.test(al))return true;return false;}

  function toMd(root,max){var out='',len=0;function add(s){if(len+s.length>max)s=s.slice(0,max-len);out+=s;len+=s.length;}function sub(n){var cs=n.childNodes;for(var i2=0;i2<cs.length;i2++)walk(cs[i2]);var sr=n.shadowRoot;if(sr){var s2=sr.childNodes;for(var i3=0;i3<s2.length;i3++)walk(s2[i3]);}}function walk(n,isRoot){if(len>=max)return;if(n.nodeType===3){var t=n.textContent.replace(/\s+/g,' ');if(t)add(t);return;}if(n.nodeType!==1)return;var tag=n.tagName;if(SKIP.indexOf(tag.toUpperCase())>=0&&!isRoot)return;if(n.hidden)return;var st=n.ownerDocument.defaultView.getComputedStyle(n);if(st&&(st.display==='none'||st.visibility==='hidden'||st.opacity==='0'))return;if(NOISE.indexOf(tag)>=0&&!isRoot)return;if(isAd(n))return;switch(tag){case 'H1':add('\n# '+n.innerText.trim()+'\n\n');return;case 'H2':add('\n## '+n.innerText.trim()+'\n\n');return;case 'H3':add('\n### '+n.innerText.trim()+'\n\n');return;case 'H4':add('\n#### '+n.innerText.trim()+'\n\n');return;case 'H5':add('\n##### '+n.innerText.trim()+'\n\n');return;case 'H6':add('\n###### '+n.innerText.trim()+'\n\n');return;case 'P':sub(n);add('\n\n');return;case 'A':var tx=n.innerText.trim();var hr=n.href;if(tx&&hr&&hr!=='#'&&hr.indexOf('javascript:')!==0){if(tx===hr)add(tx);else add('['+tx+']('+hr+')');}else if(tx)add(tx);return;case 'LI':add('- ');sub(n);add('\n');return;case 'UL':case 'OL':sub(n);add('\n');return;case 'CODE':if(n.parentElement&&n.parentElement.tagName==='PRE')return;add('`'+n.innerText+'`');return;case 'PRE':add('\n```\n'+n.innerText.trim()+'\n```\n\n');return;case 'BLOCKQUOTE':add('\n> '+n.innerText.trim().replace(/\n/g,'\n> ')+'\n\n');return;case 'IMG':var al=n.alt||'';var sr=n.src||'';if(al)add('!['+al+']('+sr+')');return;case 'TABLE':if(n.querySelector('th,thead')){var rows=n.querySelectorAll('tr');if(rows.length>0&&rows.length<50){for(var i=0;i<rows.length;i++){var cells=rows[i].querySelectorAll('th,td');var row=[];for(var j=0;j<cells.length;j++)row.push(cells[j].innerText.trim().replace(/\|/g,'\\|'));add('| '+row.join(' | ')+' |\n');if(i===0)add('|'+row.map(function(){return'---';}).join('|')+'|\n');}add('\n\n');return;}}sub(n);add('\n');return;case 'IFRAME':try{var fd=n.contentDocument;if(fd&&fd.body)walk(fd.body);}catch(e_if){}return;case 'BR':add('\n');return;case 'HR':add('\n---\n\n');return;case 'STRONG':case 'B':add('**');sub(n);add('**');return;case 'EM':case 'I':add('*');sub(n);add('*');return;case 'TR':case 'THEAD':case 'TBODY':case 'TFOOT':sub(n);add('\n');return;case 'TD':case 'TH':sub(n);add(' ');return;default:sub(n);}}walk(root,true);return out.replace(/\n{3,}/g,'\n\n').trim();}

  function desc(el){if(!el||!el.tagName)return '';var s=el.tagName.toLowerCase();if(el.id)return s+'#'+el.id;var cl=(typeof el.className==='string'&&el.className.trim())?el.className.trim().split(/\s+/)[0]:'';return cl?s+'.'+cl:s;}

  var scoped = (typeof Element !== 'undefined') && (this instanceof Element);
  var main = scoped ? this : findMain(document);
  if (!main || !main.innerHTML) return {markdown: '', main: desc(main), truncated: false, total_chars: 0};
  var md = toMd(main, MAX);
  var fl = (main.innerText || '').length;
  var truncated = fl > MAX;
  if (truncated) md += '\n\n[truncated: ' + fl + ' chars total, showed ' + MAX + ']';
  return {markdown: md, main: desc(main), scoped: scoped, truncated: truncated, total_chars: fl};
}
