package helper

// --observe follows the Window the Action opened (learn-from 01, ADR 0021
// amendment): one new Window is read whole after the Action's own Window,
// several are a note, a read that fails or a Window that closed meanwhile
// is a note, --out writes it beside the observation's file, and a failed
// Action reads nothing. The desktop is the opened tests' fake snapshot
// sequence (surfaceDesk); no window is enumerated.

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

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// windowTrees is a TreeReader with a tree, a store and an error per
// Window, which records the order of its reads.
type windowTrees struct {
	mu     sync.Mutex
	trees  map[uint64]*wstate.Tree
	stored map[uint64][]wstate.Element
	errs   map[uint64]error
	reads  []uint64
}

func (w *windowTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reads = append(w.reads, hwnd)
	if err := w.errs[hwnd]; err != nil {
		return nil, err
	}
	t := w.trees[hwnd]
	if w.stored == nil {
		w.stored = map[uint64][]wstate.Element{}
	}
	w.stored[hwnd] = t.Elements
	return t, nil
}

func (w *windowTrees) StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	els, ok := w.stored[hwnd]
	return append([]wstate.Element(nil), els...), ok
}

// saveAsTree is the Save As dialog's Window State.
func saveAsTree() *wstate.Tree {
	return &wstate.Tree{Title: "Save As", App: "notepad.exe", FocusedIndex: -1, Elements: []wstate.Element{
		{Depth: 0, Role: "dialog", Name: "Save As"},
		{Depth: 1, Role: "edit", Name: "File name"},
		{Depth: 1, Role: "button", Name: "Save"},
	}}
}

// openedFixture is the fixture App's trees before and after the Action,
// stored as the Agent's Window State of 101, and the Save As dialog.
func openedFixture(t *testing.T) *windowTrees {
	t.Helper()
	before, after := observeFixture(t)
	return &windowTrees{
		trees:  map[uint64]*wstate.Tree{101: after, saveDialog.ID: saveAsTree()},
		stored: map[uint64][]wstate.Element{101: before.Elements},
	}
}

// observeReported runs act as name, with the opened-Window report and
// --observe around it, on window 101 and the fake desk.
func observeReported(t *testing.T, trees *windowTrees, desk *surfaceDesk, name string, act Command, o *protocol.Observe) (string, *Error) {
	t.Helper()
	s := testServices(trees)
	desk.wire(&s)
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RequestTimeout+protocol.ObserveTimeout)
	defer cancel()
	return s.observed(name, s.reportOpened(name, act))(withObserve(ctx, o), json.RawMessage(`{"window":"101"}`))
}

func okCmd(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }

func TestObserveReadsTheOneOpenedWindow(t *testing.T) {
	trees := openedFixture(t)
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	out, err := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err != nil {
		t.Fatal(err)
	}
	want := `ok
Note: a new Window opened: 777 Notepad "Save As"
Window: "winuse fixture app", App: winusefixture.exe.
Changed since this agent's last read of window 101 (~ changed, * new, - gone); Element Indexes not listed are unchanged:
		~13 check box Notify me on Secondary Actions: Toggle ID: checkbox-notify (was: check box Notify me off Secondary Actions: Toggle ID: checkbox-notify)
		*26 text Saved
The Window the Action opened, 777, read whole; its Element Indexes go with window 777:
` + wstate.RenderView(saveAsTree(), wstate.View{Ref: "777", MaxChars: defaultMaxChars})
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if len(trees.reads) != 2 || trees.reads[0] != 101 || trees.reads[1] != 777 {
		t.Errorf("reads %v, want the Action's Window, then the opened one", trees.reads)
	}
	if els, _ := trees.StoredElements(context.Background(), 777); len(els) != 3 {
		t.Errorf("the read replaces the Agent's Window State of 777: %v", els)
	}
}

