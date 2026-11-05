package cli

// Offline tests for the help (help.go): the index, per-command help, the
// full reference, "did you mean", and the global flags accepted after the
// command. None of these paths reaches the helper.

import (
	"strings"
	"testing"
)

// TestEveryCommandHasHelp: every command Usage documents has an index line
// and examples, and every index entry has a block in Usage.
func TestEveryCommandHasHelp(t *testing.T) {
	documented := map[string]bool{}
	for _, b := range usageBlocks() {
		if len(b.words) == 0 {
			t.Fatalf("a Usage block has no command form: %q", b.text)
		}
		documented[b.words[0]] = true
	}
	if len(documented) < 40 {
		t.Fatalf("found only %d commands in Usage's blocks; the parser drifted", len(documented))
	}
	indexed := map[string]bool{}
	for _, g := range commandGroups {
		for _, c := range g.commands {
			if indexed[c.name] {
				t.Errorf("%s is in the index twice", c.name)
			}
			indexed[c.name] = true
			if !documented[c.name] {
				t.Errorf("the index lists %s, which Usage's commands: section does not document", c.name)
			}
			if c.summary == "" || len(c.examples) == 0 {
				t.Errorf("%s needs a summary and at least one example", c.name)
			}
			named := false
			for _, e := range c.examples {
				if !strings.HasPrefix(e, "winuse ") {
					t.Errorf("%s's example %q is not a winuse command line", c.name, e)
				}
				named = named || strings.Contains(e, " "+c.name)
			}
			if !named {
				t.Errorf("no example of %s runs it", c.name)
			}
		}
	}
	for name := range documented {
		if !indexed[name] {
			t.Errorf("Usage documents %s, which has no entry in commandGroups (help.go)", name)
		}
	}
}

func TestHelpPrintsTheIndexAndExits0(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"--headed"}, {"--json", "--help"}} {
		code, out := runMain(t, args...)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0", args, code)
		}
		if len(args) > 0 && args[0] == "--json" {
			if !strings.HasPrefix(out, `{"ok":true,"command":"--help","out":"usage: winuse`) {
				t.Errorf("%q: output %q, want the index as one JSON line", args, out[:min(len(out), 80)])
			}
			continue
		}
		if out != commandIndex() {
			t.Errorf("%q: printed %d bytes, want the %d-byte index", args, len(out), len(commandIndex()))
		}
	}
	// The index is the cheap way in: well under the 30 KB of the full
	// reference, which a harness's output cap would cut.
	if n := len(commandIndex()); n > 4<<10 {
		t.Errorf("the index is %d bytes; keep it under 4 KiB", n)
	}
}

func TestCommandHelpShowsExamplesThenUsage(t *testing.T) {
	want, ok := commandUsage([]string{"click"})
	if !ok {
		t.Fatal("no help for click")
	}
	for _, args := range [][]string{
		{"help", "click"}, {"click", "--help"}, {"click", "-h"}, {"click", "101", "--help"}, {"--help", "click"},
	} {
		code, out := runMain(t, args...)
		if code != 0 || out != want {
			t.Errorf("%q: exit %d, output %q; want exit 0 and click's help", args, code, out)
		}
	}
	ex, use := strings.Index(want, "\nexamples:\n  winuse click 4242 3\n"), strings.Index(want, "\nusage:\n  click <window>")
	if ex < 0 || use < ex {
		t.Errorf("click's help must show its examples, then its usage:\n%s", want)
	}
	if !strings.Contains(want, "--modifiers ctrl,shift,alt") || !strings.Contains(want, "<window> is a Window ID") {
		t.Errorf("click's help lacks its options or how to name a Window:\n%s", want)
	}
	for _, g := range commandGroups {
		for _, c := range g.commands {
			text, ok := commandUsage([]string{c.name})
			if !ok {
				t.Errorf("no help for %s", c.name)
				continue
			}
			if len(text) > 6<<10 {
				t.Errorf("help %s is %d bytes; keep a command's help under 6 KiB", c.name, len(text))
			}
		}
	}
}

func TestHelpNarrowsToASubcommand(t *testing.T) {
	code, out := runMain(t, "browser", "ext", "--help")
	if code != 0 || !strings.Contains(out, "  browser ext install") || strings.Contains(out, "  browser launch") {
		t.Errorf("browser ext --help: exit %d, output %q; want the browser ext blocks only", code, out)
	}
	code, out = runMain(t, "help", "wait")
	if code != 0 || !strings.Contains(out, "  wait window <app or title>") || !strings.Contains(out, "  wait notification") {
		t.Errorf("help wait: exit %d; want every wait form, got %q", code, out)
	}
}

