package uia

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// NodeData is the cached property set the walk reads per element, on the
// worker thread, from the cache request built by the COM source. The
// basic ticket-03 fields render role/name/state; the pattern fields
// (ticket 07) render the rich state (value, toggle, selection, expand,
// settable) and the Secondary Actions line — value.go owns the policy
// they feed. The Has* gates carry pattern availability, so the zero
// NodeData a fake source builds renders no rich state.
type NodeData struct {
	ControlType  int32
	Name         string
	AutomationId string
	HelpText     string
	Enabled      bool
	Focused      bool

	// IsPassword is the cached IsPassword; PasswordUnread says it did not
	// read. Either on an Edit withholds the Value (secret).
	IsPassword     bool
	PasswordUnread bool

	// Pattern facts (ticket 07), from cached pattern properties.
	HasWindow bool
	HasScroll bool
	ScrollH   bool
	ScrollV   bool
	HasExpand bool
	// ExpandState is the ExpandCollapse state, -1 when unreadable. State
	// tokens render only while HasExpand; a leaf node (3) offers no
	// Expand/Collapse either.
	ExpandState int32
	HasToggle   bool
	// ToggleState is the Toggle state (-1 unreadable, gated on HasToggle).
	ToggleState      int32
	HasSelectionItem bool
	Selected         bool
	HasValue         bool
	ValueReadOnly    bool
	ValueValue       string
	HasRangeValue    bool
}

// ErrChunkTimedOut reports that a cache chunk exceeded the UIA transaction
// timeout: the provider did not answer in time. A node's children that time
// out as one chunk are read one at a time (ChildStepper) and the walk goes
// on with the node's siblings; a provider that does not answer a single
// step either stops the walk with the time-limit note.
var ErrChunkTimedOut = errors.New("ui automation chunk timed out")

// TreeSource supplies the elements of one Window's control-view tree, one
// chunk at a time. T is the live element handle type (a COM interface
// pointer in the real source; a plain value in tests). Reading a tree needs
// exactly Root once and Children once per expanded node, so the cost is one
// round trip per chunk, and the target app pumps messages between chunks.
//
// Children receives the walk's remaining budget so the implementation can
// bound the cross-process call (the UIA transaction timeout). It returns the
// node's control-view children together with their cached data, in order.
// A nil slice without error means a leaf; an error means not every child
// could be read: the walk keeps the children returned with it (the ones that
// did read; none when the list itself failed) and marks the tree partial.
type TreeSource[T any] interface {
	Root() (T, NodeData, error)
	Children(node T, remaining time.Duration) ([]T, []NodeData, error)
}

// ChildStepper is a TreeSource that can also read a node's children one at
// a time (a tree walker's first child, then next sibling, each with its
// properties cached). UI Automation cannot cap how many children one cached
// read returns, so a huge child list (tens of thousands of list items) can
// time out as one chunk; the walk then steps through the first MaxChildren
// of them instead (learn-from 08).
//
// FirstChild and NextSibling answer ok false when there is no such child.
// ItemsAfter says how many items of its set follow child, when its provider
// says (UIA SizeOfSet minus PositionInSet), for the note on a cut list.
type ChildStepper[T any] interface {
	FirstChild(node T, remaining time.Duration) (child T, data NodeData, ok bool, err error)
	NextSibling(prev T, remaining time.Duration) (sibling T, data NodeData, ok bool, err error)
	ItemsAfter(child T) (n int, ok bool)
}

// ChildCounter is a TreeSource that can count a node's children without
// reading their properties or keeping them (a cache request with no
// properties and no live Elements). The skeleton counts with it; a source
// without it is counted by reading the children and releasing them.
type ChildCounter[T any] interface {
	CountChildren(node T, remaining time.Duration) (int, error)
}

// countBudget bounds one skeleton count: a count is a hint, so one huge
// list must not spend the read's budget on it. A count that does not come
// back in time prints no number and is named in a note.
const countBudget = 250 * time.Millisecond

