package uia

// extract on native lists and grids (bladebro ideas, ticket 27), the UI
// Automation half: find the list or grid, then read its rows in one
// bounded read, into an extract.NativeRead that internal/extract turns
// into rows. Ticket 13's probe set the rules:
//
//   - Detection reads the Window's tree as a Window State read does, but
//     never below a list or grid (prunedSource): a 22,915-row list is not
//     listed to find it. A combo box's drop-down list is no candidate.
//   - Table and Grid give only the headers (GetCurrentColumnHeaders) and
//     RowCount. GridPattern.GetItem is never called: it is broken in
//     Services and .NET 8 and scrolls a WPF grid.
//   - A grid whose RowCount is within the limit is read in one cached
//     request of its subtree (no live Elements); anything else, or a bulk
//     read that does not answer in time, is read one row at a time with
//     the row's subtree cached, and stops one data row past the limit or at
//     the time limit (stepRows). Nothing is ever asked for every row.
//   - A virtualized list's total comes from RowCount or, when the rows read
//     match it, from counting the ItemContainer's items (placeholders,
//     never Realized: Realize scrolls the App), bounded in time and count.
//   - It reads only: no focus, no scroll, no Element Indexes stored.

import (
	"context"
	"errors"
	"slices"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// Pattern ids, IIDs, slots and properties the table read adds (SDK
// UIAutomationClient.h 10.0.26100.0).
const (
	patternTable = 10012

	tableGetCurrentColumnHeaders = 4  // IUIAutomationTablePattern::GetCurrentColumnHeaders(**arr)
	elCurrentName                = 23 // IUIAutomationElement::get_CurrentName(*BSTR)

	treeScopeSubtree = 0x7 // TreeScope_Element|Children|Descendants

	propIsGridItemAvailable = 30029
	propIsGridAvailable     = 30030
	propIsTableAvailable    = 30038
	propGridRowCount        = 30062
	propGridItemColumn      = 30065
)

var iidTablePattern = windows.GUID{
	Data1: 0x620e691c, Data2: 0xea96, Data3: 0x4710,
	Data4: [8]byte{0xa8, 0x50, 0x75, 0x4b, 0x24, 0xce, 0x24, 0x17},
}

// boxProperties are cached on every Element detection reads, besides the
// Window State's: what tells a list or grid and its size.
var boxProperties = []uint32{propIsGridAvailable, propIsTableAvailable, propGridRowCount, propIsItemContainerAvailable}

// rowProperties are cached on a row and its subtree.
var rowProperties = []uint32{propControlType, propName, propIsValueAvailable, propValueValue, propIsGridItemAvailable, propGridItemColumn}

// The table read's bounds.
const (
	maxCellDepth = 4      // levels below a row read into its cells' text
	maxHeaders   = 200    // column headers read
	maxItems     = 100000 // ItemContainer items counted
	itemsBudget  = 2 * time.Second
)

// The reasons a row read stops early (extract.NativeRead.Stopped).
const (
	stoppedTime   = "stopped at the time limit"
	stoppedUnread = "stopped where a row did not read"
)

// ExtractNative reads hwnd's list or grid for extract: the Element behind
// root of the Agent's latest Window State (or the list holding it, when
// root is one of its items; -1: the Window), or the one detection picks
// under it (extract.PickContainer), and its rows up to one past limit,
// within budget. found is false when there is no list or grid.
func (c *Client) ExtractNative(ctx context.Context, hwnd uint64, root, limit int, budget time.Duration) (read extract.NativeRead, found bool, err error) {
	if limit <= 0 {
		limit = extract.NativeLimit
	}
	cfg := walkConfig(ctx, budget)
	deadline := time.Now().Add(cfg.Budget)
	cfg.Budget /= 2 // detection leaves half for the rows
	var jobErr error
	runErr := c.RunFor(ctx, hwnd, func(w *worker, r *run) {
		var start comPtr
		if root >= 0 {
			el, lerr := c.lookup(ctx, hwnd, root)
			if lerr != nil {
				jobErr = lerr
				return
			}
			defer c.store.release(el)
			start = el
		}
		if w.thread == nil {
			jobErr = ErrWindowGone // fake workers carry no COM thread
			return
		}
		read, found, jobErr = w.thread.extractNative(uintptr(hwnd), start, limit, cfg, deadline)
	})
	if runErr != nil {
		return extract.NativeRead{}, false, runErr
	}
	return read, found, jobErr
}

// extractNative is ExtractNative on the worker thread.
func (th *thread) extractNative(hwnd uintptr, start comPtr, limit int, cfg WalkConfig, deadline time.Time) (extract.NativeRead, bool, error) {
	src := &comSource{th: th, hwnd: hwnd, from: start}
	defer src.dispose()
	props := append(slices.Clone(treeProperties), boxProperties...)
	var err error
	if src.req, err = src.newRequest(props, treeScopeElementChildren, automationElementModeFull); err != nil {
		return extract.NativeRead{}, false, err
	}
	if src.stepReq, err = src.newRequest(props, treeScopeElement, automationElementModeFull); err != nil {
		return extract.NativeRead{}, false, err
	}
	if start != nil && isRow(currentState(start, propControlType)) {
		// One item of the list: read the list holding it.
		if parent := th.controlParentOf(start); parent != nil {
			defer release(parent)
			src.from = parent
		}
	}

	res, boxes, err := findBoxes[comPtr](src, comIsBox, cfg)
	defer func() {
		releaseAll(res.Live)
		releaseAll(res.Dropped)
	}()
	if err != nil {
		return extract.NativeRead{}, false, err
	}
	if len(boxes) == 0 {
		return extract.NativeRead{}, false, nil
	}
	pick := 0
	if len(boxes) > 1 {
		cands := make([]extract.Candidate, len(boxes))
		for i, b := range boxes {
			el := res.Live[b]
			cands[i] = extract.Candidate{Role: res.Elements[b].Role, Name: res.Elements[b].Name,
				Table: propBool(el, propIsGridAvailable) || propBool(el, propIsTableAvailable), Rows: src.boxRows(el, deadline)}
		}
		pick = extract.PickContainer(cands)
	}
	box, e := res.Live[boxes[pick]], res.Elements[boxes[pick]]
	return th.readBox(src, box, e, limit, deadline), true, nil
}

// readBox reads the chosen list or grid.
func (th *thread) readBox(src *comSource, box comPtr, e wstate.Element, limit int, deadline time.Time) extract.NativeRead {
	n := extract.NativeRead{Role: e.Role, Name: e.Name}
	grid, table := propBool(box, propIsGridAvailable), propBool(box, propIsTableAvailable)
	n.Table = grid || table
	if rc := propState(box, propGridRowCount); grid && rc > 0 {
		n.RowCount = int(rc)
	}
	if table {
		n.Headers = columnHeaders(box)
	}

	rows, bulk := 0, false
	if n.RowCount > 0 && n.RowCount <= limit {
		if kids, err := th.bulkRows(src, box, time.Until(deadline)); err == nil {
			n.Children, bulk = kids, true
			for _, k := range kids {
				rows += extract.Rows(k)
			}
		}
	}
	if !bulk {
		cr := &comRows{th: th, src: src}
		defer cr.dispose()
		got, last, ok := stepRows[comPtr](cr, box, limit, deadline, time.Now)
		n.Children, rows, n.Stopped = got.children, got.rows, got.stopped
		if ok {
			if rows > limit && n.RowCount == 0 {
				if size := currentState(last, propSizeOfSet); size > 0 {
					n.SetSize = int(size)
				}
			}
			release(last)
		}
	}

	if rows <= limit && n.Stopped == "" && (n.RowCount == 0 || n.RowCount <= rows) && propBool(box, propIsItemContainerAvailable) {
		n.Items, n.ItemsCut = countItems(box, min(time.Until(deadline), itemsBudget))
	}
	return n
}

// isRow says a control type is a list's item: --root on one reads its list.
func isRow(ct int32) bool {
	return ct == wstate.ControlTypeListItem || ct == wstate.ControlTypeDataItem || ct == wstate.ControlTypeTreeItem
}

// comIsBox says an Element detection read is a list or grid: the Grid or
// Table pattern (a calendar's month grid aside), or the List, DataGrid or
// Table control type.
func comIsBox(el comPtr) bool {
	switch ct := cachedInt32(el, elCachedControlType); ct {
	case wstate.ControlTypeCalendar:
		return false
	case wstate.ControlTypeList, wstate.ControlTypeDataGrid, wstate.ControlTypeTable:
		return true
	}
	return propBool(el, propIsGridAvailable) || propBool(el, propIsTableAvailable)
}

// boxRows is a candidate's size for the pick: its RowCount, else its
// child count within countBudget; -1 when that count did not come back in
// time (a long list).
func (s *comSource) boxRows(el comPtr, deadline time.Time) int {
	if propBool(el, propIsGridAvailable) {
		if rc := propState(el, propGridRowCount); rc >= 0 {
			return int(rc)
		}
	}
	n, err := s.CountChildren(el, max(min(time.Until(deadline), countBudget), time.Millisecond))
	if errors.Is(err, ErrChunkTimedOut) {
		return -1
	}
	return n
}

// controlParentOf is el's control-view parent, nil when it has none.
func (th *thread) controlParentOf(el comPtr) comPtr {
	var walker, parent comPtr
	if hr := call(th.auto, uiaGetControlViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
		return nil
	}
	defer release(walker)
	if hr := call(walker, walkerGetParent, uintptr(el), uintptr(unsafe.Pointer(&parent))); hr.Failed() {
		return nil
	}
	return parent
}

// columnHeaders reads the names of the Table pattern's column headers.
func columnHeaders(box comPtr) []string {
	pat, hr := currentPattern(box, patternTable, &iidTablePattern)
	if hr.Failed() || pat == nil {
		return nil
	}
	defer release(pat)
	var arr comPtr
	if hr := call(pat, tableGetCurrentColumnHeaders, uintptr(unsafe.Pointer(&arr))); hr.Failed() || arr == nil {
		return nil
	}
	defer release(arr)
	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n <= 0 {
		return nil
	}
	names := make([]string, 0, min(int(n), maxHeaders))
	for i := int32(0); i < n && len(names) < maxHeaders; i++ {
		var el comPtr
		if hr := call(arr, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
			names = append(names, "")
			continue
		}
		names = append(names, cachedBSTR(el, elCurrentName)) // a BSTR getter, the current one
		release(el)
	}
	return names
}

// bulkRows reads box's children with their subtrees in one cached request
// with no live Elements, bounded like a chunk: it fails with
// ErrChunkTimedOut when the provider does not answer in time.
func (th *thread) bulkRows(src *comSource, box comPtr, remaining time.Duration) ([]extract.Node, error) {
	if remaining <= 0 {
		return nil, ErrChunkTimedOut
	}
	req, err := src.newRequest(rowProperties, treeScopeSubtree, automationElementModeNone)
	if req != nil {
		defer release(req)
	}
	if err != nil {
		return nil, err
	}
	src.putTimeout(remaining)
	defer src.restoreTimeout()
	var fresh comPtr
	if hr := call(box, elBuildUpdatedCache, uintptr(req), uintptr(unsafe.Pointer(&fresh))); hr.Failed() || fresh == nil {
		return nil, chunkErr("reading the rows", hr)
	}
	defer release(fresh)
	return cachedNodes(fresh, 0), nil
}

// cachedNodes is el's cached children as Nodes, maxCellDepth levels down
// from depth.
func cachedNodes(el comPtr, depth int) []extract.Node {
	var arr comPtr
	if hr := call(el, elGetCachedChildren, uintptr(unsafe.Pointer(&arr))); hr.Failed() || arr == nil {
		return nil
	}
	defer release(arr)
	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n <= 0 {
		return nil
	}
	out := make([]extract.Node, 0, n)
	for i := int32(0); i < n; i++ {
		var kid comPtr
		if hr := call(arr, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&kid))); hr.Failed() || kid == nil {
			continue
		}
		out = append(out, cachedNode(kid, depth))
		release(kid)
	}
	return out
}

