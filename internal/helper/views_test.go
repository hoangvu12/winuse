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
		"\n" +
		"Note: this Window State is partial: the tree read exceeded the 8s time limit.\n" +
		"Note: --interactive left out 1 of the 3 Elements; their Element Indexes still work\n"
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
	out, e := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101", MaxChars: 460})
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
	out, e = runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "button", MaxChars: 470})
	if e != nil || len(out) > 470 || !strings.Contains(out, "\t3 button Save ID: save\n") ||
		!strings.Contains(out, "stopped the output after Element 3: 1 more Element lines (from Element 4 on) are not printed; their Element Indexes still work. Narrow the filters, pass --limit, or raise --max-chars\n") {
		t.Errorf("--max-chars 470 (%d characters): %q, %+v", len(out), out, e)
	}
}
