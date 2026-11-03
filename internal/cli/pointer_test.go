package cli

// Offline tests for the pointer commands' argument parsing (scroll, drag,
// hover): the @x,y point syntax, negative scroll amounts, and the usage
// errors. None of these dial the helper, so no desktop is touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseScrollArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantX      int
		wantY      int
		wantDX     int
		wantDY     int
		wantErr    string
	}{
		{name: "down and right", args: []string{"101", "@400,235", "240", "600"},
			wantWindow: "101", wantX: 400, wantY: 235, wantDX: 240, wantDY: 600},
		{name: "up and left (negative amounts)", args: []string{"notepad", "@10,10", "-240", "-360"},
			wantWindow: "notepad", wantX: 10, wantY: 10, wantDX: -240, wantDY: -360},
		{name: "spaces around the point parts", args: []string{"101", "@ 400 , 235 ", "0", "120"},
			wantWindow: "101", wantX: 400, wantY: 235, wantDX: 0, wantDY: 120},
		{name: "zero and zero parses", args: []string{"101", "@1,1", "0", "0"},
			wantWindow: "101", wantX: 1, wantY: 1, wantDX: 0, wantDY: 0},

		{name: "too few arguments", args: []string{"101", "@1,1", "120"},
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
		{name: "too many arguments", args: []string{"101", "@1,1", "120", "120", "extra"},
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
		{name: "point without @", args: []string{"101", "400,235", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "point with one coordinate", args: []string{"101", "@400", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "negative point", args: []string{"101", "@-1,5", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "amount is not a number", args: []string{"101", "@1,1", "lots", "120"},
			wantErr: `the scroll amount must be an integer, got "lots"`},
		{name: "vertical amount is not a number", args: []string{"101", "@1,1", "120", "down"},
			wantErr: `the scroll amount must be an integer, got "down"`},
		{name: "no arguments", args: nil,
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, x, y, dx, dy, err := parseScrollArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || x != tc.wantX || y != tc.wantY || dx != tc.wantDX || dy != tc.wantDY {
				t.Errorf("parsed %q @%d,%d %d,%d, want %q @%d,%d %d,%d",
					window, x, y, dx, dy, tc.wantWindow, tc.wantX, tc.wantY, tc.wantDX, tc.wantDY)
			}
		})
	}
}

func TestParseScrollCommandElementForms(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.ScrollArgs
		wantErr string
	}{
		{name: "index and direction", args: []string{"101", "4", "down"},
			want: protocol.ScrollArgs{Window: "101", ElementIndex: intPtr(4), Direction: "down"}},
		{name: "steps", args: []string{"101", "4", "Up", "10"},
			want: protocol.ScrollArgs{Window: "101", ElementIndex: intPtr(4), Direction: "up", Steps: intPtr(10)}},
		{name: "name reference", args: []string{"notepad", "name:File list", "left", "2"},
			want: protocol.ScrollArgs{Window: "notepad", Ref: "name:File list", Direction: "left", Steps: intPtr(2)}},
		{name: "id reference into view", args: []string{"101", "id:row40", "into-view"},
			want: protocol.ScrollArgs{Window: "101", Ref: "id:row40", Direction: "into-view"}},
		{name: "into view in any case", args: []string{"101", "7", "Into-View"},
			want: protocol.ScrollArgs{Window: "101", ElementIndex: intPtr(7), Direction: "into-view"}},
		{name: "the steps range is the helper's", args: []string{"101", "4", "down", "0"},
			want: protocol.ScrollArgs{Window: "101", ElementIndex: intPtr(4), Direction: "down", Steps: intPtr(0)}},
		{name: "the point form is unchanged", args: []string{"101", "@400,235", "0", "600"},
			want: protocol.ScrollArgs{Window: "101", X: 400, Y: 235, ScrollY: 600}},

		{name: "no direction", args: []string{"101", "4"}, wantErr: "scroll takes a window reference"},
		{name: "bad direction", args: []string{"101", "4", "sideways"},
			wantErr: `the scroll direction must be up, down, left, right or into-view, got "sideways"`},
		{name: "steps not a number", args: []string{"101", "4", "down", "lots"}, wantErr: `steps must be an integer, got "lots"`},
		{name: "into-view with steps", args: []string{"101", "4", "into-view", "3"}, wantErr: "into-view takes no steps"},
		{name: "too many arguments", args: []string{"101", "4", "down", "3", "4"}, wantErr: "scroll takes a window reference"},
		{name: "bad target", args: []string{"101", "four", "down"},
			wantErr: `the scroll target must be an element index, name:<text>, id:<AutomationId> or @x,y, got "four"`},
		{name: "unquoted name", args: []string{"101", "name:File", "list", "down"}, wantErr: `quote it, e.g. "name:File list"`},
		{name: "unquoted name with steps", args: []string{"101", "name:My", "big", "list", "down", "3"}, wantErr: "quote it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseScrollCommand(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parsed %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseDragArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantFromX  int
		wantFromY  int
		wantToX    int
		wantToY    int
		wantErr    string
	}{
		{name: "from and to", args: []string{"101", "@100,50", "@400,235"},
			wantWindow: "101", wantFromX: 100, wantFromY: 50, wantToX: 400, wantToY: 235},
		{name: "same point", args: []string{"notepad", "@5,5", "@5,5"},
			wantWindow: "notepad", wantFromX: 5, wantFromY: 5, wantToX: 5, wantToY: 5},

		{name: "too few arguments", args: []string{"101", "@1,1"},
			wantErr: "drag takes a window reference and two @x,y points (from, to)"},
		{name: "too many arguments", args: []string{"101", "@1,1", "@2,2", "@3,3"},
			wantErr: "drag takes a window reference and two @x,y points (from, to)"},
		{name: "bad from point", args: []string{"101", "1,1", "@2,2"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "bad to point", args: []string{"101", "@1,1", "2,2"},
			wantErr: "a coordinate reference must be @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, fx, fy, tx, ty, err := parseDragArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || fx != tc.wantFromX || fy != tc.wantFromY || tx != tc.wantToX || ty != tc.wantToY {
				t.Errorf("parsed %q (%d,%d)→(%d,%d), want %q (%d,%d)→(%d,%d)",
					window, fx, fy, tx, ty, tc.wantWindow, tc.wantFromX, tc.wantFromY, tc.wantToX, tc.wantToY)
			}
		})
	}
}

func TestParseHoverArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantX      int
		wantY      int
		wantErr    string
	}{
		{name: "point", args: []string{"101", "@200,42"},
			wantWindow: "101", wantX: 200, wantY: 42},
		{name: "app name", args: []string{"notepad", "@0,0"},
			wantWindow: "notepad", wantX: 0, wantY: 0},

		{name: "too few arguments", args: []string{"101"},
			wantErr: "hover takes a window reference and an @x,y point"},
		{name: "too many arguments", args: []string{"101", "@1,1", "@2,2"},
			wantErr: "hover takes a window reference and an @x,y point"},
		{name: "bad point", args: []string{"101", "200,42"},
			wantErr: "a coordinate reference must be @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, x, y, err := parseHoverArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || x != tc.wantX || y != tc.wantY {
				t.Errorf("parsed %q @%d,%d, want %q @%d,%d", window, x, y, tc.wantWindow, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestPointerUsageErrorsExit2WithoutDialing pins the dispatch: usage
// failures exit 2 and never reach the helper.
func TestPointerUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"scroll with no arguments", []string{"scroll"}, scrollUsage},
		{"scroll with a bad point", []string{"scroll", "101", "1,2", "120", "120"}, "a coordinate reference must be @x,y"},
		{"scroll with a bad amount", []string{"scroll", "101", "@1,2", "many", "120"}, "the scroll amount must be an integer"},
		{"scroll with a bad direction", []string{"scroll", "101", "3", "sideways"}, "the scroll direction must be"},
		{"scroll with --headed and no direction", []string{"--headed", "scroll", "101", "3"}, scrollUsage},
		{"drag with one point", []string{"drag", "101", "@1,1"}, dragUsage},
		{"hover with no point", []string{"hover", "101"}, hoverUsage},
		{"hover with a bad point", []string{"hover", "101", "1,1"}, "a coordinate reference must be @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stderr")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create stderr file: %v", err)
			}
			oldStderr, oldStdout := os.Stderr, os.Stdout
			os.Stderr, os.Stdout = f, f
			code := Main(tc.args)
			os.Stderr, os.Stdout = oldStderr, oldStdout
			f.Close()
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error)", code)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read stderr: %v", err)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestUsageMentionsPointerCommands(t *testing.T) {
	for _, cmd := range []string{"scroll", "drag", "hover"} {
		if !strings.Contains(Usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}

// TestDragAndHoverTakeElementsAndADropDelay: desktop-parity 13. Both
// commands take any Element (the helper routes a Page Element Headless and
// a UIA Element Headed), and drag takes --drop-delay <ms> anywhere, also
// as a Batch step.
func TestDragAndHoverTakeElementsAndADropDelay(t *testing.T) {
	cases := []struct {
		args []string
		want protocol.DragArgs
	}{
		{[]string{"101", "@1,2", "@3,4", "--drop-delay", "300"},
			protocol.DragArgs{Window: "101", FromX: 1, FromY: 2, ToX: 3, ToY: 4, DropDelayMS: 300, Headed: true}},
		{[]string{"101", "3", "--drop-delay=50", "id:target"},
			protocol.DragArgs{Window: "101", FromIndex: intPtr(3), ToRef: "id:target", DropDelayMS: 50, Headed: true}},
		{[]string{"101", "name:Card", "@40,50"},
			protocol.DragArgs{Window: "101", FromRef: "name:Card", ToX: 40, ToY: 50, Headed: true}},
	}
	for _, c := range cases {
		got, err := dragRequest(c.args, true)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("dragRequest(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	for _, bad := range [][]string{
		{"101", "3", "5", "--drop-delay"},
		{"101", "3", "5", "--drop-delay", "soon"},
		{"101", "3", "5", "--drop-delay=1.5"},
	} {
		if _, err := dragRequest(bad, true); err == nil || !strings.Contains(err.Error(), "--drop-delay needs a value in milliseconds") {
			t.Errorf("dragRequest(%q) = %v, want the --drop-delay usage error", bad, err)
		}
	}
	if h, err := hoverRequest([]string{"101", "id:canvas"}, true); err != nil || h.Ref != "id:canvas" || !h.Headed {
		t.Errorf("hover on an Element = %+v, %v", h, err)
	}

	steps := map[string]any{
		"drag @1,2 @3,4 --drop-delay 20": protocol.DragArgs{FromX: 1, FromY: 2, ToX: 3, ToY: 4, DropDelayMS: 20},
		"drag 3 5 --drop-delay=400":      protocol.DragArgs{FromIndex: intPtr(3), ToIndex: intPtr(5), DropDelayMS: 400},
		"hover id:canvas":                protocol.HoverArgs{Ref: "id:canvas"},
	}
	for line, want := range steps {
		step, err := parseBatchStep(strings.Fields(line))
		if err != nil {
			t.Errorf("batch step %q: %v", line, err)
			continue
		}
		got := reflect.New(reflect.TypeOf(want))
		if err := json.Unmarshal(step.Args, got.Interface()); err != nil || !reflect.DeepEqual(got.Elem().Interface(), want) {
			t.Errorf("batch step %q = %s, want %+v", line, step.Args, want)
		}
	}
	if _, err := parseBatchStep([]string{"drag", "3", "5", "--drop-delay"}); err == nil {
		t.Error("a batch drag with --drop-delay and no value was accepted")
	}
}
