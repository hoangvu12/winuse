// winuse extension: serves winuse a browser-level CDP endpoint backed by
// chrome.debugger, so winuse can drive the pages of the user's real,
// running browser (logins intact) with no --remote-debugging-port, no
// profile copy and no "Allow remote debugging?" prompt.
//
//   winuse helper --(loopback TCP, token)--> native host (winuse.exe,
//   started by the browser) --(native messaging)--> this service worker
//   --> chrome.debugger / chrome.tabs
//
// Ported from use-browser v0.9.0 (extension/background.js; see
// third_party/use-browser/NOTICE). The Go side mirrors the routing rules
// below in internal/bridge (Refusal) and its tests (bridgetest).
//
// Routing:
//   - A message without sessionId is browser-level. Only the Target.*
//     methods in EMULATED exist here, emulated on chrome.tabs; every other
//     browser-level method is refused with CODE_REFUSED.
//   - A tab session's id is its target id. A child session (an
//     out-of-process iframe auto-attached with flatten:true) keeps the id
//     Chrome gave it and is sent to chrome.debugger as {targetId, sessionId}.
//   - Events go out with the sessionId they belong to: the tab's target id,
//     or the child's own session id.
//
// Never take the foreground: agent tabs open in the background, inside
// their Agent's tab group ("winuse" for the default Agent); Page.bringToFront and Target.activateTarget are
// refused; Winuse.activateTab switches a tab inside its window and never
// focuses or raises the window; Winuse.navigateTab only changes a tab's
// URL. chrome.windows is read (getAll, for Winuse.windows) and updated
// only to minimize a popup window a held tab opened.
//
// Background tabs (ADR 0017): Winuse.holdTab keeps a tab an agent works in
// from being discarded (autoDiscardable:false, restored by
// Winuse.releaseTab or when the debugger detaches). A tab a held tab opens
// (a target=_blank link, window.open) never stays the selected one: the
// user's tab is selected again, the new tab joins its opener's group, and
// a popup window it opened in is minimized.
//
// The user stays in charge: the browser shows its "started debugging" bar
// while a tab is attached, and Cancel there, or a click on the toolbar
// icon, turns winuse off until the icon is clicked again.

const HOST = "com.hoangvu12.winuse";
const IDLE_DETACH_MS = 5 * 60 * 1000;
const QUICK_RECONNECT_AFTER_MS = 5000;
const GROUP_TITLE = "winuse";

// Error codes the Go side recognizes (internal/bridge/bridge.go).
const CODE_FAILED = -32000;
const CODE_REFUSED = -32010;
const CODE_PAUSED = -32011;
const CODE_FOREGROUND = -32013;

const EMULATED = new Set([
  "Target.getTargets",
  "Target.attachToTarget",
  "Target.detachFromTarget",
  "Target.createTarget",
  "Target.closeTarget",
]);
const FOREGROUND = new Set(["Page.bringToFront", "Target.activateTarget"]);
const BROWSER_DOMAINS = new Set(["Browser", "SystemInfo", "Tethering"]);

let port = null;
let paused = false;
let lastError = "";
let instance = "";
const attached = new Map(); // tab target id -> idle timer
const children = new Map(); // child session id -> its tab's target id
const conns = new Map(); // winuse connection id -> Set of tab target ids it attached
const partial = new Map(); // winuse connection id -> a chunked command so far
const held = new Map(); // tab id -> {targetId, discardable}: the tabs agents work in (Winuse.holdTab)
const spawned = new Map(); // tab id -> when a held tab opened it
const selected = new Map(); // window id -> [the tab selected before, the tab selected now]

// SPAWN_GUARD_MS is how long after a held tab opened a tab that tab is
// kept from becoming the selected one: long enough for the browser's own
// selection, short enough that the user can click it afterwards.
const SPAWN_GUARD_MS = 2000;

// ACTING_MS is how long after winuse held a tab for a command a tab
// created in its window with no opener (a target=_blank link is noopener,
// and the browser may leave openerTabId out) is taken for that tab's.
const ACTING_MS = 10000;
const acting = new Map(); // window id -> {tabId, until}: a command on a held tab there is running

