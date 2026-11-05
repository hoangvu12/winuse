package page

import (
	"fmt"
	"strings"
)

// tn is a node of a test DOM: what a page's DOMSnapshot.captureSnapshot
// answer is built from (snapshotOf), so the snapshot pass is checked
// against pages written as trees instead of hand-indexed arrays.
type tn struct {
	name  string            // "DIV", "#text", "::before"; "#shadow", "#ua-shadow" or "#light" (slotted) groups
	value string            // a text node's text
	attrs []string          // name, value, name, value ...
	style map[string]string // computed styles; cursor, visibility and text-transform inherit
	box   []float64         // layout bounds x, y, w, h; nil: no layout object
	text  *string           // a text node's layout text when it differs from value
	kids  []*tn
	id    int64 // backendNodeId, set by number
}

// el is an element with a 10×10 box (shown) unless style sets "box" to
// "none" (no layout object) or "zero" (a 0×0 box).
func el(name string, attrs []string, style map[string]string, kids ...*tn) *tn {
	n := &tn{name: name, attrs: attrs, style: map[string]string{}, kids: kids, box: []float64{0, 0, 10, 10}}
	for k, v := range style {
		n.style[k] = v
	}
	switch n.style["box"] {
	case "none":
		n.box = nil
	case "zero":
		n.box = []float64{0, 0, 0, 0}
	}
	delete(n.style, "box")
	return n
}

func txt(s string) *tn { return &tn{name: "#text", value: s} }

// page wraps body's children in a document, html, head and body.
func page(body ...*tn) *tn {
	return &tn{name: "#document", kids: []*tn{el("HTML", nil, nil, el("HEAD", nil, map[string]string{"box": "none"}), el("BODY", nil, map[string]string{"display": "block"}, body...))}}
}

// number gives every node a backendNodeId, in document order, from first.
func (n *tn) number(first int64) int64 {
	n.id = first
	next := first + 1
	for _, k := range n.kids {
		next = k.number(next)
	}
	return next
}

// find is the first node (document order) whose text or id attribute is s.
func (n *tn) find(s string) *tn {
	if n.value == s {
		return n
	}
	for i := 0; i+1 < len(n.attrs); i += 2 {
		if n.attrs[i] == "id" && n.attrs[i+1] == s {
			return n
		}
	}
	for _, k := range n.kids {
		if f := k.find(s); f != nil {
			return f
		}
	}
	return nil
}

// snapshotOf is the captureSnapshot answer for documents (each a #document
// tn) with their frame ids, computed styles in snapshotStyles' order.
func snapshotOf(frames []string, docs ...*tn) map[string]any {
	b := &snapBuilder{index: map[string]int{}}
	var out []any
	for i, d := range docs {
		out = append(out, b.doc(frames[i], d))
	}
	return map[string]any{"documents": out, "strings": b.strings}
}

type snapBuilder struct {
	strings []string
	index   map[string]int
}

func (b *snapBuilder) s(v string) int {
	if v == "" {
		return -1
	}
	if i, ok := b.index[v]; ok {
		return i
	}
	b.strings = append(b.strings, v)
	b.index[v] = len(b.strings) - 1
	return len(b.strings) - 1
}

type snapNodes struct {
	ParentIndex    []int          `json:"parentIndex"`
	NodeType       []int          `json:"nodeType"`
	ShadowRootType map[string]any `json:"shadowRootType"`
	NodeName       []int          `json:"nodeName"`
	NodeValue      []int          `json:"nodeValue"`
	BackendNodeID  []int64        `json:"backendNodeId"`
	Attributes     [][]int        `json:"attributes"`
	PseudoType     map[string]any `json:"pseudoType"`
}

type snapLayout struct {
	NodeIndex []int       `json:"nodeIndex"`
	Styles    [][]int     `json:"styles"`
	Bounds    [][]float64 `json:"bounds"`
	Text      []int       `json:"text"`
}

