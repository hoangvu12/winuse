package uia

import (
	"context"
	"strings"
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
//   - a nearly empty first read (the walk's sparse note) is retried once
//     after a short delay, so the tree Chromium has meanwhile built is read;
//   - a Chromium/Electron window whose tree is complete but carries no
//     Document element (the web content's root) is retried once too: the
//     first read may have caught the tree mid-build;
//   - a tree that is still nearly empty after the retry is reported with
//     the sparse-after-retry note, which tells the agent to request a
//     screenshot instead (canvas apps and some Electron apps never expose
//     more; spec user story 22).

// a11yRetryDelay is the delay before the retry: long enough for Chromium
// to switch its accessibility tree on and build it after the first contact,
// short enough to stay well inside the request budget (research proposal
// 300–500 ms, uia-go notes §5; the value is 500 ms).
const a11yRetryDelay = 500 * time.Millisecond

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

// treeRead performs one bounded tree read. cfg is the walk's budget
// (bounded to the request's remaining time by walkConfig).
type treeRead func(cfg WalkConfig) (*readResult, error)

// readResult is one completed read as the retry policy sees it.
type readResult struct {
	tree      *wstate.Tree
	rootClass string // the root element's UIA ClassName
}

// readTree reads one Window's tree through read, applying the
// Chromium/Electron lazy-accessibility retry. app is the App's exe file
// name (header App line); budget is the per-read walk budget. sleep is
// injectable for tests.
func readTree(ctx context.Context, app string, budget time.Duration, read treeRead, sleep func(context.Context, time.Duration) error) (*wstate.Tree, error) {
	first, err := read(walkConfig(ctx, budget))
	if err != nil {
		return nil, err
	}
	if !worthRetry(first, app) {
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

// worthRetry reports whether a read should be retried once. Either the
// tree is nearly empty (any app: the accessibility tree may be switching
// on), or the Window is Chromium/Electron and its tree is complete but
// carries no Document element, so the web content has not been exposed
// yet. A partial (cut) tree is never retried on the second signal: the
// Document may simply be past the cut.
func worthRetry(r *readResult, app string) bool {
	if treeIsSparse(r.tree) {
		return true
	}
	return treeIsComplete(r.tree) && isChromium(r.rootClass, app) && !hasDocument(r.tree)
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

// hasDocument reports whether the tree carries a Document Element
// (Chromium's web-content root).
func hasDocument(t *wstate.Tree) bool {
	for i := range t.Elements {
		if t.Elements[i].Role == documentRole {
			return true
		}
	}
	return false
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
