package uia

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// Chromium and Electron apps expose their web content through UI
// Automation, but only lazily: Chromium enables its accessibility tree when
// a UI Automation client first appears (the first cache build is what
// switches it on), so the first read of a Chrome, Edge or Electron window
// is often nearly empty and the web content shows up only in a read that
// happens after a short delay (spec: Observation details; uia-go notes
// §5). readTree implements that policy around one Window's reads:
//
//   - a Chromium/Electron window whose complete tree has thin web content
//     (no Document element, or one with almost nothing under it) and whose
//     Page is not read over a Browser Connection is settled: it is read
//     again every chromiumSettleEvery until the Documents grow and then
//     stop growing, within a settle budget (ticket 18 of
//     .scratch/headless-max: a cold Chromium can take 10-25 s, and a fully
//     covered one may not build its web tree at all). A tree still thin at
//     the end carries the thin-page note, and the Window is remembered: its
//     later thin reads get the one short retry below instead of the whole
//     budget, until a read finds it built;
//   - any other nearly empty first read (the walk's sparse note) is
//     retried once after a short delay, so the tree the app has meanwhile
//     built is read;
//   - a tree that is still nearly empty after the retry is reported with
//     the sparse-after-retry note, which tells the agent to request a
//     screenshot instead (canvas apps expose little; spec user story 22).
//
// Neither ever activates or uncovers the Window to make it build.

// a11yRetryDelay is the delay before the retry: long enough for Chromium
// to switch its accessibility tree on and build it after the first contact,
// short enough to stay well inside the request budget (research proposal
// 300–500 ms, uia-go notes §5; the value is 500 ms).
const a11yRetryDelay = 500 * time.Millisecond

// DefaultChromiumSettle is the settle budget for a thin Chromium tree: how
// long readTree keeps polling for the web content to build. Capture passes
// it, or the wider one a caller-set timeout (desktop-parity ticket 14) put
// on the context with WithSettle, through readTree's settle parameter. The
// request's own deadline always bounds it.
const DefaultChromiumSettle = 5 * time.Second

// settleKey is the context key of a caller-set settle budget.
type settleKey struct{}

// WithSettle returns ctx carrying a Chromium settle budget for the reads
// made under it (get-window-state --timeout widens it). A budget at or
// under DefaultChromiumSettle leaves the default.
func WithSettle(ctx context.Context, settle time.Duration) context.Context {
	return context.WithValue(ctx, settleKey{}, settle)
}

// SettleOf is the Chromium settle budget of a read under ctx: the one
// WithSettle set when it is wider, else DefaultChromiumSettle.
func SettleOf(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(settleKey{}).(time.Duration); ok && d > DefaultChromiumSettle {
		return d
	}
	return DefaultChromiumSettle
}

// chromiumSettleEvery is the settle loop's poll interval (the ticket's
// 250-500 ms): each poll is one tree read.
const chromiumSettleEvery = 300 * time.Millisecond

// settleReadReserve is kept between the last settle poll and the response
// margin, so the poll's read still has time to walk.
const settleReadReserve = time.Second

// thinDocumentNodes is the web content size below which a Chromium tree is
// thin: fewer Elements than this under its Document elements (none at all
// when there is no Document). A cold or covered Chromium exposes its
// Document empty or not at all; a built page has far more.
const thinDocumentNodes = 3

// chromiumClassPrefix marks Chromium-derived apps (Chrome, Edge, Electron
// and the other Chromium-based browsers): their main windows use Win32
// classes like "Chrome_WidgetWin_1".
const chromiumClassPrefix = "Chrome_"

// chromiumExeNames are the executable names of the known Chromium-based
// browsers, as the helper passes them (the App's exe file name). A second
// Chromium signal for Windows whose root element reports no class name;
// Electron apps are caught by the class name instead, since their
// executable names are arbitrary.
var chromiumExeNames = map[string]bool{
	"chrome.exe":   true,
	"chromium.exe": true,
	"msedge.exe":   true,
	"brave.exe":    true,
	"opera.exe":    true,
	"vivaldi.exe":  true,
}