func (b *snapBuilder) doc(frame string, d *tn) map[string]any {
	nodes := &snapNodes{}
	layout := &snapLayout{}
	var shadowIdx, shadowVal, pseudoIdx, pseudoVal []int
	inherited := map[string]string{"cursor": "auto", "visibility": "visible"}
	var walk func(n *tn, parent int, parentStyle map[string]string, parentShown, inShadow bool)
	walk = func(n *tn, parent int, parentStyle map[string]string, parentShown, inShadow bool) {
		// The snapshot is the flat tree: a shadow tree's nodes go under their
		// host, marked; a #light node's (slotted) under the <slot> it is in; a
		// user-agent shadow tree is left out.
		switch n.name {
		case "#shadow", "#light":
			for _, k := range n.kids {
				walk(k, parent, parentStyle, parentShown, n.name == "#shadow")
			}
			return
		case "#ua-shadow":
			return
		}
		i := len(nodes.NodeType)
		if inShadow {
			shadowIdx, shadowVal = append(shadowIdx, i), append(shadowVal, b.s("open"))
		}
		nodes.ParentIndex = append(nodes.ParentIndex, parent)
		nodes.BackendNodeID = append(nodes.BackendNodeID, n.id)
		var attrs []int
		for _, a := range n.attrs {
			attrs = append(attrs, b.s(a))
		}
		if attrs == nil {
			attrs = []int{}
		}
		nodes.Attributes = append(nodes.Attributes, attrs)
		typ, name, value := 1, n.name, -1
		switch {
		case n.name == "#document":
			typ = 9
		case n.name == "#text":
			typ, value = 3, b.s(n.value)
		case strings.HasPrefix(n.name, "::"):
			pseudoIdx, pseudoVal = append(pseudoIdx, i), append(pseudoVal, b.s(strings.TrimPrefix(n.name, "::")))
		}
		nodes.NodeType = append(nodes.NodeType, typ)
		nodes.NodeName = append(nodes.NodeName, b.s(name))
		nodes.NodeValue = append(nodes.NodeValue, value)

		style := map[string]string{}
		for k, v := range parentStyle {
			style[k] = v
		}
		for k, v := range n.style {
			style[k] = v
		}
		if typ == 1 && style["display"] == "" {
			style["display"] = "inline"
		}
		disp := n.style["display"]
		shown := false
		switch typ {
		case 1:
			shown = n.box != nil && parentShown && disp != "none" && disp != "contents"
		case 3:
			shown = parentShown
		case 9:
			shown = true
		}
		if shown && typ != 9 {
			layout.NodeIndex = append(layout.NodeIndex, i)
			var st []int
			for _, k := range snapshotStyles {
				st = append(st, b.s(style[k]))
			}
			layout.Styles = append(layout.Styles, st)
			box := n.box
			if typ == 3 {
				box = []float64{0, 0, 5, 5}
			}
			layout.Bounds = append(layout.Bounds, box)
			t := -1
			if typ == 3 {
				t = b.s(n.value)
				if n.text != nil {
					t = b.s(*n.text)
				}
			}
			layout.Text = append(layout.Text, t)
		}
		kidStyle := map[string]string{"cursor": style["cursor"], "visibility": style["visibility"], "text-transform": style["text-transform"]}
		if typ == 3 {
			return
		}
		for _, k := range n.kids {
			// A display:contents element's children render, though it has no
			// box itself.
			walk(k, i, kidStyle, shown || (typ == 1 && disp == "contents" && parentShown && n.box != nil), inShadow)
		}
	}
	walk(d, -1, inherited, true, false)
	nodes.ShadowRootType = map[string]any{"index": nonNil(shadowIdx), "value": nonNil(shadowVal)}
	nodes.PseudoType = map[string]any{"index": nonNil(pseudoIdx), "value": nonNil(pseudoVal)}
	return map[string]any{"frameId": b.s(frame), "documentURL": b.s("https://test/" + frame), "nodes": nodes, "layout": layout}
}

func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

// String renders the tree for failure messages.
func (n *tn) String() string {
	var sb strings.Builder
	var walk func(n *tn, depth int)
	walk = func(n *tn, depth int) {
		fmt.Fprintf(&sb, "%s%s %q %v #%d\n", strings.Repeat("  ", depth), n.name, n.value, n.attrs, n.id)
		for _, k := range n.kids {
			walk(k, depth+1)
		}
	}
	walk(n, 0)
	return sb.String()
}