func TestHelpAllIsTheFullReference(t *testing.T) {
	code, out := runMain(t, "help", "all")
	if code != 0 || out != Usage {
		t.Errorf("help all: exit %d, %d bytes; want exit 0 and Usage's %d", code, len(out), len(Usage))
	}
	code, out = runMain(t, "help", "global")
	if code != 0 || !strings.HasPrefix(out, "global options:\n") || !strings.Contains(out, "--json") ||
		!strings.Contains(out, "headless (the default") || !strings.Contains(out, "a <window> is a Window ID") || strings.Contains(out, "\ncommands:\n") {
		t.Errorf("help global: exit %d, output %q", code, out)
	}
}

func TestUnknownCommandSuggestsTheNearestOne(t *testing.T) {
	cases := []struct {
		args       []string
		near, form string
	}{
		{[]string{"clik", "101", "3"}, "click", "usage: winuse click <window>"},
		{[]string{"get-windowstate", "101"}, "get-window-state", "usage: winuse get-window-state <window>"},
		{[]string{"listwindows"}, "list-windows", "usage: winuse list-windows"},
		{[]string{"Click"}, "click", "usage: winuse click"},
		{[]string{"navi"}, "navigate", "usage: winuse navigate <window> <url>"},
		{[]string{"help", "clik"}, "click", "usage: winuse click"},
	}
	for _, tc := range cases {
		code, out := runMain(t, tc.args...)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", tc.args, code)
		}
		if !strings.Contains(out, "did you mean "+tc.near+"?") || !strings.Contains(out, tc.form) {
			t.Errorf("%q: output %q, want did you mean %s and its usage line", tc.args, out, tc.near)
		}
		if len(out) > 400 || strings.Count(out, "\n") != 3 {
			t.Errorf("%q: %d bytes in %d lines; a typo gets three short lines", tc.args, len(out), strings.Count(out, "\n"))
		}
	}
	code, out := runMain(t, "frobnicate")
	if code != 2 || strings.Contains(out, "did you mean") || !strings.Contains(out, "winuse help") {
		t.Errorf("frobnicate: exit %d, output %q; want exit 2, no guess, and where the commands are", code, out)
	}
}

func TestHoistGlobalFlags(t *testing.T) {
	cases := []struct{ args, want string }{
		{"click 101 3", "click 101 3"},
		{"click 101 3 --json", "--json click 101 3"},
		{"click --json 101 3", "--json click 101 3"},
		{"click 101 3 --trace t.jsonl", "--trace t.jsonl click 101 3"},
		{"click 101 3 --trace=t.jsonl --trace-unredacted", "--trace=t.jsonl --trace-unredacted click 101 3"},
		{"--headed click 101 3 --json", "--headed --json click 101 3"},
		{"click 101 3 --headed", "--headed click 101 3"},
		{"click --help", "--help click"},
		{"click 101 -h", "-h click 101"},
		{"browser ext status --help", "--help browser ext status"},
		// A bare --trace at the end is left for the command to refuse:
		// moved, it would take the command as its file.
		{"click 101 3 --trace", "click 101 3 --trace"},
		// Literal text keeps every flag.
		{"type-text 101 --json", "type-text 101 --json"},
		{"type-text 101 hello --help", "type-text 101 hello --help"},
		{"type-text --help", "--help type-text"},
		{"type-text 101 --into 3 --json", "type-text 101 --into 3 --json"},
		{"type-text 101 --json --into 3 hi", "--json type-text 101 --into 3 hi"},
		{"set-value 101 3 --json", "set-value 101 3 --json"},
		{"set-value 101 --help", "--help set-value 101"},
		{"clipboard-set --json", "clipboard-set --json"},
		{"batch 101 type-text --json then click 3 --json", "--json batch 101 type-text --json then click 3"},
		{"batch 101 set-value 4 --help", "batch 101 set-value 4 --help"},
		{"launch-app app --arg --json", "launch-app app --arg --json"},
		{"launch-app app --arg x --json", "--json launch-app app --arg x"},
	}
	for _, tc := range cases {
		if got := strings.Join(hoistGlobalFlags(split(tc.args)), " "); got != tc.want {
			t.Errorf("hoistGlobalFlags(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A trailing --json makes the line JSON, as a leading one does; the
// command's usage error shows it ran as the command.
func TestTrailingJSONIsTheGlobalFlag(t *testing.T) {
	code, out := runMain(t, "get-window-state", "--json")
	if code != 2 || !strings.HasPrefix(out, `{"ok":false,"command":"get-window-state"`) {
		t.Errorf("get-window-state --json: exit %d, output %q; want a JSON usage error", code, out)
	}
}
