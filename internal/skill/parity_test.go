package skill

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cli"
)

// The recovered Codex Computer Use Windows surface (from
// .notes/codex-surface.md §1, OpenAI's api.md plus the reverse-engineered
// helper): every tool and every parameter. The parity checklist must
// cover all of them — that is the ticket's criterion, and this test is
// the proof it stays covered.
var codexTools = map[string][]string{
	"list_windows":             {},
	"get_window":               {"id", "app"},
	"list_apps":                {},
	"launch_app":               {"app"},
	"get_window_state":         {"window", "include_screenshot", "include_text"},
	"click":                    {"window", "element_index", "x", "y", "click_count", "mouse_button", "screenshotId"},
	"press_key":                {"window", "key"},
	"type_text":                {"window", "text"},
	"scroll":                   {"window", "x", "y", "scrollX", "scrollY", "screenshotId"},
	"set_value":                {"window", "element_index", "value"},
	"drag":                     {"window", "from_x", "from_y", "to_x", "to_y", "screenshotId"},
	"perform_secondary_action": {"window", "element_index", "action"},
	"activate_window":          {"window"},
}

// The cross-cutting behaviours the checklist must take a stance on.
var parityTopics = []string{
	"process model", "timeouts", "esc stop", "overlay", "error style",
	"admin windows", "locked desktop", "owned dialogs",
	"dpi and coordinates", "ui automation reads", "screenshots",
	"tree format", "secondary actions", "key names",
	"app identifiers", "one request at a time", "stale elements",
	"extensions beyond Codex",
}

var parityStatuses = []string{"matched", "intentionally different", "extension"}

// backtickSpan extracts every `token` from a table cell.
var backtickSpan = regexp.MustCompile("`([^`]+)`")

// commandLike is a hyphenated, backticked lowercase token — the shape of
// winuse command names.
var commandLike = regexp.MustCompile(`^[a-z]+(-[a-z]+)+$`)

func TestParityChecklistCoversTheCodexSurface(t *testing.T) {
	raw, err := os.ReadFile("../../docs/codex-parity.md")
	if err != nil {
		t.Fatalf("reading docs/codex-parity.md: %v", err)
	}
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")

	// Split into level-3 sections: a heading and its body (tables and
	// prose), so parameters are checked per tool and statuses per row.
	type section struct {
		heading string
		body    string
	}
	var sections []section
	heading := ""
	var body strings.Builder
	flush := func() {
		if heading != "" {
			sections = append(sections, section{heading, body.String()})
		}
		body.Reset()
	}
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "### ") {
			flush()
			heading = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			continue
		}
		if heading != "" {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()
	byHeading := map[string]section{}
	for _, s := range sections {
		byHeading[s.heading] = s
	}

	// Every Codex tool has a section, and every parameter a row.
	for tool, params := range codexTools {
		s, ok := byHeading[tool]
		if !ok {
			t.Errorf("the parity checklist has no section for Codex's %s", tool)
			continue
		}
		got := map[string]bool{}
		rows := 0
		for _, line := range strings.Split(s.body, "\n") {
			if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|--") || strings.HasPrefix(line, "| --") {
				continue
			}
			cells := strings.Split(line, "|")
			if len(cells) < 3 {
				continue
			}
			rows++
			if strings.EqualFold(strings.TrimSpace(cells[1]), "Parameter") {
				continue // the table's header row
			}
			status := strings.TrimSpace(cells[len(cells)-2]) // last cell, before the trailing |
			if !hasStatus(status) {
				t.Errorf("%s: row %q has no matched / intentionally different / extension status", tool, line)
			}
			for _, m := range backtickSpan.FindAllStringSubmatch(cells[1], -1) {
				got[m[1]] = true
			}
		}
		if rows == 0 {
			t.Errorf("%s: the section has no parameter rows", tool)
		}
		for _, p := range params {
			if !got[p] {
				t.Errorf("%s: parameter %q is not covered by a row", tool, p)
			}
		}
	}

	// Every cross-cutting topic is present and takes a stance.
	for _, topic := range parityTopics {
		s, ok := byHeading[topic]
		if !ok {
			t.Errorf("the parity checklist has no section for %q", topic)
			continue
		}
		if !hasStatus(s.body) {
			t.Errorf("%q does not mark its status (matched / intentionally different / extension)", topic)
		}
	}

	// A command the checklist names must exist on the real CLI.
	for _, m := range backtickSpan.FindAllStringSubmatch(doc, -1) {
		tok := m[1]
		if !commandLike.MatchString(tok) {
			continue
		}
		if !strings.Contains(cli.Usage, tok) {
			t.Errorf("the checklist names %q, which is not a winuse command (see cli.Usage)", tok)
		}
	}
}

// hasStatus reports whether the text marks one of the three statuses.
func hasStatus(s string) bool {
	for _, status := range parityStatuses {
		if strings.Contains(s, status) {
			return true
		}
	}
	return false
}