// documentRole is the role of Chromium's web-content root element: the
// page itself is exposed as a Document under the browser chrome.
var documentRole = wstate.RoleName(wstate.ControlTypeDocument)

// sparseAfterRetryNote replaces the walk's nearly-empty note when the tree
// stays nearly empty across the retry: it says so and points the agent at
// the screenshot, which is the only useful observation for such a Window
// (spec user story 22).
const sparseAfterRetryNote = "this Window's tree is nearly empty even after one retry; request a screenshot to see its content (Chromium and Electron expose web content lazily, and canvas apps expose little to UI Automation)"

// thinPageNote is the note on a Chromium tree that stayed thin through the
// whole settle. Chromium builds its web tree only while the Window renders;
// a fully covered Window may never build it, and winuse never uncovers it.
// An Electron or WebView2 App has no Browser Connection, so it gets the
// screenshot instead.
const thinPageNote = "the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered. Connect the browser (winuse browser status says how) or uncover the Window; for an Electron or WebView2 App, request a screenshot"

// treeRead performs one bounded tree read. cfg is the walk's budget
// (bounded to the request's remaining time by walkConfig).
type treeRead func(cfg WalkConfig) (*readResult, error)

// readResult is one completed read as the retry policy sees it.
type readResult struct {
	tree      *wstate.Tree
	rootClass string // the root element's UIA ClassName
	paged     bool   // a Document was left out: its Page is read over CDP
}

// readTree reads one Window's tree through read, applying the
// Chromium/Electron settle and the lazy-accessibility retry. app is the
// App's exe file name (header App line); budget is the per-read walk
// budget; settle is the Chromium settle budget (DefaultChromiumSettle, or
// a caller's wider one). mark remembers that this Window already stayed
// thin through a settle, so its later thin reads get the one retry, not
// the whole budget again; a zero mark remembers nothing. sleep is
// injectable for tests.
func readTree(ctx context.Context, app string, budget, settle time.Duration, mark thinMark, read treeRead, sleep func(context.Context, time.Duration) error) (*wstate.Tree, error) {
	first, err := read(walkConfig(ctx, budget))
	if err != nil {
		return nil, err
	}
	// A skeleton leaves the Documents' content out by design, so it always
	// looks thin: settling it would spend the whole budget waiting for
	// content it never reads (perf ticket 03), and it says nothing about
	// whether the page has built, so it leaves the mark as it is.
	if skeleton(ctx) {
		if !treeIsSparse(first.tree) {
			return first.tree, nil
		}
	} else if worthSettle(first, app) {
		if mark.marked() {
			return retryThin(ctx, first, budget, mark, read, sleep)
		}
		return settleTree(ctx, first, budget, settle, mark, read, sleep)
	} else if !first.paged {
		mark.forget()
	}
	if !treeIsSparse(first.tree) {
		return first.tree, nil
	}
	// The retry is bounded by the request's own deadline: if there is no
	// time left for it, the first read stands (with its note).
	if serr := sleep(ctx, a11yRetryDelay); serr != nil {
		return first.tree, nil
	}
	second, err := read(walkConfig(ctx, budget))
	if err != nil {
		// The first read succeeded but the retry failed (the Window closed
		// mid-retry, COM failed): report the failure rather than a tree
		// that may be stale.
		return nil, err
	}
	if treeIsSparse(second.tree) {
		noteRetry(second.tree)
	}
	return second.tree, nil
}

