package uia

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// The offline sparse-tree tests (ticket 14): recorded Chromium/Electron
// trees (test/e2e's HTML page predicted for Chrome, an Electron window that
// stays nearly empty) are fed through the lazy-accessibility retry policy,
// and the resulting Window State is asserted. The live behaviour (Chrome
// and Edge really exposing web content) is the user-run e2e test
// test/e2e/chromium_test.go.

// chromiumRootClass is the Win32 class of a Chromium-derived main window
// (Chrome, Edge, Electron), as the walk records it from the root element.
const chromiumRootClass = "Chrome_WidgetWin_1"

// fakeReader serves queued read results in order, recording the configs it
// was given; an entry in errs makes that read fail. Reads happen
// synchronously on the test goroutine, so t.Fatalf is safe inside read.
type fakeReader struct {
	t       *testing.T
	results []*readResult
	errs    []error
	configs []WalkConfig
	calls   int
}

func (r *fakeReader) read(cfg WalkConfig) (*readResult, error) {
	r.t.Helper()
	i := r.calls
	r.calls++
	r.configs = append(r.configs, cfg)
	if i < len(r.errs) && r.errs[i] != nil {
		return nil, r.errs[i]
	}
	if i >= len(r.results) {
		r.t.Fatalf("read %d: no queued result (only %d)", i, len(r.results))
	}
	return r.results[i], nil
}

// fakeSleep records the delays the policy asked to sleep and returns a
// canned error (nil by default).
type fakeSleep struct {
	delays []time.Duration
	err    error
}

func (s *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	s.delays = append(s.delays, d)
	return s.err
}

// loadReadFixture loads a recorded tree fixture and reports it as one read
// result with the given root class.
func loadReadFixture(t *testing.T, path, rootClass string) *readResult {
	t.Helper()
	tree, err := wstate.LoadFixture(path)
	if err != nil {
		t.Fatalf("loading fixture: %v", err)
	}
	return &readResult{tree: tree, rootClass: rootClass}
}

// readFixtureTree runs readTree over queued results and returns the
// rendered Window State plus the reader and sleeper for assertions.
func readFixtureTree(t *testing.T, app string, results []*readResult, sleepErr error) (string, *fakeReader, *fakeSleep) {
	t.Helper()
	r := &fakeReader{t: t, results: results}
	s := &fakeSleep{err: sleepErr}
	tree, err := readTree(context.Background(), app, time.Minute, r.read, s.sleep)
	if err != nil {
		t.Fatalf("readTree: %v", err)
	}
	return wstate.Render(tree), r, s
}