// cachedNode is one Element of a row read, from its cache (rowProperties),
// with its children down to maxCellDepth.
func cachedNode(el comPtr, depth int) extract.Node {
	n := extract.Node{Role: wstate.RoleName(cachedInt32(el, elCachedControlType)), Name: cachedBSTR(el, elCachedName)}
	if propBool(el, propIsValueAvailable) {
		v := propString(el, propValueValue)
		n.Value = &v
	}
	if propBool(el, propIsGridItemAvailable) {
		if col := int(propState(el, propGridItemColumn)); col >= 0 {
			n.Column = &col
		}
	}
	if depth < maxCellDepth {
		n.Children = cachedNodes(el, depth+1)
	}
	return n
}

// countItems counts box's items through ItemContainer.FindItemByProperty
// (property 0: every item, the virtualized placeholders too, none
// realized), within budget and maxItems; cut says it stopped before the
// end. Zero when the pattern is missing or its first step fails.
func countItems(box comPtr, budget time.Duration) (n int, cut bool) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return 0, false // the VARIANT argument is passed by pointer on x64 and arm64 only
	}
	pat, hr := currentPattern(box, patternItemContainer, &iidItemContainerPattern)
	if hr.Failed() || pat == nil {
		return 0, false
	}
	defer release(pat)
	deadline := time.Now().Add(budget)
	var prev comPtr
	defer func() { release(prev) }()
	var empty variant // VT_EMPTY: any value
	for {
		if n >= maxItems || time.Now().After(deadline) {
			return n, true
		}
		var found comPtr
		if hr := call(pat, icFindItemByProperty, uintptr(prev), 0, uintptr(unsafe.Pointer(&empty)), uintptr(unsafe.Pointer(&found))); hr.Failed() {
			return n, n > 0
		}
		if found == nil {
			return n, false
		}
		release(prev)
		prev = found
		n++
	}
}

