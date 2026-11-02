package page

// Choices on a Page (browser merge ticket 06): what set-value does to a
// Page Element that takes no typed text.
//
//   - A <select> list: set-value picks an option by its label or value.
//     The option is set from the page's own JS (selected, then bubbling
//     input and change events), as agent-browser does. A trusted click
//     would open the list's popup, which Chrome draws as a window of its
//     own on the user's screen; keys need the focus and a typed prefix
//     that may match another option first.
//   - A check box, a radio button or an ARIA check box, switch or radio:
//     set-value on|off clicks it with trusted input only when its state
//     differs, then reads the state back and fails when it did not change.
//     There is no JS fallback: a page that ignores the trusted click would
//     then be toggled behind its own back.
//
// Also here: what upload needs of a file input, and the guard that keeps
// Chrome's own context menu from opening on a right click.
//
// The JS is winuse's own.

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Option is one option of a <select>.
type Option struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Selected bool   `json:"selected"`
	Disabled bool   `json:"disabled"`
}

// Choice is what kind of choice a node is.
type Choice struct {
	// Kind is "select" (a <select>), "check" (a check box or switch),
	// "radio", or "" for anything else.
	Kind     string   `json:"kind"`
	Checked  bool     `json:"checked"`
	Mixed    bool     `json:"mixed"` // aria-checked="mixed" or an indeterminate box
	Disabled bool     `json:"disabled"`
	Multiple bool     `json:"multiple"`
	Options  []Option `json:"options"`
	More     int      `json:"more"` // options left out after the first 500
}

const choiceJS = `function() {
  /* winuse:choice */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {kind: 'gone'};
  const disabled = el.matches(':disabled') || el.getAttribute('aria-disabled') === 'true' || !!el.closest('[inert]');
  if (el.tagName === 'SELECT') {
    const all = Array.from(el.options);
    const options = all.slice(0, 500).map(o => ({label: (o.label || o.text || '').replace(/\s+/g, ' ').trim(), value: o.value, selected: o.selected, disabled: o.disabled}));
    return {kind: 'select', disabled, multiple: el.multiple, options, more: Math.max(0, all.length - 500)};
  }
  const t = (el.getAttribute('type') || '').toLowerCase();
  if (el.tagName === 'INPUT' && (t === 'checkbox' || t === 'radio')) {
    return {kind: t === 'radio' ? 'radio' : 'check', checked: el.checked, mixed: !!el.indeterminate, disabled};
  }
  const role = (el.getAttribute('role') || '').toLowerCase();
  if (role === 'checkbox' || role === 'switch' || role === 'menuitemcheckbox' || role === 'radio' || role === 'menuitemradio') {
    const a = (el.getAttribute('aria-checked') || '').toLowerCase();
    return {kind: role.indexOf('radio') >= 0 ? 'radio' : 'check', checked: a === 'true', mixed: a === 'mixed', disabled};
  }
  return {kind: ''};
}`

// ReadChoice reads what kind of choice the node is, and its state.
func (o Origin) ReadChoice(ctx context.Context) (Choice, error) {
	c, err := o.caller()
	if err != nil {
		return Choice{}, err
	}
	var ch Choice
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, choiceJS, nil, &ch); err != nil {
		return Choice{}, staleOr(err)
	}
	if ch.Kind == "gone" {
		return Choice{}, fmt.Errorf("%w: the node left the document", ErrStale)
	}
	return ch, nil
}

// OptionError says no option, or several, match the value asked for.
type OptionError struct {
	Want    string
	Several bool
	Labels  []string // the list's options, or the matching ones
}

func (e *OptionError) Error() string {
	shown := e.Labels
	more := ""
	if len(shown) > 12 {
		more = fmt.Sprintf(", and %d more", len(shown)-12)
		shown = shown[:12]
	}
	quoted := make([]string, len(shown))
	for i, l := range shown {
		quoted[i] = fmt.Sprintf("%q", l)
	}
	if e.Several {
		return fmt.Sprintf("several options match %q: %s%s", e.Want, strings.Join(quoted, ", "), more)
	}
	if len(quoted) == 0 {
		return fmt.Sprintf("the list has no option %q, and no options at all", e.Want)
	}
	return fmt.Sprintf("the list has no option %q; its options are %s%s", e.Want, strings.Join(quoted, ", "), more)
}

