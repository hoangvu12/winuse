package helper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// fakeTrees is a TreeReader over canned trees: it records the window it was
// asked for and returns the tree for that window (or an error).
type fakeTrees struct {
	mu     sync.Mutex
	hwnd   uint64
	budget time.Duration
	calls  int
	trees  map[uint64]*wstate.Tree
	err    error
}

func (f *fakeTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	f.mu.Lock()
	f.hwnd = hwnd
	f.budget = budget
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	tree, ok := f.trees[hwnd]
	if !ok {
		tree = &wstate.Tree{Title: title, App: app, FocusedIndex: -1}
	}
	return tree, nil
}

func cannedTree() *wstate.Tree {
	return &wstate.Tree{
		Title: "title",
		App:   "app.exe",
		Elements: []wstate.Element{
			{Depth: 0, Role: "window", Name: "title", AutomationId: "w"},
			{Depth: 1, Role: "button", Name: "OK", AutomationId: "ok"},
			{Depth: 1, Role: "button", Name: "Cancel", AutomationId: "cancel", Disabled: true},
		},
		FocusedIndex: -1,
		Notes:        []string{"this Window State is partial: the tree read exceeded the 8s time limit."},
	}
}

func fakeWindows() []win.WindowInfo {
	return []win.WindowInfo{
		{ID: 101, App: "Notepad", ExePath: `C:\Windows\notepad.exe`, Title: "Untitled - Notepad",
			Bounds: win.Rect{Left: 10, Top: 20, Right: 810, Bottom: 490}},
		{ID: 202, App: "Chrome", ExePath: `C:\Chrome\chrome.exe`, Title: "One"},
		{ID: 203, App: "Chrome", ExePath: `C:\Chrome\chrome.exe`, Title: "Two"},
	}
}

func testServices(trees TreeReader) Services {
	return Services{
		Windows: func() ([]win.WindowInfo, error) { return fakeWindows(), nil },
		Trees:   trees,
		Budget:  3 * time.Second,
	}
}

func runCommand(t *testing.T, cmds map[string]Command, cmd string, args any) (string, *Error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	c, ok := cmds[cmd]
	if !ok {
		t.Fatalf("command %q not registered", cmd)
	}
	return c(context.Background(), raw)
}

func TestGetWindowStateByWindowID(t *testing.T) {
	trees := &fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}
	out, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "get-window-state", windowRef{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("get-window-state: %v", cmdErr.Error())
	}
	want := wstate.Render(cannedTree())
	if out != want {
		t.Errorf("output:\n got %q\nwant %q", out, want)
	}
	if trees.hwnd != 101 {
		t.Errorf("captured window %d, want 101", trees.hwnd)
	}
	if trees.budget != 3*time.Second {
		t.Errorf("capture budget = %s, want the service's 3s", trees.budget)
	}
}

func TestGetWindowStateByAppNameUnique(t *testing.T) {
	trees := &fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}
	out, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "get-window-state", windowRef{Window: "notepad"})
	if cmdErr != nil {
		t.Fatalf("get-window-state by app name: %v", cmdErr.Error())
	}
	if !strings.Contains(out, "Window: \"title\", App: app.exe.") {
		t.Errorf("output missing the header: %q", out)
	}
}

func TestGetWindowStateAmbiguousAppListsCandidates(t *testing.T) {
	trees := &fakeTrees{}
	_, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "get-window-state", windowRef{Window: "Chrome"})
	if cmdErr == nil {
		t.Fatal("an ambiguous app name must fail")
	}
	if !strings.Contains(cmdErr.Error(), `app "Chrome" has 2 open Windows`) {
		t.Errorf("error = %q", cmdErr.Error())
	}
	if !strings.Contains(cmdErr.Hint, "202\tChrome\tOne") || !strings.Contains(cmdErr.Hint, "203\tChrome\tTwo") {
		t.Errorf("hint must list the candidates, got %q", cmdErr.Hint)
	}
	if trees.calls != 0 {
		t.Error("no tree may be read when the window does not resolve")
	}
}

func TestGetWindowStateUnknownWindow(t *testing.T) {
	_, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "get-window-state", windowRef{Window: "404"})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "no open Window with ID 404") {
		t.Fatalf("want unknown-window failure, got %v", cmdErr)
	}
}

func TestGetWindowStateAppWithNoWindows(t *testing.T) {
	_, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "get-window-state", windowRef{Window: "Paint"})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), `app "Paint" has no open Windows`) {
		t.Fatalf("want no-windows failure, got %v", cmdErr)
	}
}

func TestGetWindowStateRejectsMissingWindowArgument(t *testing.T) {
	for _, args := range []any{nil, windowRef{}} {
		_, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "get-window-state", args)
		if cmdErr == nil {
			t.Fatalf("args %v must be refused", args)
		}
	}
}

func TestGetWindowStateMalformedArguments(t *testing.T) {
	cmds := CommandsWith(testServices(&fakeTrees{}))
	c := cmds["get-window-state"]
	_, cmdErr := c(context.Background(), json.RawMessage(`{"window": 12}`)) // wrong type
	if cmdErr == nil {
		t.Fatal("malformed arguments must be refused")
	}
}

func TestGetWindowStateTreeReadFailure(t *testing.T) {
	trees := &fakeTrees{err: errors.New("window is gone: reading the Window's tree failed (HRESULT 0x80040201)")}
	_, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "get-window-state", windowRef{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "0x80040201") {
		t.Fatalf("the HRESULT must reach the user, got %v", cmdErr)
	}
}