class BridgeError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

// ---- native host connection ----

async function connect() {
  if (port) return;
  const st = await chrome.storage.local.get(["paused", "instance"]);
  paused = !!st.paused;
  instance = st.instance;
  if (!instance) {
    instance = crypto.randomUUID().slice(0, 8);
    await chrome.storage.local.set({ instance });
  }
  if (port) return; // another event connected while we awaited
  try {
    port = chrome.runtime.connectNative(HOST);
  } catch (e) {
    lastError = String(e.message || e);
    updateBadge();
    return;
  }
  port.onMessage.addListener(onHostMessage);
  const since = Date.now();
  port.onDisconnect.addListener(() => {
    lastError = chrome.runtime.lastError ? chrome.runtime.lastError.message : "the winuse native host exited";
    port = null;
    conns.clear();
    partial.clear();
    detachAll();
    updateBadge();
    // A host that ran a while and went away (a newer winuse ended an old
    // one) is started again at once; one that fails to start waits for the
    // reconnect alarm.
    if (Date.now() - since > QUICK_RECONNECT_AFTER_MS) setTimeout(connect, 1000);
  });
  port.postMessage({
    hello: { browser: browserName(), instance, version: chrome.runtime.getManifest().version, build: loadedBuild() },
  });
  lastError = "";
  updateBadge();
}

// loadedBuild is the build winuse stamped into the manifest the browser
// loaded ("version_name": "<version> build <hash>"; internal/bridge
// WriteExtension). The browser reads the manifest only when it loads or
// reloads the extension, so this names the code that is running even after
// a newer winuse rewrote the folder (a restarted worker may even run a
// newer background.js under the old manifest). No stamp gives "", which
// winuse treats as outdated and reloads.
function loadedBuild() {
  const name = chrome.runtime.getManifest().version_name || "";
  const i = name.indexOf(" build ");
  return i < 0 ? "" : name.slice(i + " build ".length);
}

function browserName() {
  const brands = ((navigator.userAgentData && navigator.userAgentData.brands) || []).map((b) => b.brand);
  if (brands.includes("Microsoft Edge")) return "edge";
  if (brands.includes("Brave") || navigator.brave) return "brave";
  if (brands.includes("Opera")) return "opera";
  if (brands.includes("Vivaldi")) return "vivaldi";
  if (brands.includes("Google Chrome")) return "chrome";
  return "chromium";
}

function send(c, m) {
  if (port) port.postMessage({ c, m });
}

// onHostMessage handles one envelope from the host: {c, m} carries a
// command, {c, chunk, last} carries part of one too large for a single
// native message, and {c, close} says the winuse connection ended.
async function onHostMessage(env) {
  if (env.close) {
    conns.delete(env.c);
    partial.delete(env.c);
    return;
  }
  let m = env.m;
  if (typeof env.chunk === "string") {
    const text = (partial.get(env.c) || "") + env.chunk;
    if (!env.last) {
      partial.set(env.c, text);
      return;
    }
    partial.delete(env.c);
    try {
      m = JSON.parse(text);
    } catch (_) {
      return;
    }
  }
  if (!m || typeof m !== "object") return;
  let reply;
  try {
    const result = await handle(env.c, m);
    reply = { id: m.id, result: result || {} };
  } catch (e) {
    reply = { id: m.id, error: { code: e.code || CODE_FAILED, message: errorText(e) } };
  }
  if (m.sessionId) reply.sessionId = m.sessionId;
  send(env.c, reply);
}

async function handle(c, m) {
  // Status and reload work while turned off: the off switch survives a
  // reload, and winuse must be able to tell the user why nothing works.
  if (m.method === "Winuse.status") {
    return { browser: browserName(), version: chrome.runtime.getManifest().version, build: loadedBuild(), instance, paused };
  }
  if (m.method === "Winuse.reload") {
    // A new winuse rewrote this folder: reload from it.
    setTimeout(() => chrome.runtime.reload(), 100);
    return {};
  }
  if (paused) {
    throw new BridgeError(CODE_PAUSED, "winuse is turned off in this browser: the user pressed Cancel on the debugging bar or clicked the winuse icon.");
  }
  const refused = refusal(m.method, m.sessionId);
  if (refused) throw new BridgeError(refused.code, refused.message);
  if (m.method === "Winuse.activateTab") return activateTab(m.params || {});
  if (m.method === "Winuse.windows") return listWindows();
  if (m.method === "Winuse.navigateTab") return navigateTab(m.params || {});
  if (m.method === "Winuse.holdTab") return holdTab(m.params || {});
  if (m.method === "Winuse.releaseTab") return releaseTab(m.params || {});
  if (m.method === "Winuse.adoptTab") return adoptTab(m.params || {});
  return m.sessionId ? sessionCall(m) : browserCall(c, m);
}