// settleTree polls a thin Chromium tree until its web content has built:
// the Documents grow and then stop growing. It stops early when a read
// leaves the Document to a Browser Connection or comes back partial (too
// big for the walk, so not thin). A tree still thin when the polls run out
// gets the thin-page note; one that grew but had not stopped growing is
// returned as it is. A poll that fails fails the read, as the retry does.
// A Window that ends thin is marked, so the next read of it does not pay
// the budget again; one that built is unmarked.
func settleTree(ctx context.Context, first *readResult, budget, settle time.Duration, mark thinMark, read treeRead, sleep func(context.Context, time.Duration) error) (*wstate.Tree, error) {
	last, size, grew := first, documentSize(first.tree), false
	for polls := settlePolls(ctx, settle); polls > 0; polls-- {
		if serr := sleep(ctx, chromiumSettleEvery); serr != nil {
			break
		}
		next, err := read(walkConfig(ctx, budget))
		if err != nil {
			return nil, err
		}
		last = next
		if next.paged {
			return next.tree, nil
		}
		if !treeIsComplete(next.tree) {
			mark.forget()
			return next.tree, nil
		}
		n := documentSize(next.tree)
		if grew && n <= size {
			markBuilt(mark, next.tree)
			return next.tree, nil
		}
		if n > size {
			grew = true
		}
		size = n
	}
	if isThin(last.tree) {
		noteThin(last.tree)
		mark.remember()
	} else {
		mark.forget()
	}
	return last.tree, nil
}

// retryThin reads a Window that already stayed thin through a settle once
// more after the short retry delay, as ADR 0005's one retry: a Chrome
// popup, an empty page or a canvas-only Electron App pays 500 ms per read,
// not the whole settle budget. A tree still thin carries the thin-page
// note; one that built clears the mark, so a later thin spell settles
// again.
func retryThin(ctx context.Context, first *readResult, budget time.Duration, mark thinMark, read treeRead, sleep func(context.Context, time.Duration) error) (*wstate.Tree, error) {
	if serr := sleep(ctx, a11yRetryDelay); serr != nil {
		noteThin(first.tree)
		return first.tree, nil
	}
	second, err := read(walkConfig(ctx, budget))
	if err != nil {
		return nil, err
	}
	if second.paged {
		return second.tree, nil
	}
	if treeIsComplete(second.tree) && isThin(second.tree) {
		noteThin(second.tree)
		return second.tree, nil
	}
	mark.forget()
	return second.tree, nil
}

// markBuilt updates the mark after a settled read: a tree that settled
// thin (it grew a little and stopped, still under thinDocumentNodes) is
// noted and marked like one that never grew; a built one is unmarked.
func markBuilt(mark thinMark, t *wstate.Tree) {
	if isThin(t) {
		noteThin(t)
		mark.remember()
		return
	}
	mark.forget()
}

// maxThinWindows bounds the thin-Window memory. HWNDs of closed Windows
// are never pruned one by one; when the memory is full an arbitrary mark
// is dropped, which costs that Window one more settle at worst.
const maxThinWindows = 256

// thinWindows remembers the Windows whose settle ran out while they were
// still thin (keyed by HWND). It is the Client's; reads of different
// Windows may run concurrently, so it is locked.
type thinWindows struct {
	mu sync.Mutex
	m  map[uint64]struct{}
}

func newThinWindows() *thinWindows {
	return &thinWindows{m: map[uint64]struct{}{}}
}

// window is the mark of one Window. A nil memory gives a zero mark.
func (t *thinWindows) window(hwnd uint64) thinMark {
	return thinMark{set: t, hwnd: hwnd}
}

// thinMark is one Window's entry in the thin-Window memory. The zero
// value (no memory) is never marked and ignores remember and forget.
type thinMark struct {
	set  *thinWindows
	hwnd uint64
}

func (m thinMark) marked() bool {
	if m.set == nil {
		return false
	}
	m.set.mu.Lock()
	defer m.set.mu.Unlock()
	_, ok := m.set.m[m.hwnd]
	return ok
}

func (m thinMark) remember() {
	if m.set == nil {
		return
	}
	m.set.mu.Lock()
	defer m.set.mu.Unlock()
	if _, ok := m.set.m[m.hwnd]; !ok && len(m.set.m) >= maxThinWindows {
		for h := range m.set.m {
			delete(m.set.m, h)
			break
		}
	}
	m.set.m[m.hwnd] = struct{}{}
}