// PickOption finds the one option a value names: its label exactly, else
// its value exactly, else its label ignoring case and spaces. Several
// matches at one rule are an error, never a guess.
func PickOption(opts []Option, want string) (int, error) {
	rules := []func(Option) bool{
		func(o Option) bool { return o.Label == want },
		func(o Option) bool { return o.Value == want },
		func(o Option) bool { return strings.EqualFold(Collapse(o.Label), Collapse(want)) },
	}
	for _, match := range rules {
		var hits []int
		for i, o := range opts {
			if match(o) {
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
			labels = append(labels, fmt.Sprintf("%s (value %s)", opts[i].Label, opts[i].Value))
		}
		return -1, &OptionError{Want: want, Several: true, Labels: labels}
	}
	var labels []string
	for _, o := range opts {
		labels = append(labels, o.Label)
	}
	return -1, &OptionError{Want: want, Labels: labels}
}

// pickJS selects option i of a <select> (only it, in a multiple list) and
// fires bubbling input and change events, as a person's choice does.
const pickJS = `function(i) {
  /* winuse:pick */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (el.multiple) { for (const o of el.options) o.selected = false; }
  el.options[i].selected = true;
  el.dispatchEvent(new Event('input', {bubbles: true}));
  el.dispatchEvent(new Event('change', {bubbles: true}));
  return true;
}`

// SelectOption selects option i of a <select> and reads it back. The
// write is the page's own JS, so the caller counts it as delivered.
func (o Origin) SelectOption(ctx context.Context, i int) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, pickJS, []any{i}, nil); err != nil {
		return staleOr(err)
	}
	ch, err := o.ReadChoice(ctx)
	if err != nil {
		return &VerifyError{Msg: "the option was chosen, but the list could not be read back"}
	}
	if i >= len(ch.Options) || !ch.Options[i].Selected {
		return &VerifyError{Msg: "the option was chosen, but the list shows another one afterwards (its page may have changed it back)"}
	}
	return nil
}

// ParseOnOff reads set-value's on|off for a check box: on, true or
// checked; off, false or unchecked (UI Automation's toggle words).
func ParseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "checked":
		return true, nil
	case "off", "false", "unchecked":
		return false, nil
	}
	return false, errors.New("a check box takes on or off (also true, false, checked or unchecked), not " + fmt.Sprintf("%q", value))
}

// FileInput is what upload needs of a node.
type FileInput struct {
	File     bool `json:"file"` // an <input type=file>
	Multiple bool `json:"multiple"`
	Disabled bool `json:"disabled"`
}

const fileInputJS = `function() {
  /* winuse:file */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {gone: true};
  const file = el.tagName === 'INPUT' && (el.getAttribute('type') || '').toLowerCase() === 'file';
  return {file, multiple: file && el.multiple, disabled: file && el.disabled};
}`

// ReadFileInput reads whether the node is a file input.
func (o Origin) ReadFileInput(ctx context.Context) (FileInput, error) {
	c, err := o.caller()
	if err != nil {
		return FileInput{}, err
	}
	var f struct {
		FileInput
		Gone bool `json:"gone"`
	}
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, fileInputJS, nil, &f); err != nil {
		return FileInput{}, staleOr(err)
	}
	if f.Gone {
		return FileInput{}, fmt.Errorf("%w: the node left the document", ErrStale)
	}
	return f.FileInput, nil
}

// SetFiles gives a file input its files (DOM.setFileInputFiles), which
// also answers a chooser the input opened. The paths must be absolute.
func SetFiles(ctx context.Context, c Caller, session string, backendNodeID int64, files []string) error {
	_, err := c.CallSession(ctx, session, "DOM.setFileInputFiles", map[string]any{"files": files, "backendNodeId": backendNodeID})
	return staleOr(err)
}

// The context menu guard. A right click fires the page's contextmenu
// event, and unless the page cancels it, Chrome opens its own context
// menu: a window of its own on the user's screen. The guard listens last
// (a listener on the node's window, in the bubbling phase): it records
// whether the page cancelled the event, and cancels it when the page did
// not, so a page with a menu of its own shows it, and Chrome's menu never
// opens. The guard takes one event only (it removes itself once the click's
// event passed) and removes itself after 10 s at the latest, so a user's
// own right click is never held back, even when ContextMenuResult cannot
// reach the node (the click removed it from the document).
const ctxGuardJS = `function() {
  /* winuse:ctxguard */
  const w = (this.ownerDocument || document).defaultView, k = Symbol.for('winuse.ctxguard');
  if (w[k]) w.removeEventListener('contextmenu', w[k].h);
  const st = {fired: false, page: false};
  const h = e => { st.fired = true; st.page = e.defaultPrevented; if (!e.defaultPrevented) e.preventDefault(); w.removeEventListener('contextmenu', h); };
  const g = {h, st};
  w[k] = g;
  w.addEventListener('contextmenu', h);
  setTimeout(() => { w.removeEventListener('contextmenu', h); if (w[k] === g) delete w[k]; }, 10000);
  return true;
}`

const ctxResultJS = `function() {
  /* winuse:ctxresult */
  const w = (this.ownerDocument || document).defaultView, k = Symbol.for('winuse.ctxguard');
  const g = w[k];
  if (!g) return {fired: false, page: false};
  w.removeEventListener('contextmenu', g.h);
  delete w[k];
  return g.st;
}`

// ContextMenu is what the guard saw.
type ContextMenu struct {
	Fired bool `json:"fired"` // the page got a contextmenu event
	Page  bool `json:"page"`  // the page cancelled it itself (it has a menu of its own)
}

// GuardContextMenu installs the guard in the node's frame.
func (o Origin) GuardContextMenu(ctx context.Context) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	return staleOr(callWith(ctx, c, o.SessionID, o.BackendNodeID, ctxGuardJS, nil, nil))
}

// ContextMenuResult removes the guard and says what it saw.
func (o Origin) ContextMenuResult(ctx context.Context) (ContextMenu, error) {
	c, err := o.caller()
	if err != nil {
		return ContextMenu{}, err
	}
	var m ContextMenu
	err = callWith(ctx, c, o.SessionID, o.BackendNodeID, ctxResultJS, nil, &m)
	return m, staleOr(err)
}
