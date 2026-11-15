package wstate

// The hint token measurement (learn-from 04): for each Window State, what
// the default view, --interactive and a find of every control print with
// the hints and without them, written as JSON lines for a tokenizer.
// Opt-in, so it never runs in go test ./...:
//
//	$env:WINUSE_HINT_COST_OUT = "<file.jsonl>"
//	$env:WINUSE_HINT_TREES = "<trees.json>"   # optional: a JSON array of printed Window States
//	go test ./internal/wstate -run TestHintCost -count=1
//
// Without WINUSE_HINT_TREES it measures the recorded fixtures alone. A
// printed state carries no reader hint (a Page's icon, image or test id),
// so it measures the row part alone; the recorded fixtures likewise.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// hintCase is one measured view of one Window State.
type hintCase struct {
	Tree     string `json:"tree"`
	Elements int    `json:"elements"`
	View     string `json:"view"`
	Hinted   int    `json:"hinted"` // lines with a hint
	With     string `json:"with"`
	Without  string `json:"without"`
}

// knownRoles are the roles a printed line can start with, longest first.
var knownRoles = func() []string {
	var out []string
	for _, r := range roleNames {
		out = append(out, r)
	}
	out = append(out, "heading", "dialog", "clickable")
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}()

var (
	moreRe  = regexp.MustCompile(` \(\+(\d+) children\)$`)
	truncRe = regexp.MustCompile(` \(truncated: (.*), omitted (\d+) children\)$`)
)

// parseLine reads one printed Element line's text after its index back
// into an Element, and reports whether it prints the same text again.
func parseLine(text string) (Element, bool) {
	var e Element
	rest := text
	if m := moreRe.FindStringSubmatch(rest); m != nil {
		e.MoreChildren, _ = strconv.Atoi(m[1])
		rest = rest[:len(rest)-len(m[0])]
	}
	if m := truncRe.FindStringSubmatch(rest); m != nil {
		n, _ := strconv.Atoi(m[2])
		e.Truncated = &Truncation{Limit: m[1], OmittedChildren: n}
		rest = rest[:len(rest)-len(m[0])]
	}
	if at := strings.LastIndex(rest, " ID: "); at >= 0 {
		rest, e.AutomationId = rest[:at], rest[at+5:]
	}
	if at := strings.LastIndex(rest, " Secondary Actions: "); at >= 0 {
		rest, e.SecondaryActions = rest[:at], strings.Split(rest[at+20:], ", ")
	}
	for _, tg := range []string{"on", "off", "indeterminate"} {
		if strings.HasSuffix(rest, " "+tg) {
			rest, e.Toggle = strings.TrimSuffix(rest, " "+tg), tg
			break
		}
	}
	if at := strings.Index(rest, " Value: "); at >= 0 {
		rest, e.Value = rest[:at], rest[at+8:]
	}
	if at := strings.Index(rest, " Description: "); at >= 0 {
		rest, e.Description = rest[:at], rest[at+14:]
	}
	for _, r := range knownRoles {
		if rest == r || strings.HasPrefix(rest, r+" ") {
			e.Role, rest = r, strings.TrimPrefix(rest[len(r):], " ")
			break
		}
	}
	if e.Role == "" {
		return Element{Role: text}, false
	}
	if strings.HasPrefix(rest, "(") {
		if end := strings.Index(rest, ")"); end > 0 && parseStates(&e, rest[1:end]) {
			rest = strings.TrimPrefix(rest[end+1:], " ")
		}
	}
	e.Name = rest
	if Line(0, &e) != "0 "+text {
		return Element{Role: text}, false
	}
	return e, true
}

// parseStates sets the states a "(...)" lists, or reports it is no state
// list.
func parseStates(e *Element, s string) bool {
	parts := strings.Split(s, ", ")
	for i := 0; i < len(parts); i++ {
		switch p := parts[i]; p {
		case "selectable":
			e.Selectable = true
		case "selected":
			e.Selected = true
		case "disabled":
			e.Disabled = true
		case "collapsed", "expanded", "partially expanded":
			e.ExpandState = p
		case "settable":
			if i+1 >= len(parts) {
				return false
			}
			e.Settable = parts[i+1]
			i++
		default:
			return false
		}
	}
	return true
}

// parseState is parsePrinted with whole Elements.
func parseState(text string) (*Tree, int, bool) {
	t, ok := parsePrinted(text)
	if !ok {
		return nil, 0, false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	parsed := 0
	for i := range t.Elements {
		m := elementLine.FindStringSubmatch(lines[i+1])
		e, ok := parseLine(m[3])
		e.Depth = t.Elements[i].Depth
		if ok {
			parsed++
		}
		t.Elements[i] = e
	}
	return t, parsed, true
}

var hintSuffix = regexp.MustCompile(` Hint: [^\n]*`)

func TestHintCost(t *testing.T) {
	out := os.Getenv("WINUSE_HINT_COST_OUT")
	if out == "" {
		t.Skip("set WINUSE_HINT_COST_OUT to measure the hints' cost")
	}
	trees := map[string]*Tree{}
	for _, name := range []string{"fixture-app", "word-style"} {
		tree, err := LoadFixture(filepath.Join("testdata", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		trees["fixture "+name] = tree
	}
	if path := os.Getenv("WINUSE_HINT_TREES"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var printed []string
		if err := json.Unmarshal(data, &printed); err != nil {
			t.Fatal(err)
		}
		for i, text := range printed {
			if tree, parsed, ok := parseState(text); ok {
				trees["state "+strconv.Itoa(i+1)] = tree
				t.Logf("state %d: %d of %d lines parsed", i+1, parsed, len(tree.Elements))
			}
		}
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for name, tree := range trees {
		hints := Hints(tree.Elements)
		var find, findPlain strings.Builder
		for i := range tree.Elements {
			if !hintRoles[tree.Elements[i].Role] {
				continue
			}
			find.WriteString("\t" + HintedLine(i, &tree.Elements[i], hintAt(hints, i)) + "\n")
			findPlain.WriteString("\t" + Line(i, &tree.Elements[i]) + "\n")
		}
		for _, c := range []struct {
			view string
			with string
		}{
			{"default", RenderView(tree, View{Ref: "1"})},
			{"--interactive", RenderView(tree, View{Interactive: true, Ref: "1"})},
			{"find every control", find.String()},
		} {
			without := hintSuffix.ReplaceAllString(c.with, "")
			if c.view == "find every control" {
				without = findPlain.String()
			}
			if err := enc.Encode(hintCase{Tree: name, Elements: len(tree.Elements), View: c.view,
				Hinted: strings.Count(c.with, " Hint: "), With: c.with, Without: without}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestParseLineRoundTrips(t *testing.T) {
	for _, tree := range []*Tree{fixtureApp(t), hintTree()} {
		for i := range tree.Elements {
			want := tree.Elements[i]
			want.Depth, want.Hint, want.RowText = 0, "", ""
			text := strings.TrimPrefix(Line(0, &want), "0 ")
			if got, ok := parseLine(text); !ok || Line(0, &got) != "0 "+text {
				t.Errorf("line %q parsed to %+v", text, got)
			}
		}
	}
}
