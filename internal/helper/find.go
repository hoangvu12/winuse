package helper

// find, region reads and the skeleton (desktop-parity ticket 04,
// agent-desktop's `find`, `--root` and `--skeleton`): ways to work in a
// dense Window without reading, or rereading, all of it.
//
//	winuse find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --limit N | --count] [--max-chars N]
//	winuse get-window-state <window> --root <element> [--depth N] [--interactive]
//	winuse get-window-state <window> --skeleton
//
// A full read takes the output budgets too (speed 05, views.go):
// --interactive, --depth, --compact, --max-chars and --out shape what is
// printed of the Window State, which is stored whole.
//
// find walks the live tree past a printed Window State's caps and prints
// the matching Elements' lines; --root reads one Element's subtree. Both
// add the Elements they print after the Window State's own Element
// Indexes (the user's option c), so the indexes the agent already has keep
// working until the next full read, which renumbers from 0 again. The
// skeleton is a full read three levels deep with "(+N children)" where it
// stops, so it replaces the Window State like any full read.
//
// A browser Window's Page is read over CDP whole, so find there filters a
// fresh full read (which replaces the earlier indexes, said in a note),
// and --root on a Page Element prints its subtree from the stored Window
// State with the indexes it already has.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// Finder is the tree reader's side of find and the region read:
// *uia.Client.
type Finder interface {
	Find(ctx context.Context, hwnd uint64, budget time.Duration, match func(wstate.Element) bool, limit int, countOnly bool) (uia.FindResult, error)
	ReadRegion(ctx context.Context, hwnd uint64, index, depth int, interactive bool, budget time.Duration) (uia.Added, error)
}

var _ Finder = (*uia.Client)(nil)

// findUsage is find's usage line.
const findUsage = "usage: winuse find <window> [--role <role>] [--name <text>] [--text <text>] [--state <state>] [--first | --limit N | --count] [--max-chars N] [--timeout <seconds>]"

// find answers `winuse find <window> ...`.
func find(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.FindArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("find: "+err.Error(), findUsage)
		}
		if a.First && a.Count || a.Limit > 0 && (a.First || a.Count) {
			return "", Fail("find: pass one of --first, --limit and --count", findUsage)
		}
		if a.Limit < 0 || a.MaxChars != nil && *a.MaxChars < 0 {
			return "", Fail("find: --limit takes a positive number and --max-chars a number (0: no cap)", findUsage)
		}
		if a.Full && a.MaxChars != nil {
			return "", Fail("find: --full prints every match uncut, with every ID; it takes no --max-chars", findUsage)
		}
		if a.First {
			a.Limit = 1
		}
		if a.State != "" && !wstate.ValidState(a.State) {
			return "", Fail(fmt.Sprintf("find: unknown state %q", a.State), "a state is one of "+strings.Join(wstate.States, ", "))
		}
		q := wstate.Query{Role: a.Role, Name: a.Name, Text: a.Text, State: a.State}
		w, rerr := resolveWindow(s, a.Window)
		if rerr != nil {
			return "", rerr
		}
		if s.Pages != nil && s.Pages.Browser(w) != "" {
			return findInFreshRead(s, ctx, w, q, a)
		}
		f, ok := s.Trees.(Finder)
		if !ok {
			return "", Fail("find is unavailable: no UIA client", "")
		}
		res, err := f.Find(ctx, w.ID, s.Budget, q.Match, a.Limit, a.Count)
		if err != nil {
			return "", treeReadError(err)
		}
		if a.Count {
			return strconv.Itoa(res.Count) + "\n", nil
		}
		if res.Count == 0 {
			return "", noMatch(w, q, res.RolesPresent)
		}
		cut := newFindCut(a, res.Count, res.SharedIDs)
		return renderAdded(w, res.Added, true, "matched "+q.String(), cut), nil
	}
}

