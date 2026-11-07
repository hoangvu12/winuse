package page

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// Limits bound the Page part of a Window State. The caps are ADR 0004's,
// shared with the UI Automation frame: MaxNodes is what the frame left of
// the element cap, MaxDepth what it left of the depth cap below the
// document Element. The labels are the whole caps, as the notes and the
// truncation suffix name them.
type Limits struct {
	MaxNodes    int // Page Elements allowed
	MaxDepth    int // the greatest relative depth still expanded (0 = the document's children)
	MaxChildren int // per Element
	NodeLabel   int // the element cap the notes name (3000)
	DepthLabel  int // the depth cap the notes name (48)
}

// Result is the Page rendered as Window State Elements.
type Result struct {
	// Elements are the Page's Elements, depth-first pre-order, depth 0
	// being the document's children. Origins is parallel to it.
	Elements []wstate.Element
	Origins  []Origin
	// Focused is the Elements index of the Page's focused Element, or -1.
	// RootFocused says the document itself has the focus.
	Focused     int
	RootFocused bool
	// DocTruncated is set when the cap cut the document's own children;
	// the document Element's line carries it.
	DocTruncated *wstate.Truncation
	// Reasons are the partial-tree reasons, worded like the UIA walk's.
	Reasons []string
}

// Partial says the read did not see the whole Page: a cut reason, or a
// truncation marker the children cap left (reliability council 03).
func (r *Result) Partial() bool {
	if len(r.Reasons) > 0 || r.DocTruncated != nil {
		return true
	}
	for i := range r.Elements {
		if r.Elements[i].Truncated != nil {
			return true
		}
	}
	return false
}

// Build renders a Raw read into Window State Elements: CDP roles mapped to
// the UIA role words (roles.go), ignored and wrapper nodes left out with
// their children taking their place, text that repeats its parent's name
// dropped, role-less clickables from the cursor-pointer pass added, and
// iframes read on their own inlined under their iframe Element. It is
// pure, so recorded getFullAXTree answers render offline.
func Build(raw *Raw, lim Limits) Result {
	b := &builder{raw: raw, frames: map[int64]*Frame{}}
	for i := range raw.Frames {
		b.frames[raw.Frames[i].Owner] = &raw.Frames[i]
	}
	main := b.doc(&raw.Main, nil)
	res := Result{Focused: -1}
	var roots []*vnode
	if root := main.root(); root != nil {
		if prop(root, "focused") == "true" {
			res.RootFocused = true
		}
		roots = b.children(main, root, scope{})
	}
	e := &emitter{lim: lim, res: &res}
	e.top(roots)
	return res
}

// Collapse is wstate.Collapse plus the zero-width characters browsers put
// in titles (Edge's "Microsoft", a zero-width space, " Edge").
func Collapse(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, s)
	return wstate.Collapse(s)
}

// vnode is one Element to be: its line, its origin and its children.
type vnode struct {
	el      wstate.Element
	origin  Origin
	focused bool
	kids    []*vnode
}

type builder struct {
	raw    *Raw
	frames map[int64]*Frame // by the owner iframe's backendNodeId
}

// docState is one document being rendered.
type docState struct {
	d          *Doc
	owner      *Owner // set for a document inlined under an iframe
	byID       map[string]*AXNode
	clickables map[int64]string
	passwords  map[int64]bool
	fieldNames map[string]bool // names of fields, for dropping their labels
}

func (b *builder) doc(d *Doc, owner *Owner) *docState {
	s := &docState{d: d, owner: owner, byID: make(map[string]*AXNode, len(d.Nodes)),
		clickables: map[int64]string{}, passwords: map[int64]bool{}, fieldNames: map[string]bool{}}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		s.byID[n.NodeID] = n
		if r := rule(n.Role.String()); fieldRoles[r.role] || r.role == "check box" || r.role == "radio button" {
			if name := Collapse(n.Name.String()); name != "" {
				s.fieldNames[name] = true
			}
		}
	}
	if d.Marks != nil {
		for _, c := range d.Marks.Clickables {
			s.clickables[c.BackendNodeID] = Collapse(c.Text)
		}
		for _, p := range d.Marks.Passwords {
			s.passwords[p] = true
		}
	}
	return s
}

// root is the document's root node: the RootWebArea, else the first node
// without a parent.
func (s *docState) root() *AXNode {
	var first *AXNode
	for i := range s.d.Nodes {
		n := &s.d.Nodes[i]
		if n.ParentID != "" {
			continue
		}
		if strings.EqualFold(n.Role.String(), "RootWebArea") {
			return n
		}
		if first == nil {
			first = n
		}
	}
	return first
}

// scope is what a node's rendering needs from its ancestors.
type scope struct {
	named       string // the nearest Element line's name
	interactive bool   // inside an interactive Element
	inFrame     bool   // inside an inlined iframe document (one level deep)
}