// A dialog UI Automation already lists inside its owner's tree (a classic
// dialog, a WinForms form) printed with the Action's Window: a note names
// its Element there and it is not read again.
func TestObserveOpenedWindowInsideTheReadIsANote(t *testing.T) {
	trees := openedFixture(t)
	after := *trees.trees[101]
	after.Elements = append(append([]wstate.Element(nil), after.Elements[:1]...),
		append([]wstate.Element{{Depth: 1, Role: "window", Name: "Save As"}, {Depth: 2, Role: "button", Name: "Save"}}, after.Elements[1:]...)...)
	trees.trees[101] = &after
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	out, err := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveFull})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "Note: the Window the Action opened, 777 \"Save As\", is Element 1 of the read above, so it is not read again; act on it through that read, or read it alone with winuse get-window-state 777\n") {
		t.Errorf("got:\n%s", out)
	}
	if len(trees.reads) != 1 {
		t.Errorf("reads %v, want the Action's Window alone", trees.reads)
	}
	// Unowned, the same title is another Window's: it is read.
	trees = openedFixture(t)
	trees.trees[101] = &after
	unowned := saveDialog
	unowned.Owner = 0
	out, _ = observeReported(t, trees, newDesk([]win.WindowInfo{unowned, notepadMain, otherApp}), "click", okCmd, &protocol.Observe{Mode: protocol.ObserveFull})
	if !strings.Contains(out, "The Window the Action opened, 777, read whole") {
		t.Errorf("an unowned Window is read:\n%s", out)
	}
}

// The opened Window prints whole even with a baseline (a reused menu
// Window ID), and through the observation's view.
func TestObserveOpenedWindowWholeThroughTheView(t *testing.T) {
	trees := openedFixture(t)
	trees.stored[saveDialog.ID] = saveAsTree().Elements[:1]
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	out, _ := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveDiff, Interactive: true})
	_, opened, ok := strings.Cut(out, "read whole; its Element Indexes go with window 777:\n")
	if !ok || opened != wstate.RenderView(saveAsTree(), wstate.View{Ref: "777", Interactive: true, MaxChars: defaultMaxChars}) {
		t.Errorf("the opened Window prints whole through --interactive:\n%s", out)
	}
	if strings.Contains(opened, "Changed since") {
		t.Errorf("the opened Window is never a diff:\n%s", opened)
	}
}

func TestObserveSeveralOpenedWindowsIsANote(t *testing.T) {
	trees := openedFixture(t)
	desk := newDesk([]win.WindowInfo{saveDialog, editMenu, notepadMain, otherApp})
	out, err := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveFull})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "Note: --observe read none of the 2 Windows the Action opened (777, 778); read the one you need with winuse get-window-state <Window ID>\n") {
		t.Errorf("got:\n%s", out)
	}
	if len(trees.reads) != 1 {
		t.Errorf("reads %v, want the Action's Window alone", trees.reads)
	}
}

func TestObserveOpenedWindowFailuresAreNotes(t *testing.T) {
	// A read that fails is a note; the Action's own observation stands.
	trees := openedFixture(t)
	trees.errs = map[uint64]error{saveDialog.ID: errors.New("the element is not available")}
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	out, err := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err != nil || !strings.Contains(out, "*26 text Saved\n") ||
		!strings.HasSuffix(out, "Note: --observe could not read 777 after the Action, which ran as reported above: the element is not available\n") {
		t.Errorf("got %q, %v", out, err)
	}

	// A Window that closed between the settle and the read is not read.
	trees = openedFixture(t)
	desk = newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp}, []win.WindowInfo{notepadMain, otherApp})
	out, _ = observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveDiff})
	if !strings.HasSuffix(out, "Note: --observe could not read 777 after the Action, which ran as reported above: the Window the Action opened closed before the read\n") || len(trees.reads) != 1 {
		t.Errorf("got %q after reads %v", out, trees.reads)
	}

	// A failed Action reads nothing, the opened Window included.
	trees = openedFixture(t)
	desk = newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	failed := func(context.Context, json.RawMessage) (string, *Error) {
		return "", Fail("element 7 is not available", "read again")
	}
	if _, err := observeReported(t, trees, desk, "click", failed, &protocol.Observe{Mode: protocol.ObserveDiff}); err == nil || len(trees.reads) != 0 {
		t.Errorf("a failed Action: %v, reads %v", err, trees.reads)
	}
}