// WalkConfig bounds one tree read. The caps keep output and time bounded on
// runaway trees; the budget is the walk's soft time limit (checked before
// every chunk, so a timeout returns the partial tree with a note rather than
// hanging the request). Now is injectable for tests.
type WalkConfig struct {
	MaxNodes    int
	MaxDepth    int // the greatest depth whose nodes are still expanded
	MaxChildren int // per node
	Budget      time.Duration
	Now         func() time.Time // nil: time.Now

	// Cut, when set, leaves out the descendants of the first Document
	// Element (in Element Index order) it names: their content is read
	// elsewhere. While its answer is not known, the walk reads on and asks
	// again before every chunk inside a Document, so a late answer still
	// stops the read there: what was read under the cut Document is dropped
	// as if never read. A Document read whole before the answer stays whole.
	Cut DocumentCut

	// CountHidden makes MaxDepth a chosen shape rather than a cap (the
	// skeleton read, desktop-parity 04): a node at MaxDepth gets its
	// children counted (Element.MoreChildren) instead of the depth-cap
	// note. The counted children are released unread.
	CountHidden bool
}

// DocumentCut picks the one Document Element whose descendants a walk
// leaves out because another reader replaces them: a browser Window's
// Page, read over CDP (browser merge ticket 04, ADR 0012). It is asked with
// a Document Element's name and Value (a browser gives its URL) and
// answers whether that Document is the one, and whether the answer is
// known yet. It is called on the worker thread and must not block.
type DocumentCut func(name, value string) (cut, known bool)

// Default walk budgets (research proposal, codex-surface.md §7: node cap
// 1000–4000, depth 32–48, children 200–1000).
const (
	DefaultMaxNodes    = 3000
	DefaultMaxDepth    = 48
	DefaultMaxChildren = 1000
)

// DefaultTreeBudget is the walk's soft time limit: comfortably inside the
// 10 s request budget so a partial tree (with its note) still renders and
// reaches the agent before the request itself times out.
const DefaultTreeBudget = 8 * time.Second

// nearEmptyNodes is the element count at or below which the tree is called
// nearly empty (canvas apps, some Electron apps) so the agent knows to rely
// on a screenshot instead (spec user story 22).
const nearEmptyNodes = 3

// nearEmptyNote is the note the walk appends to a sparse tree: an uncut
// tree of at most nearEmptyNodes elements with no partial-tree note. The
// retry policy (retry.go) detects a sparse read by this exact note, and
// recorded fixtures carry it, so the text is a shared contract.
const nearEmptyNote = "this Window's tree is nearly empty; the app may expose little to UI Automation"

// partialNotePrefix is the fixed start of the walk's partial-tree note.
// The retry policy uses it to recognise a partial (cut) read.
const partialNotePrefix = "this Window State is partial:"

// WalkResult is one completed walk.
type WalkResult[T any] struct {
	// Elements is the tree's elements, depth-first pre-order; the slice
	// position of each is its Element Index.
	Elements []wstate.Element
	// Live holds the live element handle behind each Element, parallel to
	// Elements (the caller moves these into the live-Element store).
	Live []T
	// Dropped holds handles the source produced but the walk never visited
	// (cut by a budget); the caller releases them.
	Dropped []T
	// FocusedIndex is the Element Index of the focused element, or -1.
	FocusedIndex int
	// Notes are the partial-tree / sparse-tree notes for the Window State.
	Notes []string
	// Cut is the Element Index of the Document Element whose descendants
	// were left out (WalkConfig.Cut), or -1.
	Cut int
	// Slow lists the Elements whose children did not come back as one
	// chunk in time and were cut (the partial-tree note names them).
	Slow []SlowChildren
	// Uncounted lists the skeleton's Elements whose children were not
	// counted in time (a note names them).
	Uncounted []int
	// Partial says the walk did not see the whole Window (reliability
	// council 03): a partial-tree note (time, node cap, depth cap,
	// unreadable children, a cut slow list), a truncation marker (the
	// children cap), or a skeleton node whose children were counted, or
	// could not be. A cut Document is not a loss: its content is read
	// elsewhere.
	Partial bool

	// notes rebuilds Notes with Elements named by label: a find or a
	// region read names them by the indexes it added, not by walk position.
	notes func(label func(int) string) []string
}

// SlowChildren is one Element whose children were too slow to read as one
// chunk: the walk read Read of them one at a time and cut the rest. More is
// how many items its provider says follow the last one read, or -1.
type SlowChildren struct {
	Index int
	Read  int
	More  int
}

// NotesWith returns the walk's notes with the Elements they name labelled
// by label (from the walk position) instead of "Element <position>".
func (r WalkResult[T]) NotesWith(label func(int) string) []string {
	if r.notes == nil {
		return r.Notes
	}
	return r.notes(label)
}