// refusal mirrors internal/bridge's Refusal: null when the method may
// pass, else the error code and message.
function refusal(method, sessionId) {
  method = String(method || "");
  if (FOREGROUND.has(method)) {
    return { code: CODE_FOREGROUND, message: `${method} would bring the browser window to the front, and winuse never takes the foreground.` };
  }
  const domain = method.split(".")[0];
  if (domain === "Winuse" && !sessionId) return null;
  if (BROWSER_DOMAINS.has(domain) || domain === "Winuse" || (!sessionId && !EMULATED.has(method))) {
    return { code: CODE_REFUSED, message: `${method} needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone.` };
  }
  return null;
}

// chrome.debugger rejects with the CDP error object serialized as JSON.
function errorText(e) {
  const s = String((e && e.message) || e);
  try {
    const o = JSON.parse(s);
    if (o && o.message) return o.message;
  } catch (_) {}
  return s;
}

// ---- browser-level CDP, emulated ----

async function browserCall(c, m) {
  const p = m.params || {};
  switch (m.method) {
    case "Target.getTargets":
      return { targetInfos: await pageTargets() };
    case "Target.attachToTarget": {
      await ensureAttached(p.targetId);
      if (!conns.has(c)) conns.set(c, new Set());
      conns.get(c).add(p.targetId);
      return { sessionId: p.targetId };
    }
    case "Target.detachFromTarget": {
      // The tab stays attached for other connections; the idle timer
      // releases it.
      const id = p.sessionId || p.targetId;
      if (conns.has(c)) conns.get(c).delete(id);
      return {};
    }
    case "Target.createTarget":
      return { targetId: await createTab(p.url || "about:blank", p.windowId, p.winuseGroup) };
    case "Target.closeTarget": {
      const tab = await tabFor(p.targetId);
      await chrome.tabs.remove(tab.id);
      return { success: true };
    }
  }
  throw new BridgeError(CODE_REFUSED, `${m.method} is not available through the winuse extension.`);
}

// pageTargets lists real tabs in tab-strip order (window by window). The
// extra tabId, windowId and active fields let winuse map a browser window
// to its tabs; CDP clients ignore unknown fields.
async function pageTargets() {
  const [targets, tabs] = await Promise.all([chrome.debugger.getTargets(), chrome.tabs.query({})]);
  const byId = new Map(tabs.map((t) => [t.id, t]));
  return targets
    .filter((t) => t.type === "page" && t.tabId !== undefined && byId.has(t.tabId))
    .sort((a, b) => {
      const ta = byId.get(a.tabId);
      const tb = byId.get(b.tabId);
      return ta.windowId - tb.windowId || ta.index - tb.index;
    })
    .map((t) => {
      const tab = byId.get(t.tabId);
      return {
        targetId: t.id,
        type: "page",
        title: t.title,
        url: t.url,
        attached: t.attached,
        tabId: tab.id,
        windowId: tab.windowId,
        active: tab.active,
      };
    });
}

async function tabFor(targetId) {
  const t = (await chrome.debugger.getTargets()).find((x) => x.id === targetId);
  if (!t || t.tabId === undefined) throw new Error(`no tab with target id ${targetId}`);
  return chrome.tabs.get(t.tabId);
}

// listWindows reports every browser window's bounds (DIPs) and state, so
// winuse can tell which window a desktop Window is: the extension cannot
// use Browser.getWindowForTarget. It only reads chrome.windows.
async function listWindows() {
  const wins = await chrome.windows.getAll({ windowTypes: ["normal", "popup", "app"] });
  return {
    windows: wins.map((w) => ({ windowId: w.id, left: w.left, top: w.top, width: w.width, height: w.height, state: w.state })),
  };
}