// ---- the parts that need no COM ----

// prunedSource is a TreeSource that never reads below a box (a list or
// grid): detection sees every list without listing its rows.
type prunedSource[T any] struct {
	src   TreeSource[T]
	isBox func(T) bool
}

func (p *prunedSource[T]) Root() (T, NodeData, error) { return p.src.Root() }

func (p *prunedSource[T]) Children(node T, remaining time.Duration) ([]T, []NodeData, error) {
	if p.isBox(node) {
		return nil, nil, nil
	}
	return p.src.Children(node, remaining)
}

// errNoStepping answers a step when the source cannot step: the walk then
// leaves that node's children out with its note.
var errNoStepping = errors.New("this source reads no child alone")

func (p *prunedSource[T]) FirstChild(node T, remaining time.Duration) (T, NodeData, bool, error) {
	if st, ok := p.src.(ChildStepper[T]); ok && !p.isBox(node) {
		return st.FirstChild(node, remaining)
	}
	var zero T
	return zero, NodeData{}, false, errNoStepping
}

func (p *prunedSource[T]) NextSibling(prev T, remaining time.Duration) (T, NodeData, bool, error) {
	if st, ok := p.src.(ChildStepper[T]); ok {
		return st.NextSibling(prev, remaining)
	}
	var zero T
	return zero, NodeData{}, false, errNoStepping
}

