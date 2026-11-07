package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// TestFullReadViews: --interactive, --depth and --compact on a full read
// print less of the Window State, never renumbering it, and say so.
func TestFullReadViews(t *testing.T) {
	s, _ := findServices()
	cmds := CommandsWith(s)
	out, e := runCommand(t, cmds, "get-window-state", windowRef{Window: "101", Interactive: true})
	if e != nil {
		t.Fatalf("--interactive: %+v", e)
	}
	want := "Window: \"title\", App: app.exe.\n" +
		"\t0 window title ID: w\n" +
		"\t\t1 button OK ID: ok\n" +
		"\t\t2 button (disabled) Cancel ID: cancel\n" +
		"\n" +
		"Note: this Window State is partial: the tree read exceeded the 8s time limit.\n"
	if out != want {
		t.Errorf("--interactive:\n%q\nwant:\n%q", out, want)
	}
	out, e = runCommand(t, cmds, "get-window-state", windowRef{Window: "101", Depth: 1, Compact: true})
	if e != nil || !strings.Contains(out, "\t\t2 button (disabled) Cancel ID: cancel\n") || strings.Contains(out, "left out") {
		t.Errorf("--depth 1 --compact leaves nothing out of a one-level tree: %q, %+v", out, e)
	}
}

// TestFullReadMaxChars: the cap keeps the header and the walk's notes
// and names what it did not print.
func TestFullReadMaxChars(t *testing.T) {
	s, _ := findServices()
	out, e := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101", MaxChars: intPtr(460)})
	if e != nil {
		t.Fatalf("--max-chars: %+v", e)
	}
	if len(out) > 460 || !strings.Contains(out, "Note: this Window State is partial") ||
		!strings.Contains(out, "Note: --max-chars 460 stopped the output after Element ") {
		t.Errorf("--max-chars 460 (%d characters):\n%s", len(out), out)
	}
}

// TestFullReadOut: --out writes the whole Window State to the file and
// prints its header, the file's line and the notes.
func TestFullReadOut(t *testing.T) {
	s, _ := findServices()
	file := filepath.Join(t.TempDir(), "state.txt")
	out, e := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101", Out: file})
	if e != nil {
		t.Fatalf("--out: %+v", e)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\t\t2 button (disabled) Cancel ID: cancel\n") {
		t.Errorf("the file must hold the whole Window State:\n%s", data)
	}
	want := "Window: \"title\", App: app.exe.\n" +
		"Window State: " + file + " (3 Elements, 1 interactive; " + strconv.Itoa(len([]rune(string(data)))) + " characters; its Element Indexes work as printed there)\n" +
		"\n" +
		"Note: this Window State is partial: the tree read exceeded the 8s time limit.\n"
	if out != want {
		t.Errorf("--out:\n%q\nwant:\n%q", out, want)
	}
}

// meetingTrees is a tree reader that starts reading, then waits for the
// capture to finish: with a capture that waits for the read to start, a
// screenshot completes only if the two overlap.
type meetingTrees struct {
	started, captured chan struct{}
	cancelled         bool
}

func (m *meetingTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	close(m.started)
	select {
	case <-m.captured:
		return cannedTree(), nil
	case <-ctx.Done():
		m.cancelled = true
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, errors.New("the capture never finished")
	}
}

// TestScreenshotReadsTheTreeWhileCapturing (speed 08): the tree
// read runs alongside the capture, so a screenshot costs the slower of the
// two; a failed capture cancels the read.
func TestScreenshotReadsTheTreeWhileCapturing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := newShotSpy(t)
		s.frame = syntheticFrame(800, 470, 100, 200)
		trees := &meetingTrees{started: make(chan struct{}), captured: make(chan struct{})}
		s.svc.Trees = trees
		s.capture = func(uint64) (shot.Frame, error) {
			select {
			case <-trees.started:
			case <-time.After(5 * time.Second):
				return shot.Frame{}, errors.New("the tree read never started while capturing")
			}
			if fail {
				return shot.Frame{}, errors.New("capture failed") // trees.captured stays open: only the cancel ends the read
			}
			close(trees.captured)
			return s.frame, nil
		}
		out, e := runCommand(t, CommandsWith(s.svc), "get-window-state", windowRef{Window: "101", Screenshot: true})
		switch {
		case fail && (e == nil || !strings.Contains(e.Msg, "capture failed") || !trees.cancelled):
			t.Errorf("a failed capture: %q, %+v, read cancelled %v", out, e, trees.cancelled)
		case !fail && (e != nil || !strings.Contains(out, "\t\t1 button OK ID: ok\n") || !strings.Contains(out, "Screenshot: ")):
			t.Errorf("screenshot: %q, %+v", out, e)
		}
	}
}

// TestFindLimit: --limit keeps the first matches, says how many there
// were, and --first is --limit 1.
func TestFindLimit(t *testing.T) {
	s, trees := findServices()
	cmds := CommandsWith(s)
	trees.found = uia.FindResult{Count: 5, Added: uia.Added{Indexes: []int{3, 4}, Elements: cannedFindElements[1:3]}}
	out, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "button", Limit: 2})
	if e != nil || trees.limit != 2 {
		t.Fatalf("--limit 2: %+v, limit %d", e, trees.limit)
	}
	if !strings.Contains(out, "Note: these are the first 2 of 5 matches (--limit 2); narrow the filters, or pass a larger --limit\n") {
		t.Errorf("no note on the matches --limit left out:\n%s", out)
	}
	for _, a := range []protocol.FindArgs{
		{Window: "101", Limit: 2, First: true},
		{Window: "101", Limit: 2, Count: true},
		{Window: "101", Limit: -1},
	} {
		if _, e := runCommand(t, cmds, "find", a); e == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
	trees.found.Count = 2
	out, e = runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "button", MaxChars: intPtr(470)})
	if e != nil || len(out) > 470 || !strings.Contains(out, "\t3 button Save ID: save\n") ||
		!strings.Contains(out, "stopped the output after Element 3: 1 more Element lines (from Element 4 on) are not printed; their Element Indexes still work. Narrow the filters, pass --limit, or raise --max-chars\n") {
		t.Errorf("--max-chars 470 (%d characters): %q, %+v", len(out), out, e)
	}
}