// assertElementLine checks that the Window State carries an Element line
// for (role, name) at any index: "\t...\t<N> <role> <name>".
func assertElementLine(t *testing.T, rendered, role, name string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\t+\d+ ` + regexp.QuoteMeta(role) + ` ` + regexp.QuoteMeta(name) + `\b`)
	if !re.MatchString(rendered) {
		t.Errorf("Window State missing the %s %q Element line:\n%s", role, name, rendered)
	}
}

func TestReadTreeRetriesNearEmptyChromiumFirstRead(t *testing.T) {
	first := loadReadFixture(t, "testdata/chromium-first-read.json", chromiumRootClass)
	second := loadReadFixture(t, "testdata/chromium-second-read.json", chromiumRootClass)
	// The recorded first read must be the sparse one (guards the fixture's
	// note text against drift from nearEmptyNote).
	if !treeIsSparse(first.tree) {
		t.Fatal("the recorded first read must carry the nearly-empty note")
	}

	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{first, second}, nil)

	// Exactly one retry, after the short delay.
	if reader.calls != 2 {
		t.Errorf("reads = %d, want exactly 2 (the first read plus one retry)", reader.calls)
	}
	if len(sleeper.delays) != 1 || sleeper.delays[0] != a11yRetryDelay {
		t.Errorf("sleep delays = %v, want exactly one %s", sleeper.delays, a11yRetryDelay)
	}
	// The retried Window State is the second read: the page's web content
	// (document, link, button, edit, check box) is exposed, with no sparse
	// note left.
	assertElementLine(t, rendered, "document", "winuse e2e page 14")
	assertElementLine(t, rendered, "link", "winuse e2e link")
	assertElementLine(t, rendered, "button", "winuse e2e button")
	assertElementLine(t, rendered, "edit", "winuse e2e input")
	assertElementLine(t, rendered, "check box", "winuse e2e checkbox")
	if strings.Contains(rendered, "nearly empty") {
		t.Errorf("a successful retry must not leave a sparse note:\n%s", rendered)
	}
	// Both reads ran under the walk's budget with the default caps.
	for i, cfg := range reader.configs {
		if cfg.Budget != time.Minute {
			t.Errorf("read %d budget = %s, want the full %s (no request deadline in tests)", i, cfg.Budget, time.Minute)
		}
		if cfg.MaxNodes != DefaultMaxNodes || cfg.MaxDepth != DefaultMaxDepth || cfg.MaxChildren != DefaultMaxChildren {
			t.Errorf("read %d config = %+v, want the default caps", i, cfg)
		}
	}
}

func TestReadTreePersistentSparseTreeGetsScreenshotNote(t *testing.T) {
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass)
	rendered, reader, sleeper := readFixtureTree(t, "electronapp.exe", []*readResult{sparse, sparse}, nil)

	if reader.calls != 2 {
		t.Errorf("reads = %d, want exactly 2 (the sparse tree is retried once)", reader.calls)
	}
	if len(sleeper.delays) != 1 {
		t.Errorf("sleep delays = %v, want one", sleeper.delays)
	}
	// The persistently sparse tree says so, and tells the agent to request
	// a screenshot: exactly one note, in the retry-aware wording.
	if got := strings.Count(rendered, "Note: "); got != 1 {
		t.Fatalf("note lines = %d, want exactly 1:\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "Note: "+sparseAfterRetryNote) {
		t.Errorf("the persistent sparse note must be %q, got:\n%s", sparseAfterRetryNote, rendered)
	}
	if !strings.Contains(sparseAfterRetryNote, "request a screenshot") {
		t.Errorf("the sparse note must point the agent at a screenshot")
	}
}

func TestReadTreeFullFirstReadIsNotRetried(t *testing.T) {
	full := loadReadFixture(t, "testdata/chromium-second-read.json", chromiumRootClass)
	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{full}, nil)

	if reader.calls != 1 || len(sleeper.delays) != 0 {
		t.Errorf("reads = %d, sleeps = %v; a full first read must not be retried", reader.calls, sleeper.delays)
	}
	assertElementLine(t, rendered, "link", "winuse e2e link")
}

func TestReadTreeChromiumDocumentlessCompleteTreeIsRetried(t *testing.T) {
	// The first read caught the tree mid-build: browser chrome, complete,
	// but no Document element yet.
	midBuild := &readResult{
		tree: &wstate.Tree{
			Title: "winuse e2e page 14 - Google Chrome",
			App:   "chrome.exe",
			Elements: []wstate.Element{
				{Depth: 0, Role: "window", Name: "winuse e2e page 14 - Google Chrome"},
				{Depth: 1, Role: "pane"},
				{Depth: 2, Role: "toolbar", Name: "Address and search bar"},
				{Depth: 3, Role: "button", Name: "Reload"},
			},
			FocusedIndex: -1,
		},
		rootClass: chromiumRootClass,
	}
	full := loadReadFixture(t, "testdata/chromium-second-read.json", chromiumRootClass)

	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{midBuild, full}, nil)

	if reader.calls != 2 || len(sleeper.delays) != 1 {
		t.Errorf("reads = %d, sleeps = %v; a documentless Chromium tree must be retried once", reader.calls, sleeper.delays)
	}
	assertElementLine(t, rendered, "document", "winuse e2e page 14")
}

func TestReadTreeChromiumDocumentlessPartialTreeIsNotRetried(t *testing.T) {
	// A partial (time-cut) Chromium tree without a Document: the Document
	// may be past the cut, so retrying is pointless — the partial tree with
	// its note is the answer.
	partial := &readResult{
		tree: &wstate.Tree{
			Title: "winuse e2e page 14 - Google Chrome",
			App:   "chrome.exe",
			Elements: []wstate.Element{
				{Depth: 0, Role: "window", Name: "winuse e2e page 14 - Google Chrome"},
				{Depth: 1, Role: "pane"},
			},
			FocusedIndex: -1,
			Notes:        []string{partialNotePrefix + " the tree read exceeded the 8s time limit."},
		},
		rootClass: chromiumRootClass,
	}
	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{partial}, nil)

	if reader.calls != 1 || len(sleeper.delays) != 0 {
		t.Errorf("reads = %d, sleeps = %v; a partial tree must not be retried", reader.calls, sleeper.delays)
	}
	if !strings.Contains(rendered, "Note: "+partialNotePrefix) {
		t.Errorf("the partial-tree note must survive:\n%s", rendered)
	}
}

func TestReadTreeNonChromiumDocumentlessTreeIsNotRetried(t *testing.T) {
	// A complete non-Chromium tree without a Document element (an ordinary
	// Win32 app): no retry, whatever the size.
	app := &readResult{
		tree: &wstate.Tree{
			Title: "winuse fixture app",
			App:   "winusefixture.exe",
			Elements: []wstate.Element{
				{Depth: 0, Role: "window", Name: "winuse fixture app"},
				{Depth: 1, Role: "button", Name: "Click Me", AutomationId: "button-click"},
				{Depth: 1, Role: "edit", Name: "Text Field", AutomationId: "text-field"},
			},
			FocusedIndex: -1,
		},
		rootClass: "WinUseFixtureWnd",
	}
	_, reader, sleeper := readFixtureTree(t, "winusefixture.exe", []*readResult{app}, nil)

	if reader.calls != 1 || len(sleeper.delays) != 0 {
		t.Errorf("reads = %d, sleeps = %v; an ordinary app tree must not be retried", reader.calls, sleeper.delays)
	}
}

func TestReadTreeChromiumByExeNameWithoutClass(t *testing.T) {
	// The root element reports no class name (some providers do): the App's
	// executable name is the second Chromium signal.
	midBuild := &readResult{
		tree: &wstate.Tree{
			Title: "winuse e2e page 14 - Google Chrome",
			App:   "chrome.exe",
			Elements: []wstate.Element{
				{Depth: 0, Role: "window", Name: "winuse e2e page 14 - Google Chrome"},
				{Depth: 1, Role: "toolbar"},
			},
			FocusedIndex: -1,
		},
		rootClass: "",
	}
	full := loadReadFixture(t, "testdata/chromium-second-read.json", chromiumRootClass)

	_, reader, sleeper := readFixtureTree(t, "Chrome.exe", []*readResult{midBuild, full}, nil)
	if reader.calls != 2 || len(sleeper.delays) != 1 {
		t.Errorf("reads = %d, sleeps = %v; exe-name matching must be case-insensitive and trigger the retry", reader.calls, sleeper.delays)
	}
}

func TestReadTreeFirstReadErrorPropagates(t *testing.T) {
	r := &fakeReader{t: t, errs: []error{ErrWindowGone}}
	s := &fakeSleep{}
	_, err := readTree(context.Background(), "chrome.exe", time.Minute, r.read, s.sleep)
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("readTree error = %v, want ErrWindowGone", err)
	}
	if len(s.delays) != 0 {
		t.Errorf("a failed first read must not sleep, got %v", s.delays)
	}
}

func TestReadTreeSecondReadErrorPropagates(t *testing.T) {
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse}, errs: []error{nil, ErrWindowGone}}
	s := &fakeSleep{}
	_, err := readTree(context.Background(), "electronapp.exe", time.Minute, r.read, s.sleep)
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("readTree error = %v, want the retry's ErrWindowGone", err)
	}
}

func TestReadTreeNoTimeForRetryReturnsFirstRead(t *testing.T) {
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse}}
	s := &fakeSleep{err: context.DeadlineExceeded}
	tree, err := readTree(context.Background(), "electronapp.exe", time.Minute, r.read, s.sleep)
	if err != nil {
		t.Fatalf("an interrupted retry must return the first read, got error %v", err)
	}
	if r.calls != 1 {
		t.Errorf("reads = %d, want 1 (the retry never ran)", r.calls)
	}
	// The first read stands with the walk's own nearly-empty note (no retry
	// happened, so the note is not upgraded).
	rendered := wstate.Render(tree)
	if !strings.Contains(rendered, "Note: "+nearEmptyNote) {
		t.Errorf("the unretried sparse tree keeps its note:\n%s", rendered)
	}
	if strings.Contains(rendered, "even after one retry") {
		t.Errorf("the note must not claim a retry that did not happen:\n%s", rendered)
	}
}

func TestReadTreeWalkBudgetRespectsRequestDeadline(t *testing.T) {
	// A tight request deadline caps each read's walk budget to the
	// remaining slack minus the response margin; the second read gets the
	// time that is left after the first read and the delay.
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r := &fakeReader{t: t, results: []*readResult{sparse, sparse}}
	s := &fakeSleep{}
	_, err := readTree(ctx, "electronapp.exe", time.Minute, r.read, s.sleep)
	if err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if len(r.configs) != 2 {
		t.Fatalf("configs = %d, want 2", len(r.configs))
	}
	for i, cfg := range r.configs {
		if cfg.Budget <= 0 || cfg.Budget > 4*time.Second-responseMargin {
			t.Errorf("read %d budget = %s, want it capped by the request deadline", i, cfg.Budget)
		}
	}
}

func TestReadTreeRetryDelayBoundedBySleep(t *testing.T) {
	// The policy asks to sleep for exactly a11yRetryDelay (the real sleep
	// honours the request deadline; that is sleepCtx's own contract).
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse, sparse}}
	s := &fakeSleep{}
	if _, err := readTree(context.Background(), "electronapp.exe", time.Minute, r.read, s.sleep); err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if len(s.delays) != 1 || s.delays[0] != a11yRetryDelay {
		t.Errorf("delays = %v, want [%s]", s.delays, a11yRetryDelay)
	}
}

func TestSleepCtxReturnsOnDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := sleepCtx(ctx, 10*time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sleepCtx error = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("sleepCtx took %s, it must return at the deadline", elapsed)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepCtx: %v", err)
	}
}

func TestNoteTextContracts(t *testing.T) {
	// The walk's nearly-empty note and the partial prefix are the shared
	// contract between the walk, the retry policy and recorded fixtures;
	// pin the exact strings.
	if nearEmptyNote != "this Window's tree is nearly empty; the app may expose little to UI Automation" {
		t.Errorf("nearEmptyNote drifted: %q", nearEmptyNote)
	}
	if partialNotePrefix != "this Window State is partial:" {
		t.Errorf("partialNotePrefix drifted: %q", partialNotePrefix)
	}
	if !strings.Contains(sparseAfterRetryNote, "even after one retry") || !strings.Contains(sparseAfterRetryNote, "request a screenshot") {
		t.Errorf("sparseAfterRetryNote must mention the retry and the screenshot: %q", sparseAfterRetryNote)
	}
	if got := wstate.RoleName(wstate.ControlTypeDocument); got != "document" {
		t.Errorf("document role = %q, want \"document\"", got)
	}
	// The retry delay is a bounded fraction of the default tree budget: one
	// retry may not more than halve a full-budget read sequence.
	if a11yRetryDelay <= 0 || a11yRetryDelay > DefaultTreeBudget/2 {
		t.Errorf("a11yRetryDelay %s must stay well under the default tree budget %s", a11yRetryDelay, DefaultTreeBudget)
	}
}

func TestIsChromiumSignals(t *testing.T) {
	cases := []struct {
		rootClass, app string
		want           bool
	}{
		{"Chrome_WidgetWin_1", "anything.exe", true},
		{"Chrome_WidgetWin_0", "anything.exe", true},
		{"", "msedge.exe", true},
		{"", "Chrome.exe", true},
		{"WinUseFixtureWnd", "winusefixture.exe", false},
		{"", "discord.exe", false}, // Electron apps are caught by class, not exe
		{"", "", false},
	}
	for _, tc := range cases {
		if got := isChromium(tc.rootClass, tc.app); got != tc.want {
			t.Errorf("isChromium(%q, %q) = %v, want %v", tc.rootClass, tc.app, got, tc.want)
		}
	}
}

// The recorded fixtures load and are marked as hand-authored (the live
// captures need a user-run recording).
func TestRetryFixturesAreMarkedHandAuthored(t *testing.T) {
	for _, path := range []string{
		"testdata/chromium-first-read.json",
		"testdata/chromium-second-read.json",
		"testdata/electron-sparse.json",
	} {
		tree, err := wstate.LoadFixture(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if tree.Format != wstate.FixtureFormat {
			t.Errorf("%s: format %d, want %d", path, tree.Format, wstate.FixtureFormat)
		}
		if !strings.Contains(tree.Source, "hand-authored") {
			t.Errorf("%s: source must mark the fixture as hand-authored, got %q", path, tree.Source)
		}
		if tree.FocusedIndex != -1 {
			t.Errorf("%s: focusedIndex = %d, want -1 (the zero value claims the root)", path, tree.FocusedIndex)
		}
		if len(tree.Elements) == 0 {
			t.Errorf("%s: no elements", path)
		}
	}
}

// A sanity check that the second-read fixture's depth order is valid:
// each element's depth is at most one more than its predecessor's.
func TestRetryFixtureDepthsAreWellFormed(t *testing.T) {
	for _, path := range []string{"testdata/chromium-first-read.json", "testdata/chromium-second-read.json", "testdata/electron-sparse.json"} {
		tree, err := wstate.LoadFixture(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		prev := -1
		for i, e := range tree.Elements {
			if e.Depth < 0 || e.Depth > prev+1 {
				t.Errorf("%s: element %d depth %d after depth %d", path, i, e.Depth, prev)
			}
			prev = e.Depth
		}
	}
}
