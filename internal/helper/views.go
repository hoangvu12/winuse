package helper

// The output budgets of a full get-window-state (speed 05): what
// --interactive, --depth, --compact, --max-chars and --out print of a
// Window State. They shape the text only. The Window State the read
// stored is the whole tree, so every Element Index printed is the full
// read's and one left out still works.
//
//	winuse get-window-state <window> [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]
//
// Two of them are the default (speed 13): a read leaves out the IDs two
// or more of its Elements share (id: refuses those), and stops before
// defaultMaxChars characters with a note. --max-chars 0 lifts the cap;
// --full prints Codex's text byte for byte, every ID, never cut. find and
// --root print their lines the same way.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// defaultMaxChars is the cap a read prints within unless it passes
// --max-chars or --full: under Claude Code's 30,000-character cap on a
// Bash command's output, so the Harness never cuts a tree in the middle,
// with 2,000 left for the shell's own lines. The --max-chars note, and
// the Screenshot line (screenshotReserve), fit within it.
const defaultMaxChars = 28000

// screenshotReserve is the room a read with --screenshot keeps under its
// cap for the Screenshot line printed after the tree (its %TEMP% path
// included).
const screenshotReserve = 300

// maxChars is the cap a's tree prints within: --max-chars, else the
// default; 0 under --full.
func (a windowRef) maxChars() int {
	switch {
	case a.Full:
		return 0
	case a.MaxChars != nil:
		return *a.MaxChars
	}
	return defaultMaxChars
}

// view is the wstate.View a's flags ask for; ref names the Window in its
// notes' commands.
func (a windowRef) view(ref string) wstate.View {
	limit := a.maxChars()
	if limit > 0 && a.Screenshot {
		limit = max(limit-screenshotReserve, 1)
	}
	return wstate.View{Full: a.Full, Interactive: a.Interactive, Depth: a.Depth, Compact: a.Compact, MaxChars: limit, Ref: ref}
}

// regionCut is how a --root read prints its lines: a's cap and, unless
// --full, without the region's shared IDs.
func (a windowRef) regionCut(shared map[string]bool) findCut {
	c := findCut{maxChars: a.maxChars(), narrow: "Narrow the region with a deeper --root, --depth or --interactive, or raise --max-chars"}
	if !a.Full {
		c.shared = shared
	}
	return c
}

// budgetError refuses the output budgets a read cannot take together.
func budgetError(a windowRef) *Error {
	explicitCap := a.MaxChars != nil && *a.MaxChars != 0
	switch {
	case a.Depth < 0 || a.MaxChars != nil && *a.MaxChars < 0:
		return Fail("get-window-state: --depth takes a positive number and --max-chars a number (0: no cap)", windowStateUsage)
	case a.Full && (a.Compact || a.MaxChars != nil || a.Root == "" && (a.Interactive || a.Depth != 0)):
		return Fail("get-window-state: --full prints the Window State as Codex does, whole and uncut; it takes no --interactive, --depth, --compact or --max-chars (with --root, --depth and --interactive still shape the read)", windowStateUsage)
	case explicitCap && a.Out != "":
		return Fail("get-window-state: pass --out or --max-chars, not both: --out writes the whole Window State to the file", windowStateUsage)
	}
	return nil
}

// renderState prints a full read's Window State through the view a's
// flags ask for or, with --out, writes it to that file, uncut, and prints
// its header, the file's line and the tail (the focused Element and the
// notes), which carry what the agent must not miss: a partial read, a
// thin tree.
func renderState(tree *wstate.Tree, a windowRef, ref string) (string, *Error) {
	if e := budgetError(a); e != nil {
		return "", e
	}
	v := a.view(ref)
	if a.Out == "" {
		return wstate.RenderView(tree, v), nil
	}
	v.MaxChars = 0
	text := wstate.RenderView(tree, v)
	if err := writeFileAtomic(a.Out, []byte(text)); err != nil {
		return "", Fail("writing the Window State to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
	}
	interactive := 0
	for i := range tree.Elements {
		if tree.Elements[i].Interactive() {
			interactive++
		}
	}
	head, _, _ := strings.Cut(text, "\n")
	var b strings.Builder
	b.WriteString(head + "\n")
	fmt.Fprintf(&b, "Window State: %s (%d Elements, %d interactive; %d characters; its Element Indexes work as printed there)\n",
		a.Out, len(tree.Elements), interactive, utf8.RuneCountInString(text))
	if _, tail, ok := strings.Cut(text, "\n\n"); ok {
		b.WriteString("\n" + tail)
	}
	return b.String(), nil
}
