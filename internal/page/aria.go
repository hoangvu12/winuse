package page

// Custom ARIA lists on a Page (learn-from 05): what set-value needs of a
// widget a page builds itself instead of a <select>, such as Radix Select,
// Headless UI Listbox and Combobox, MUI Autocomplete or React Select.
//
//   - The widget is the Element set-value names: an element with
//     role=combobox (a button-like trigger, or a text field that filters as
//     it is typed into), a button with aria-haspopup=listbox, or a
//     role=listbox itself (choiceJS gives it Kind "aria").
//   - Its list is found through the widget's own links, in this order:
//     itself when it is a listbox, aria-controls and aria-owns (a listbox
//     or something holding one), the listbox of its aria-activedescendant,
//     a listbox inside it. When none is linked, a listbox that appeared
//     since the read before the Action (ReadARIAList "before") is taken
//     when it is the only one; several are an error, never a guess.
//   - Its options are the shown role=option elements of the list (and of
//     the list's aria-owns), named by aria-labelledby, else aria-label,
//     else their text: about what the accessibility tree names them.
//   - PickARIAOption matches by name only: exactly (spaces collapsed),
//     else ignoring case. Several matches at one rule are an error.
//
// The JS is winuse's own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ARIAOption is one shown option of a custom list.
type ARIAOption struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"` // aria-selected or aria-checked true, or a selected data-state
	Disabled bool   `json:"disabled"`
}

// ARIAList is a custom list as its widget shows it now.
type ARIAList struct {
	// State is "ok" (a list is shown), "none" (no list is shown) or
	// "several" (no list is linked to the widget, and several appeared).
	State    string       `json:"state"`
	Lists    int          `json:"lists"`
	Multi    bool         `json:"multi"` // aria-multiselectable
	Expanded bool         `json:"expanded"`
	Options  []ARIAOption `json:"options"`
	More     int          `json:"more"` // options left out after the first 500
	// Value is what the widget shows as its value: a text field's value,
	// or a trigger's text without its list. "" for a listbox itself.
	Value string `json:"value"`
	// Around is the text next to a text field, up to the nearest ancestor
	// holding another control (React Select shows the choice beside its
	// field), without the list.
	Around string `json:"around"`
}

// ariaFindJS finds the widget's lists and their options (see the file
// comment). It expects mode; it may return {state: 'gone'} or
// sets several (the count of unlinked lists that appeared, when more than
// one did; lists stays empty then), and leaves el, lists, several,
// options, roleOf, clean and nameOf for the code after it.
const ariaFindJS = `
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {state: 'gone'};
  const doc = el.ownerDocument, root = el.getRootNode(), win = doc.defaultView;
  const roleOf = n => ((n && n.getAttribute && n.getAttribute('role')) || '').toLowerCase();
  const byId = id => (root.getElementById && root.getElementById(id)) || doc.getElementById(id);
  const refs = (n, a) => (n.getAttribute(a) || '').split(/\s+/).filter(Boolean).map(byId).filter(Boolean);
  const shown = n => {
    if (!n || !n.isConnected || !n.getClientRects().length) return false;
    const cs = getComputedStyle(n);
    return cs.visibility !== 'hidden' && cs.display !== 'none';
  };
  const listboxes = () => {
    const a = Array.from(doc.querySelectorAll('[role=listbox]'));
    if (root !== doc && root.querySelectorAll) a.push(...root.querySelectorAll('[role=listbox]'));
    return a;
  };
  const key = Symbol.for('winuse.arialists');
  const self = roleOf(el) === 'listbox';
  let lists = [], several = 0;
  const add = l => { if (l && !lists.includes(l)) lists.push(l); };
  if (self) add(el);
  else {
    for (const t of refs(el, 'aria-controls').concat(refs(el, 'aria-owns'))) {
      if (roleOf(t) === 'listbox') { add(t); continue; }
      const inner = t.querySelectorAll('[role=listbox]');
      if (inner.length) inner.forEach(add);
      else if (t.querySelector('[role=option]')) add(t);
    }
    for (const a of refs(el, 'aria-activedescendant')) add(a.closest('[role=listbox]'));
    el.querySelectorAll('[role=listbox]').forEach(add);
  }
  lists = lists.filter(shown);
  if (mode === 'before') {
    // The lists shown before the Action; an open widget's own list counts
    // as new, so a list it shows unlinked is still found.
    win[key] = el.getAttribute('aria-expanded') === 'true' ? null : new WeakSet(listboxes().filter(shown));
  } else if (!lists.length && !self) {
    const seen = win[key];
    const fresh = listboxes().filter(l => shown(l) && !(seen && seen.has(l)));
    // Several new lists name no option, but the widget's own value is still
    // read (ariaListJS): a list it closed leaves the page's others behind.
    if (fresh.length > 1) several = fresh.length;
    else if (fresh.length === 1) add(fresh[0]);
  }
  const options = [], had = new Set();
  for (const l of lists) {
    const cands = Array.from(l.querySelectorAll('[role=option]'));
    for (const o of refs(l, 'aria-owns')) {
      if (roleOf(o) === 'option') cands.push(o); else cands.push(...o.querySelectorAll('[role=option]'));
    }
    for (const o of cands) if (!had.has(o) && shown(o)) { had.add(o); options.push(o); }
  }
  const clean = s => String(s || '').replace(/\s+/g, ' ').trim();
  const nameOf = n => {
    let t = refs(n, 'aria-labelledby').map(e => e.innerText || e.textContent || '').join(' ');
    if (!clean(t)) t = n.getAttribute('aria-label') || '';
    if (!clean(t)) t = n.innerText || n.textContent || '';
    return clean(t);
  };
  const selectedOf = n => n.getAttribute('aria-selected') === 'true' || n.getAttribute('aria-checked') === 'true' ||
    n.getAttribute('data-state') === 'checked' || (n.hasAttribute('data-selected') && n.getAttribute('data-selected') !== 'false');
`

