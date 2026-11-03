package cli

// The stdin Batch's tests (desktop-parity ticket 20): the Window line and
// its flags, one step per line with no then, quoting, long text, skipped
// blank and # lines, and usage errors that name their line and exit 2
// without dialing the helper.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseBatchScript(t *testing.T) {
	script := "\ufeff# fill the form\r\n" +
		"101 --stop-on-error --timeout 30\r\n" +
		"\r\n" +
		"click name:\"File name\"\r\n" +
		"   # an indented comment\n" +
		"type-text \"say \\\"hi\\\"  then   go\" and C:\\temp\\a.txt\n" +
		"press-key Return\n" +
		"set-value id:field \"\"\n"
	window, steps, timeout, stop, err := parseBatchScript(script)
	if err != nil {
		t.Fatalf("parseBatchScript: %v", err)
	}
	if window != "101" || timeout != 30*time.Second || !stop {
		t.Errorf("head = %q, %v, %v; want 101, 30s, stop", window, timeout, stop)
	}
	want := []struct{ kind, args string }{
		{"click", `{"window":"","element_index":null,"ref":"name:File name","click_count":1,"mouse_button":"left"}`},
		// The quoted words keep their spacing and quotes, a then inside a
		// line is text, and a backslash outside quotes is itself.
		{"type-text", `{"window":"","text":"say \"hi\"  then   go and C:\\temp\\a.txt"}`},
		{"press-key", `{"window":"","key":"Return"}`},
		{"set-value", `{"window":"","element_index":null,"ref":"id:field","value":""}`},
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %+v, want %d", steps, len(want))
	}
	for i, w := range want {
		var got, exp any
		json.Unmarshal(steps[i].Args, &got)
		json.Unmarshal([]byte(w.args), &exp)
		if steps[i].Kind != w.kind || !reflect.DeepEqual(got, exp) {
			t.Errorf("step %d = %s %s, want %s %s", i+1, steps[i].Kind, steps[i].Args, w.kind, w.args)
		}
	}
}

func TestParseBatchScriptLongTextOnALine(t *testing.T) {
	// Text far past a command line's length, on one line, arrives whole.
	text := strings.Repeat("αβγ déjà vu, ", 2000)
	_, steps, _, _, err := parseBatchScript("101\ntype-text \"" + text + "\"\n")
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, %v", steps, err)
	}
	var a protocol.TypeTextArgs
	if err := json.Unmarshal(steps[0].Args, &a); err != nil || a.Text != text {
		t.Errorf("typed %d bytes, want the %d of the line (%v)", len(a.Text), len(text), err)
	}
}

func TestParseBatchScriptErrorsNameTheLine(t *testing.T) {
	cases := []struct {
		name, script, want string
	}{
		{"an unknown action", "101\nclick 3\n\nzoom 1,2,3,4\n", `batch stdin line 4: unknown batch action "zoom"`},
		{"a bad step", "# c\n101\nscroll @1,2 3\n", "batch stdin line 3: a batch scroll Action takes an @x,y point and two scroll amounts"},
		{"an open quote", "101\nclick 3\ntype-text \"no end\n", "batch stdin line 3: a double quote is not closed"},
		{"a step on the Window's line", "101 click 3\n", `batch stdin line 1: the first line takes the Window and the Batch flags (--timeout, --stop-on-error); "click" is neither`},
		{"--headed on the first line", "101 --headed\nclick 3\n", "batch stdin line 1: --headed goes on the command line: winuse --headed batch -"},
		{"a bad timeout", "\n101 --timeout 900\nclick 3\n", "batch stdin line 2: --timeout must be a number of seconds above 0 and at most 300"},
		{"no Window line", "\n# only comments\n", "batch - read no Window from stdin"},
		{"no Action", "101 --stop-on-error\n", "batch - read no Action from stdin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, _, err := parseBatchScript(c.script)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want %q", err, c.want)
			}
		})
	}
	if _, _, _, _, err := batchFromStdin([]string{"101"}, strings.NewReader("101\nclick 3\n")); err == nil || !strings.Contains(err.Error(), "batch - takes nothing after the -") {
		t.Errorf("batch - 101: %v, want the refusal", err)
	}
}

func TestBatchStdinUsageErrorExits2WithoutDialing(t *testing.T) {
	// A bad line is a usage error: exit 2, the line named, the usage shown,
	// and the helper never dialed.
	dir := t.TempDir()
	in := filepath.Join(dir, "stdin")
	if err := os.WriteFile(in, []byte("101\nclick 3\nclick @1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	errPath := filepath.Join(dir, "stderr")
	f, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldErr, oldOut := os.Stdin, os.Stderr, os.Stdout
	os.Stdin, os.Stderr, os.Stdout = stdin, f, f
	code := Main([]string{"batch", "-"})
	os.Stdin, os.Stderr, os.Stdout = oldIn, oldErr, oldOut
	f.Close()
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	got, _ := os.ReadFile(errPath)
	if !strings.Contains(string(got), "batch stdin line 3: ") || !strings.Contains(string(got), "winuse batch -") {
		t.Errorf("stderr = %q, want line 3 named and the usage", got)
	}
}
