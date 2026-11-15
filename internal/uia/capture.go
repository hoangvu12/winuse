package uia

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// maxTransactionTimeout caps each cache chunk's UIA transaction timeout
// (re-put before every chunk to min(remaining, this)); the default is 20 s,
// far too long for a bounded walk. 2 s follows the project research
// (uia-go notes §5).
const maxTransactionTimeout = 2 * time.Second

// workerTransactionTimeout is the worker's UIA transaction timeout outside
// a cache chunk: put at worker start and restored after every chunk, so a
// root read, Invoke or SetValue never runs under UIA's 20 s default or
// under the few milliseconds a chunk near its time limit left behind
// (reliability-council 01).
const workerTransactionTimeout = maxTransactionTimeout

// putTransactionTimeout puts auto's (IUIAutomation2) transaction timeout.
func putTransactionTimeout(auto comPtr, tx time.Duration) {
	call(auto, uia2PutTransactionTimeout, uintptr(uint32(tx.Milliseconds())))
}

// captureResult is one completed tree read on the worker thread: the
// Window State, the live elements behind it (the caller moves them into
// the live-Element store), and the root element's ClassName.
type captureResult struct {
	tree      *wstate.Tree
	live      []comPtr
	rootClass string // the root element's UIA ClassName, for Chromium/Electron detection
	cut       int    // the Document Element whose descendants were left out (WalkConfig.Cut), or -1
	partial   bool   // the walk did not see the whole Window (WalkResult.Partial)
}

// capture reads one Window's tree on the worker thread: it builds the cache
// request scoped to the Window, walks it through Walk, releases everything
// not kept live, and returns the Window State plus the live elements
// parallel to its Element Indexes.
func (th *thread) capture(hwnd uintptr, title, app string, cfg WalkConfig) (*captureResult, error) {
	src := &comSource{th: th, hwnd: hwnd}
	defer src.dispose()

	res, err := Walk[comPtr](src, cfg)
	// Handles the walk never visited are ours; release them (the visited
	// ones move to the live-Element store via the caller).
	for _, p := range res.Dropped {
		release(p)
	}
	if err != nil {
		return nil, err
	}
	return &captureResult{
		tree: &wstate.Tree{
			Title:        title,
			App:          app,
			Elements:     res.Elements,
			FocusedIndex: res.FocusedIndex,
			Notes:        res.Notes,
		},
		live:      res.Live,
		rootClass: src.rootClass,
		cut:       res.Cut,
		partial:   res.Partial,
	}, nil
}

// comSource is the TreeSource over raw COM calls. Its methods run on the
// worker thread only.
//
// The chunking follows the bounded-read design (uia-go notes §5): one cache
// request (TreeScope Element|Children, TreeFilter ControlViewCondition,
// AutomationElementMode Full) applied per node via BuildUpdatedCache, so the
// walk costs one cross-process round trip per expanded node and the target
// app pumps messages between chunks — never a single bulk Subtree read
// (Codex's freeze class).
type comSource struct {
	th        *thread
	hwnd      uintptr
	from      comPtr // a region read's root Element (region.go); nil: the Window's
	req       comPtr // cache request, created on first use
	rootClass string // the root element's ClassName, read in Root (Chromium/Electron detection)

	// Created on first use, for a slow list and the skeleton's counts
	// (learn-from 08).
	stepReq  comPtr // the tree properties for one Element alone (TreeScope_Element)
	countReq comPtr // no properties, control-view children, no live Elements
	walker   comPtr // the control-view tree walker
}

// ensureRequest lazily builds the cache request: the properties of the basic
// Window State, control-view children, Full element mode.
func (s *comSource) ensureRequest() error {
	if s.req != nil {
		return nil
	}
	req, err := s.newRequest(treeProperties, treeScopeElementChildren, automationElementModeFull)
	s.req = req
	return err
}

