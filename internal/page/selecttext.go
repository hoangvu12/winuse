package page

// select-text on a Page Element (headless-max ticket 10): an input's or a
// text area's setSelectionRange, or a document Selection over the text
// nodes of editable text, read back in the same call. Offsets are UTF-16
// code units, which is how JavaScript counts a string; editable text
// counts its text nodes' data, as Range.toString does.

import (
	"context"
	"fmt"
)

// TextField is what select-text reads before it selects: the kind of field
// (as Field.Kind), its length in UTF-16 units, and whether it takes a
// selection (an email or number input has none).
type TextField struct {
	Kind       string `json:"kind"`
	Length     int    `json:"length"`
	Selectable bool   `json:"selectable"`
}

const textFieldJS = `function() {
  /* winuse:textfield */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {kind: 'gone'};
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
    let selectable = false;
    try { selectable = el.selectionStart !== null; } catch (x) {}
    return {kind: 'text', length: String(el.value).length, selectable: selectable};
  }
  if (!el.isContentEditable) return {kind: ''};
  const w = el.ownerDocument.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  let n = 0;
  for (let t = w.nextNode(); t; t = w.nextNode()) n += t.data.length;
  return {kind: 'editable', length: n, selectable: true};
}`

// selectTextJS selects start to end (end undefined: the whole text) and reads
// the selection back as offsets from the field's start.
const selectTextJS = `function(start, end) {
  /* winuse:selecttext */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
    const n = String(el.value).length;
    if (end == null) { start = 0; end = n; }
    el.setSelectionRange(start, end);
    return {start: el.selectionStart, end: el.selectionEnd, length: n};
  }
  const doc = el.ownerDocument, w = doc.createTreeWalker(el, NodeFilter.SHOW_TEXT), nodes = [];
  let n = 0;
  for (let t = w.nextNode(); t; t = w.nextNode()) { nodes.push(t); n += t.data.length; }
  if (end == null) { start = 0; end = n; }
  const at = (off) => {
    let acc = 0;
    for (const t of nodes) { if (off <= acc + t.data.length) return [t, off - acc]; acc += t.data.length; }
    return [el, el.childNodes.length];
  };
  const rg = doc.createRange(), s = at(start), e = at(end);
  rg.setStart(s[0], s[1]); rg.setEnd(e[0], e[1]);
  const sel = doc.getSelection();
  sel.removeAllRanges(); sel.addRange(rg);
  if (!sel.rangeCount) return {start: -1, end: -1, length: n};
  const got = sel.getRangeAt(0);
  if (!el.contains(got.startContainer) || !el.contains(got.endContainer)) return {start: -1, end: -1, length: n};
  const off = (node, o) => { const r = doc.createRange(); r.selectNodeContents(el); r.setEnd(node, o); return r.toString().length; };
  return {start: off(got.startContainer, got.startOffset), end: off(got.endContainer, got.endOffset), length: n};
}`

// ReadTextField reads what select-text selects in.
func (o Origin) ReadTextField(ctx context.Context) (TextField, error) {
	c, err := o.caller()
	if err != nil {
		return TextField{}, err
	}
	var f TextField
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, textFieldJS, nil, &f); err != nil {
		return TextField{}, staleOr(err)
	}
	if f.Kind == "gone" {
		return TextField{}, fmt.Errorf("%w: the node left the document", ErrStale)
	}
	return f, nil
}

// SelectText selects length UTF-16 units from start of a text field, or
// with all its whole text, with no OS focus: DOM.focus (so a type-text
// after it replaces the selection), then the selection, read back in the
// same call. The selection call counts as one delivered event in
// in.Sent; the Esc checkpoint runs before it. A read-back that differs is
// a *VerifyError.
func (o Origin) SelectText(ctx context.Context, in *Input, start, length int, all bool) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	if in.stopped != nil && in.stopped() {
		return ErrStopped
	}
	if _, err := c.CallSession(ctx, o.SessionID, "DOM.focus", map[string]any{"backendNodeId": o.BackendNodeID}); err != nil {
		return staleOr(err)
	}
	var args []any
	if all {
		args = []any{0, nil}
	} else {
		args = []any{start, start + length}
	}
	in.Sent++
	var got struct {
		Start  int `json:"start"`
		End    int `json:"end"`
		Length int `json:"length"`
	}
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, selectTextJS, args, &got); err != nil {
		return &VerifyError{Msg: "the selection was delivered, but the field's selection could not be read back"}
	}
	wantStart, wantEnd := start, start+length
	if all {
		wantStart, wantEnd = 0, got.Length
	}
	if got.Start != wantStart || got.End != wantEnd {
		if got.Start < 0 {
			return &VerifyError{Msg: fmt.Sprintf("the selection was delivered, but the field holds no selection afterwards, not characters %d to %d", wantStart, wantEnd)}
		}
		return &VerifyError{Msg: fmt.Sprintf("the selection was delivered, but the field selects characters %d to %d afterwards, not %d to %d", got.Start, got.End, wantStart, wantEnd)}
	}
	return nil
}