func TestGetWindowStateWindowGoneHint(t *testing.T) {
	trees := &fakeTrees{err: uia.ErrWindowGone}
	_, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "get-window-state", windowRef{Window: "101"})
	if cmdErr == nil {
		t.Fatal("want failure")
	}
	if !strings.Contains(cmdErr.Hint, "list-windows") {
		t.Errorf("a gone window needs a recovery hint, got %q", cmdErr.Hint)
	}
}

func TestGetWindowStateEnumerationFailure(t *testing.T) {
	s := Services{
		Windows: func() ([]win.WindowInfo, error) { return nil, errors.New("EnumWindows: access denied") },
		Trees:   &fakeTrees{},
	}
	_, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "enumerating windows") {
		t.Fatalf("want enumeration failure, got %v", cmdErr)
	}
}

func TestRecordTreeWritesFixture(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tree.json")
	trees := &fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}
	out, cmdErr := runCommand(t, CommandsWith(testServices(trees)), "record-tree", recordArgs{Window: "notepad", File: file})
	if cmdErr != nil {
		t.Fatalf("record-tree: %v", cmdErr.Error())
	}
	if !strings.Contains(out, file) || !strings.Contains(out, "3 elements") {
		t.Errorf("output = %q", out)
	}
	fixture, err := wstate.LoadFixture(file)
	if err != nil {
		t.Fatalf("the recorder's output must be a loadable fixture: %v", err)
	}
	if fixture.Format != wstate.FixtureFormat || fixture.Captured == nil {
		t.Errorf("fixture header = format %d, captured %v", fixture.Format, fixture.Captured)
	}
	if !strings.Contains(fixture.Source, "Window 101") {
		t.Errorf("source = %q, want the recording provenance", fixture.Source)
	}
	if wstate.Render(fixture) != wstate.Render(cannedTree()) {
		t.Errorf("rendered fixture differs from the live render")
	}
}

func TestRecordTreeRequiresFile(t *testing.T) {
	_, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "record-tree", recordArgs{Window: "101"})
	if cmdErr == nil {
		t.Fatal("record-tree without a file must fail")
	}
}

func TestRecordTreePartialNoteInOutput(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "partial.json")
	trees := &fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}
	out, _ := runCommand(t, CommandsWith(testServices(trees)), "record-tree", recordArgs{Window: "101", File: file})
	if !strings.Contains(out, "this Window State is partial") {
		t.Errorf("a partial read must be noted in the recorder's output: %q", out)
	}
}

func TestWriteFileAtomicIsAtomicAndCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "f.json")
	if err := writeFileAtomic(file, []byte("data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "data" {
		t.Fatalf("read back %q, %v", got, err)
	}
	// No temp leftovers.
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %d entries", len(entries))
	}
}

func TestCommandsRegistryHasWindowStateCommands(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"list-windows", "get-window", "get-window-state", "record-tree"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
}

func TestTreeBudgetFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
		err   bool
	}{
		{"", uia.DefaultTreeBudget, false},
		{"1500ms", 1500 * time.Millisecond, false},
		{"4", 4 * time.Second, false},
		{"nope", 0, true},
	}
	for _, tc := range cases {
		t.Setenv("WINUSE_TREE_BUDGET", tc.value)
		got, err := treeBudgetFromEnv(uia.DefaultTreeBudget)
		if tc.err {
			if err == nil {
				t.Errorf("WINUSE_TREE_BUDGET=%q: want error", tc.value)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("WINUSE_TREE_BUDGET=%q: got %s, %v; want %s", tc.value, got, err, tc.want)
		}
	}
}

func TestGetWindowPrintsOneListWindowsLine(t *testing.T) {
	// By Window ID and by unambiguous App name: the one line, exactly as
	// list-windows prints it (Codex's get_window re-resolve step).
	for _, ref := range []string{"101", "notepad"} {
		out, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "get-window", windowRef{Window: ref})
		if cmdErr != nil {
			t.Fatalf("get-window %s: %v", ref, cmdErr.Error())
		}
		want := win.FormatWindows([]win.WindowInfo{fakeWindows()[0]})
		if out != want {
			t.Errorf("get-window %s output = %q, want %q", ref, out, want)
		}
	}
}

func TestGetWindowFailuresShareTheResolveRules(t *testing.T) {
	// Ambiguity lists the candidates; a missing Window points at
	// list-windows; the argument is required.
	cases := []struct {
		ref     string
		wantMsg string
	}{
		{"Chrome", `app "Chrome" has 2 open Windows`},
		{"404", "no open Window with ID 404"},
		{"", "a window reference is required"},
	}
	for _, tc := range cases {
		_, cmdErr := runCommand(t, CommandsWith(testServices(&fakeTrees{})), "get-window", windowRef{Window: tc.ref})
		if cmdErr == nil || !strings.Contains(cmdErr.Error(), tc.wantMsg) {
			t.Fatalf("get-window %q: %v, want it to contain %q", tc.ref, cmdErr, tc.wantMsg)
		}
	}
	// Malformed arguments (a non-string window) are refused.
	cmds := CommandsWith(testServices(&fakeTrees{}))
	_, cmdErr := cmds["get-window"](context.Background(), json.RawMessage(`{"window": 12}`))
	if cmdErr == nil {
		t.Fatal("malformed arguments must be refused")
	}
}
