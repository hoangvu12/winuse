package wstate

// The --observe token measurement (speed observe, ADR 0021): for each
// Window State and each simulated Action effect, what --observe=diff
// prints next to what --observe=full prints, written as JSON lines for a
// tokenizer. Opt-in, so it never runs in go test ./...:
//
//	$env:WINUSE_OBSERVE_COST_OUT = "<file.jsonl>"
//	$env:WINUSE_OBSERVE_TREES = "<trees.json>"   # optional: a JSON array of printed Window States
//	go test ./internal/wstate -run TestObserveCost -count=1
//
// Without WINUSE_OBSERVE_TREES it measures the recorded fixtures alone.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// costCase is one measured observation.
type costCase struct {
	Tree     string `json:"tree"`
	Elements int    `json:"elements"`
	Effect   string `json:"effect"`
	Changes  int    `json:"changes"`
	Whole    bool   `json:"whole"`
	Diff     string `json:"diff"`
	Full     string `json:"full"`
}

var elementLine = regexp.MustCompile(`^(\t+)(\d+) (.*)$`)

// parsePrinted turns a printed Window State back into a Tree whose
// Elements print the same lines: each line's text after its index becomes
// the Element's role, the one field Render prints when the rest are empty,
// but its trailing ID, kept as the AutomationId so the default view can
// leave a shared one out.
func parsePrinted(text string) (*Tree, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], `Window: "`) {
		return nil, false
	}
	t := &Tree{FocusedIndex: -1}
	head := strings.TrimSuffix(strings.TrimPrefix(lines[0], `Window: "`), ".")
	t.Title, t.App, _ = strings.Cut(head, `", App: `)
	i := 1
	for ; i < len(lines); i++ {
		m := elementLine.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		if n, _ := strconv.Atoi(m[2]); n != len(t.Elements) {
			return nil, false
		}
		e := Element{Depth: len(m[1]) - 1, Role: m[3]}
		if at := strings.LastIndex(e.Role, " ID: "); at >= 0 && !strings.Contains(e.Role[at:], " (") {
			e.Role, e.AutomationId = e.Role[:at], e.Role[at+5:]
		}
		t.Elements = append(t.Elements, e)
	}
	for ; i < len(lines); i++ {
		if rest, ok := strings.CutPrefix(lines[i], "The focused UI element is "); ok {
			n, _, _ := strings.Cut(rest, " ")
			t.FocusedIndex, _ = strconv.Atoi(n)
		} else if rest, ok := strings.CutPrefix(lines[i], "Note: "); ok {
			t.Notes = append(t.Notes, rest)
		}
	}
	return t, len(t.Elements) > 1
}

// flat is a tree whose Elements print their lines from the role alone
// (parsePrinted's form), so the effects below edit the printed text.
func flat(t *Tree) *Tree {
	p, _ := parsePrinted(Render(t))
	return p
}

// effects are simulated Action effects on a flat tree: each returns the
// tree after, or false when the tree has nothing it applies to.
var effects = []struct {
	name string
	do   func(t *Tree) bool
}{
	{"no change", func(t *Tree) bool { return true }},
	{"set a field's value", func(t *Tree) bool {
		return editFirst(t, func(r string) (string, bool) {
			if !strings.Contains(r, "(settable, string)") {
				return "", false
			}
			if at := strings.Index(r, " Value: "); at >= 0 {
				return r[:at] + " Value: invoice 2026" + cutField(r[at+8:]), true
			}
			if at := strings.Index(r, " ID: "); at >= 0 {
				return r[:at] + " Value: invoice 2026" + r[at:], true
			}
			return r + " Value: invoice 2026", true
		})
	}},
	{"toggle a check box", func(t *Tree) bool {
		return editFirst(t, func(r string) (string, bool) {
			switch {
			case strings.Contains(r, " off"):
				return strings.Replace(r, " off", " on", 1), true
			case strings.Contains(r, " on Secondary Actions: Toggle"):
				return strings.Replace(r, " on", " off", 1), true
			}
			return "", false
		})
	}},
	{"status text changes", func(t *Tree) bool {
		for i := len(t.Elements) - 1; i > 0; i-- {
			if strings.HasPrefix(t.Elements[i].Role, "text ") {
				t.Elements[i].Role = "text Saved at 10:42"
				return true
			}
		}
		return false
	}},
	{"three Elements appear at the end", func(t *Tree) bool {
		t.Elements = append(t.Elements,
			Element{Depth: 1, Role: "group Notification"},
			Element{Depth: 2, Role: "text Your changes were saved"},
			Element{Depth: 2, Role: "button Undo"})
		return true
	}},
	{"a node expands in the middle", func(t *Tree) bool {
		mid := len(t.Elements) / 2
		d := t.Elements[mid].Depth + 1
		ins := []Element{{Depth: d, Role: "tree item Alpha"}, {Depth: d, Role: "tree item Bravo"}, {Depth: d, Role: "tree item Charlie"}}
		t.Elements = append(t.Elements[:mid+1], append(ins, t.Elements[mid+1:]...)...)
		return true
	}},
}

// editFirst edits the first Element f applies to.
func editFirst(t *Tree, f func(role string) (string, bool)) bool {
	for i := 1; i < len(t.Elements); i++ {
		if r, ok := f(t.Elements[i].Role); ok {
			t.Elements[i].Role = r
			return true
		}
	}
	return false
}

// cutField drops a Value's text up to the next field.
func cutField(s string) string {
	for _, f := range []string{" Description: ", " Secondary Actions: ", " ID: ", " on", " off"} {
		if at := strings.Index(s, f); at >= 0 {
			return s[at:]
		}
	}
	return ""
}

func TestObserveCost(t *testing.T) {
	out := os.Getenv("WINUSE_OBSERVE_COST_OUT")
	if out == "" {
		t.Skip("set WINUSE_OBSERVE_COST_OUT to measure --observe's output")
	}
	trees := map[string]*Tree{}
	for _, name := range []string{"fixture-app", "word-style"} {
		tree, err := LoadFixture(filepath.Join("testdata", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		trees["fixture "+name] = flat(tree)
	}
	if path := os.Getenv("WINUSE_OBSERVE_TREES"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var printed []string
		if err := json.Unmarshal(data, &printed); err != nil {
			t.Fatal(err)
		}
		for i, text := range printed {
			if tree, ok := parsePrinted(text); ok {
				trees["state "+strconv.Itoa(i+1)] = tree
			}
		}
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for name, before := range trees {
		for _, e := range effects {
			after := &Tree{Title: before.Title, App: before.App, FocusedIndex: before.FocusedIndex, Notes: before.Notes,
				Elements: append([]Element(nil), before.Elements...)}
			if !e.do(after) {
				continue
			}
			v := View{Ref: "1", MaxChars: 28000} // a read's defaults (helper defaultMaxChars)
			got := RenderChanges(before.Elements, after, v, "window 1")
			if err := enc.Encode(costCase{Tree: name, Elements: len(before.Elements), Effect: e.name,
				Changes: got.Changes, Whole: got.Whole, Diff: got.Text, Full: RenderView(after, v)}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestParsePrintedRoundTrips(t *testing.T) {
	tree := fixtureApp(t)
	if got := Render(flat(tree)); got != Render(tree) {
		t.Errorf("a printed Window State parses back to the same lines:\n%s\nwant:\n%s", got, Render(tree))
	}
}