// findCut is what find or a region read leaves out of its answer: the
// matches past --limit, the lines past --max-chars, and the IDs the
// walk's Elements share (speed 13). The zero findCut leaves nothing out.
type findCut struct {
	limit, matched int // --limit, and every match
	maxChars       int
	shared         map[string]bool // IDs left out of the lines
	narrow         string          // how to see the lines --max-chars cut, after "Narrow ..."; "" for find's
}

// newFindCut is find's cut: --limit, --max-chars or the default cap, and
// the walk's shared IDs, all off with --full.
func newFindCut(a protocol.FindArgs, matched int, shared map[string]bool) findCut {
	c := findCut{limit: a.Limit, matched: matched}
	if a.Full {
		return c
	}
	c.maxChars, c.shared = defaultMaxChars, shared
	if a.MaxChars != nil {
		c.maxChars = *a.MaxChars
	}
	return c
}

// line is Element e's line at index, indented by tabs, its shared ID left
// out.
func (c findCut) line(tabs, index int, e wstate.Element) wstate.PrintedLine {
	e = e.WithoutSharedID(c.shared)
	return wstate.PrintedLine{Index: index, Text: strings.Repeat("\t", tabs) + wstate.Line(index, &e) + "\n"}
}

// limitNote is the note on the matches --limit left out, or "".
func (c findCut) limitNote(shown int) string {
	if c.limit <= 0 || c.matched <= shown {
		return ""
	}
	return fmt.Sprintf("%sthese are the first %d of %d matches (--limit %d); narrow the filters, or pass a larger --limit\n", notePrefix, shown, c.matched, c.limit)
}

// hint is how to see the lines --max-chars did not print.
func (c findCut) hint() string {
	if c.narrow != "" {
		return c.narrow
	}
	return "Narrow the filters, pass --limit, or raise --max-chars"
}

// findInFreshRead is find on a browser Window: a full read (its Page over
// CDP), filtered, printed with the read's own Element Indexes.
func findInFreshRead(s Services, ctx context.Context, w win.WindowInfo, q wstate.Query, a protocol.FindArgs) (string, *Error) {
	tree, terr := readWindowTree(s, ctx, w)
	if terr != nil {
		return "", terr
	}
	var matches []int
	for i := range tree.Elements {
		if q.Match(tree.Elements[i]) {
			matches = append(matches, i)
		}
	}
	if a.Count {
		return strconv.Itoa(len(matches)) + "\n", nil
	}
	if len(matches) == 0 {
		return "", noMatch(w, q, wstate.RolesPresent(tree.Elements))
	}
	cut := newFindCut(a, len(matches), wstate.SharedIDs(tree.Elements))
	var lines []wstate.PrintedLine
	for _, i := range matches {
		if a.Limit <= 0 || len(lines) < a.Limit {
			lines = append(lines, cut.line(1, i, tree.Elements[i]))
		}
	}
	var tail strings.Builder
	how := " through its Browser Connection"
	for _, n := range tree.Notes {
		tail.WriteString(notePrefix + n + "\n")
		if strings.HasPrefix(n, page.FallbackPrefix) {
			how = "" // read through UI Automation: saying otherwise contradicted the note above (browser-ux 07)
		}
	}
	tail.WriteString(cut.limitNote(len(lines)))
	fmt.Fprintf(&tail, "%sfind read window %d's Window State afresh%s, so its earlier Element Indexes were replaced by these\n", notePrefix, w.ID, how)
	return wstate.Cut(header(w), lines, tail.String(), cut.maxChars, cut.hint()), nil
}

// noMatch is find's failure when nothing matched; a role filter lists the
// roles the Window shows.
func noMatch(w win.WindowInfo, q wstate.Query, roles []string) *Error {
	hint := "loosen the filters, or read the Window State: winuse get-window-state " + strconv.FormatUint(w.ID, 10)
	if q.Role != "" {
		hint = "the Window shows these roles: " + strings.Join(roles, ", ")
	}
	return Fail(fmt.Sprintf("no Element of window %d matches %s; nothing was added", w.ID, q), hint)
}

