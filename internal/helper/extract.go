package helper

// `winuse extract <window>[:<tab>] [--root <element>] [--as list|table|jsonld]
// [--limit N] [--max-chars N | --out <file>]` (bladebro ideas, ticket 26):
// the rows of a list, a table or the page's JSON-LD, printed as one header
// line naming the container and the count, then a JSON array
// (internal/extract). It reads only: no cursor, no input, no Element
// Indexes, and the Window State stays as it was.
//
// Pages only for now: page.Extract runs extract.js in winuse's isolated
// world of the Tab, or of the --root node's frame. A Window that is no
// browser is refused; native lists and grids (ticket 27) plug in at
// extractNative with the same arguments and the same extract.Result.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

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
				return s.extractNative(ctx, a, w.ID)
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

// extractNative is extract on a Window that is no browser Page. Ticket 27
// reads native lists and grids here; until then it is refused.
func (s Services) extractNative(ctx context.Context, a protocol.ExtractArgs, id uint64) (string, *Error) {
	ref := strconv.FormatUint(id, 10)
	return "", Fail(fmt.Sprintf("window %d is not a browser Window, and extract reads only the lists and tables of a browser Page for now", id),
		"read the Window's lists with winuse get-window-state "+ref+" or winuse find "+ref+" --role \"list item\"")
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
	o := extract.Options{MaxChars: defaultMaxChars, Out: a.Out, Write: writeFileAtomic}
	if a.MaxChars != nil {
		o.MaxChars = *a.MaxChars
	}
	if a.Out != "" {
		o.MaxChars = 0
	}
	if root == nil {
		o.RootHint = stateCmd
	}
	out, err := extract.Render(res, o)
	if err != nil {
		return "", Fail("writing the rows to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
	}
	return out, nil
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
