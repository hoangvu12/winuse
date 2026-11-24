package helper

// `winuse extract <window>[:<tab>] [--root <element>] [--as list|table|jsonld]
// [--limit N] [--max-chars N | --out <file>]` (bladebro ideas, tickets 26
// and 27): the rows of a list, a table or the page's JSON-LD, printed as
// one header line naming the container and the count, then a JSON array
// (internal/extract). It reads only: no cursor, no input, no Element
// Indexes, and the Window State stays as it was.
//
// On a Page, page.Extract runs extract.js in winuse's isolated world of
// the Tab, or of the --root node's frame. On a native Window (ticket 27),
// the UIA client reads the Window's list or grid in one bounded read
// (uia.Client.ExtractNative) and extract.FromNative makes its rows; --as
// is a Page's only, and --limit defaults to 500 there.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// NativeExtractor reads a native Window's list or grid for extract
// (ticket 27): the Element behind root of the Agent's latest Window State,
// or the list holding that item (-1: extract picks), and its rows up to
// one past limit, within budget; found is false when the Window shows
// none. The real one is *uia.Client; Services.Trees is type-asserted to
// it, so tests fake it with recorded reads.
type NativeExtractor interface {
	ExtractNative(ctx context.Context, hwnd uint64, root, limit int, budget time.Duration) (read extract.NativeRead, found bool, err error)
}

var _ NativeExtractor = (*uia.Client)(nil)

func extractCommand(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ExtractArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("extract: "+err.Error(), protocol.ExtractUsage)
		}
		if rerr := extractArgsError(a); rerr != nil {
			return "", rerr
		}
		if tabTargetOf(ctx) == nil {
			w, rerr := resolveWindow(s, a.Window)
			if rerr != nil {
				return "", rerr
			}
			if s.Pages == nil || s.Pages.Browser(w) == "" {
				return s.extractNative(ctx, a, w)
			}
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				out, rerr := s.extractPage(ctx, pw, a)
				return out, false, rerr
			},
		})
	}
}

// extractArgsError checks extract's arguments before anything is read.
func extractArgsError(a protocol.ExtractArgs) *Error {
	switch {
	case strings.TrimSpace(a.Window) == "":
		return Fail("extract: a window reference is required", protocol.ExtractUsage)
	case a.As != "" && !slices.Contains(page.ExtractModes, a.As):
		return Fail(fmt.Sprintf("extract: --as takes list, table or jsonld, got %q", a.As), protocol.ExtractUsage)
	case a.Limit < 0 || a.Limit > page.ExtractMaxLimit:
		return Fail(fmt.Sprintf("extract: --limit takes 1 to %d rows", page.ExtractMaxLimit), protocol.ExtractUsage)
	case a.MaxChars != nil && *a.MaxChars < 0:
		return Fail("extract: --max-chars takes a number of characters (0: no cap)", protocol.ExtractUsage)
	case a.MaxChars != nil && *a.MaxChars > 0 && a.Out != "":
		return Fail("extract: pass --out or --max-chars, not both: --out writes every row to the file", protocol.ExtractUsage)
	}
	return nil
}

// extractNative is extract on a Window that is no browser Page: its list
// or grid, through the UIA client.
func (s Services) extractNative(ctx context.Context, a protocol.ExtractArgs, w win.WindowInfo) (string, *Error) {
	ref := strconv.FormatUint(w.ID, 10)
	if a.As != "" {
		return "", Fail(fmt.Sprintf("extract: --as works on a browser Page only; window %d is native, and extract reads its list or grid as it is", w.ID),
			"run winuse extract "+ref+" without --as; pass --root <index> of the list from winuse get-window-state "+ref+" to pick another")
	}
	ex, ok := s.Trees.(NativeExtractor)
	if !ok {
		return "", Fail("extract is unavailable: no UIA client", "")
	}
	root := -1
	if a.Root != "" {
		var rerr *Error
		if root, rerr = s.nativeRoot(ctx, w, a.Root); rerr != nil {
			return "", rerr
		}
	}
	limit := a.Limit
	if limit <= 0 {
		limit = extract.NativeLimit
	}
	read, found, err := ex.ExtractNative(ctx, w.ID, root, limit, s.Budget)
	switch {
	case err != nil && root >= 0 && (errors.Is(err, uia.ErrStale) || errors.Is(err, uia.ErrNoState) || errors.Is(err, uia.ErrPageElement)):
		return "", elementError(err, w, root)
	case err != nil:
		return "", treeReadError(err)
	case !found:
		return extract.NoList + "\n", nil
	}
	hint := ""
	if root < 0 {
		hint = "winuse get-window-state " + ref
	}
	return renderExtract(extract.FromNative(read, limit), a, hint)
}