// bigTree is a Window State past the default cap: 1,200 named buttons,
// two of them sharing an ID.
func bigTree() *wstate.Tree {
	els := []wstate.Element{{Role: "window", Name: "big", AutomationId: "w"}}
	for i := 1; i <= 1200; i++ {
		els = append(els, wstate.Element{Depth: 1, Role: "button", Name: "Button number " + strconv.Itoa(i), AutomationId: "b" + strconv.Itoa(i)})
	}
	els[2].AutomationId, els[3].AutomationId = "twin", "twin"
	return &wstate.Tree{Title: "big", App: "app.exe", Elements: els, FocusedIndex: 2, Notes: []string{"a walk note"}}
}

// TestDefaultReadBudget (speed 13): a plain read leaves out the shared
// IDs and stops within defaultMaxChars, its Screenshot line included;
// --max-chars 0 lifts the cap, --out writes it uncut, and --full prints
// Codex's text byte for byte.
func TestDefaultReadBudget(t *testing.T) {
	tree := bigTree()
	s := testServices(&fakeTrees{trees: map[uint64]*wstate.Tree{101: tree}})
	cmds := CommandsWith(s)
	run := func(a windowRef) string {
		t.Helper()
		out, e := runCommand(t, cmds, "get-window-state", a)
		if e != nil {
			t.Fatalf("%+v: %+v", a, e)
		}
		return out
	}
	out := run(windowRef{Window: "101"})
	if n := utf8.RuneCountInString(out); n > defaultMaxChars || n < defaultMaxChars-1000 {
		t.Errorf("the default read printed %d characters, want just under %d", n, defaultMaxChars)
	}
	if !strings.Contains(out, "Note: --max-chars 28000 stopped the output after Element ") || !strings.Contains(out, "Note: a walk note\n") ||
		strings.Contains(out, "ID: twin") || !strings.Contains(out, "\t\t4 button Button number 4 ID: b4\n") ||
		!strings.Contains(out, "The focused UI element is 2 button Button number 2.\n") {
		t.Errorf("the default read must cap, keep the notes and unique IDs, and drop the shared one:\n%s", out[len(out)-600:])
	}

	uncut := run(windowRef{Window: "101", MaxChars: intPtr(0)})
	if strings.Contains(uncut, "stopped the output") || !strings.Contains(uncut, "\t\t1200 button Button number 1200 ID: b1200\n") || strings.Contains(uncut, "ID: twin") {
		t.Errorf("--max-chars 0 prints every line, the shared ID still left out:\n%s", uncut[len(uncut)-300:])
	}
	if full := run(windowRef{Window: "101", Full: true}); full != wstate.Render(tree) {
		t.Errorf("--full must print Codex's text byte for byte")
	}

	file := filepath.Join(t.TempDir(), "state.txt")
	run(windowRef{Window: "101", Out: file})
	if data, err := os.ReadFile(file); err != nil || string(data) != uncut {
		t.Errorf("--out writes the read uncut: %v", err)
	}
	run(windowRef{Window: "101", Out: file, Full: true})
	if data, _ := os.ReadFile(file); string(data) != wstate.Render(tree) {
		t.Errorf("--out --full writes Codex's text")
	}

	shot := newShotSpy(t)
	shot.svc.Trees = &fakeTrees{trees: map[uint64]*wstate.Tree{101: tree}}
	shot.frame = syntheticFrame(800, 470, 100, 200)
	out, e := runCommand(t, CommandsWith(shot.svc), "get-window-state", windowRef{Window: "101", Screenshot: true})
	if e != nil || utf8.RuneCountInString(out) > defaultMaxChars || !strings.Contains(out, "\nScreenshot: ") {
		t.Errorf("with --screenshot the Screenshot line fits under the cap too (%d characters): %+v", utf8.RuneCountInString(out), e)
	}
}

// TestFindDropsSharedIDs: find leaves out the IDs its walk's Elements
// share, and --full keeps them.
func TestFindDropsSharedIDs(t *testing.T) {
	s, trees := findServices()
	cmds := CommandsWith(s)
	trees.found = uia.FindResult{Count: 2, Added: uia.Added{Indexes: []int{3, 4}, Elements: cannedFindElements[1:3]}, SharedIDs: map[string]bool{"save": true}}
	out, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "button"})
	if e != nil || !strings.Contains(out, "\t3 button Save\n") {
		t.Errorf("the shared ID must be left out: %q, %+v", out, e)
	}
	out, e = runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "button", Full: true})
	if e != nil || !strings.Contains(out, "\t3 button Save ID: save\n") {
		t.Errorf("--full keeps every ID: %q, %+v", out, e)
	}
	if _, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Full: true, MaxChars: intPtr(100)}); e == nil {
		t.Error("--full with --max-chars was accepted")
	}
}
