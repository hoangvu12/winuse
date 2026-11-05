package page

import (
	"fmt"
	"strings"
)

// tn is a node of a test DOM: the benchmark page is written as a tree.
type tn struct {
	name  string            // "DIV", "#text", "#shadow" (an open shadow root), "::before"
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
