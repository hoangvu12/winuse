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
// timeout: the provider did not answer in time. The walk stops and returns
// the partial tree with the time-limit note.
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
// A nil slice without error means a leaf; an error means the children could
// not be read and the node is treated as a childless element.
type TreeSource[T any] interface {
	Root() (T, NodeData, error)
	Children(node T, remaining time.Duration) ([]T, []NodeData, error)
}

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
}

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
	w := &walker[T]{src: src, cfg: cfg, now: now, deadline: now().Add(cfg.Budget), res: &res}
	root, data, err := src.Root()
	if err != nil {
		return res, err
	}
	w.visit(root, data, 0)
	if reason, ok := w.reasonNote(); ok {
		res.Notes = append(res.Notes, reason)
	} else if !w.truncated && len(res.Elements) <= nearEmptyNodes {
		// Only an uncut tree this small is "nearly empty": a tree cut by a
		// budget is partial, not sparse (its note already says so).
		res.Notes = append(res.Notes, nearEmptyNote)
	}
	return res, nil
}

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
	truncated bool // any element carries a truncation marker
	reasons   [reasonCount]bool
}

func (w *walker[T]) visit(node T, data NodeData, depth int) {
	index := len(w.res.Elements)
	w.res.Elements = append(w.res.Elements, elementFrom(data, depth))
	w.res.Live = append(w.res.Live, node)
	if data.Focused && w.res.FocusedIndex < 0 {
		w.res.FocusedIndex = index
	}

	// Expansion. Depth first: a node at MaxDepth is rendered but never
	// expanded (its children would exceed the depth cap).
	if depth >= w.cfg.MaxDepth {
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
			w.reasons[reasonTime] = true
			w.stopped = true
		} else {
			w.reasons[reasonUnreadable] = true
		}
		return
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
	}
	w.drop(kids[limit:])
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
// fixed order so the text is stable.
func (w *walker[T]) reasonNote() (string, bool) {
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
	if len(parts) == 0 {
		return "", false
	}
	return partialNotePrefix + " " + strings.Join(parts, "; "), true
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
	if d.HasValue {
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