// newRequest builds a cache request over the control view with the given
// properties, scope and element mode. The request is returned even with an
// error once created, so the caller's dispose releases it.
func (s *comSource) newRequest(props []uint32, scope, mode uintptr) (comPtr, error) {
	var req comPtr
	if hr := call(s.th.auto, uiaCreateCacheRequest, uintptr(unsafe.Pointer(&req))); hr.Failed() || req == nil {
		return nil, fmt.Errorf("%w: creating the cache request failed (HRESULT %s)", ErrInit, hr)
	}
	for _, prop := range props {
		if hr := call(req, reqAddProperty, uintptr(prop)); hr.Failed() {
			return req, fmt.Errorf("AddProperty(%d) failed (HRESULT %s)", prop, hr)
		}
	}
	var cond comPtr
	if hr := call(s.th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return req, fmt.Errorf("get_ControlViewCondition failed (HRESULT %s)", hr)
	}
	defer release(cond) // the request holds its own reference
	if hr := call(req, reqPutTreeFilter, uintptr(cond)); hr.Failed() {
		return req, fmt.Errorf("put_TreeFilter failed (HRESULT %s)", hr)
	}
	if hr := call(req, reqPutTreeScope, scope); hr.Failed() {
		return req, fmt.Errorf("put_TreeScope failed (HRESULT %s)", hr)
	}
	if hr := call(req, reqPutAutomationElementMode, mode); hr.Failed() {
		return req, fmt.Errorf("put_AutomationElementMode failed (HRESULT %s)", hr)
	}
	return req, nil
}

// dispose releases the cache requests and the tree walker (the only
// objects comSource owns).
func (s *comSource) dispose() {
	for _, p := range []*comPtr{&s.req, &s.stepReq, &s.countReq, &s.walker} {
		if *p != nil {
			release(*p)
			*p = nil
		}
	}
}

// putTimeout bounds the next cross-process call: the UIA transaction
// timeout is set to chunkTimeout(remaining). The caller defers
// restoreTimeout, which puts the worker's timeout back (reliability-council
// 01), so a root read, Invoke or SetValue never runs under what a chunk or
// a step near its time limit left behind.
func (s *comSource) putTimeout(remaining time.Duration) {
	putTransactionTimeout(s.th.auto, chunkTimeout(remaining))
}

// restoreTimeout puts the worker's transaction timeout back.
func (s *comSource) restoreTimeout() {
	putTransactionTimeout(s.th.auto, workerTransactionTimeout)
}

// chunkErr maps a failed cache call's HRESULT: UIA_E_TIMEOUT is
// ErrChunkTimedOut.
func chunkErr(what string, hr HRESULT) error {
	if hr == uiaErrTimeout {
		return fmt.Errorf("%w (HRESULT %s)", ErrChunkTimedOut, hr)
	}
	return fmt.Errorf("%s failed (HRESULT %s)", what, hr)
}

// Root returns the Window's root element with its properties cached,
// scoped to the target HWND. It also records the root element's ClassName:
// Chromium-derived apps (Chrome, Edge, Electron) build their main windows
// with Win32 classes like "Chrome_WidgetWin_1", which is what the retry
// policy (retry.go) keys its Chromium detection on.
func (s *comSource) Root() (comPtr, NodeData, error) {
	if err := s.ensureRequest(); err != nil {
		return nil, NodeData{}, err
	}
	var el comPtr
	if s.from != nil {
		// A region read: the Element itself, its properties cached afresh.
		if hr := call(s.from, elBuildUpdatedCache, uintptr(s.req), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
			if hrStale(hr) {
				return nil, NodeData{}, staleErr(hr)
			}
			return nil, NodeData{}, fmt.Errorf("reading the Element failed (HRESULT %s)", hr)
		}
		return el, s.data(el), nil
	}
	if hr := call(s.th.auto, uiaElementFromHandleBuildCache, s.hwnd, uintptr(s.req), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
		return nil, NodeData{}, rootReadErr(s.hwnd, hr)
	}
	s.rootClass = cachedBSTR(el, elCachedClassName)
	return el, s.data(el), nil
}

// isWindow reports whether hwnd still names a Window (user32 IsWindow);
// tests replace it so they never touch a real Window.
var isWindow = func(hwnd uintptr) bool {
	return windows.IsWindow(windows.HWND(hwnd))
}

// rootReadErr classifies a failed root read (ElementFromHandleBuildCache):
// ErrWindowGone only when the Window no longer exists, else a
// TreeReadError with the HRESULT (reliability-council 04: a provider
// rebuilding its tree fails for a moment while the Window stays).
func rootReadErr(hwnd uintptr, hr HRESULT) error {
	if !isWindow(hwnd) {
		return fmt.Errorf("%w: reading the Window's tree failed (HRESULT %s)", ErrWindowGone, hr)
	}
	return &TreeReadError{HR: hr}
}

// chunkRetryDelay is the pause before a chunk that failed with E_FAIL is
// read once more (reliability-council 04, cua's 40 ms): a provider
// rebuilding the subtree answers again by then.
const chunkRetryDelay = 40 * time.Millisecond

// chunkTimeout is a chunk's UIA transaction timeout:
// min(remaining, maxTransactionTimeout), at least 1 ms.
func chunkTimeout(remaining time.Duration) time.Duration {
	tx := remaining
	if tx > maxTransactionTimeout {
		tx = maxTransactionTimeout
	}
	if tx < time.Millisecond {
		tx = time.Millisecond
	}
	return tx
}

