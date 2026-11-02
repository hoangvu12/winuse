// winuse's cursor-pointer pass (browser merge ticket 04). It runs in a
// document and returns a flat array the helper reads with
// Runtime.getProperties: [element, kind, text, element, kind, text, ...],
// where kind is "p" for a password input and "c" for an element that takes
// clicks but has no interactive role (cursor:pointer, onclick, tabindex),
// which the accessibility tree would otherwise leave out.
(() => {
  const out = [];
  for (const el of document.querySelectorAll("input[type=password]")) {
    out.push(el, "p", "");
  }
  const body = document.body;
  if (!body) return out;
  const nativeTags = new Set(["A", "BUTTON", "INPUT", "SELECT", "TEXTAREA", "SUMMARY", "OPTION", "LABEL", "DETAILS", "IFRAME", "VIDEO", "AUDIO"]);
  const widgetRoles = new Set(["button", "link", "checkbox", "radio", "switch", "tab", "menuitem", "menuitemcheckbox", "menuitemradio", "option", "combobox", "textbox", "searchbox", "slider", "spinbutton", "treeitem", "gridcell", "listbox"]);
  const insideWidget = "a[href],button,input,select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=menuitem],[role=option],[role=tab]";
  const pointerBody = getComputedStyle(body).cursor === "pointer";
  const all = body.querySelectorAll("*");
  let found = 0;
  for (let i = 0; i < all.length && i < 5000 && found < 200; i++) {
    const el = all[i];
    if (nativeTags.has(el.tagName) || el.isContentEditable) continue;
    const role = (el.getAttribute("role") || "").trim().toLowerCase();
    if (widgetRoles.has(role)) continue;
    const tabindex = el.getAttribute("tabindex");
    const style = getComputedStyle(el);
    const parent = el.parentElement;
    const pointer = !pointerBody && style.cursor === "pointer" && !(parent && getComputedStyle(parent).cursor === "pointer");
    if (!pointer && !el.hasAttribute("onclick") && !(tabindex !== null && tabindex !== "-1")) continue;
    if (style.visibility === "hidden" || style.display === "none") continue;
    const box = el.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) continue;
    if (parent && parent.closest(insideWidget)) continue;
    const text = (el.innerText || el.getAttribute("aria-label") || el.getAttribute("title") || "").replace(/\s+/g, " ").trim().slice(0, 80);
    out.push(el, "c", text);
    found++;
  }
  return out;
})()