// activateTab makes a tab the selected one in its window. It never focuses
// or raises the window: the windows API is never called.
async function activateTab(p) {
  const tab = await tabFor(p.targetId);
  await chrome.tabs.update(tab.id, { active: true });
  return {};
}

// navigateTab opens a URL in a tab. winuse uses it on a tab that shows a
// browser page (chrome://newtab/), where chrome.debugger cannot attach, so
// Page.navigate cannot reach it. Only the url changes: the selected tab,
// the window's focus and its place in the z-order stay as they are.
async function navigateTab(p) {
  if (typeof p.url !== "string" || p.url === "") throw new Error("url is required");
  const tab = await tabFor(p.targetId);
  await chrome.tabs.update(tab.id, { url: p.url });
  return {};
}

// holdTab marks a tab an agent works in while the user looks at another
// one (ADR 0017): the browser must not discard it, and the tabs it opens
// are guarded. The tab's own autoDiscardable is remembered for the release.
async function holdTab(p) {
  const tab = await tabFor(p.targetId);
  if (!held.has(tab.id)) held.set(tab.id, { targetId: p.targetId, discardable: tab.autoDiscardable !== false });
  await chrome.tabs.update(tab.id, { autoDiscardable: false });
  acting.set(tab.windowId, { tabId: tab.id, until: Date.now() + ACTING_MS });
  // The service worker may have started after the user's last switch:
  // note the tab selected now, the one a guarded new tab gives way to.
  const [now] = await chrome.tabs.query({ active: true, windowId: tab.windowId });
  const s = selected.get(tab.windowId) || [];
  if (now && s[1] !== now.id) selected.set(tab.windowId, [s[1], now.id]);
  return {};
}

async function releaseTab(p) {
  const tab = await tabFor(p.targetId);
  unhold(tab.id);
  return {};
}

// unhold gives a held tab its own autoDiscardable back.
function unhold(tabId) {
  const h = held.get(tabId);
  if (!h) return;
  held.delete(tabId);
  chrome.tabs.update(tabId, { autoDiscardable: h.discardable }).catch(() => {});
}

// unholdTarget releases the held tab of a target id (its debugger
// detached).
function unholdTarget(targetId) {
  for (const [tabId, h] of [...held]) {
    if (h.targetId === targetId) unhold(tabId);
  }
}

// A held tab opened a tab: guard the user's selection, group the new tab
// with its opener, and minimize a popup window it opened in.
chrome.tabs.onCreated.addListener(async (tab) => {
  const openerId = openerOf(tab);
  if (openerId === undefined) return;
  // A tab opened from the tab the user is looking at is theirs to follow,
  // even when an agent held that tab once (the new tab may already be the
  // selected one here).
  const s = selected.get(tab.windowId) || [];
  if (s[1] === openerId || (s[1] === tab.id && s[0] === openerId)) return;
  spawned.set(tab.id, Date.now());
  if (tab.active) keepUsersTab(tab.windowId, tab.id);
  await adopt(tab, openerId);
});

// openerOf is the held tab a new tab came from: its opener when that is
// held, else, for a tab with no opener and a site's URL, the held tab a
// command is running on in its window. A tab the user opens (Ctrl+T shows
// a browser page) or one opened from a tab no agent holds is nobody's.
function openerOf(tab) {
  if (tab.openerTabId !== undefined) return held.has(tab.openerTabId) ? tab.openerTabId : undefined;
  const a = acting.get(tab.windowId);
  const url = tab.pendingUrl || tab.url || "";
  if (!a || Date.now() > a.until || !held.has(a.tabId) || !/^(https?|file|data|blob):/i.test(url)) return undefined;
  return a.tabId;
}

