package uia

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// maxTransactionTimeout caps each cache chunk's UIA transaction timeout
// (re-put before every chunk to min(remaining, this)); the default is 20 s,
// far too long for a bounded walk. 2 s follows the project research
// (uia-go notes §5).
const maxTransactionTimeout = 2 * time.Second

// captureResult is one completed tree read on the worker thread: the
// Window State, the live elements behind it (the caller moves them into
// the live-Element store), and the root element's ClassName.
type captureResult struct {
	tree      *wstate.Tree
	live      []comPtr
	rootClass string // the root element's UIA ClassName, for Chromium/Electron detection
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
	req       comPtr // cache request, created on first use
	rootClass string // the root element's ClassName, read in Root (Chromium/Electron detection)
}

// ensureRequest lazily builds the cache request: the properties of the basic
// Window State, control-view children, Full element mode.
func (s *comSource) ensureRequest() error {
	if s.req != nil {
		return nil
	}
	var req comPtr
	if hr := call(s.th.auto, uiaCreateCacheRequest, uintptr(unsafe.Pointer(&req))); hr.Failed() || req == nil {
		return fmt.Errorf("%w: creating the cache request failed (HRESULT %s)", ErrInit, hr)
	}
	s.req = req
	for _, prop := range treeProperties {
		if hr := call(req, reqAddProperty, uintptr(prop)); hr.Failed() {
			return fmt.Errorf("AddProperty(%d) failed (HRESULT %s)", prop, hr)
		}
	}
	var cond comPtr
	if hr := call(s.th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return fmt.Errorf("get_ControlViewCondition failed (HRESULT %s)", hr)
	}
	defer release(cond) // the request holds its own reference
	if hr := call(req, reqPutTreeFilter, uintptr(cond)); hr.Failed() {
		return fmt.Errorf("put_TreeFilter failed (HRESULT %s)", hr)
	}
	if hr := call(req, reqPutTreeScope, treeScopeElementChildren); hr.Failed() {
		return fmt.Errorf("put_TreeScope failed (HRESULT %s)", hr)
	}
	if hr := call(req, reqPutAutomationElementMode, automationElementModeFull); hr.Failed() {
		return fmt.Errorf("put_AutomationElementMode failed (HRESULT %s)", hr)
	}
	return nil
}

// dispose releases the cache request (the only object comSource owns).
func (s *comSource) dispose() {
	if s.req != nil {
		release(s.req)
		s.req = nil
	}
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
	if hr := call(s.th.auto, uiaElementFromHandleBuildCache, s.hwnd, uintptr(unsafe.Pointer(&s.req)), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
		return nil, NodeData{}, fmt.Errorf("%w: reading the Window's tree failed (HRESULT %s)", ErrWindowGone, hr)
	}
	s.rootClass = cachedBSTR(el, elCachedClassName)
	return el, s.data(el), nil
}

// Children fetches node's control-view children (with cached properties) in
// one chunk: BuildUpdatedCache(node) + GetCachedChildren. The fresh node and
// the array are released before returning; the children are the caller's.
//
// remaining bounds the chunk: the UIA transaction timeout is set to
// min(remaining, maxTransactionTimeout), so a provider that does not answer
// in time fails with UIA_E_TIMEOUT, mapped to ErrChunkTimedOut (the walk
// then returns the partial tree with the time-limit note).
func (s *comSource) Children(node comPtr, remaining time.Duration) ([]comPtr, []NodeData, error) {
	tx := remaining
	if tx > maxTransactionTimeout {
		tx = maxTransactionTimeout
	}
	if tx < time.Millisecond {
		tx = time.Millisecond
	}
	call(s.th.auto, uia2PutTransactionTimeout, uintptr(uint32(tx.Milliseconds())))

	var fresh comPtr
	if hr := call(node, elBuildUpdatedCache, uintptr(unsafe.Pointer(&s.req)), uintptr(unsafe.Pointer(&fresh))); hr.Failed() || fresh == nil {
		if hr == uiaErrTimeout {
			return nil, nil, fmt.Errorf("%w (HRESULT %s)", ErrChunkTimedOut, hr)
		}
		return nil, nil, fmt.Errorf("updating the element cache failed (HRESULT %s)", hr)
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

	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n < 0 {
		return nil, nil, nil
	}
	kids := make([]comPtr, 0, n)
	for i := int32(0); i < n; i++ {
		var el comPtr
		if hr := call(arr, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
			continue
		}
		kids = append(kids, el)
	}
	data := make([]NodeData, len(kids))
	for i, k := range kids {
		data[i] = s.data(k)
	}
	return kids, data, nil
}

// data reads an element's cached properties (in-process reads from the
// cache; no provider round trip). The pattern properties come through
// GetCachedPropertyValue as VARIANTs (value.go's prop* readers).
func (s *comSource) data(el comPtr) NodeData {
	return NodeData{
		ControlType:  cachedInt32(el, elCachedControlType),
		Name:         cachedBSTR(el, elCachedName),
		AutomationId: cachedBSTR(el, elCachedAutomationId),
		HelpText:     cachedBSTR(el, elCachedHelpText),
		Enabled:      cachedBool(el, elCachedIsEnabled),
		Focused:      cachedBool(el, elCachedHasKeyboardFocus),

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
