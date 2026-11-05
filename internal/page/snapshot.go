package page

import (
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// The marks from DOMSnapshot.captureSnapshot (speed issue 07): the
// cursor-pointer pass of clickables.js, computed from one snapshot of the
// session's documents instead of a script whose elements then need a
// Runtime.getProperties and one DOM.describeNode each. The snapshot carries
// every node's backendNodeId, attributes and parent, and for each node with
// a layout object its box, its layout text and the computed styles asked
// for. marksOf applies clickables.js's rules to it, with its caps; where a
// rule cannot be applied exactly the difference is noted at the rule.

// snapshotStyles are the computed styles the snapshot asks for, in order.
var snapshotStyles = []string{"cursor", "visibility", "display", "text-transform"}

// snapshotParams is DOMSnapshot.captureSnapshot's request: the styles only,
// no DOM rects, paint order or colours.
var snapshotParams = map[string]any{"computedStyles": snapshotStyles}

// The caps clickables.js applies.
const (
	marksScanCap  = 5000 // elements under body looked at
	marksFoundCap = 200  // clickables reported
	marksTextCap  = 80   // UTF-16 units of a clickable's text
)

// domSnapshot is a captureSnapshot answer: one entry per document the
// session holds (its own and same-process iframes'), and the string table
// every index points into (-1 is no string).
type domSnapshot struct {
	Documents []snapDoc `json:"documents"`
	Strings   []string  `json:"strings"`
}

type snapDoc struct {
	FrameID int `json:"frameId"`
	Nodes   struct {
		ParentIndex    []int      `json:"parentIndex"`
		NodeType       []int      `json:"nodeType"`
		ShadowRootType rareString `json:"shadowRootType"`
		NodeName       []int      `json:"nodeName"`
		NodeValue      []int      `json:"nodeValue"`
		BackendNodeID  []int64    `json:"backendNodeId"`
		Attributes     [][]int    `json:"attributes"`
		PseudoType     rareString `json:"pseudoType"`
	} `json:"nodes"`
	Layout struct {
		NodeIndex []int       `json:"nodeIndex"`
		Styles    [][]int     `json:"styles"`
		Bounds    [][]float64 `json:"bounds"`
		Text      []int       `json:"text"`
	} `json:"layout"`
}

type rareString struct {
	Index []int `json:"index"`
	Value []int `json:"value"`
}

// decodeSnapshot reads a captureSnapshot answer.
func decodeSnapshot(res json.RawMessage) (*domSnapshot, error) {
	var s domSnapshot
	if err := json.Unmarshal(res, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// doc is the snapshot's document of a frame, or nil.
func (s *domSnapshot) doc(frameID string) *snapDoc {
	if s == nil {
		return nil
	}
	for i := range s.Documents {
		if s.str(s.Documents[i].FrameID) == frameID {
			return &s.Documents[i]
		}
	}
	return nil
}

func (s *domSnapshot) str(i int) string {
	if i < 0 || i >= len(s.Strings) {
		return ""
	}
	return s.Strings[i]
}

// docView is one snapshot document indexed for marksOf.
type docView struct {
	s       *domSnapshot
	d       *snapDoc
	n       int
	kids    [][]int
	layout  []int  // node to its first layout entry, -1 for none
	outside []bool // in a shadow tree, a template's content or a pseudo-element
}

func newDocView(s *domSnapshot, d *snapDoc) *docView {
	n := len(d.Nodes.ParentIndex)
	if len(d.Nodes.NodeType) < n || len(d.Nodes.NodeName) < n || len(d.Nodes.BackendNodeID) < n {
		return nil
	}
	v := &docView{s: s, d: d, n: n, kids: make([][]int, n), layout: make([]int, n), outside: make([]bool, n)}
	for i := range v.layout {
		v.layout[i] = -1
	}
	for li, ni := range d.Layout.NodeIndex {
		if ni >= 0 && ni < n && v.layout[ni] < 0 {
			v.layout[ni] = li
		}
	}
	pseudo := map[int]bool{}
	for _, i := range d.Nodes.PseudoType.Index {
		pseudo[i] = true
	}
	for i := 0; i < n; i++ {
		p := d.Nodes.ParentIndex[i]
		// Nodes come in document order, a parent before its children.
		out := pseudo[i] || d.Nodes.NodeType[i] == 11
		if p >= 0 && p < i {
			v.kids[p] = append(v.kids[p], i)
			out = out || v.outside[p]
		}
		v.outside[i] = out
	}
	return v
}

func (v *docView) name(i int) string { return v.s.str(v.d.Nodes.NodeName[i]) }

func (v *docView) isElement(i int) bool { return v.d.Nodes.NodeType[i] == 1 }

func (v *docView) parent(i int) int {
	p := v.d.Nodes.ParentIndex[i]
	if p < 0 || p >= i {
		return -1
	}
	return p
}

// attr is an attribute's value and whether the element has it.
func (v *docView) attr(i int, name string) (string, bool) {
	if i >= len(v.d.Nodes.Attributes) {
		return "", false
	}
	a := v.d.Nodes.Attributes[i]
	for k := 0; k+1 < len(a); k += 2 {
		if v.s.str(a[k]) == name {
			return v.s.str(a[k+1]), true
		}
	}
	return "", false
}

// style is a computed style of a node with a layout object ("" without).
func (v *docView) style(i int, name string) string {
	li := v.layout[i]
	if li < 0 || li >= len(v.d.Layout.Styles) {
		return ""
	}
	for k, s := range snapshotStyles {
		if s == name && k < len(v.d.Layout.Styles[li]) {
			return v.s.str(v.d.Layout.Styles[li][k])
		}
	}
	return ""
}

// empty reports a node with no layout object or a zero-width or
// zero-height box (getBoundingClientRect's 0 × 0 for an element not
// rendered).
func (v *docView) empty(i int) bool {
	li := v.layout[i]
	if li < 0 || li >= len(v.d.Layout.Bounds) {
		return true
	}
	b := v.d.Layout.Bounds[li]
	return len(b) < 4 || b[2] == 0 || b[3] == 0
}

// cursor is a node's computed cursor. An element with no layout object
// (display:contents) has none in the snapshot: its nearest ancestor's with
// one stands in, which is what it inherits unless it sets its own.
func (v *docView) cursor(i int) string {
	for ; i >= 0; i = v.parent(i) {
		if v.layout[i] >= 0 {
			return v.style(i, "cursor")
		}
	}
	return ""
}

// body is document.body: the first BODY or FRAMESET child of the HTML
// document element, or -1.
func (v *docView) body() int {
	for _, root := range v.kids[0] {
		if !v.isElement(root) {
			continue
		}
		if !strings.EqualFold(v.name(root), "HTML") {
			return -1
		}
		for _, k := range v.kids[root] {
			if v.isElement(k) && (strings.EqualFold(v.name(k), "BODY") || strings.EqualFold(v.name(k), "FRAMESET")) {
				return k
			}
		}
		return -1
	}
	return -1
}

// clickables.js's sets.
var (
	nativeTags  = setOf("A", "BUTTON", "INPUT", "SELECT", "TEXTAREA", "SUMMARY", "OPTION", "LABEL", "DETAILS", "IFRAME", "VIDEO", "AUDIO")
	widgetRoles = setOf("button", "link", "checkbox", "radio", "switch", "tab", "menuitem", "menuitemcheckbox", "menuitemradio", "option", "combobox", "textbox", "searchbox", "slider", "spinbutton", "treeitem", "gridcell", "listbox")
	widgetTags  = setOf("BUTTON", "INPUT", "SELECT", "TEXTAREA", "SUMMARY") // plus a[href]
	insideRoles = setOf("button", "link", "checkbox", "menuitem", "option", "tab")
)

// blockDisplay reports a display that puts a line break before and after
// the box in innerText.
func blockDisplay(d string) bool {
	return d != "" && !strings.HasPrefix(d, "inline") && d != "contents" && d != "none"
}

func setOf(s ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range s {
		m[v] = true
	}
	return m
}

// marksOf is clickables.js run over a snapshot document. passwords adds
// the password fields of every document in the snapshot, shadow trees
// included (clickables.js sees its own document's light tree only): a
// field the snapshot shows to be a password never shows its value.
func marksOf(s *domSnapshot, d *snapDoc) *Marks {
	v := newDocView(s, d)
	if v == nil || v.n == 0 {
		return nil
	}
	m := &Marks{}
	for di := range s.Documents {
		dv := v
		if &s.Documents[di] != d {
			if dv = newDocView(s, &s.Documents[di]); dv == nil {
				continue
			}
		}
		for i := 0; i < dv.n; i++ {
			if dv.isElement(i) && strings.EqualFold(dv.name(i), "INPUT") {
				if t, ok := dv.attr(i, "type"); ok && strings.EqualFold(t, "password") {
					m.Passwords = append(m.Passwords, dv.d.Nodes.BackendNodeID[i])
				}
			}
		}
	}
	body := v.body()
	if body < 0 {
		return m
	}
	pointerBody := v.cursor(body) == "pointer"
	under := make([]bool, v.n)
	under[body] = false
	scanned, found := 0, 0
	for i := body + 1; i < v.n && scanned < marksScanCap && found < marksFoundCap; i++ {
		p := v.parent(i)
		under[i] = p >= 0 && (p == body || under[p])
		if !under[i] || v.outside[i] || !v.isElement(i) {
			continue
		}
		scanned++
		tag := v.name(i)
		if nativeTags[tag] || v.editable(i) {
			continue
		}
		role, _ := v.attr(i, "role")
		if widgetRoles[strings.ToLower(strings.TrimSpace(role))] {
			continue
		}
		tabindex, hasTab := v.attr(i, "tabindex")
		parentEl := p
		if parentEl >= 0 && !v.isElement(parentEl) {
			parentEl = -1
		}
		pointer := !pointerBody && v.cursor(i) == "pointer" && !(parentEl >= 0 && v.cursor(parentEl) == "pointer")
		_, onclick := v.attr(i, "onclick")
		if !pointer && !onclick && !(hasTab && tabindex != "-1") {
			continue
		}
		// No layout object: display:none, or no box (getBoundingClientRect
		// 0 × 0); either way clickables.js skips it.
		if v.style(i, "visibility") == "hidden" || v.empty(i) {
			continue
		}
		if parentEl >= 0 && v.insideWidget(parentEl) {
			continue
		}
		text := v.innerText(i)
		if text == "" {
			text, _ = v.attr(i, "aria-label")
		}
		if text == "" {
			text, _ = v.attr(i, "title")
		}
		m.Clickables = append(m.Clickables, Clickable{BackendNodeID: v.d.Nodes.BackendNodeID[i], Text: jsCut(jsCollapse(text), marksTextCap)})
		found++
	}
	return m
}

// editable is isContentEditable from the contenteditable attributes of the
// element and its ancestors (a page in designMode is not seen).
func (v *docView) editable(i int) bool {
	for ; i >= 0 && v.isElement(i); i = v.parent(i) {
		ce, ok := v.attr(i, "contenteditable")
		if !ok {
			continue
		}
		switch strings.ToLower(ce) {
		case "", "true", "plaintext-only":
			return true
		case "false":
			return false
		}
	}
	return false
}

// insideWidget is el.closest(insideWidget) of clickables.js.
func (v *docView) insideWidget(i int) bool {
	for ; i >= 0 && v.isElement(i); i = v.parent(i) {
		tag := strings.ToUpper(v.name(i))
		if widgetTags[tag] {
			return true
		}
		if _, ok := v.attr(i, "href"); ok && tag == "A" {
			return true
		}
		if r, ok := v.attr(i, "role"); ok && insideRoles[r] {
			return true
		}
	}
	return false
}

// innerText approximates el.innerText, which clickables.js reads, then
// collapses, as Chromium computes it (element_inner_text.cc): the rendered
// text nodes among the element's descendants in document order (not in a
// shadow tree or generated content), none whose style hides it, a break
// around every block-level box and at a <br>, and a <select>'s options'
// text. Only an HTML element has innerText: an SVG element's is undefined,
// so clickables.js falls back to its labels.
func (v *docView) innerText(i int) string {
	if name := v.name(i); name != strings.ToUpper(name) {
		return "" // an SVG or MathML element: its nodeName is not upper case
	}
	var sb strings.Builder
	var walk func(i int)
	walk = func(i int) {
		for _, k := range v.kids[i] {
			if v.outside[k] {
				continue
			}
			switch v.d.Nodes.NodeType[k] {
			case 3:
				if v.layout[k] >= 0 && v.style(k, "visibility") == "visible" {
					sb.WriteString(v.textOf(k))
				}
			case 1:
				shown := v.layout[k] >= 0
				switch {
				case shown && v.name(k) == "BR":
					sb.WriteByte('\n')
					continue
				case shown && v.name(k) == "SELECT":
					for _, o := range v.options(k) {
						sb.WriteString("\n" + v.textContent(o) + "\n")
					}
					continue
				}
				block := shown && blockDisplay(v.style(k, "display"))
				if block {
					sb.WriteByte('\n')
				}
				walk(k)
				if block {
					sb.WriteByte('\n')
				}
			}
		}
	}
	walk(i)
	return strings.TrimFunc(sb.String(), jsSpace)
}

// options are a <select>'s options, its own and its optgroups'.
func (v *docView) options(i int) []int {
	var out []int
	for _, k := range v.kids[i] {
		switch v.name(k) {
		case "OPTION":
			out = append(out, k)
		case "OPTGROUP":
			for _, o := range v.kids[k] {
				if v.name(o) == "OPTION" {
					out = append(out, o)
				}
			}
		}
	}
	return out
}

// textContent is the text of every text node under a node, rendered or
// not (an option's text).
func (v *docView) textContent(i int) string {
	var sb strings.Builder
	for _, k := range v.kids[i] {
		switch {
		case v.outside[k]:
		case v.d.Nodes.NodeType[k] == 3 && k < len(v.d.Nodes.NodeValue):
			sb.WriteString(v.s.str(v.d.Nodes.NodeValue[k]))
		case v.isElement(k) && v.name(k) != "SCRIPT":
			sb.WriteString(v.textContent(k))
		}
	}
	return sb.String()
}

// textOf is a text node's rendered text with its text-transform: the
// layout text, which has it, when that is the node's text up to case and
// spacing, else the node's own text (a ::first-letter splits the layout
// text) with an upper- or lowercase transform applied here.
func (v *docView) textOf(i int) string {
	value := ""
	if i < len(v.d.Nodes.NodeValue) {
		value = v.s.str(v.d.Nodes.NodeValue[i])
	}
	if li := v.layout[i]; li >= 0 && li < len(v.d.Layout.Text) {
		if lt := v.s.str(v.d.Layout.Text[li]); strings.EqualFold(jsCollapse(lt), jsCollapse(value)) {
			value = lt
		}
	}
	switch v.style(i, "text-transform") {
	case "uppercase":
		return strings.ToUpper(value)
	case "lowercase":
		return strings.ToLower(value)
	}
	return value
}

// jsSpace is JavaScript's \s.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsCollapse is s.replace(/\s+/g, " ").trim().
func jsCollapse(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range s {
		if jsSpace(r) {
			space = true
			continue
		}
		if space && sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}

// jsCut is s.slice(0, n): n UTF-16 units, so a pair split at the cut
// leaves its first half (as a replacement character in Go's string).
func jsCut(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	return string(utf16.Decode(u[:n]))
}