func (m thinMark) forget() {
	if m.set == nil {
		return
	}
	m.set.mu.Lock()
	defer m.set.mu.Unlock()
	delete(m.set.m, m.hwnd)
}

// settlePolls is how many polls fit in the settle budget, which the
// request deadline bounds: each poll needs its interval, and the last one
// the read's reserve before the response margin. Polls are counted, not
// timed, so a faked sleep keeps the tests instant.
func settlePolls(ctx context.Context, settle time.Duration) int {
	if d, ok := ctx.Deadline(); ok {
		if left := time.Until(d) - responseMargin - settleReadReserve; left < settle {
			settle = left
		}
	}
	if settle <= 0 {
		return 0
	}
	return int(settle / chromiumSettleEvery)
}

// worthSettle reports whether a read is a thin Chromium/Electron tree the
// settle loop should wait on: a Chromium Window read whole (a partial
// tree's Document may simply be past the cut), whose web content is not
// read over a Browser Connection, and whose Documents are thin.
func worthSettle(r *readResult, app string) bool {
	return isChromium(r.rootClass, app) && !r.paged && treeIsComplete(r.tree) && isThin(r.tree)
}

// isChromium reports whether the Window belongs to a Chromium-derived app,
// by the root window's Win32 class or the App's executable name.
func isChromium(rootClass, app string) bool {
	return strings.HasPrefix(rootClass, chromiumClassPrefix) || chromiumExeNames[strings.ToLower(app)]
}

// treeIsSparse reports whether the Window State is a nearly empty one: the
// walk marked it so with its nearly-empty note (the note is the shared
// contract between the walk, the retry policy and recorded fixtures).
func treeIsSparse(t *wstate.Tree) bool {
	for _, n := range t.Notes {
		if n == nearEmptyNote {
			return true
		}
	}
	return false
}

// treeIsComplete reports whether the walk delivered the whole tree: no
// partial-tree note and no truncation marker.
func treeIsComplete(t *wstate.Tree) bool {
	for _, n := range t.Notes {
		if strings.HasPrefix(n, partialNotePrefix) {
			return false
		}
	}
	for i := range t.Elements {
		if t.Elements[i].Truncated != nil {
			return false
		}
	}
	return true
}

// documentSize counts the Elements under the tree's Document Elements
// (Chromium's web-content roots), each counted once: a Document nested in
// another (an iframe) counts as part of the outer one.
func documentSize(t *wstate.Tree) int {
	n, inside := 0, -1 // inside: the depth of the Document being counted
	for i := range t.Elements {
		e := &t.Elements[i]
		if inside >= 0 && e.Depth > inside {
			n++
			continue
		}
		inside = -1
		if e.Role == documentRole {
			inside = e.Depth
		}
	}
	return n
}

// isThin reports whether the tree's web content is thin: fewer than
// thinDocumentNodes Elements under its Documents.
func isThin(t *wstate.Tree) bool {
	return documentSize(t) < thinDocumentNodes
}

// noteRetry upgrades a tree that stayed nearly empty across the retry: the
// walk's nearly-empty note is replaced with the retry-aware wording that
// tells the agent to request a screenshot.
func noteRetry(t *wstate.Tree) {
	for i, n := range t.Notes {
		if n == nearEmptyNote {
			t.Notes[i] = sparseAfterRetryNote
		}
	}
}

// noteThin marks a Chromium tree that stayed thin through the settle with
// the thin-page note, replacing the walk's nearly-empty note when it has
// one, so the agent gets one note that says what to do.
func noteThin(t *wstate.Tree) {
	for i, n := range t.Notes {
		if n == nearEmptyNote {
			t.Notes[i] = thinPageNote
			return
		}
	}
	t.Notes = append(t.Notes, thinPageNote)
}

// sleepCtx pauses for d, returning early with ctx's error if the context
// ends first (a client that gave up, or the request deadline).
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