// ariaListJS reads the widget's list (ARIAList). mode "before" also
// records the lists shown now, for the fallback after the Action.
const ariaListJS = `function(mode) {
  /* winuse:arialist */` + ariaFindJS + `
  const textOf = n => {
    let s = '';
    const walk = x => {
      for (const c of x.childNodes) {
        if (c.nodeType === 3) s += c.data + ' ';
        else if (c.nodeType === 1 && !lists.includes(c) && roleOf(c) !== 'listbox' && roleOf(c) !== 'option' &&
          !/^(SCRIPT|STYLE|SELECT|TEMPLATE)$/.test(c.tagName) && getComputedStyle(c).display !== 'none') walk(c);
      }
    };
    walk(n);
    return clean(s);
  };
  const field = el.tagName === 'INPUT' || el.tagName === 'TEXTAREA';
  let value = '', around = '';
  if (!self) {
    value = field ? clean(el.value) : textOf(el);
    if (field || el.isContentEditable) {
      let a = el;
      for (let i = 0; i < 4 && a.parentElement && a.parentElement !== doc.body; i++) {
        const p = a.parentElement;
        if (p.querySelectorAll('input:not([type=hidden]),select,textarea,button,[role=combobox]').length > 1) break;
        a = p;
      }
      if (a !== el) around = textOf(a);
    }
  }
  const opts = options.slice(0, 500).map(o => ({name: nameOf(o), selected: selectedOf(o),
    disabled: o.getAttribute('aria-disabled') === 'true' || !!(o.matches && o.matches(':disabled'))}));
  if (several) return {state: 'several', lists: several, options: [], expanded: el.getAttribute('aria-expanded') === 'true', value, around};
  return {state: lists.length ? 'ok' : 'none', lists: lists.length, multi: lists.some(l => l.getAttribute('aria-multiselectable') === 'true'),
    expanded: el.getAttribute('aria-expanded') === 'true', options: opts, more: Math.max(0, options.length - 500), value, around};
}`

// ariaOptionJS returns option i of the widget's list as a node, when it
// still has the name read before; null otherwise.
const ariaOptionJS = `function(i, name) {
  /* winuse:ariaoption */
  const mode = 'after';` + ariaFindJS + `
  const o = several ? null : options[i];
  return o && nameOf(o) === name ? o : null;
}`

// ariaOptionStateJS reads an option node's own state after the click.
const ariaOptionStateJS = `function() {
  /* winuse:ariaoptstate */
  const n = this;
  if (!n.isConnected) return {name: '', selected: false, disabled: false, gone: true};
  const selected = n.getAttribute('aria-selected') === 'true' || n.getAttribute('aria-checked') === 'true' ||
    n.getAttribute('data-state') === 'checked' || (n.hasAttribute('data-selected') && n.getAttribute('data-selected') !== 'false');
  return {name: '', selected, disabled: n.getAttribute('aria-disabled') === 'true', gone: false};
}`

