package uia

import (
	"context"
	"errors"
	"regexp"
	"strconv"
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

// canvasRootClass is the Win32 class of a non-Chromium canvas app's main
// window: its nearly empty tree takes the one-retry path, not the settle.
const canvasRootClass = "WinUseCanvasWnd"

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
	tree, err := readTree(context.Background(), app, time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
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

func TestReadTreeSettlesNearEmptyChromiumFirstRead(t *testing.T) {
	first := loadReadFixture(t, "testdata/chromium-first-read.json", chromiumRootClass)
	second := loadReadFixture(t, "testdata/chromium-second-read.json", chromiumRootClass)
	// The recorded first read must be the sparse one (guards the fixture's
	// note text against drift from nearEmptyNote).
	if !treeIsSparse(first.tree) {
		t.Fatal("the recorded first read must carry the nearly-empty note")
	}

	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{first, second, second}, nil)

	// The settle: the tree grows on the first poll and is the same on the
	// second, which ends it.
	if reader.calls != 3 {
		t.Errorf("reads = %d, want 3 (the first read, the poll it grew on, the poll it stopped growing on)", reader.calls)
	}
	if len(sleeper.delays) != 2 || sleeper.delays[0] != chromiumSettleEvery || sleeper.delays[1] != chromiumSettleEvery {
		t.Errorf("sleep delays = %v, want two of %s", sleeper.delays, chromiumSettleEvery)
	}
	// The settled Window State is the last read: the page's web content
	// (document, link, button, edit, check box) is exposed, with no sparse
	// note left.
	assertElementLine(t, rendered, "document", "winuse e2e page 14")
	assertElementLine(t, rendered, "link", "winuse e2e link")
	assertElementLine(t, rendered, "button", "winuse e2e button")
	assertElementLine(t, rendered, "edit", "winuse e2e input")
	assertElementLine(t, rendered, "check box", "winuse e2e checkbox")
	if strings.Contains(rendered, "nearly empty") || strings.Contains(rendered, "still thin") {
		t.Errorf("a settled tree must not carry a sparse or thin note:\n%s", rendered)
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
	// A non-Chromium canvas app: one retry, then the screenshot note.
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", canvasRootClass)
	rendered, reader, sleeper := readFixtureTree(t, "canvasapp.exe", []*readResult{sparse, sparse}, nil)

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

func TestReadTreeChromiumDocumentlessCompleteTreeIsSettled(t *testing.T) {
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

	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{midBuild, full, full}, nil)

	if reader.calls != 3 || len(sleeper.delays) != 2 {
		t.Errorf("reads = %d, sleeps = %v; a documentless Chromium tree must be settled", reader.calls, sleeper.delays)
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

	_, reader, sleeper := readFixtureTree(t, "Chrome.exe", []*readResult{midBuild, full, full}, nil)
	if reader.calls != 3 || len(sleeper.delays) != 2 {
		t.Errorf("reads = %d, sleeps = %v; exe-name matching must be case-insensitive and trigger the settle", reader.calls, sleeper.delays)
	}
}

func TestReadTreeFirstReadErrorPropagates(t *testing.T) {
	r := &fakeReader{t: t, errs: []error{ErrWindowGone}}
	s := &fakeSleep{}
	_, err := readTree(context.Background(), "chrome.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("readTree error = %v, want ErrWindowGone", err)
	}
	if len(s.delays) != 0 {
		t.Errorf("a failed first read must not sleep, got %v", s.delays)
	}
}

func TestReadTreeSecondReadErrorPropagates(t *testing.T) {
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", canvasRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse}, errs: []error{nil, ErrWindowGone}}
	s := &fakeSleep{}
	_, err := readTree(context.Background(), "canvasapp.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("readTree error = %v, want the retry's ErrWindowGone", err)
	}
}

func TestReadTreeNoTimeForRetryReturnsFirstRead(t *testing.T) {
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", canvasRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse}}
	s := &fakeSleep{err: context.DeadlineExceeded}
	tree, err := readTree(context.Background(), "canvasapp.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
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
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", canvasRootClass)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r := &fakeReader{t: t, results: []*readResult{sparse, sparse}}
	s := &fakeSleep{}
	_, err := readTree(ctx, "canvasapp.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
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
	sparse := loadReadFixture(t, "testdata/electron-sparse.json", canvasRootClass)
	r := &fakeReader{t: t, results: []*readResult{sparse, sparse}}
	s := &fakeSleep{}
	if _, err := readTree(context.Background(), "canvasapp.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep); err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if len(s.delays) != 1 || s.delays[0] != a11yRetryDelay {
		t.Errorf("delays = %v, want [%s]", s.delays, a11yRetryDelay)
	}
}

// The settle tests (ticket 18 of .scratch/headless-max): a faked Chromium
// tree whose web content grows from read to read, or never does (a fully
// covered cold Chrome), fed through the settle loop with a faked sleep.

// chromeRead is one read of a Chrome Window whose Document holds n
// Elements (no Document at all when n < 0).
func chromeRead(n int) *readResult {
	els := []wstate.Element{
		{Depth: 0, Role: "window", Name: "winuse e2e page 14 - Google Chrome"},
		{Depth: 1, Role: "pane"},
		{Depth: 2, Role: "toolbar", Name: "Address and search bar"},
		{Depth: 3, Role: "button", Name: "Reload"},
	}
	if n >= 0 {
		els = append(els, wstate.Element{Depth: 2, Role: "document", Name: "winuse e2e page 14"})
		for i := 0; i < n; i++ {
			els = append(els, wstate.Element{Depth: 3, Role: "button", Name: "winuse e2e button " + strconv.Itoa(i)})
		}
	}
	return &readResult{
		tree: &wstate.Tree{
			Title:        "winuse e2e page 14 - Google Chrome",
			App:          "chrome.exe",
			Elements:     els,
			FocusedIndex: -1,
		},
		rootClass: chromiumRootClass,
	}
}

// chromeReads is one chromeRead per size, in order.
func chromeReads(sizes ...int) []*readResult {
	rs := make([]*readResult, len(sizes))
	for i, n := range sizes {
		rs[i] = chromeRead(n)
	}
	return rs
}

func TestReadTreeSettleWaitsForGrowthToStop(t *testing.T) {
	// The web content builds over a few polls, then stops growing: the
	// settle returns the first read after it stopped.
	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", chromeReads(0, 0, 2, 6, 10, 10), nil)

	if reader.calls != 6 {
		t.Errorf("reads = %d, want 6 (it stopped growing on the sixth)", reader.calls)
	}
	if len(sleeper.delays) != 5 {
		t.Errorf("sleeps = %v, want 5 polls", sleeper.delays)
	}
	for _, d := range sleeper.delays {
		if d != chromiumSettleEvery {
			t.Errorf("poll delay %s, want %s", d, chromiumSettleEvery)
		}
	}
	assertElementLine(t, rendered, "button", "winuse e2e button 9")
	if strings.Contains(rendered, "Note: ") {
		t.Errorf("a settled tree carries no note:\n%s", rendered)
	}
}

func TestReadTreeSettleNeverSettlesGetsThinNote(t *testing.T) {
	// A fully covered cold Chrome: the Document stays empty for the whole
	// settle budget. Every poll fits in the budget, and the thin tree is
	// returned with the thin-page note, once.
	polls := int(DefaultChromiumSettle / chromiumSettleEvery)
	sizes := make([]int, polls+1)
	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", chromeReads(sizes...), nil)

	if reader.calls != polls+1 {
		t.Errorf("reads = %d, want %d (the first read plus every poll)", reader.calls, polls+1)
	}
	var slept time.Duration
	for _, d := range sleeper.delays {
		slept += d
	}
	if slept > DefaultChromiumSettle {
		t.Errorf("the settle slept %s, more than its %s budget", slept, DefaultChromiumSettle)
	}
	if got := strings.Count(rendered, "Note: "); got != 1 {
		t.Fatalf("note lines = %d, want exactly 1:\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "Note: "+thinPageNote) {
		t.Errorf("the never-settled tree must carry %q:\n%s", thinPageNote, rendered)
	}
}

func TestReadTreeSettleElectronSparseGetsOneThinNote(t *testing.T) {
	// A persistently sparse Electron window (Chromium class): settled, and
	// the walk's nearly-empty note is replaced by the thin-page note, which
	// also names the screenshot.
	sparse := func() *readResult { return loadReadFixture(t, "testdata/electron-sparse.json", chromiumRootClass) }
	polls := int(DefaultChromiumSettle / chromiumSettleEvery)
	results := make([]*readResult, polls+1)
	for i := range results {
		results[i] = sparse()
	}
	rendered, reader, _ := readFixtureTree(t, "electronapp.exe", results, nil)

	if reader.calls != polls+1 {
		t.Errorf("reads = %d, want %d", reader.calls, polls+1)
	}
	if got := strings.Count(rendered, "Note: "); got != 1 {
		t.Fatalf("note lines = %d, want exactly 1:\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "Note: "+thinPageNote) {
		t.Errorf("the sparse Electron tree must carry the thin-page note:\n%s", rendered)
	}
	if !strings.Contains(thinPageNote, "request a screenshot") {
		t.Errorf("the thin-page note must name the screenshot for Electron: %q", thinPageNote)
	}
}

func TestReadTreeSettleStillGrowingAtTheEndHasNoNote(t *testing.T) {
	// The tree grew past thin and was still growing when the budget ran
	// out: it is returned as it is, without the thin note.
	polls := int(DefaultChromiumSettle / chromiumSettleEvery)
	sizes := make([]int, polls+1)
	for i := range sizes {
		sizes[i] = i
	}
	rendered, reader, _ := readFixtureTree(t, "chrome.exe", chromeReads(sizes...), nil)

	if reader.calls != polls+1 {
		t.Errorf("reads = %d, want %d", reader.calls, polls+1)
	}
	assertElementLine(t, rendered, "button", "winuse e2e button "+strconv.Itoa(polls-1))
	if strings.Contains(rendered, "Note: ") {
		t.Errorf("a grown tree must not be called thin:\n%s", rendered)
	}
}

func TestReadTreeSettleStopsWhenThePageTakesOver(t *testing.T) {
	// The Browser Connection's Page read ends during the settle: the next
	// poll leaves the Document out (paged), which ends the settle.
	paged := chromeRead(0)
	paged.paged = true
	rendered, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{chromeRead(0), paged}, nil)

	if reader.calls != 2 || len(sleeper.delays) != 1 {
		t.Errorf("reads = %d, sleeps = %v; a paged poll must end the settle", reader.calls, sleeper.delays)
	}
	if strings.Contains(rendered, "Note: ") {
		t.Errorf("a paged read gets no thin note:\n%s", rendered)
	}
}

func TestReadTreePagedFirstReadIsNotSettled(t *testing.T) {
	// A Window whose Page is read over a Browser Connection: its Document
	// was left out, so it reads thin, and that is not a cold tree.
	paged := chromeRead(0)
	paged.paged = true
	_, reader, sleeper := readFixtureTree(t, "chrome.exe", []*readResult{paged}, nil)

	if reader.calls != 1 || len(sleeper.delays) != 0 {
		t.Errorf("reads = %d, sleeps = %v; a paged read must not be settled", reader.calls, sleeper.delays)
	}
}

func TestReadTreeSettleStopsOnPartialPoll(t *testing.T) {
	// A poll that comes back partial is too big for the walk, so not thin:
	// it ends the settle with its own note.
	partial := chromeRead(5)
	partial.tree.Notes = []string{partialNotePrefix + " the tree read exceeded the 8s time limit."}
	rendered, reader, _ := readFixtureTree(t, "chrome.exe", []*readResult{chromeRead(0), partial}, nil)

	if reader.calls != 2 {
		t.Errorf("reads = %d, want 2", reader.calls)
	}
	if !strings.Contains(rendered, "Note: "+partialNotePrefix) || strings.Contains(rendered, "still thin") {
		t.Errorf("the partial poll's note must stand alone:\n%s", rendered)
	}
}

func TestReadTreeWarmChromiumIsNotSettled(t *testing.T) {
	_, reader, sleeper := readFixtureTree(t, "chrome.exe", chromeReads(thinDocumentNodes), nil)
	if reader.calls != 1 || len(sleeper.delays) != 0 {
		t.Errorf("reads = %d, sleeps = %v; a built page must not be settled", reader.calls, sleeper.delays)
	}
}

func TestReadTreeSettlePollErrorPropagates(t *testing.T) {
	r := &fakeReader{t: t, results: chromeReads(0), errs: []error{nil, ErrWindowGone}}
	s := &fakeSleep{}
	_, err := readTree(context.Background(), "chrome.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("readTree error = %v, want the poll's ErrWindowGone", err)
	}
}

func TestReadTreeSettleInterruptedSleepReturnsLastRead(t *testing.T) {
	// The request ends during a poll's sleep: the last read stands, with
	// the thin note since it is thin.
	r := &fakeReader{t: t, results: chromeReads(0)}
	s := &fakeSleep{err: context.DeadlineExceeded}
	tree, err := readTree(context.Background(), "chrome.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep)
	if err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if r.calls != 1 {
		t.Errorf("reads = %d, want 1", r.calls)
	}
	if !strings.Contains(wstate.Render(tree), "Note: "+thinPageNote) {
		t.Errorf("the thin tree must carry the thin note:\n%s", wstate.Render(tree))
	}
}

func TestReadTreeSettleBoundedByRequestDeadline(t *testing.T) {
	// A 4 s request leaves room for fewer polls than the 5 s settle: the
	// polls stop with the read reserve and the response margin still left.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	sizes := make([]int, 40)
	r := &fakeReader{t: t, results: chromeReads(sizes...)}
	s := &fakeSleep{}
	if _, err := readTree(ctx, "chrome.exe", time.Minute, DefaultChromiumSettle, thinMark{}, r.read, s.sleep); err != nil {
		t.Fatalf("readTree: %v", err)
	}
	var slept time.Duration
	for _, d := range s.delays {
		slept += d
	}
	if limit := 4*time.Second - responseMargin - settleReadReserve; slept > limit || slept == 0 {
		t.Errorf("the settle slept %s, want some polls and at most %s", slept, limit)
	}
	for i, cfg := range r.configs {
		if cfg.Budget <= 0 || cfg.Budget > 4*time.Second-responseMargin {
			t.Errorf("read %d budget = %s, want it capped by the request deadline", i, cfg.Budget)
		}
	}
	if got := settlePolls(ctx, time.Minute); got > int((4*time.Second-responseMargin-settleReadReserve)/chromiumSettleEvery) {
		t.Errorf("a wide settle must still be bounded by the deadline: %d polls", got)
	}
}

func TestReadTreeWiderSettleWaitsLonger(t *testing.T) {
	// A caller-set settle (desktop-parity ticket 14's --timeout) widens the
	// loop past the default when the request has the time.
	wide := 2 * DefaultChromiumSettle
	polls := int(wide / chromiumSettleEvery)
	r := &fakeReader{t: t, results: chromeReads(make([]int, polls+1)...)}
	s := &fakeSleep{}
	if _, err := readTree(context.Background(), "chrome.exe", time.Minute, wide, thinMark{}, r.read, s.sleep); err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if r.calls != polls+1 {
		t.Errorf("reads = %d, want %d under a %s settle", r.calls, polls+1, wide)
	}
	if settlePolls(context.Background(), 0) != 0 {
		t.Error("a zero settle must not poll")
	}
}

// readMarked runs readTree over queued results with the given mark, for
// the thin-Window memory tests.
func readMarked(t *testing.T, mark thinMark, results []*readResult) (string, *fakeReader, *fakeSleep) {
	t.Helper()
	r := &fakeReader{t: t, results: results}
	s := &fakeSleep{}
	tree, err := readTree(context.Background(), "chrome.exe", time.Minute, DefaultChromiumSettle, mark, r.read, s.sleep)
	if err != nil {
		t.Fatalf("readTree: %v", err)
	}
	return wstate.Render(tree), r, s
}

// neverSettles is the reads of a Window that stays thin for a whole settle.
func neverSettles() []*readResult {
	return chromeReads(make([]int, int(DefaultChromiumSettle/chromiumSettleEvery)+1)...)
}

func TestReadTreeThinWindowSettlesOnceThenRetries(t *testing.T) {
	// A Chrome popup or an empty page: the first read pays the settle and
	// marks the Window; the second gets one short retry, not the loop, and
	// still carries the thin-page note.
	mark := newThinWindows().window(7)

	_, reader, _ := readMarked(t, mark, neverSettles())
	if reader.calls != len(neverSettles()) {
		t.Fatalf("first read: reads = %d, want the whole settle (%d)", reader.calls, len(neverSettles()))
	}
	if !mark.marked() {
		t.Fatal("a Window that stayed thin through the settle must be marked")
	}

	rendered, reader, sleeper := readMarked(t, mark, chromeReads(0, 0))
	if reader.calls != 2 {
		t.Errorf("second read: reads = %d, want 2 (one retry)", reader.calls)
	}
	if len(sleeper.delays) != 1 || sleeper.delays[0] != a11yRetryDelay {
		t.Errorf("second read: sleeps = %v, want one %s", sleeper.delays, a11yRetryDelay)
	}
	if got := strings.Count(rendered, "Note: "); got != 1 || !strings.Contains(rendered, "Note: "+thinPageNote) {
		t.Errorf("the retried thin tree must carry the thin-page note once:\n%s", rendered)
	}
	if !mark.marked() {
		t.Error("a Window still thin after the retry stays marked")
	}
}

func TestReadTreeMarkClearsAfterNonThinRead(t *testing.T) {
	mark := newThinWindows().window(7)
	mark.remember()

	// The retry finds the page built: the mark clears and no note.
	rendered, _, _ := readMarked(t, mark, chromeReads(0, 10))
	if mark.marked() {
		t.Error("a retry that finds the Window built must clear the mark")
	}
	if strings.Contains(rendered, "Note: ") {
		t.Errorf("a built tree carries no note:\n%s", rendered)
	}

	// Thin again later: it settles again (the whole loop).
	_, reader, _ := readMarked(t, mark, neverSettles())
	if reader.calls != len(neverSettles()) {
		t.Errorf("reads = %d; an unmarked Window that goes thin again must settle again", reader.calls)
	}

	// A first read that is already built clears the mark too.
	mark.remember()
	_, reader, _ = readMarked(t, mark, chromeReads(10))
	if reader.calls != 1 || mark.marked() {
		t.Errorf("reads = %d, marked %v; a built first read is read once and clears the mark", reader.calls, mark.marked())
	}
}

func TestReadTreeSettleThatBuildsLeavesNoMark(t *testing.T) {
	mark := newThinWindows().window(7)
	readMarked(t, mark, chromeReads(0, 6, 6))
	if mark.marked() {
		t.Error("a settle that built the page must not mark the Window")
	}
}

func TestReadTreePagedReadKeepsTheMark(t *testing.T) {
	// A paged read says nothing about the UIA tree's size: the mark stays.
	mark := newThinWindows().window(7)
	mark.remember()
	paged := chromeRead(0)
	paged.paged = true
	readMarked(t, mark, []*readResult{paged})
	if !mark.marked() {
		t.Error("a paged read must leave the mark alone")
	}
}

func TestReadTreeMarkedRetryInterruptedStillNotes(t *testing.T) {
	mark := newThinWindows().window(7)
	mark.remember()
	r := &fakeReader{t: t, results: chromeReads(0)}
	s := &fakeSleep{err: context.DeadlineExceeded}
	tree, err := readTree(context.Background(), "chrome.exe", time.Minute, DefaultChromiumSettle, mark, r.read, s.sleep)
	if err != nil {
		t.Fatalf("readTree: %v", err)
	}
	if r.calls != 1 || !strings.Contains(wstate.Render(tree), "Note: "+thinPageNote) {
		t.Errorf("reads = %d; the thin first read must stand with the thin note:\n%s", r.calls, wstate.Render(tree))
	}
}

func TestThinWindowsIsBoundedAndPerWindow(t *testing.T) {
	set := newThinWindows()
	for h := uint64(1); h <= maxThinWindows+10; h++ {
		set.window(h).remember()
	}
	if n := len(set.m); n > maxThinWindows {
		t.Errorf("the memory holds %d Windows, want at most %d", n, maxThinWindows)
	}
	if !set.window(maxThinWindows + 10).marked() {
		t.Error("the latest mark must be kept")
	}
	set.window(maxThinWindows + 10).forget()
	if set.window(maxThinWindows + 10).marked() {
		t.Error("forget must clear the Window's mark")
	}
	var zero thinMark
	zero.remember()
	if zero.marked() {
		t.Error("the zero mark remembers nothing")
	}
	var none *thinWindows
	if none.window(1).marked() {
		t.Error("a nil memory gives a zero mark")
	}
}

func TestDocumentSize(t *testing.T) {
	cases := []struct {
		name string
		els  []wstate.Element
		want int
	}{
		{"no document", []wstate.Element{{Depth: 0, Role: "window"}, {Depth: 1, Role: "pane"}}, 0},
		{"empty document", []wstate.Element{{Depth: 0, Role: "window"}, {Depth: 1, Role: "document"}, {Depth: 1, Role: "toolbar"}}, 0},
		{"document then frame", []wstate.Element{
			{Depth: 0, Role: "window"},
			{Depth: 1, Role: "document"}, {Depth: 2, Role: "link"}, {Depth: 3, Role: "text"},
			{Depth: 1, Role: "toolbar"}, {Depth: 2, Role: "button"},
		}, 2},
		{"iframe counted once", []wstate.Element{
			{Depth: 0, Role: "window"},
			{Depth: 1, Role: "document"}, {Depth: 2, Role: "document"}, {Depth: 3, Role: "button"},
		}, 2},
		{"two documents summed", []wstate.Element{
			{Depth: 0, Role: "window"},
			{Depth: 1, Role: "document"}, {Depth: 2, Role: "link"},
			{Depth: 1, Role: "document"}, {Depth: 2, Role: "link"}, {Depth: 2, Role: "button"},
		}, 3},
	}
	for _, tc := range cases {
		if got := documentSize(&wstate.Tree{Elements: tc.els}); got != tc.want {
			t.Errorf("%s: documentSize = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSettleContracts(t *testing.T) {
	// The ticket's 250-500 ms poll and 5 s budget, and the note's parts.
	if chromiumSettleEvery < 250*time.Millisecond || chromiumSettleEvery > 500*time.Millisecond {
		t.Errorf("chromiumSettleEvery %s, want 250-500 ms", chromiumSettleEvery)
	}
	if DefaultChromiumSettle != 5*time.Second {
		t.Errorf("DefaultChromiumSettle %s, want 5s", DefaultChromiumSettle)
	}
	for _, part := range []string{"still thin", "fully covered", "winuse browser", "uncover the Window"} {
		if !strings.Contains(thinPageNote, part) {
			t.Errorf("thinPageNote must say %q: %q", part, thinPageNote)
		}
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

// TestSettleOf: a read's settle budget is DefaultChromiumSettle unless a
// caller-set timeout put a wider one on the context (desktop-parity
// ticket 14); a narrower one leaves the default.
func TestSettleOf(t *testing.T) {
	if got := SettleOf(context.Background()); got != DefaultChromiumSettle {
		t.Errorf("no settle on the context: %s", got)
	}
	if got := SettleOf(WithSettle(context.Background(), 30*time.Second)); got != 30*time.Second {
		t.Errorf("a wider settle: %s, want 30s", got)
	}
	if got := SettleOf(WithSettle(context.Background(), time.Second)); got != DefaultChromiumSettle {
		t.Errorf("a narrower settle: %s, want the default", got)
	}
}
