package helper

// The output budgets of a full get-window-state (speed 05): what
// --interactive, --depth, --compact, --max-chars and --out print of a
// Window State. They shape the text only. The Window State the read
// stored is the whole tree, so every Element Index printed is the full
// read's and one left out still works.
//
//	winuse get-window-state <window> [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>]

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// view is the wstate.View a's flags ask for; ref names the Window in its
// notes' commands.
func (a windowRef) view(ref string) wstate.View {
	return wstate.View{Interactive: a.Interactive, Depth: a.Depth, Compact: a.Compact, MaxChars: a.MaxChars, Ref: ref}
}

// renderState prints a full read's Window State through the view a's
// flags ask for or, with --out, writes it to that file and prints its
// header, the file's line and the tail (the focused Element and the
// notes), which carry what the agent must not miss: a partial read, a
// thin tree.
func renderState(tree *wstate.Tree, a windowRef, ref string) (string, *Error) {
	if a.Out == "" {
		return wstate.RenderView(tree, a.view(ref)), nil
	}
	if a.MaxChars != 0 {
		return "", Fail("get-window-state: pass --out or --max-chars, not both: --out writes the whole Window State to the file", windowStateUsage)
	}
	text := wstate.RenderView(tree, a.view(ref))
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