// adopt puts a tab a held tab opened in its opener's group, or minimizes
// the popup window it opened in. Best effort: the Action's note names it.
async function adopt(tab, openerId) {
  try {
    const opener = await chrome.tabs.get(openerId);
    if (opener.windowId === tab.windowId && opener.groupId >= 0) {
      await chrome.tabs.group({ tabIds: [tab.id], groupId: opener.groupId });
      return;
    }
    const w = await chrome.windows.get(tab.windowId);
    if (w.type === "popup") await chrome.windows.update(tab.windowId, { state: "minimized" });
  } catch (_) {}
}

// adoptTab is Winuse.adoptTab: winuse found a tab an Action on a held tab
// opened that the guard missed, and selected the user's tab again; the
// new tab joins its opener's group.
async function adoptTab(p) {
  const tab = await tabFor(p.targetId);
  const opener = await tabFor(p.openerId);
  await adopt(tab, opener.id);
  return {};
}

chrome.tabs.onActivated.addListener(({ tabId, windowId }) => {
  const at = spawned.get(tabId);
  if (at !== undefined && Date.now() - at < SPAWN_GUARD_MS) {
    keepUsersTab(windowId, tabId);
    return;
  }
  const s = selected.get(windowId) || [];
  selected.set(windowId, [s[1], tabId]);
});

chrome.tabs.onRemoved.addListener((tabId, info) => {
  spawned.delete(tabId);
  held.delete(tabId);
  if (info && info.isWindowClosing) selected.delete(info.windowId);
});

// keepUsersTab selects again the tab the user had selected in a window
// before a held tab's new tab took its place. Only the selection inside
// the window changes; the window is never focused.
function keepUsersTab(windowId, tabId) {
  const s = selected.get(windowId) || [];
  const back = s[1] === tabId ? s[0] : s[1];
  if (back === undefined || back === tabId) return;
  chrome.tabs.update(back, { active: true }).catch(() => {});
}

// The colours chrome.tabGroups accepts.
const GROUP_COLORS = ["grey", "blue", "red", "yellow", "green", "pink", "purple", "cyan", "orange"];

// tabGroupFor is the group an agent tab joins: the one winuse names in
// winuseGroup (its Agent's name and the colour nearest its Agent Cursor's,
// browser-ux 10), else the default blue "winuse" group.
function tabGroupFor(g) {
  const title = g && typeof g.title === "string" && g.title.trim() ? g.title.trim().slice(0, 40) : GROUP_TITLE;
  const color = g && GROUP_COLORS.includes(g.color) ? g.color : "blue";
  return { title, color };
}