// ReadARIAList reads the widget's list as it is now. mode is "before"
// (the read before the Action, which records the lists shown now) or
// "after".
func (o Origin) ReadARIAList(ctx context.Context, mode string) (ARIAList, error) {
	c, err := o.caller()
	if err != nil {
		return ARIAList{}, err
	}
	var l ARIAList
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, ariaListJS, []any{mode}, &l); err != nil {
		return ARIAList{}, staleOr(err)
	}
	if l.State == "gone" {
		return ARIAList{}, fmt.Errorf("%w: the node left the document", ErrStale)
	}
	for i := range l.Options {
		l.Options[i].Name = Collapse(l.Options[i].Name)
	}
	return l, nil
}

// Names is the list's option names, for an error.
func (l ARIAList) Names() []string {
	names := make([]string, len(l.Options))
	for i, o := range l.Options {
		names[i] = o.Name
	}
	return names
}

// PickARIAOption finds the one option a name names: its name exactly
// (spaces collapsed on both sides), else the same name ignoring case. No
// prefix or part of a name ever matches. Several matches at one rule are
// an error, never a guess, and so is no match (the error lists the
// options).
func PickARIAOption(opts []ARIAOption, want string) (int, error) {
	w := Collapse(want)
	rules := []func(string) bool{
		func(n string) bool { return n == w },
		func(n string) bool { return strings.EqualFold(n, w) },
	}
	for _, match := range rules {
		var hits []int
		for i, o := range opts {
			if match(o.Name) {
				hits = append(hits, i)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		}
		var labels []string
		for _, i := range hits {
			labels = append(labels, fmt.Sprintf("%s (option %d of %d)", opts[i].Name, i+1, len(opts)))
		}
		return -1, &OptionError{Want: want, Several: true, Labels: labels}
	}
	names := make([]string, len(opts))
	for i, o := range opts {
		names[i] = o.Name
	}
	return -1, &OptionError{Want: want, Labels: names}
}

// NoListError says the widget shows no list of options set-value can
// find, or several unlinked ones.
type NoListError struct{ Several int }

func (e *NoListError) Error() string {
	if e.Several > 1 {
		return fmt.Sprintf("%d lists of options (role=listbox) are shown and none is linked to it (aria-controls), so winuse cannot tell which is its", e.Several)
	}
	return "it shows no list of options (role=listbox with role=option items) that winuse can find"
}

// ARIA waits. Package variables so tests can shorten them.
var (
	// ARIAListWait bounds the wait for the option named to show in the
	// widget's list after it was opened or typed into: a list that loads
	// or filters as it is typed into takes a moment.
	ARIAListWait = 2 * time.Second
	// ARIANoListWait is how long a text field that was typed into may show
	// no list at all before it counts as a plain field: free text, with
	// suggestions it did not offer.
	ARIANoListWait = 600 * time.Millisecond
	// ARIAVerifyWait bounds the read-back after the option's click: a
	// framework updates the widget a moment later.
	ARIAVerifyWait = time.Second
	ariaPoll       = 50 * time.Millisecond
)

// WaitARIAOption reads the widget's list until it shows the option want
// names, for up to ARIAListWait. It returns the list and the option's
// index, or the last list read and:
//
//   - a *OptionError at once when several options match;
//   - a *OptionError when the list shows no such option by the end;
//   - a *NoListError when no list (or several unlinked ones) showed by the
//     end, or, with plain set, when none showed within ARIANoListWait:
//     then the error is nil and the list's State is "none" (a text field
//     with no suggestions).
func (o Origin) WaitARIAOption(ctx context.Context, want string, plain bool) (ARIAList, int, error) {
	start := time.Now()
	for {
		l, err := o.ReadARIAList(ctx, "after")
		if err != nil {
			return l, -1, err
		}
		var last error
		switch l.State {
		case "ok":
			i, perr := PickARIAOption(l.Options, want)
			if perr == nil {
				return l, i, nil
			}
			if oe, ok := perr.(*OptionError); ok && oe.Several {
				return l, -1, perr
			}
			last = perr
		case "several":
			last = &NoListError{Several: l.Lists}
		default:
			if plain && time.Since(start) >= ARIANoListWait {
				return l, -1, nil
			}
			last = &NoListError{}
		}
		if time.Since(start) >= ARIAListWait {
			return l, -1, last
		}
		select {
		case <-ctx.Done():
			return l, -1, ctx.Err()
		case <-time.After(ariaPoll):
		}
	}
}

// ARIAOptionOrigin is option i of the widget's list (read by
// WaitARIAOption) as an Origin of its own, for the click: the same session
// and frame as the widget. It is stale when the list changed so that
// option i no longer has that name.
func (o Origin) ARIAOptionOrigin(ctx context.Context, i int, name string) (Origin, error) {
	c, err := o.caller()
	if err != nil {
		return Origin{}, err
	}
	node, err := nodeFrom(ctx, c, o.SessionID, o.BackendNodeID, ariaOptionJS, []any{i, name})
	if err != nil {
		return Origin{}, staleOr(err)
	}
	if node == 0 {
		return Origin{}, fmt.Errorf("%w: the list changed before its option %q could be clicked", ErrStale, name)
	}
	opt := o
	opt.BackendNodeID = node
	return opt, nil
}

// ARIAOptionState reads an option node's state; gone when it left the
// document (a list that closes after the choice takes its options along).
func (o Origin) ARIAOptionState(ctx context.Context) (opt ARIAOption, gone bool, err error) {
	c, err := o.caller()
	if err != nil {
		return ARIAOption{}, true, err
	}
	var st struct {
		ARIAOption
		Gone bool `json:"gone"`
	}
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, ariaOptionStateJS, nil, &st); err != nil {
		if err := staleOr(err); errors.Is(err, ErrStale) {
			return ARIAOption{}, true, nil
		}
		return ARIAOption{}, true, err
	}
	return st.ARIAOption, st.Gone, nil
}