func (p *prunedSource[T]) ItemsAfter(child T) (int, bool) {
	if st, ok := p.src.(ChildStepper[T]); ok {
		return st.ItemsAfter(child)
	}
	return 0, false
}

// findBoxes walks src without reading below a box and returns the walk and
// the Element Indexes of the boxes in it, but a combo box's drop-down
// list. The caller releases the walk's Live and Dropped handles.
func findBoxes[T any](src TreeSource[T], isBox func(T) bool, cfg WalkConfig) (WalkResult[T], []int, error) {
	res, err := Walk[T](&prunedSource[T]{src: src, isBox: isBox}, cfg)
	if err != nil {
		return res, nil, err
	}
	var boxes, stack []int
	for i, e := range res.Elements {
		if len(stack) > e.Depth {
			stack = stack[:e.Depth]
		}
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, i)
		if isBox(res.Live[i]) && (parent < 0 || res.Elements[parent].Role != wstate.RoleName(wstate.ControlTypeComboBox)) {
			boxes = append(boxes, i)
		}
	}
	return res, boxes, nil
}

// rowStepper reads a container's children one at a time, each with its
// subtree: the first, then the one after prev; ok false past the last.
type rowStepper[T any] interface {
	firstRow(box T, remaining time.Duration) (T, extract.Node, bool, error)
	nextRow(prev T, remaining time.Duration) (T, extract.Node, bool, error)
	release(T)
}

