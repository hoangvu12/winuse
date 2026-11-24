// wall.js - names the wall a short challenge or block page shows, for the
// note after navigate, back, forward and reload, and for `winuse read`.
//
// Derived from bladebro src/page/js/detect_block.js (bladebro 4.8.3,
// https://github.com/dondai44423/bladebro at d50ee9e), Copyright the
// bladebro authors, licensed under the Apache License 2.0; a copy is in
// third_party/bladebro/LICENSE.
//
// Changes from the original (winuse, 2026, .scratch/bladebro-ideas ticket
// 05):
//   - only the generic signs are kept: the Reddit branches and every
//     solving or remediation step are left out, and the kinds are named as
//     winuse prints them (Cloudflare, DataDome, PerimeterX, reCAPTCHA,
//     hCaptcha, Akamai, rate-limit), "" for none;
//   - it is one function expression over a document (the current one when
//     none is given), so read.js and extract.js embed it and the
//     navigation check calls it alone;
//   - every sign needs a small page: the Cloudflare title and challenge
//     form too, under 1500 characters of text;
//   - a few more signs: the Cloudflare titles "attention required" and
//     "checking your browser" and its older challenge elements, PerimeterX's
//     "press & hold" wording, and the reCAPTCHA wording "not a robot",
//     "verify you are human" and "unusual traffic", and hCaptcha's widget
//     (which winuse's first read.js check caught too);
//   - it never clicks, types or waits.
function (d) {
  /* winuse:wall */
  d = d || document;
  var body = d.body;
  if (!body) return '';

  // The page's text, lowercased: innerText on a page of up to 1500
  // elements; on a bigger one a walk of the text nodes (open shadow roots
  // too) that stops past 1600 characters, as only a small page counts.
  function sample() {
    var all = d.getElementsByTagName('*').length;
    if (all <= 1500) return (body.innerText || body.textContent || '');
    var parts = [], n = 0;
    var SKIP = {SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, TEMPLATE: 1};
    (function walk(node) {
      if (n > 1600 || !node) return;
      if (node.nodeType === 3) { var t = node.nodeValue || ''; if (t.trim()) { parts.push(t); n += t.length; } return; }
      if (node.nodeType !== 1 && node.nodeType !== 11) return;
      if (node.nodeType === 1 && SKIP[(node.tagName || '').toUpperCase()]) return;
      if (node.shadowRoot) walk(node.shadowRoot);
      for (var c = node.firstChild; c && n <= 1600; c = c.nextSibling) walk(c);
    })(body);
    return parts.join(' ');
  }

  var text = sample().replace(/\s+/g, ' ').trim().toLowerCase();
  var bl = text.length;
  var title = (d.title || '').toLowerCase();
  function has(sel) { try { return !!d.querySelector(sel); } catch (e) { return false; } }
  function any(s, words) { for (var i = 0; i < words.length; i++) if (s.indexOf(words[i]) >= 0) return true; return false; }

  // Cloudflare: the interstitial's title or challenge form; its challenge
  // script on a nearly empty page; an unsolved Turnstile on a nearly empty
  // page with no field to fill (a login or contact form embeds one too).
  if (bl < 1500 && (any(title, ['just a moment', 'attention required', 'checking your browser']) || has('#challenge-form,#challenge-running,#cf-challenge-running,.cf-browser-verification'))) return 'Cloudflare';
  if (bl < 800 && has('script[src*="challenge-platform"]')) return 'Cloudflare';
  if (bl < 800 && has('.cf-turnstile') && !has('input:not([type=hidden]):not([type=submit]):not([type=button]),textarea,select')) {
    var token = d.querySelector('input[name="cf-turnstile-response"]');
    if (!token || !token.value) return 'Cloudflare';
  }
  // DataDome: its captcha frame, or a frame and its name.
  if (bl < 1200 && (has('iframe[src*="captcha-delivery"]') || has('iframe') && text.indexOf('datadome') >= 0)) return 'DataDome';
  // PerimeterX: its captcha box or script, or its press-and-hold wording.
  if (bl < 1500 && (has('#px-captcha,script[src*="px-captcha"]') || any(text, ['press & hold', 'press and hold']))) return 'PerimeterX';
  // The wording that asks to prove a human (or Google's "unusual traffic"
  // page).
  var HUMAN = ['prove your humanity', 'are you human', 'not a robot', 'verify you are human', 'verify you are a human', 'unusual traffic'];
  // reCAPTCHA: the widget with that wording.
  if (bl < 1500 && has('.g-recaptcha,iframe[src*="recaptcha"]') &&
      (any(title, ['prove', 'humanity', 'robot']) || any(text, HUMAN))) return 'reCAPTCHA';
  // hCaptcha: its widget with the same wording.
  if (bl < 1500 && has('.h-captcha,iframe[src*="hcaptcha"]') &&
      (any(title, ['prove', 'humanity', 'robot']) || any(text, HUMAN))) return 'hCaptcha';
  // Akamai: access denied with its reference number.
  if (bl < 1000 && text.indexOf('access denied') >= 0 && (text.indexOf('reference') >= 0 || text.indexOf('akamai') >= 0)) return 'Akamai';
  // A rate limit: a real 429 page is tiny, so docs that mention one are not.
  if (bl < 800 && (text.indexOf('too many requests') >= 0 || text.indexOf('rate limit') >= 0)) return 'rate-limit';
  return '';
}
