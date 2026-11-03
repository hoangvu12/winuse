package helper

// find, region reads and the skeleton (desktop-parity ticket 04,
// agent-desktop's `find`, `--root` and `--skeleton`): ways to work in a
// dense Window without reading, or rereading, all of it.
//
//	winuse find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --count]
//	winuse get-window-state <window> --root <element> [--depth N] [--interactive]
//	winuse get-window-state <window> --skeleton
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

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// Finder is the tree reader's side of find and the region read:
// *uia.Client.
type Finder interface {
	Find(ctx context.Context, hwnd uint64, budget time.Duration, match func(wstate.Element) bool, first, countOnly bool) (uia.FindResult, error)
	ReadRegion(ctx context.Context, hwnd uint64, index, depth int, interactive bool, budget time.Duration) (uia.Added, error)
}

var _ Finder = (*uia.Client)(nil)

// findUsage is find's usage line.
const findUsage = "usage: winuse find <window> [--role <role>] [--name <text>] [--text <text>] [--state <state>] [--first | --count] [--timeout <seconds>]"

// find answers `winuse find <window> ...`.
func find(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.FindArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("find: "+err.Error(), findUsage)
		}
		if a.First && a.Count {
			return "", Fail("find: pass --first or --count, not both", findUsage)
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
		res, err := f.Find(ctx, w.ID, s.Budget, q.Match, a.First, a.Count)
		if err != nil {
			return "", treeReadError(err)
		}
		if a.Count {
			return strconv.Itoa(res.Count) + "\n", nil
		}
		if res.Count == 0 {
			return "", noMatch(w, q, res.RolesPresent)
		}
		return renderAdded(w, res.Added, true, "matched "+q.String()), nil
	}
}

// findInFreshRead is find on a browser Window: a full read (its Page over
// CDP), filtered, printed with the read's own Element Indexes.
func findInFreshRead(s Services, ctx context.Context, w win.WindowInfo, q wstate.Query, a protocol.FindArgs) (string, *Error) {
	tree, terr := readWindowTree(s, ctx, w)
	if terr != nil {
		return "", terr
	}
	var b strings.Builder
	count := 0
	for i := range tree.Elements {
		if !q.Match(tree.Elements[i]) || (a.First && count > 0) {
			continue
		}
		count++
		b.WriteString("\t" + wstate.Line(i, &tree.Elements[i]) + "\n")
	}
	if a.Count {
		return strconv.Itoa(count) + "\n", nil
	}
	if count == 0 {
		return "", noMatch(w, q, wstate.RolesPresent(tree.Elements))
	}
	out := header(w) + b.String() + "\n"
	for _, n := range tree.Notes {
		out += notePrefix + n + "\n"
	}
	return out + fmt.Sprintf("%sfind read window %d's Window State afresh through its Browser Connection, so its earlier Element Indexes were replaced by these\n", notePrefix, w.ID), nil
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
// indented by depth (a region), then the walk's notes and the numbering
// note.
func renderAdded(w win.WindowInfo, added uia.Added, flat bool, what string) string {
	var b strings.Builder
	b.WriteString(header(w))
	for i := range added.Elements {
		e := &added.Elements[i]
		tabs := 1
		if !flat {
			tabs = e.Depth + 1
		}
		b.WriteString(strings.Repeat("\t", tabs) + wstate.Line(added.Indexes[i], e) + "\n")
	}
	b.WriteByte('\n')
	for _, n := range added.Notes {
		b.WriteString(notePrefix + n + "\n")
	}
	fmt.Fprintf(&b, "%sthese Elements (%s) are in window %d's Window State as %s; its earlier Element Indexes still work until the next full get-window-state\n",
		notePrefix, what, w.ID, indexList(added.Indexes))
	return b.String()
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
	switch {
	case a.Skeleton && a.Root != "":
		return "", Fail("get-window-state: --skeleton reads the whole Window and --root one Element; pass one", windowStateUsage), true
	case a.Root == "" && (a.Depth != 0 || a.Interactive):
		return "", Fail("get-window-state: --depth and --interactive go with --root <element>", windowStateUsage), true
	case a.Depth < 0:
		return "", Fail("get-window-state: --depth must be a positive number", windowStateUsage), true
	case a.Skeleton:
		tree, err := s.Trees.Capture(uia.WithSkeleton(ctx), w.ID, w.Title, win.AppExeName(w), s.Budget)
		if err != nil {
			return "", treeReadError(err), true
		}
		tree.Notes = append(tree.Notes, fmt.Sprintf("this is a skeleton: %d levels below the Window, with each deeper part's child count; read a part whole with winuse get-window-state %d --root <element>", uia.SkeletonDepth, w.ID))
		return wstate.Render(tree), nil, true
	case a.Root != "":
		out, rerr := readRegion(s, ctx, w, a)
		return out, rerr, true
	}
	return "", nil, false
}

// windowStateUsage is get-window-state's usage line.
const windowStateUsage = "usage: winuse get-window-state <window> [--screenshot] [--skeleton | --root <element> [--depth N] [--interactive]] [--timeout <seconds>]"

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
	return renderAdded(w, added, false, what), nil
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
	var b strings.Builder
	b.WriteString(header(w))
	root := els[index].Depth
	for i := index; i < len(els); i++ {
		e := &els[i]
		if i > index && e.Depth <= root {
			break
		}
		rel := e.Depth - root
		if a.Depth > 0 && rel > a.Depth || i > index && a.Interactive && !e.Interactive() {
			continue
		}
		b.WriteString(strings.Repeat("\t", rel+1) + wstate.Line(i, e) + "\n")
	}
	fmt.Fprintf(&b, "\n%sElement %d holds Page Elements, read whole with the Window State, so these keep their Element Indexes\n", notePrefix, index)
	return b.String(), nil
}
