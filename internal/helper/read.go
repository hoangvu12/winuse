package helper

// `winuse read <window>[:<tab>] [--root <element>] [--urls full]
// [--max-chars N | --out <file>]` (.scratch/bladebro-ideas ticket 25): a
// Page's main content, or one Page Element's subtree, as Markdown, with no
// Element Indexes. It reads only: the Window State and the Element Indexes
// stay as they are, nothing reaches the Page's input, and read.js
// (page.ReadMarkdown) runs in an isolated world. Pages only: a native
// Window is refused, pointing at get-window-state.
//
// The Markdown prints within the reads' cap (defaultMaxChars, or
// --max-chars; 0 lifts it), ending with a "(cut at N of M characters)"
// line when it was cut; --out writes it whole to a file instead.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// pageRead runs `winuse read`.
func pageRead(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ReadArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("read: "+err.Error(), protocol.ReadUsage)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("read: a window reference is required", protocol.ReadUsage)
		}
		switch {
		case a.URLs != "" && a.URLs != "full":
			return "", Fail(fmt.Sprintf("read: --urls takes full, not %q (without it, links are shortened)", a.URLs), protocol.ReadUsage)
		case a.MaxChars != nil && *a.MaxChars < 0:
			return "", Fail("read: --max-chars takes a number of characters (0: no cap)", protocol.ReadUsage)
		case a.Out != "" && a.MaxChars != nil && *a.MaxChars != 0:
			return "", Fail("read: pass --out or --max-chars, not both: --out writes the whole Markdown to the file", protocol.ReadUsage)
		}
		opts := page.ReadOptions{FullURLs: a.URLs == "full"}
		if tabTargetOf(ctx) == nil {
			w, rerr := resolveWindow(s, a.Window)
			if rerr != nil {
				return "", rerr
			}
			if s.Pages == nil || s.Pages.Browser(w) == "" {
				id := strconv.FormatUint(w.ID, 10)
				return "", Fail(fmt.Sprintf("window %d is not a browser Window: read prints a web Page's text", w.ID),
					"read this Window with winuse get-window-state "+id)
			}
		}
		if a.Root != "" {
			return s.readRoot(ctx, a, opts)
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				md, err := page.ReadMarkdown(ctx, pw.conn.Client, pw.session, "", 0, opts)
				if err != nil {
					return "", false, readError(err, pw.w, pw.name)
				}
				head := fmt.Sprintf("Page: %s - %s (main content: %s; --root <element> reads another part)", page.Collapse(md.Title), md.URL, md.Main)
				out, rerr := renderMarkdown(head, md, a)
				return out, false, rerr
			},
		})
	}
}

// readRoot is read --root: the Page Element's subtree, through its own
// session and frame, after the staleness checks every Page Element gets.
func (s Services) readRoot(ctx context.Context, a protocol.ReadArgs, opts page.ReadOptions) (string, *Error) {
	root := strings.TrimSpace(a.Root)
	var t elementTarget
	if i, err := strconv.Atoi(root); err == nil && i >= 0 {
		t.index = &i
	} else if strings.HasPrefix(root, wstate.NameRefPrefix) || strings.HasPrefix(root, wstate.IDRefPrefix) {
		t.ref = root
	} else {
		return "", Fail(fmt.Sprintf("read: --root %q is not an Element Index, name: or id: reference", root), protocol.ReadUsage)
	}
	w, index, o, refused := s.pageResolve(ctx, a.Window, t)
	switch {
	case refused != nil:
		return "", refused
	case o == nil:
		id := strconv.FormatUint(w.ID, 10)
		if t.index != nil {
			// An index past the Window State, or no Window State: its own
			// read-the-State-again failure.
			if _, perr := pageOrigin(ctx, s, w, index); perr != nil {
				return "", perr
			}
		}
		return "", Fail(fmt.Sprintf("element %s is not in the Page of window %d: read reads Page Elements", root, w.ID),
			"read it with winuse get-window-state "+id+" --root "+root+", or name an Element of the Page")
	}
	if rerr := s.pageFresh(ctx, w, index, o); rerr != nil {
		return "", rerr
	}
	noteRoute(ctx, w, protocol.RouteCDP)
	md, err := page.ReadMarkdown(ctx, o.Client, o.SessionID, o.FrameID, o.BackendNodeID, opts)
	if err != nil {
		if errors.Is(err, page.ErrStale) {
			return "", elementError(fmt.Errorf("%w: %v", uia.ErrStale, err), w, index)
		}
		return "", readError(err, w, o.Browser)
	}
	return renderMarkdown(fmt.Sprintf("Element %d (%s):", index, md.Main), md, a)
}

// renderMarkdown prints the Markdown under its header: cut at --max-chars
// (or the default cap) with the cut line, or written to --out with a line
// naming the file; a wall note last.
func renderMarkdown(head string, md page.Markdown, a protocol.ReadArgs) (string, *Error) {
	text := md.Text
	if strings.TrimSpace(text) == "" {
		text = "(the Page shows no text here)"
	}
	total := utf8.RuneCountInString(text)
	var b strings.Builder
	b.WriteString(head + "\n")
	if a.Out != "" {
		if err := writeFileAtomic(a.Out, []byte(text+"\n")); err != nil {
			return "", Fail("writing the Markdown to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
		}
		fmt.Fprintf(&b, "Markdown: %s (%d characters)\n", a.Out, total)
	} else {
		limit := defaultMaxChars
		if a.MaxChars != nil {
			limit = *a.MaxChars
		}
		b.WriteString("\n")
		if limit > 0 && total > limit {
			b.WriteString(strings.TrimRight(string([]rune(text)[:limit]), " \n") + "\n")
			fmt.Fprintf(&b, "(cut at %d of %d characters; --max-chars raises the cut, 0 lifts it, --out <file> writes it whole)\n", limit, total)
		} else {
			b.WriteString(text + "\n")
		}
	}
	if md.Block != "" {
		b.WriteString(notePrefix + "the Page looks like a " + md.Block + " wall; ask the user to handle it\n")
	}
	return b.String(), nil
}

// readError maps a read.js run that failed.
func readError(err error, w win.WindowInfo, browserName string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	if p, ok := browser.AsProblem(err, browserName); ok {
		return Fail(p.Sentence(), p.Next())
	}
	return Fail(fmt.Sprintf("reading the Page of window %d failed: %v", w.ID, err),
		"retry; read winuse get-window-state "+id+" if it keeps failing")
}