// children renders a node's children, in order.
func (b *builder) children(s *docState, n *AXNode, sc scope) []*vnode {
	var out []*vnode
	for _, id := range n.ChildIDs {
		if c := s.byID[id]; c != nil {
			out = append(out, b.node(s, c, sc)...)
		}
	}
	return out
}

// node renders one node: itself as a line with its children under it, or
// (a wrapper, an ignored node) its children in its place, or nothing.
func (b *builder) node(s *docState, n *AXNode, sc scope) []*vnode {
	cdpRole := n.Role.String()
	r := rule(cdpRole)
	name := Collapse(n.Name.String())
	clickText, clickable := s.clickables[n.BackendDOMNodeID]
	clickable = clickable && n.BackendDOMNodeID != 0 && !sc.interactive && !interactiveRoles[r.role]

	if r.kind == drop {
		return nil
	}
	if strings.EqualFold(cdpRole, "LabelText") && s.fieldNames[Collapse(textOf(s, n))] {
		return nil // the field it labels already carries the text as its name
	}
	if n.Ignored && !clickable {
		return b.children(s, n, sc)
	}
	role := r.role
	switch {
	case clickable:
		role = "clickable"
		if name == "" {
			name = clickText
		}
	case r.role == "" && r.kind == wrapper && !known(cdpRole) && name != "":
		// A role this table does not know, but named: still worth a line.
		role = "group"
		if len(n.ChildIDs) == 0 {
			role = "text"
		}
	case r.kind == wrapper, r.kind == keepNamed && name == "":
		return b.children(s, n, sc)
	}
	if role == "text" && name != "" && strings.Contains(sc.named, name) {
		return nil // the text its parent is already named after
	}

	v := &vnode{el: b.element(s, n, role, name), origin: b.origin(s, n), focused: prop(n, "focused") == "true"}
	if r.kind == leaf && !clickable {
		return []*vnode{v}
	}
	inner := scope{named: name, interactive: sc.interactive || interactiveRoles[role] || clickable, inFrame: sc.inFrame}
	if f := b.frames[n.BackendDOMNodeID]; f != nil && !sc.inFrame && s.owner == nil {
		// A cross-origin (or left out) iframe: its own document goes under
		// the iframe Element, one level deep.
		v.kids = append(v.kids, b.frameDoc(s, f)...)
	}
	v.kids = append(v.kids, b.children(s, n, inner)...)
	return []*vnode{v}
}

// known reports whether a CDP role is in the role table.
func known(cdpRole string) bool {
	_, ok := roleTable[strings.ToLower(cdpRole)]
	return ok
}

// frameDoc renders an iframe's own document as a document Element.
func (b *builder) frameDoc(parent *docState, f *Frame) []*vnode {
	owner := &Owner{
		SessionID:     parent.d.SessionID,
		BackendNodeID: f.Owner,
		FrameID:       parent.d.FrameID,
		LoaderID:      parent.d.Loaders[parent.d.FrameID],
	}
	s := b.doc(&f.Doc, owner)
	root := s.root()
	if root == nil {
		return nil
	}
	name := Collapse(root.Name.String())
	v := &vnode{
		el:      wstate.Element{Role: "document", Name: name},
		origin:  b.origin(s, root),
		focused: prop(root, "focused") == "true",
	}
	v.kids = b.children(s, root, scope{inFrame: true})
	return []*vnode{v}
}

// textOf is the text a node's StaticText descendants carry.
func textOf(s *docState, n *AXNode) string {
	var parts []string
	var walk func(n *AXNode, depth int)
	walk = func(n *AXNode, depth int) {
		if depth > 8 {
			return
		}
		if strings.EqualFold(n.Role.String(), "StaticText") {
			parts = append(parts, n.Name.String())
			return
		}
		for _, id := range n.ChildIDs {
			if c := s.byID[id]; c != nil {
				walk(c, depth+1)
			}
		}
	}
	walk(n, 0)
	return strings.Join(parts, " ")
}

// element builds a node's line: role, name, description, states, value
// and Secondary Actions, in the UIA vocabulary.
func (b *builder) element(s *docState, n *AXNode, role, name string) wstate.Element {
	e := wstate.Element{Role: role, Name: name}
	if desc := Collapse(n.Description.String()); desc != "" && desc != name {
		e.Description = desc
	}
	cdpRole := strings.ToLower(n.Role.String())
	e.Disabled = prop(n, "disabled") == "true"

	if selectableRoles[cdpRole] {
		e.Selectable = true
		if cdpRole == "radio" {
			e.Selected = prop(n, "checked") == "true"
		} else {
			e.Selected = prop(n, "selected") == "true"
		}
	}
	switch prop(n, "expanded") {
	case "true":
		e.ExpandState = "expanded"
	case "false":
		e.ExpandState = "collapsed"
	}
	readonly := prop(n, "readonly") == "true"
	switch role {
	case "edit":
		if !readonly {
			e.Settable = "string"
		}
	case "combo box":
		if !readonly && prop(n, "editable") != "" {
			e.Settable = "string"
		}
	case "slider", "spinner":
		if !readonly {
			e.Settable = "float"
		}
	}
	if fieldRoles[role] {
		e.Value = b.value(s, n, role)
	}
	toggle := ""
	switch {
	case role == "check box" || cdpRole == "menuitemcheckbox":
		toggle = prop(n, "checked")
	case role == "button":
		toggle = prop(n, "pressed")
	}
	switch toggle {
	case "true":
		e.Toggle = "on"
	case "false":
		e.Toggle = "off"
	case "mixed":
		e.Toggle = "indeterminate"
	}

	// Secondary Actions, in the canonical order (uia's table): Expand,
	// Collapse, Toggle, Select.
	switch e.ExpandState {
	case "collapsed":
		e.SecondaryActions = append(e.SecondaryActions, "Expand")
	case "expanded":
		e.SecondaryActions = append(e.SecondaryActions, "Collapse")
	}
	if e.Toggle != "" {
		e.SecondaryActions = append(e.SecondaryActions, "Toggle")
	}
	if e.Selectable && !e.Disabled {
		e.SecondaryActions = append(e.SecondaryActions, "Select")
	}
	return e
}