// ARIAChosen decides from the reads before and after the option's click
// whether the widget shows the option chosen:
//
//   - the option itself reads selected afterwards; or
//   - a trigger's shown text holds the option's name, and did not already
//     hold it while the option was not selected (a placeholder that holds
//     the name proves nothing); or
//   - a text field's list closed (none shown) and the field, or the text
//     beside it, holds the name.
//
// Names compare with spaces collapsed, ignoring case.
func ARIAChosen(editable bool, before, after ARIAList, optBefore, optAfter ARIAOption, name string) bool {
	if optAfter.Selected {
		return true
	}
	has := func(s string) bool {
		return name != "" && strings.Contains(strings.ToLower(Collapse(s)), strings.ToLower(Collapse(name)))
	}
	if editable {
		return after.State == "none" && (has(after.Value) || has(after.Around))
	}
	return has(after.Value) && (!has(before.Value) || optBefore.Selected)
}

// WaitARIAChosen reads the widget and the option after the click until
// ARIAChosen holds, for up to ARIAVerifyWait.
func (o Origin) WaitARIAChosen(ctx context.Context, opt Origin, editable bool, before ARIAList, optBefore ARIAOption, name string) bool {
	start := time.Now()
	for {
		after, err := o.ReadARIAList(ctx, "after")
		if err == nil {
			st, _, _ := opt.ARIAOptionState(ctx)
			if ARIAChosen(editable, before, after, optBefore, st, name) {
				return true
			}
		}
		if time.Since(start) >= ARIAVerifyWait {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(ariaPoll):
		}
	}
}

// nodeFrom calls fn on a node and returns the backendNodeId of the DOM
// node fn returns, 0 when it returns anything else (null). Both objects
// are released.
func nodeFrom(ctx context.Context, c Caller, session string, backendNodeID int64, fn string, args []any) (int64, error) {
	res, err := c.CallSession(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID, "objectGroup": objectGroup})
	if err != nil {
		return 0, err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if json.Unmarshal(res, &rn) != nil || rn.Object.ObjectID == "" {
		return 0, fmt.Errorf("%w: the node no longer resolves", ErrStale)
	}
	release := func(id string) {
		c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObject", map[string]any{"objectId": id})
	}
	defer release(rn.Object.ObjectID)
	callArgs := make([]map[string]any, len(args))
	for i, a := range args {
		callArgs[i] = map[string]any{"value": a}
	}
	res, err = c.CallSession(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"objectId": rn.Object.ObjectID, "functionDeclaration": fn, "arguments": callArgs, "silent": true, "objectGroup": objectGroup,
	})
	if err != nil {
		return 0, err
	}
	var cr struct {
		Result struct {
			Subtype  string `json:"subtype"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &cr); err != nil {
		return 0, err
	}
	if cr.ExceptionDetails != nil {
		return 0, fmt.Errorf("reading the node failed: %s", cr.ExceptionDetails.Text)
	}
	if cr.Result.ObjectID == "" {
		return 0, nil
	}
	defer release(cr.Result.ObjectID)
	if cr.Result.Subtype != "node" {
		return 0, nil
	}
	res, err = c.CallSession(ctx, session, "DOM.describeNode", map[string]any{"objectId": cr.Result.ObjectID})
	if err != nil {
		return 0, err
	}
	var dn struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	if err := json.Unmarshal(res, &dn); err != nil {
		return 0, err
	}
	return dn.Node.BackendNodeID, nil
}