// createTab opens agent tabs in the background, in their Agent's tab
// group, so an agent never takes over the tab the user is looking at and
// each Agent's tabs stay together. A windowId (winuse tab new) opens it in
// that window; without one the browser picks its last focused window.
// Neither focuses the window.
async function createTab(url, windowId, winuseGroup) {
  const props = { url, active: false };
  if (Number.isInteger(windowId)) props.windowId = windowId;
  const tab = await chrome.tabs.create(props);
  try {
    const want = tabGroupFor(winuseGroup);
    const [group] = await chrome.tabGroups.query({ title: want.title, color: want.color, windowId: tab.windowId });
    if (group) {
      await chrome.tabs.group({ tabIds: [tab.id], groupId: group.id });
    } else {
      const groupId = await chrome.tabs.group({ tabIds: [tab.id], createProperties: { windowId: tab.windowId } });
      await chrome.tabGroups.update(groupId, want);
    }
  } catch (_) {
    // grouping is cosmetic; a browser without tab groups still gets the tab
  }
  // The target id appears a moment after the tab does.
  for (let i = 0; i < 50; i++) {
    const t = (await chrome.debugger.getTargets()).find((x) => x.tabId === tab.id);
    if (t) return t.id;
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error("tab created but its target id never appeared");
}

// ---- page and child sessions ----

async function sessionCall(m) {
  const parent = children.get(m.sessionId);
  if (parent) {
    // A child session lives on its tab's debugger connection (Chrome 125+).
    if (!attached.has(parent)) throw new Error(`the frame session ${m.sessionId} was detached`);
    touch(parent);
    return chrome.debugger.sendCommand({ targetId: parent, sessionId: m.sessionId }, m.method, m.params || {});
  }
  // winuse may outlive an idle detach, so attach again transparently.
  await ensureAttached(m.sessionId);
  return chrome.debugger.sendCommand({ targetId: m.sessionId }, m.method, m.params || {});
}

async function ensureAttached(targetId) {
  if (!targetId) throw new Error("targetId is required");
  if (!attached.has(targetId)) {
    try {
      await chrome.debugger.attach({ targetId }, "1.3");
    } catch (e) {
      // Already attached by us (a previous worker life) is fine.
      if (!/already attached/i.test(errorText(e))) throw e;
    }
    attached.set(targetId, null);
    updateBadge();
  }
  touch(targetId);
}

// touch restarts the idle timer: a tab nobody has driven for a while is
// released, which also takes the "started debugging" bar away.
function touch(targetId) {
  clearTimeout(attached.get(targetId));
  attached.set(targetId, setTimeout(() => detach(targetId), IDLE_DETACH_MS));
}

function detach(targetId) {
  clearTimeout(attached.get(targetId));
  attached.delete(targetId);
  unholdTarget(targetId);
  dropChildren(targetId);
  chrome.debugger.detach({ targetId }).catch(() => {});
  updateBadge();
}

function detachAll() {
  for (const id of [...attached.keys()]) detach(id);
}

// dropChildren forgets a tab's child sessions and tells every connection
// on that tab, so their pending calls fail at once.
function dropChildren(tabId) {
  for (const [sid, parent] of [...children]) {
    if (parent !== tabId) continue;
    children.delete(sid);
    forward(tabId, { method: "Target.detachedFromTarget", params: { sessionId: sid }, sessionId: tabId });
  }
}

function forward(tabId, msg) {
  for (const [c, targets] of conns) {
    if (targets.has(tabId)) send(c, msg);
  }
}

chrome.debugger.onEvent.addListener((source, method, params) => {
  const tabId = source.targetId;
  if (!tabId) return;
  // A child's events carry the child's session id; the tab's own carry
  // the tab's target id (which is its session id here).
  const sessionId = source.sessionId || tabId;
  if (method === "Target.attachedToTarget" && params && params.sessionId) {
    children.set(params.sessionId, tabId);
  } else if (method === "Target.detachedFromTarget" && params && params.sessionId) {
    children.delete(params.sessionId);
  }
  forward(tabId, { method, params, sessionId });
});

chrome.debugger.onDetach.addListener((source, reason) => {
  const id = source.targetId;
  if (id) {
    clearTimeout(attached.get(id));
    attached.delete(id);
    unholdTarget(id);
    dropChildren(id);
    forward(id, { method: "Target.detachedFromTarget", params: { sessionId: id, targetId: id } });
  }
  // Cancel on the "started debugging this browser" bar is the user saying
  // stop. Honour it for every tab, not just this one.
  if (reason === "canceled_by_user") setPaused(true);
  updateBadge();
});

// ---- on/off switch and badge ----

async function setPaused(on) {
  paused = on;
  await chrome.storage.local.set({ paused: on });
  if (on) detachAll();
  updateBadge();
}

chrome.action.onClicked.addListener(() => setPaused(!paused));

function updateBadge() {
  let text = "";
  let color = "#5f6368";
  let title;
  if (paused) {
    text = "OFF";
    title = "winuse is off. Click to let winuse drive this browser.";
  } else if (!port) {
    text = "!";
    color = "#d93025";
    title = `winuse cannot reach its native host: ${lastError}\nRun: winuse browser ext install`;
  } else if (attached.size > 0) {
    text = String(attached.size);
    color = "#1a73e8";
    title = `winuse is driving ${attached.size} tab(s). Click to turn it off.`;
  } else {
    title = "winuse is on and idle. Click to turn it off.";
  }
  chrome.action.setBadgeText({ text });
  chrome.action.setBadgeBackgroundColor({ color });
  chrome.action.setTitle({ title });
}

// ---- lifecycle ----

// The open native port keeps this worker alive (Chrome 105+). The alarm is
// the safety net that reconnects if the host dies or the worker was stopped.
chrome.runtime.onStartup.addListener(connect);
chrome.runtime.onInstalled.addListener(connect);
chrome.alarms.create("reconnect", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener(connect);
connect();