// A Batch that ran past a failed step reads the Window it left open too.
func TestObserveFailedBatchReadsTheOpenedWindow(t *testing.T) {
	trees := openedFixture(t)
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	batchFailed := func(context.Context, json.RawMessage) (string, *Error) {
		return "", &Error{Msg: "batch: 1 of 2 Actions failed", Exit: 1, Out: "Action 1 (click): ok\nAction 2 (click): failed: x\n"}
	}
	_, err := observeReported(t, trees, desk, "batch", batchFailed, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err == nil || !strings.Contains(err.Out, "*26 text Saved\nThe Window the Action opened, 777, read whole") || !strings.Contains(err.Out, "\t1 edit File name\n") {
		t.Errorf("a failed Batch reads the opened Window: %+v", err)
	}
}

func TestObserveOpenedWindowOut(t *testing.T) {
	trees := openedFixture(t)
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	file := filepath.Join(t.TempDir(), "observed.txt")
	out, err := observeReported(t, trees, desk, "click", okCmd, &protocol.Observe{Mode: protocol.ObserveDiff, Out: file})
	if err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(file), "observed-777.txt")
	data, rerr := os.ReadFile(sibling)
	if rerr != nil || !strings.Contains(string(data), "\t2 button Save\n") {
		t.Fatalf("the opened Window's file: %q (%v)", data, rerr)
	}
	if !strings.Contains(out, "Observed Window State of window 777: "+sibling+" (3 Elements") {
		t.Errorf("the output names the file:\n%s", out)
	}
	if got := openedOut(`C:\tmp\state`, 5); got != `C:\tmp\state-5` {
		t.Errorf("openedOut without an extension = %q", got)
	}
}

// A Headless ContextMenu reports its menu itself, and --observe reads it,
// through the whole registry.
func TestObserveReadsTheContextMenu(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	before, after := observeFixture(t)
	menu := &wstate.Tree{Title: "", App: "notepad.exe", FocusedIndex: -1, Elements: []wstate.Element{
		{Depth: 0, Role: "menu", Name: "Context"}, {Depth: 1, Role: "menu item", Name: "Undo"},
	}}
	trees := &windowTrees{trees: map[uint64]*wstate.Tree{101: after, editMenu.ID: menu}, stored: map[uint64][]wstate.Element{101: before.Elements}}
	spy.svc.Trees = trees
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RequestTimeout+protocol.ObserveTimeout)
	defer cancel()
	raw, _ := json.Marshal(contextMenuArgs("101", 3))
	out, e := CommandsWith(spy.svc)["perform-secondary-action"](withObserve(ctx, &protocol.Observe{Mode: protocol.ObserveDiff}), raw)
	if e != nil {
		t.Fatalf("ContextMenu: %v", e.Error())
	}
	if !strings.Contains(out, "The Window the Action opened, 778, read whole; its Element Indexes go with window 778:\n") || !strings.Contains(out, "\t1 menu item Undo\n") {
		t.Errorf("got:\n%s", out)
	}
}

// The latest record wins: a Batch's settle around all its steps replaces
// the menu a step recorded, and outside --observe nothing is recorded.
func TestRecordOpenedLatestWins(t *testing.T) {
	rec := &openedRecord{}
	ctx := context.WithValue(context.Background(), openedRecordKey{}, rec)
	recordOpened(ctx, []win.WindowInfo{editMenu})
	recordOpened(ctx, nil)
	if got := rec.get(); len(got) != 0 {
		t.Errorf("record = %v, want the settle's (none)", got)
	}
	recordOpened(context.Background(), []win.WindowInfo{editMenu}) // no --observe: no panic, no record
}