// stepped is what stepRows read.
type stepped struct {
	children []extract.Node
	rows     int    // data rows among them (extract.Rows)
	stopped  string // stoppedTime, stoppedUnread or ""
}

// stepRows reads box's children until one data row past limit, the last
// child, the deadline, or a child that does not read. Each handle is
// released once the next is read; the last one read is returned (ok) for
// the caller to read its set size and release.
func stepRows[T any](src rowStepper[T], box T, limit int, deadline time.Time, now func() time.Time) (out stepped, last T, ok bool) {
	for out.rows <= limit {
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			out.stopped = stoppedTime
			break
		}
		var el T
		var n extract.Node
		var more bool
		var err error
		if ok {
			el, n, more, err = src.nextRow(last, remaining)
		} else {
			el, n, more, err = src.firstRow(box, remaining)
		}
		if err != nil {
			out.stopped = stoppedUnread
			if errors.Is(err, ErrChunkTimedOut) || !deadline.After(now()) {
				out.stopped = stoppedTime
			}
			break
		}
		if !more {
			break
		}
		if ok {
			src.release(last)
		}
		last, ok = el, true
		out.children = append(out.children, n)
		out.rows += extract.Rows(n)
	}
	return out, last, ok
}

// comRows steps through a container's children over COM: the control-view
// tree walker, each child with its subtree cached (rowProperties), live so
// the next step can start from it.
type comRows struct {
	th  *thread
	src *comSource
	req comPtr
}

func (r *comRows) dispose() {
	if r.req != nil {
		release(r.req)
		r.req = nil
	}
}

func (r *comRows) firstRow(box comPtr, remaining time.Duration) (comPtr, extract.Node, bool, error) {
	return r.step(walkerGetFirstChildBuildCache, box, remaining)
}

func (r *comRows) nextRow(prev comPtr, remaining time.Duration) (comPtr, extract.Node, bool, error) {
	return r.step(walkerGetNextSiblingBuildCache, prev, remaining)
}

func (r *comRows) release(el comPtr) { release(el) }

func (r *comRows) step(slot uintptr, from comPtr, remaining time.Duration) (comPtr, extract.Node, bool, error) {
	if r.req == nil {
		req, err := r.src.newRequest(rowProperties, treeScopeSubtree, automationElementModeFull)
		r.req = req
		if err != nil {
			return nil, extract.Node{}, false, err
		}
	}
	if err := r.src.ensureStepping(); err != nil { // the tree walker
		return nil, extract.Node{}, false, err
	}
	r.src.putTimeout(remaining)
	defer r.src.restoreTimeout()
	var el comPtr
	if hr := call(r.src.walker, slot, uintptr(from), uintptr(r.req), uintptr(unsafe.Pointer(&el))); hr.Failed() {
		return nil, extract.Node{}, false, chunkErr("reading a row", hr)
	}
	if el == nil {
		return nil, extract.Node{}, false, nil
	}
	return el, cachedNode(el, 0), true, nil
}