// Children fetches node's control-view children (with cached properties) in
// one chunk: BuildUpdatedCache(node) + GetCachedChildren. The fresh node and
// the array are released before returning; the children are the caller's.
//
// remaining bounds the chunk: the UIA transaction timeout is set to
// min(remaining, maxTransactionTimeout), so a provider that does not answer
// in time fails with UIA_E_TIMEOUT, mapped to ErrChunkTimedOut (the walk
// then reads the node's children one at a time through FirstChild and
// NextSibling). The worker's timeout (workerTransactionTimeout) is put back
// when the chunk ends. A cache build that fails with E_FAIL is tried once
// more after chunkRetryDelay when the time allows; a second failure is the
// chunk's error, which the walk marks unreadable.
func (s *comSource) Children(node comPtr, remaining time.Duration) ([]comPtr, []NodeData, error) {
	s.putTimeout(remaining)
	defer s.restoreTimeout()

	var fresh comPtr
	hr := call(node, elBuildUpdatedCache, uintptr(s.req), uintptr(unsafe.Pointer(&fresh)))
	if hr == eFail && remaining > 2*chunkRetryDelay {
		time.Sleep(chunkRetryDelay)
		s.putTimeout(remaining - chunkRetryDelay)
		fresh = nil
		hr = call(node, elBuildUpdatedCache, uintptr(s.req), uintptr(unsafe.Pointer(&fresh)))
	}
	if hr.Failed() || fresh == nil {
		return nil, nil, chunkErr("updating the element cache", hr)
	}
	defer release(fresh)

	var arr comPtr
	if hr := call(fresh, elGetCachedChildren, uintptr(unsafe.Pointer(&arr))); hr.Failed() {
		return nil, nil, fmt.Errorf("reading cached children failed (HRESULT %s)", hr)
	}
	if arr == nil {
		return nil, nil, nil // no cached children: a leaf
	}
	defer release(arr)

	// A failed or negative length, or a child that did not read, is an
	// error with the children that did read: the walk keeps them and marks
	// the read partial (reliability-council 05), so a node is never passed
	// off as a leaf or a complete list.
	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() {
		return nil, nil, fmt.Errorf("reading the cached children's count failed (HRESULT %s)", hr)
	}
	if n < 0 {
		return nil, nil, fmt.Errorf("the cached children's count is %d", n)
	}
	kids := make([]comPtr, 0, n)
	var err error
	for i := int32(0); i < n; i++ {
		var el comPtr
		if hr := call(arr, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
			if err == nil {
				err = fmt.Errorf("reading cached child %d of %d failed (HRESULT %s)", i, n, hr)
			}
			continue
		}
		kids = append(kids, el)
	}
	data := make([]NodeData, len(kids))
	for i, k := range kids {
		data[i] = s.data(k)
	}
	return kids, data, err
}

// The COM source reads a slow list one Element at a time and counts
// without properties.
var (
	_ ChildStepper[comPtr] = (*comSource)(nil)
	_ ChildCounter[comPtr] = (*comSource)(nil)
)

// ensureStepping lazily builds what a slow list's one-at-a-time read needs:
// the control-view tree walker and a cache request for one Element.
func (s *comSource) ensureStepping() error {
	if s.walker == nil {
		var walker comPtr
		if hr := call(s.th.auto, uiaGetControlViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
			return fmt.Errorf("get_ControlViewWalker failed (HRESULT %s)", hr)
		}
		s.walker = walker
	}
	if s.stepReq == nil {
		req, err := s.newRequest(treeProperties, treeScopeElement, automationElementModeFull)
		s.stepReq = req
		return err
	}
	return nil
}

// FirstChild reads node's first control-view child with its properties
// cached: one round trip, bounded like a chunk (ChildStepper).
func (s *comSource) FirstChild(node comPtr, remaining time.Duration) (comPtr, NodeData, bool, error) {
	return s.step(walkerGetFirstChildBuildCache, node, remaining)
}

// NextSibling reads prev's next control-view sibling (ChildStepper).
func (s *comSource) NextSibling(prev comPtr, remaining time.Duration) (comPtr, NodeData, bool, error) {
	return s.step(walkerGetNextSiblingBuildCache, prev, remaining)
}

