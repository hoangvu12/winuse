// winuse's exception listener (.scratch/bladebro-ideas ticket 30), run in
// the Page's main world by Page.addScriptToEvaluateOnNewDocument before the
// Page's own scripts. It catches uncaught errors and unhandled rejections
// and hands each to winuse's isolated world as a CustomEvent named
// __NAME__ (random per Tab) on the document, whose detail is a JSON string.
//
// It must stay invisible to the Page: it sets no global and no on-handler,
// never wraps console or anything else, never prevents an event's default
// or stops its propagation, and calls only the built-ins it saved here, so
// a Page that patches them later never sees a call. Objects it builds have
// no prototype, so no getter a Page puts on Object.prototype runs. A stack
// is read through a saved property-descriptor read, never when the Page
// set its own Error.prepareStackTrace (reading would run it), and is cut.
(() => {
  if (window !== window.top) return; // the main frame's document only
  const name = '__NAME__';
  const apply = Reflect.apply;
  const getDesc = Object.getOwnPropertyDescriptor;
  const getProto = Object.getPrototypeOf;
  const addListener = EventTarget.prototype.addEventListener;
  const dispatch = EventTarget.prototype.dispatchEvent;
  const Custom = CustomEvent;
  const stringify = JSON.stringify;
  const toString = String;
  const slice = String.prototype.slice;
  const ErrorCtor = Error;
  const perf = performance;
  const perfNow = Performance.prototype.now;
  const origin = apply(getDesc(Performance.prototype, 'timeOrigin').get, perf, []);
  const getter = (proto, key) => getDesc(proto, key).get;
  const evMessage = getter(ErrorEvent.prototype, 'message');
  const evFile = getter(ErrorEvent.prototype, 'filename');
  const evLine = getter(ErrorEvent.prototype, 'lineno');
  const evError = getter(ErrorEvent.prototype, 'error');
  const evReason = getter(PromiseRejectionEvent.prototype, 'reason');
  const doc = document;
  const win = window;
  const TEXT = 1000, URLCAP = 500, STACK = 4000;
  // The engine's own stack accessor, if an Error's stack is one: the only
  // getter ever run on a thrown value.
  const probe = getDesc(new ErrorCtor(), 'stack');
  const stackGet = probe && probe.get;

  const cut = (s, n) => (typeof s === 'string' ? apply(slice, s, [0, n]) : '');
  const isObject = (v) => v !== null && (typeof v === 'object' || typeof v === 'function');
  // own is an own data property's value, without running a getter.
  const own = (o, key) => {
    try {
      const d = getDesc(o, key);
      return d && 'value' in d ? d.value : undefined;
    } catch (_) {
      return undefined;
    }
  };
  const stackOf = (v) => {
    if (!isObject(v)) return '';
    try {
      if (getDesc(ErrorCtor, 'prepareStackTrace')) return '';
      const d = getDesc(v, 'stack');
      if (!d) return '';
      if ('value' in d) return cut(d.value, STACK);
      return stackGet && d.get === stackGet ? cut(apply(stackGet, v, []), STACK) : '';
    } catch (_) {
      return '';
    }
  };
  const nameOf = (v) => {
    let o = v;
    for (let i = 0; i < 8 && o; i++) {
      const n = own(o, 'name');
      if (typeof n === 'string') return n;
      try {
        o = getProto(o);
      } catch (_) {
        return '';
      }
    }
    return '';
  };
  const describe = (v) => {
    if (!isObject(v)) {
      try {
        return toString(v);
      } catch (_) {
        return '';
      }
    }
    const n = nameOf(v);
    const m = own(v, 'message');
    if (typeof m === 'string') return n ? n + ': ' + m : m;
    return n || 'a value that is no Error';
  };
  const now = () => origin + apply(perfNow, perf, []);
  const send = (record) => {
    try {
      apply(dispatch, doc, [new Custom(name, { __proto__: null, detail: stringify(record) })]);
    } catch (_) {}
  };

  apply(addListener, win, ['error', (e) => {
    let message, file, line, error;
    try {
      message = apply(evMessage, e, []);
      file = apply(evFile, e, []);
      line = apply(evLine, e, []);
      error = apply(evError, e, []);
    } catch (_) {
      return; // no ErrorEvent: a resource that failed to load
    }
    send({ __proto__: null, t: now(), m: cut(message, TEXT), u: cut(file, URLCAP), l: typeof line === 'number' ? line : 0, s: stackOf(error) });
  }]);
  apply(addListener, win, ['unhandledrejection', (e) => {
    let reason;
    try {
      reason = apply(evReason, e, []);
    } catch (_) {
      return;
    }
    send({ __proto__: null, t: now(), m: cut('Uncaught (in promise) ' + describe(reason), TEXT), s: stackOf(reason) });
  }]);
  // The isolated world asks whether this document has the listener (a new
  // session's world starts empty); the answer, like this first one, marks
  // the document covered.
  apply(addListener, doc, [name + '?', () => send({ __proto__: null, hello: 1 })]);
  send({ __proto__: null, hello: 1 });
})();