// header is a Window State's header line for w.
func header(w win.WindowInfo) string {
	return fmt.Sprintf("Window: %q, App: %s.\n", wstate.Collapse(w.Title), win.AppExeName(w))
}

// renderAdded prints Elements find or a region read added: flat (find) or
// indented by depth (a region), then the walk's notes, what cut left out,
// and the numbering note.
func renderAdded(w win.WindowInfo, added uia.Added, flat bool, what string, cut findCut) string {
	lines := make([]wstate.PrintedLine, len(added.Elements))
	for i, e := range added.Elements {
		tabs := 1
		if !flat {
			tabs = e.Depth + 1
		}
		lines[i] = cut.line(tabs, added.Indexes[i], e)
	}
	var tail strings.Builder
	for _, n := range added.Notes {
		tail.WriteString(notePrefix + n + "\n")
	}
	tail.WriteString(cut.limitNote(len(lines)))
	fmt.Fprintf(&tail, "%sthese Elements (%s) are in window %d's Window State as %s; its earlier Element Indexes still work until the next full get-window-state\n",
		notePrefix, what, w.ID, indexList(added.Indexes))
	return wstate.Cut(header(w), lines, tail.String(), cut.maxChars, cut.hint())
}

// indexList names Element Indexes: "241 to 245" when they run in order,
// else each one.
func indexList(idx []int) string {
	if len(idx) == 1 {
		return strconv.Itoa(idx[0])
	}
	run := true
	for i := 1; i < len(idx); i++ {
		run = run && idx[i] == idx[i-1]+1
	}
	if run && len(idx) > 1 {
		return fmt.Sprintf("%d to %d", idx[0], idx[len(idx)-1])
	}
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

// windowStateShape answers the get-window-state forms that read part of a
// Window, or its outline: --root and --skeleton. handled is false for a
// plain read.
func windowStateShape(s Services, ctx context.Context, w win.WindowInfo, a windowRef) (out string, rerr *Error, handled bool) {
	if e := budgetError(a); e != nil {
		return "", e, true
	}
	switch {
	case a.Skeleton && a.Root != "":
		return "", Fail("get-window-state: --skeleton reads the whole Window and --root one Element; pass one", windowStateUsage), true
	case a.Skeleton && a.Depth != 0:
		return "", Fail(fmt.Sprintf("get-window-state: --skeleton reads %d levels; --depth goes with a full read or --root <element>", uia.SkeletonDepth), windowStateUsage), true
	case a.Root != "" && (a.Compact || a.Out != ""):
		return "", Fail("get-window-state: --compact and --out shape a full read; --root takes --depth, --interactive, --max-chars and --full", windowStateUsage), true
	case a.Skeleton:
		tree, err := s.Trees.Capture(uia.WithSkeleton(ctx), w.ID, w.Title, win.AppExeName(w), s.Budget)
		if err != nil {
			return "", treeReadError(err), true
		}
		tree.Notes = append(tree.Notes, fmt.Sprintf("this is a skeleton: %d levels below the Window, with each deeper part's child count; read a part whole with winuse get-window-state %d --root <element>", uia.SkeletonDepth, w.ID))
		out, rerr := renderState(tree, a, strconv.FormatUint(w.ID, 10), nil, "")
		return out, rerr, true
	case a.Root != "":
		out, rerr := readRegion(s, ctx, w, a)
		return out, rerr, true
	}
	return "", nil, false
}

// windowStateUsage is get-window-state's usage line.
const windowStateUsage = "usage: winuse get-window-state <window> [--screenshot] [--skeleton | --root <element>] [--depth N] [--interactive] [--compact] [--max-chars N | --out <file>] [--full] [--diff] [--timeout <seconds>]"

// readRegion is get-window-state --root: the Element's subtree, read
// afresh and added after the Window State's indexes; a Page Element's from
// the stored Window State.
func readRegion(s Services, ctx context.Context, w win.WindowInfo, a windowRef) (string, *Error) {
	index, rerr := regionRoot(ctx, s, w, a.Root)
	if rerr != nil {
		return "", rerr
	}
	if holdsPage(ctx, s, w, index) {
		return storedSubtree(ctx, s, w, index, a)
	}
	f, ok := s.Trees.(Finder)
	if !ok {
		return "", Fail("get-window-state --root is unavailable: no UIA client", "")
	}
	added, err := f.ReadRegion(ctx, w.ID, index, a.Depth, a.Interactive, s.Budget)
	if err != nil {
		return "", regionError(err, w, index)
	}
	what := "the subtree of " + strconv.Itoa(index)
	if a.Interactive {
		what = "the interactive Elements under " + strconv.Itoa(index)
	}
	return renderAdded(w, added, false, what, a.regionCut(wstate.SharedIDs(added.Elements))), nil
}

// holdsPage reports whether the Element behind index is a Page Element,
// or the document the Page was spliced under (its first child is one):
// either way its subtree was read over CDP whole, and a UIA read of it
// would miss the Page.
func holdsPage(ctx context.Context, s Services, w win.WindowInfo, index int) bool {
	for _, i := range []int{index, index + 1} {
		if o, perr := pageOrigin(ctx, s, w, i); perr == nil && o != nil {
			return true
		}
	}
	return false
}

// regionRoot resolves --root: an Element Index, or a name:/id: reference.
func regionRoot(ctx context.Context, s Services, w win.WindowInfo, root string) (int, *Error) {
	root = strings.TrimSpace(root)
	if i, err := strconv.Atoi(root); err == nil {
		if i < 0 {
			return 0, Fail("get-window-state: --root must be an Element Index, name: or id: reference", windowStateUsage)
		}
		return i, nil
	}
	if !strings.HasPrefix(root, wstate.NameRefPrefix) && !strings.HasPrefix(root, wstate.IDRefPrefix) {
		return 0, Fail(fmt.Sprintf("get-window-state: --root %q is not an Element Index, name: or id: reference", root), windowStateUsage)
	}
	return resolveElementRef(ctx, s, w, root)
}

// regionError maps a region read's failure: a stale or missing Window
// State keeps the shared messages.
func regionError(err error, w win.WindowInfo, index int) *Error {
	e := elementError(err, w, index)
	if e.Hint == "" {
		return treeReadError(err)
	}
	return e
}

// storedSubtree prints a Page Element's subtree from the Agent's stored
// Window State, with the indexes it already has: the Page was read whole.
func storedSubtree(ctx context.Context, s Services, w win.WindowInfo, index int, a windowRef) (string, *Error) {
	st, ok := s.Actions.(storedElements)
	if !ok {
		return "", Fail("get-window-state --root is unavailable: no stored Window State", "")
	}
	els, ok := st.StoredElements(ctx, w.ID)
	if !ok || index >= len(els) {
		return "", elementError(fmt.Errorf("%w: element %d", uia.ErrStale, index), w, index)
	}
	cut := a.regionCut(wstate.SharedIDs(els))
	var lines []wstate.PrintedLine
	root := els[index].Depth
	for i := index; i < len(els); i++ {
		e := els[i]
		if i > index && e.Depth <= root {
			break
		}
		rel := e.Depth - root
		if a.Depth > 0 && rel > a.Depth || i > index && a.Interactive && !e.ActedOn() {
			continue
		}
		lines = append(lines, cut.line(rel+1, i, e))
	}
	tail := fmt.Sprintf("%sElement %d holds Page Elements, read whole with the Window State, so these keep their Element Indexes\n", notePrefix, index)
	return wstate.Cut(header(w), lines, tail, cut.maxChars, cut.hint()), nil
}