// value is a field's value for the Value: part. A password field never
// shows its value, and neither does any text field when the
// cursor-pointer pass failed (password fields could not be told apart).
// A value made only of mask characters is a password the browser masked.
func (b *builder) value(s *docState, n *AXNode, role string) string {
	if s.passwords[n.BackendDOMNodeID] {
		return ""
	}
	text := role == "edit" || role == "combo box"
	if text && s.d.Marks == nil {
		return ""
	}
	v := Collapse(n.Value.String())
	if v != "" && strings.Trim(v, "\u2022\u25cf*\u2217\u00b7") == "" {
		return ""
	}
	return v
}

// origin is where a node lives, for Element Index resolution.
func (b *builder) origin(s *docState, n *AXNode) Origin {
	frame := n.FrameID
	if frame == "" {
		frame = s.d.FrameID
	}
	return Origin{
		Browser:       b.raw.Browser,
		Client:        b.raw.Client,
		TargetID:      b.raw.TargetID,
		SessionID:     s.d.SessionID,
		FrameID:       frame,
		LoaderID:      s.d.Loaders[frame],
		BackendNodeID: n.BackendDOMNodeID,
		Owner:         s.owner,
	}
}

// prop is a node property's value as text ("" when absent).
func prop(n *AXNode, name string) string {
	for _, p := range n.Properties {
		if p.Name == name {
			return p.Value.String()
		}
	}
	return ""
}

// emitter lays the vnodes out depth-first with ADR 0004's caps: the
// element cap stops the read and marks the cut Element, a node at the
// depth cap is shown but not expanded, and an Element with more children
// than the per-Element cap shows the first ones and is marked.
type emitter struct {
	lim     Limits
	res     *Result
	stopped bool
	nodes   bool
	depth   bool
}

func (e *emitter) top(roots []*vnode) {
	e.kids(roots, 0, func(limit, omitted int) {
		e.res.DocTruncated = &wstate.Truncation{Limit: strconv.Itoa(limit), OmittedChildren: omitted}
	})
	if e.nodes {
		e.res.Reasons = append(e.res.Reasons, fmt.Sprintf("the element cap (%d) was reached", e.lim.NodeLabel))
	}
	if e.depth {
		e.res.Reasons = append(e.res.Reasons, fmt.Sprintf("the depth cap (%d) was reached", e.lim.DepthLabel))
	}
}

// kids emits one Element's children at depth; mark sets the parent's
// truncation suffix (the first cut wins, as in the walk).
func (e *emitter) kids(kids []*vnode, depth int, mark func(limit, omitted int)) {
	marked := false
	cut := func(limit, omitted int) {
		if !marked {
			marked = true
			mark(limit, omitted)
		}
	}
	limit := len(kids)
	if e.lim.MaxChildren > 0 && limit > e.lim.MaxChildren {
		cut(e.lim.MaxChildren, limit-e.lim.MaxChildren)
		limit = e.lim.MaxChildren
	}
	for i := 0; i < limit; i++ {
		if e.stopped {
			return
		}
		if len(e.res.Elements) >= e.lim.MaxNodes {
			e.nodes = true
			e.stopped = true
			cut(e.lim.NodeLabel, len(kids)-i)
			return
		}
		e.emit(kids[i], depth)
	}
}

func (e *emitter) emit(v *vnode, depth int) {
	index := len(e.res.Elements)
	el := v.el
	el.Depth = depth
	e.res.Elements = append(e.res.Elements, el)
	e.res.Origins = append(e.res.Origins, v.origin)
	if v.focused && e.res.Focused < 0 {
		e.res.Focused = index
	}
	if len(v.kids) == 0 {
		return
	}
	if depth >= e.lim.MaxDepth {
		e.depth = true
		return
	}
	e.kids(v.kids, depth+1, func(limit, omitted int) {
		e.res.Elements[index].Truncated = &wstate.Truncation{Limit: strconv.Itoa(limit), OmittedChildren: omitted}
	})
}