// walk reasons, in the order the partial-tree note lists them.
const (
	reasonTime = iota
	reasonNodes
	reasonDepth
	reasonUnreadable
	reasonCount
)

// Walk reads a Window's tree from src within cfg's budgets. Depth-first
// pre-order, indexes from 0 at the root. An error is returned only when the
// root itself cannot be read (the Window is gone or has no provider); every
// other failure degrades to a partial tree with a note.
func Walk[T any](src TreeSource[T], cfg WalkConfig) (WalkResult[T], error) {
	var res WalkResult[T]
	res.FocusedIndex = -1
	res.Cut = -1
	if cfg.MaxNodes <= 0 {
		cfg.MaxNodes = DefaultMaxNodes
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = DefaultMaxDepth
	}
	if cfg.MaxChildren <= 0 {
		cfg.MaxChildren = DefaultMaxChildren
	}
	if cfg.Budget <= 0 {
		cfg.Budget = DefaultTreeBudget
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	w := &walker[T]{src: src, cfg: cfg, now: now, deadline: now().Add(cfg.Budget), res: &res, unwind: -1}
	root, data, err := src.Root()
	if err != nil {
		return res, err
	}
	w.visit(root, data, 0)
	res.Slow, res.Uncounted = w.slow, w.uncounted
	_, reasoned := w.reasonNote(elementLabel)
	res.Partial = reasoned || w.truncated || w.hidden
	w.res = nil // the closure below keeps the walker, not the result
	elements, cut := len(res.Elements), res.Cut
	res.notes = func(label func(int) string) []string {
		var notes []string
		if reason, ok := w.reasonNote(label); ok {
			notes = append(notes, reason)
		} else if !w.truncated && cut < 0 && elements <= nearEmptyNodes {
			// Only an uncut tree this small is "nearly empty": a tree cut by a
			// budget is partial, not sparse (its note already says so), and a
			// cut Document's content is read elsewhere.
			notes = append(notes, nearEmptyNote)
		}
		if note, ok := w.uncountedNote(label); ok {
			notes = append(notes, note)
		}
		return notes
	}
	res.Notes = res.notes(elementLabel)
	return res, nil
}

// elementLabel names an Element by its walk position, which is its Element
// Index in a full read.
func elementLabel(i int) string { return "Element " + strconv.Itoa(i) }

// walker carries one walk's state. A stopped walk (time or node cap) cuts
// remaining siblings at every level, recording them as Dropped, so their
// handles can be released.
type walker[T any] struct {
	src       TreeSource[T]
	cfg       WalkConfig
	now       func() time.Time
	deadline  time.Time
	res       *WalkResult[T]
	stopped   bool
	truncated bool // any element carries a truncation marker, or a slow list was cut
	hidden    bool // a skeleton node has children left unread, or uncounted
	reasons   [reasonCount]bool
	slow      []SlowChildren // the cut slow lists (WalkResult.Slow)
	uncounted []int          // the skeleton counts that ran out of time

	// The pending Document cut (WalkConfig.Cut).
	cutDone  bool      // answered: a Document was cut, or none will be
	docs     []docMark // the Document Elements read so far, in index order
	checked  int       // docs[:checked] are known not to be the one
	openDocs int       // Documents whose descendants are being read
	mark     docMark   // the Document being cut
	unwind   int       // that Document's index while the walk climbs back to it, or -1
}

// docMark is one Document Element a pending cut may still name, with the
// walk's notes as they were when it was reached: a cut restores them, so
// nothing read under the Document leaves a trace.
type docMark struct {
	index     int
	name      string
	value     string
	open      bool // its descendants are being read
	reasons   [reasonCount]bool
	truncated bool
	hidden    bool
	slow      int // len(walker.slow) when it was reached
	uncounted int // len(walker.uncounted) when it was reached
}

func (w *walker[T]) visit(node T, data NodeData, depth int) {
	index := len(w.res.Elements)
	w.res.Elements = append(w.res.Elements, elementFrom(data, depth))
	w.res.Live = append(w.res.Live, node)
	if data.Focused && w.res.FocusedIndex < 0 {
		w.res.FocusedIndex = index
	}
	if w.cutPending() && data.ControlType == wstate.ControlTypeDocument {
		value := ""
		if data.HasValue {
			value = data.ValueValue
		}
		w.docs = append(w.docs, docMark{index: index, name: data.Name, value: value, open: true, reasons: w.reasons, truncated: w.truncated, hidden: w.hidden,
			slow: len(w.slow), uncounted: len(w.uncounted)})
		w.openDocs++
		w.expand(node, depth, index)
		w.openDocs--
		for i := len(w.docs) - 1; i >= 0; i-- {
			if w.docs[i].index == index {
				w.docs[i].open = false
				break
			}
		}
		return
	}
	w.expand(node, depth, index)
}

// expand reads the children of the node at index and visits them.
func (w *walker[T]) expand(node T, depth, index int) {
	// A pending cut is asked before every chunk read inside a Document.
	if w.cutPending() && w.openDocs > 0 {
		if at := w.decideCut(); at == index {
			w.cutAt(index)
			return
		} else if at >= 0 {
			w.unwind = at
			return
		}
	}

	// Expansion. Depth first: a node at MaxDepth is rendered but never
	// expanded (its children would exceed the depth cap). A skeleton
	// counts them instead.
	if depth >= w.cfg.MaxDepth {
		if w.cfg.CountHidden {
			w.countHidden(node, index)
			return
		}
		w.reasons[reasonDepth] = true
		return
	}
	remaining := w.deadline.Sub(w.now())
	if remaining <= 0 {
		w.reasons[reasonTime] = true
		w.stopped = true
		return
	}
	kids, kidData, err := w.src.Children(node, remaining)
	if err != nil {
		if errors.Is(err, ErrChunkTimedOut) {
			w.drop(kids)
			if w.deadline.Sub(w.now()) <= 0 {
				// The chunk ran into the walk's own budget.
				w.reasons[reasonTime] = true
				w.stopped = true
			} else {
				// Too slow as one chunk (a huge list): one at a time instead,
				// and the walk goes on with this node's siblings.
				w.slowChildren(node, depth, index)
			}
			return
		}
		// The children that did read stay; the note names the rest.
		w.reasons[reasonUnreadable] = true
	}

	limit := len(kids)
	if limit > w.cfg.MaxChildren {
		w.markTruncated(index, w.cfg.MaxChildren, limit-w.cfg.MaxChildren)
		limit = w.cfg.MaxChildren
	}
	for i := 0; i < limit; i++ {
		if w.stopped {
			w.drop(kids[i:])
			return
		}
		if len(w.res.Live) >= w.cfg.MaxNodes {
			w.reasons[reasonNodes] = true
			w.markTruncated(index, w.cfg.MaxNodes, len(kids)-i)
			w.stopped = true
			w.drop(kids[i:])
			return
		}
		if w.deadline.Sub(w.now()) <= 0 {
			w.reasons[reasonTime] = true
			w.stopped = true
			w.drop(kids[i:])
			return
		}
		w.visit(kids[i], kidData[i], depth+1)
		if w.unwind >= 0 {
			// A late cut: the rest of this level is left unread on the way
			// back up to the cut Document.
			w.drop(kids[i+1:])
			if w.unwind == index {
				w.cutAt(index)
			}
			return
		}
	}
	w.drop(kids[limit:])
}

// slowChildren reads the children of a node whose one-chunk read timed
// out, one at a time, when the source can (ChildStepper). Without a
// stepper the node's children are left out. Either way the cut is noted
// and the walk goes on with the node's siblings.
func (w *walker[T]) slowChildren(node T, depth, index int) {
	st, ok := w.src.(ChildStepper[T])
	if !ok {
		w.markSlow(index, 0, -1)
		return
	}
	var prev T
	read := 0
	for {
		remaining := w.deadline.Sub(w.now())
		if remaining <= 0 {
			w.reasons[reasonTime] = true
			w.stopped = true
			return
		}
		var kid T
		var data NodeData
		var more bool
		var err error
		if read == 0 {
			kid, data, more, err = st.FirstChild(node, remaining)
		} else {
			kid, data, more, err = st.NextSibling(prev, remaining)
		}
		if err != nil {
			switch {
			case read == 0 && errors.Is(err, ErrChunkTimedOut):
				// Not one step answered: the provider is not answering at
				// all, not just slow on a long list. Stop, as a timed-out
				// chunk always did.
				w.reasons[reasonTime] = true
				w.stopped = true
			case w.deadline.Sub(w.now()) <= 0:
				w.reasons[reasonTime] = true
				w.stopped = true
			default:
				w.markSlow(index, read, -1)
			}
			return
		}
		if !more {
			return // every child was read
		}
		if read >= w.cfg.MaxChildren {
			// kid proves there are more; how many, only the provider knows.
			w.drop([]T{kid})
			after := -1
			if n, ok := st.ItemsAfter(prev); ok && n > 0 {
				after = n
			}
			w.markSlow(index, read, after)
			return
		}
		if len(w.res.Live) >= w.cfg.MaxNodes {
			w.reasons[reasonNodes] = true
			w.stopped = true
			w.drop([]T{kid})
			return
		}
		w.visit(kid, data, depth+1)
		if w.unwind >= 0 {
			// A late Document cut, as in expand.
			if w.unwind == index {
				w.cutAt(index)
			}
			return
		}
		prev = kid
		read++
		if w.stopped {
			return
		}
	}
}

// markSlow records a node whose children were cut after read of them were
// read one at a time; more is how many items follow, or -1.
func (w *walker[T]) markSlow(index, read, more int) {
	w.truncated = true
	w.slow = append(w.slow, SlowChildren{Index: index, Read: read, More: more})
}

// countHidden reads how many children a node below the skeleton's depth
// has, within countBudget. A count that fails leaves no number; one that
// runs out of time is named in a note, and the walk goes on.
func (w *walker[T]) countHidden(node T, index int) {
	remaining := w.deadline.Sub(w.now())
	if remaining <= 0 {
		w.reasons[reasonTime] = true
		w.stopped = true
		return
	}
	n, err := w.count(node, min(remaining, countBudget))
	if err != nil {
		w.hidden = true
		if errors.Is(err, ErrChunkTimedOut) {
			if w.deadline.Sub(w.now()) <= 0 {
				w.reasons[reasonTime] = true
				w.stopped = true
				return
			}
			w.uncounted = append(w.uncounted, index)
		}
		return
	}
	w.res.Elements[index].MoreChildren = n
	if n > 0 {
		w.hidden = true
	}
}

// count counts node's children: through the source's own count when it has
// one (no properties, nothing kept), else by reading them and releasing
// them unread.
func (w *walker[T]) count(node T, bound time.Duration) (int, error) {
	if c, ok := w.src.(ChildCounter[T]); ok {
		return c.CountChildren(node, bound)
	}
	kids, _, err := w.src.Children(node, bound)
	w.drop(kids)
	return len(kids), err
}

// cutPending reports whether the walk still has a Document cut to place.
func (w *walker[T]) cutPending() bool {
	return w.cfg.Cut != nil && !w.cutDone
}

// decideCut asks the pending cut about the Documents read so far, in index
// order. The first one it names is cut while its descendants are still
// being read (its index is returned); one already read whole stays whole,
// and nothing is cut. -1 too while the answer is not known or names none
// of them yet.
func (w *walker[T]) decideCut() int {
	for ; w.checked < len(w.docs); w.checked++ {
		d := w.docs[w.checked]
		cut, known := w.cfg.Cut(d.name, d.value)
		if !known {
			return -1
		}
		if cut {
			w.cutDone = true
			if !d.open {
				return -1
			}
			w.mark = d
			return d.index
		}
	}
	return -1
}

// cutAt leaves out the descendants of the Document Element at index. What
// was read under it (a late answer) is dropped, its handles for the caller
// to release, and the notes are restored to what they were when the walk
// reached the Document. A focus read under it moves to the Document: the
// merge reads a focus in the Document as before.
func (w *walker[T]) cutAt(index int) {
	w.res.Dropped = append(w.res.Dropped, w.res.Live[index+1:]...)
	w.res.Live = w.res.Live[:index+1]
	w.res.Elements = w.res.Elements[:index+1]
	w.res.Elements[index].Truncated = nil
	w.reasons, w.truncated, w.hidden = w.mark.reasons, w.mark.truncated, w.mark.hidden
	w.slow, w.uncounted = w.slow[:w.mark.slow], w.uncounted[:w.mark.uncounted]
	if w.res.FocusedIndex > index {
		w.res.FocusedIndex = index
	}
	w.res.Cut = index
	w.unwind = -1
}

// drop records handles the walk will not visit; the caller releases them.
func (w *walker[T]) drop(nodes []T) {
	w.res.Dropped = append(w.res.Dropped, nodes...)
}

// markTruncated sets the Codex truncation suffix on element index's line:
// (truncated: <limit>, omitted <n> children). The first marker on a line
// wins; later budgets are still named in the partial-tree note.
func (w *walker[T]) markTruncated(index, limit, omitted int) {
	w.truncated = true
	if w.res.Elements[index].Truncated != nil {
		return
	}
	w.res.Elements[index].Truncated = &wstate.Truncation{
		Limit:           strconv.Itoa(limit),
		OmittedChildren: omitted,
	}
}

// reasonNote builds the partial-tree note from the walk's reasons, in a
// fixed order so the text is stable, with the slow lists last, each named
// by label.
func (w *walker[T]) reasonNote(label func(int) string) (string, bool) {
	var parts []string
	if w.reasons[reasonTime] {
		parts = append(parts, fmt.Sprintf("the tree read exceeded the %s time limit", w.cfg.Budget))
	}
	if w.reasons[reasonNodes] {
		parts = append(parts, fmt.Sprintf("the element cap (%d) was reached", w.cfg.MaxNodes))
	}
	if w.reasons[reasonDepth] {
		parts = append(parts, fmt.Sprintf("the depth cap (%d) was reached", w.cfg.MaxDepth))
	}
	if w.reasons[reasonUnreadable] {
		parts = append(parts, "not every Element's children could be read")
	}
	for _, s := range w.slow {
		parts = append(parts, slowReason(s, label(s.Index)))
	}
	if len(parts) == 0 {
		return "", false
	}
	return partialNotePrefix + " " + strings.Join(parts, "; "), true
}

// slowReason says what happened to one slow list's children.
func slowReason(s SlowChildren, who string) string {
	if s.Read == 0 {
		return "the children of " + who + " did not answer in time and were not read"
	}
	rest := "the rest were not read"
	if s.More > 0 {
		rest = fmt.Sprintf("%d more items were not read", s.More)
	}
	return fmt.Sprintf("the children of %s were too slow to read at once, so the first %d were read one at a time and %s", who, s.Read, rest)
}

// uncountedNote names the skeleton's Elements whose children were not
// counted in time. It is not a partial-tree note: the skeleton's shape is
// chosen, only a hint is missing.
func (w *walker[T]) uncountedNote(label func(int) string) (string, bool) {
	if len(w.uncounted) == 0 {
		return "", false
	}
	names := make([]string, len(w.uncounted))
	for i, idx := range w.uncounted {
		names[i] = label(idx)
	}
	return "the children of " + strings.Join(names, ", ") + " were not counted in time", true
}

// elementFrom builds the renderable Element from one node's cached data.
func elementFrom(d NodeData, depth int) wstate.Element {
	e := wstate.Element{
		Depth:        depth,
		Role:         wstate.RoleName(d.ControlType),
		Name:         d.Name,
		AutomationId: d.AutomationId,
		Description:  d.HelpText,
		Disabled:     !d.Enabled,
	}
	// Rich state and Secondary Actions (ticket 07), in Codex's line order
	// (the policy itself lives in value.go).
	if d.HasSelectionItem {
		e.Selectable = true
		e.Selected = d.Selected
	}
	if d.HasExpand {
		switch d.ExpandState {
		case expandCollapsed:
			e.ExpandState = "collapsed"
		case expandExpanded:
			e.ExpandState = "expanded"
		case expandPartiallyExpanded:
			e.ExpandState = "partially expanded"
		}
	}
	if d.HasRangeValue {
		// Codex renders every RangeValue as settable, float (its Value
		// writeability check covers only the Value pattern).
		e.Settable = "float"
	} else if d.HasValue && !d.ValueReadOnly {
		e.Settable = "string"
	}
	if d.secret() {
		// The Value never reaches the line, the diff or the store; role,
		// name and index stay (reliability-council 07).
		e.Password = true
	} else if d.HasValue {
		e.Value = d.ValueValue
	}
	if d.HasToggle {
		switch d.ToggleState {
		case toggleOff:
			e.Toggle = "off"
		case toggleOn:
			e.Toggle = "on"
		case toggleIndeterminate:
			e.Toggle = "indeterminate"
		}
	}
	e.SecondaryActions = d.patternFacts().secondaryActions()
	return e
}

// secret reports whether a node's Value is withheld: IsPassword reads
// true, or an Edit's IsPassword did not read (agent-desktop withholds an
// unreadable answer too).
func (d NodeData) secret() bool {
	return d.IsPassword || (d.PasswordUnread && d.ControlType == wstate.ControlTypeEdit)
}