// nativeRoot resolves --root on a native Window to an Element Index of the
// Agent's latest Window State: an index, or a name:/id: reference.
func (s Services) nativeRoot(ctx context.Context, w win.WindowInfo, root string) (int, *Error) {
	root = strings.TrimSpace(root)
	var t elementTarget
	if i, err := strconv.Atoi(root); err == nil && i >= 0 {
		t.index = &i
	} else if strings.HasPrefix(root, wstate.NameRefPrefix) || strings.HasPrefix(root, wstate.IDRefPrefix) {
		t.ref = root
	} else {
		return 0, Fail(fmt.Sprintf("extract: --root %q is not an Element Index, name: or id: reference", root), protocol.ExtractUsage)
	}
	return t.resolve(ctx, s, w)
}

// renderExtract prints res within extract's budgets: --out or --max-chars
// (the read's default cap when neither is given); hint is the command
// whose Element Indexes --root takes ("" for none: a --root run).
func renderExtract(res extract.Result, a protocol.ExtractArgs, hint string) (string, *Error) {
	o := extract.Options{MaxChars: defaultMaxChars, Out: a.Out, Write: writeFileAtomic, RootHint: hint}
	if a.MaxChars != nil {
		o.MaxChars = *a.MaxChars
	}
	if a.Out != "" {
		o.MaxChars = 0
	}
	out, err := extract.Render(res, o)
	if err != nil {
		return "", Fail("writing the rows to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
	}
	return out, nil
}

// extractPage runs extract on the Page of pw: on the --root node when one
// is named, else on the whole Page.
func (s Services) extractPage(ctx context.Context, pw pageWin, a protocol.ExtractArgs) (string, *Error) {
	id := pw.id()
	stateCmd := "winuse get-window-state " + id
	if bt := tabTargetOf(ctx); bt != nil {
		// A hint line is not retargeted (retargetOut), so it names the Tab.
		stateCmd = "winuse get-window-state " + bt.ref()
	}
	var root *page.Origin
	index := -1
	if a.Root != "" {
		var rerr *Error
		if root, index, rerr = s.extractRoot(ctx, pw, a.Root); rerr != nil {
			return "", rerr
		}
	}
	res, err := page.Extract(ctx, pw.conn.Client, pw.session, root, page.ExtractOptions{As: a.As, Limit: a.Limit})
	switch {
	case err != nil && root != nil && errors.Is(err, page.ErrStale):
		return "", elementError(fmt.Errorf("%w: %v", uia.ErrStale, err), pw.w, index)
	case err != nil:
		return "", Fail(fmt.Sprintf("extract failed in window %d: %v", pw.w.ID, err),
			"retry; run winuse browser status "+pw.name+" if it keeps failing")
	}
	hint := ""
	if root == nil {
		hint = stateCmd
	}
	return renderExtract(res, a, hint)
}

// extractRoot resolves --root to a Page Element of the Window's (or the
// Tab target's) latest Window State that is still the node it showed.
func (s Services) extractRoot(ctx context.Context, pw pageWin, root string) (*page.Origin, int, *Error) {
	root = strings.TrimSpace(root)
	var t elementTarget
	if i, err := strconv.Atoi(root); err == nil && i >= 0 {
		t.index = &i
	} else if strings.HasPrefix(root, wstate.NameRefPrefix) || strings.HasPrefix(root, wstate.IDRefPrefix) {
		t.ref = root
	} else {
		return nil, 0, Fail(fmt.Sprintf("extract: --root %q is not an Element Index, name: or id: reference", root), protocol.ExtractUsage)
	}
	w, index, o, refused := s.pageResolve(ctx, pw.id(), t)
	switch {
	case refused != nil:
		return nil, index, refused
	case o == nil:
		return nil, index, Fail(fmt.Sprintf("--root %s is not an Element of the Page in the latest Window State of window %d", root, pw.w.ID),
			"read winuse get-window-state "+pw.id()+" and pass --root the Element Index of the list, the table or one item of it")
	}
	if rerr := s.pageTabCheck(ctx, w, index, o); rerr != nil {
		return nil, index, rerr
	}
	if rerr := checkPageOrigin(ctx, w, index, o); rerr != nil {
		return nil, index, rerr
	}
	return o, index, nil
}
