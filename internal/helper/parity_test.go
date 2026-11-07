package helper

// The command parity test (browser merge ticket 06, like agent-browser's
// parity_tests.rs): every command the CLI's usage text documents has a
// helper handler, every handler is documented there, and every command
// SKILL.md shows in its code blocks has a handler. The other direction for
// SKILL.md (every command is mentioned in it) is internal/skill's
// TestSkillCoversFullCommandSurface.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cli"
)

// cliOnlyCommands run in the CLI and never reach the helper.
var cliOnlyCommands = map[string]bool{"version": true, "skill": true, "trace": true, "help": true, "update": true}

// groupCommands take a subcommand: "browser status" is the wire command
// browser-status, "browser ext install" browser-ext-install.
var groupCommands = map[string]bool{"browser": true, "tab": true}

// wireName turns a documented command line's words into its wire name.
func wireName(words []string) string {
	if len(words) == 0 {
		return ""
	}
	if !groupCommands[words[0]] || len(words) < 2 {
		return words[0]
	}
	if words[0] == "browser" && words[1] == "ext" && len(words) > 2 {
		return "browser-ext-" + words[2]
	}
	return words[0] + "-" + words[1]
}

var commandWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// usageCommands are the wire names of the commands in cli.Usage: the first
// words of every line indented two spaces under "commands:".
func usageCommands(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	in := false
	for _, line := range strings.Split(cli.Usage, "\n") {
		switch {
		case line == "commands:":
			in = true
		case in && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			var words []string
			for _, w := range strings.Fields(line) {
				if !commandWord.MatchString(w) {
					break
				}
				words = append(words, w)
			}
			if name := wireName(words); name != "" {
				out[name] = true
			}
		case in && line == "":
			in = false
		}
	}
	if len(out) < 20 {
		t.Fatalf("found only %d commands in cli.Usage; the parser drifted: %v", len(out), out)
	}
	return out
}

func TestEveryDocumentedCommandHasAHandler(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	documented := usageCommands(t)
	for name := range documented {
		if cliOnlyCommands[name] {
			continue
		}
		if _, ok := cmds[name]; !ok {
			t.Errorf("cli.Usage documents %q, and the helper has no handler for it", name)
		}
	}
	var undocumented []string
	for name := range cmds {
		if !documented[name] {
			undocumented = append(undocumented, name)
		}
	}
	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Errorf("the helper handles commands cli.Usage does not document: %v", undocumented)
	}
	for _, name := range []string{"navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval"} {
		if !documented[name] {
			t.Errorf("cli.Usage does not document %q", name)
		}
	}
}

// TestEveryCommandInTheSkillHasAHandler: a `winuse <command>` line in the
// code blocks of SKILL.md and its topics names a real command. (Ticket 08
// writes the skill's browser guidance; this keeps it honest.)
func TestEveryCommandInTheSkillHasAHandler(t *testing.T) {
	raw, err := os.ReadFile("../../skills/winuse/SKILL.md")
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	topics, err := filepath.Glob("../../skills/winuse/references/*.md")
	if err != nil || len(topics) == 0 {
		t.Fatalf("listing the skill's topics: %v (%d found)", err, len(topics))
	}
	for _, path := range topics {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		raw = append(append(raw, '\n'), b...)
	}
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	inBlock := false
	seen := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inBlock = !inBlock
			continue
		}
		fields := strings.Fields(line)
		if !inBlock || len(fields) < 2 || fields[0] != "winuse" {
			continue
		}
		var words []string
		for _, w := range fields[1:] {
			if w == "--headed" {
				continue
			}
			if !commandWord.MatchString(w) {
				break
			}
			words = append(words, w)
		}
		name := wireName(words)
		if name == "" || cliOnlyCommands[name] {
			continue
		}
		seen++
		if _, ok := cmds[name]; !ok {
			t.Errorf("SKILL.md shows %q (%q), which the helper does not handle", name, line)
		}
	}
	if seen < 10 {
		t.Fatalf("found only %d commands in SKILL.md's code blocks; the parser drifted", seen)
	}
}