func (s *comSource) step(slot uintptr, from comPtr, remaining time.Duration) (comPtr, NodeData, bool, error) {
	if err := s.ensureStepping(); err != nil {
		return nil, NodeData{}, false, err
	}
	s.putTimeout(remaining)
	defer s.restoreTimeout()
	var el comPtr
	if hr := call(s.walker, slot, uintptr(from), uintptr(s.stepReq), uintptr(unsafe.Pointer(&el))); hr.Failed() {
		return nil, NodeData{}, false, chunkErr("reading the next child", hr)
	}
	if el == nil {
		return nil, NodeData{}, false, nil // no such child
	}
	return el, s.data(el), true, nil
}

// ItemsAfter reads how many items of its set follow child: its live
// SizeOfSet minus its PositionInSet, when its provider gives both.
func (s *comSource) ItemsAfter(child comPtr) (int, bool) {
	size, pos := currentState(child, propSizeOfSet), currentState(child, propPositionInSet)
	if size <= 0 || pos <= 0 || pos > size {
		return 0, false
	}
	return int(size - pos), true
}

// CountChildren counts node's control-view children with a cache request
// that asks for no property and no live Element (AutomationElementMode
// None): the provider still lists them, but nothing is read per child and
// nothing is kept (ChildCounter). remaining bounds it like a chunk.
func (s *comSource) CountChildren(node comPtr, remaining time.Duration) (int, error) {
	if s.countReq == nil {
		req, err := s.newRequest(nil, treeScopeElementChildren, automationElementModeNone)
		s.countReq = req
		if err != nil {
			return 0, err
		}
	}
	s.putTimeout(remaining)
	defer s.restoreTimeout()
	var fresh comPtr
	if hr := call(node, elBuildUpdatedCache, uintptr(s.countReq), uintptr(unsafe.Pointer(&fresh))); hr.Failed() || fresh == nil {
		return 0, chunkErr("counting the children", hr)
	}
	defer release(fresh)
	var arr comPtr
	if hr := call(fresh, elGetCachedChildren, uintptr(unsafe.Pointer(&arr))); hr.Failed() {
		return 0, fmt.Errorf("reading cached children failed (HRESULT %s)", hr)
	}
	if arr == nil {
		return 0, nil
	}
	defer release(arr)
	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n < 0 {
		return 0, nil
	}
	return int(n), nil
}

// data reads an element's cached properties (in-process reads from the
// cache; no provider round trip). The pattern properties come through
// GetCachedPropertyValue as VARIANTs (value.go's prop* readers).
func (s *comSource) data(el comPtr) NodeData {
	password, read := propBoolRead(el, propIsPassword)
	return NodeData{
		ControlType:  cachedInt32(el, elCachedControlType),
		Name:         cachedBSTR(el, elCachedName),
		AutomationId: cachedBSTR(el, elCachedAutomationId),
		HelpText:     cachedBSTR(el, elCachedHelpText),
		Enabled:      cachedBool(el, elCachedIsEnabled),
		Focused:      cachedBool(el, elCachedHasKeyboardFocus),

		IsPassword:     password,
		PasswordUnread: !read,

		HasWindow:        propBool(el, propIsWindowAvailable),
		HasScroll:        propBool(el, propIsScrollAvailable),
		ScrollH:          propBool(el, propScrollHScrollable),
		ScrollV:          propBool(el, propScrollVScrollable),
		HasExpand:        propBool(el, propIsExpandCollapseAvailable),
		ExpandState:      propState(el, propExpandCollapseState),
		HasToggle:        propBool(el, propIsToggleAvailable),
		ToggleState:      propState(el, propToggleState),
		HasSelectionItem: propBool(el, propIsSelectionItemAvailable),
		Selected:         propBool(el, propSelectionItemIsSelected),
		HasValue:         propBool(el, propIsValueAvailable),
		ValueReadOnly:    propBool(el, propValueIsReadOnly),
		ValueValue:       propString(el, propValueValue),
		HasRangeValue:    propBool(el, propIsRangeValueAvailable),
	}
}

// cachedBSTR reads a get_Cached* BSTR getter and frees the BSTR.
func cachedBSTR(el comPtr, slot uintptr) string {
	var p *uint16
	call(el, slot, uintptr(unsafe.Pointer(&p)))
	s := bstrToString(p)
	freeBSTR(p)
	return s
}

// cachedBool reads a get_Cached* BOOL getter (BOOL is a 4-byte int).
func cachedBool(el comPtr, slot uintptr) bool {
	var v int32
	call(el, slot, uintptr(unsafe.Pointer(&v)))
	return v != 0
}

// cachedInt32 reads a get_Cached* int32 getter.
func cachedInt32(el comPtr, slot uintptr) int32 {
	var v int32
	call(el, slot, uintptr(unsafe.Pointer(&v)))
	return v
}
