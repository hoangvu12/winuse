package helper

// Asset blocking (bladebro-ideas ticket 28; tickets 10 and 18 decided it):
// an opt-in "do not load images, fonts or media" for Tabs that are winuse's,
// never the user's.
//
//   - Where. `tab new <window> [url] --block images,fonts,media|none`, and
//     `navigate <window>:<tab> <url> --block ...` on a Tab tab new opened
//     (assetBlocks.opened, helper-wide) or on any Tab of a browser winuse
//     launched or cloned. On any other Tab it is refused before the Tab is
//     attached or held, with nothing sent; on a Window (whose Tab may be
//     the user's) navigate refuses it too. The hint names tab new.
//   - How. page.BlockAssets: Network.enable, then Network.setBlockedURLs
//     with urlPatterns, on the Tab's session (the block does nothing without
//     the Network domain on). It pauses nothing, so nothing can hang. This
//     is the only place winuse turns Network on, and only in a blocking Tab
//     that is winuse's; --block none turns it off again. tab new with
//     --block opens the Tab on about:blank, blocks, then navigates it, so
//     its first load is already blocked.
//   - How long. For the Tab's life: the classes are kept by target id and
//     sent again on every command on the Tab target (resolveTabTarget), so a
//     session the helper attached anew (the extension's idle detach, a
//     reconnect) blocks too. `--block none` ends it. A helper that died
//     leaves nothing: the browser drops the block with the session.
//   - Disclosure. get-window-state (its screenshot included), read and
//     --observe on a blocking Tab end with protocol.BlockNote.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// blockSendWait bounds the block sent again before a command on a blocking
// Tab.
const blockSendWait = 2 * time.Second

// assetBlocks are the Tabs winuse opened and what each blocking Tab
// blocks, by target id (Chrome's ids are unique, and outlive a reconnect).
type assetBlocks struct {
	mu     sync.Mutex
	opened map[string]bool
	on     map[string][]string
}

func newAssetBlocks() *assetBlocks {
	return &assetBlocks{opened: map[string]bool{}, on: map[string][]string{}}
}

// markOpened records a Tab tab new opened.
func (b *assetBlocks) markOpened(target string) {
	b.mu.Lock()
	b.opened[target] = true
	b.mu.Unlock()
}

// set records what a Tab blocks now; no classes forget it.
func (b *assetBlocks) set(target string, classes []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(classes) == 0 {
		delete(b.on, target)
		return
	}
	b.on[target] = append([]string(nil), classes...)
}

// classes is what a Tab blocks, nil for nothing.
func (b *assetBlocks) classes(target string) []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.on[target]) == 0 {
		return nil
	}
	return append([]string(nil), b.on[target]...)
}

// mayBlock says a Tab target is winuse's: one tab new opened, or any Tab of
// a browser winuse launched or cloned.
func (b *assetBlocks) mayBlock(bt *bgTab) bool {
	if bt.conn.Mode == browser.ModeLaunch || bt.conn.Mode == browser.ModeClone {
		return true
	}
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opened[bt.tab.TargetID]
}

// blockAsked says a command asks for --block: navigate, back, forward or
// reload with a block value, or a Batch with such a step.
func blockAsked(name string, args json.RawMessage) bool {
	switch name {
	case "navigate", "back", "forward", "reload":
		var a protocol.NavigateArgs
		return json.Unmarshal(args, &a) == nil && a.Block != ""
	case "batch":
		var b protocol.BatchArgs
		if json.Unmarshal(args, &b) != nil {
			return false
		}
		for _, st := range b.Steps {
			if blockAsked(st.Kind, st.Args) {
				return true
			}
		}
	}
	return false
}

// blockTabHint is the way to a Tab that takes --block.
func blockTabHint(window string) string {
	return "open a Tab of your own and work in it as <window>:<tab>: winuse tab new " + window + " <url> --block images,fonts,media"
}

// blockRefusal refuses --block on a Tab target that is not winuse's.
func blockRefusal(bt *bgTab) *Error {
	return nothingSent(Fail(fmt.Sprintf("--block is refused on tab %s of window %d: winuse did not open it, so it may be the user's, and blocking would change how their page loads", bt.short(), bt.w.ID),
		blockTabHint(bt.id())))
}

// refuseBlock is resolveTabTarget's check, before the Tab is attached or
// held: --block on a Tab target that is not winuse's.
func (s Services) refuseBlock(name string, args json.RawMessage) func(bt *bgTab) *Error {
	return func(bt *bgTab) *Error {
		if !blockAsked(name, args) || s.assetBlocks.mayBlock(bt) {
			return nil
		}
		return blockRefusal(bt)
	}
}

// keepBlocking sends a blocking Tab's block again on its session, before a
// command on it: the session may be new since the block was set.
func (s Services) keepBlocking(ctx context.Context, bt *bgTab) {
	classes := s.assetBlocks.classes(bt.tab.TargetID)
	if len(classes) == 0 || bt.session == "" {
		return
	}
	bctx, cancel := context.WithTimeout(ctx, blockSendWait)
	defer cancel()
	if err := page.BlockAssets(bctx, bt.conn.Client, bt.session, classes); err != nil {
		log.Printf("blocking %s in tab %s again: %v", strings.Join(classes, ", "), bt.short(), err)
	}
}

// blockReads are the commands whose output a blocking Tab's note ends:
// the reads (a screenshot is get-window-state's). --observe adds any
// Action.
var blockReads = map[string]bool{"get-window-state": true, "read": true}

// endWithBlockNote ends a read's output (or, under a failure, a Batch's
// observation) with the blocking Tab's note line.
func (s Services) endWithBlockNote(bt *bgTab, out string, e *Error) (string, *Error) {
	note := protocol.BlockNote(s.assetBlocks.classes(bt.tab.TargetID))
	if note == "" {
		return out, e
	}
	note += "\n"
	if e != nil {
		if e.Out == "" {
			return out, e
		}
		x := *e
		x.Out = withTrailingNewline(x.Out) + note
		return out, &x
	}
	if out == "" {
		return out, nil
	}
	return withTrailingNewline(out) + note, nil
}

func withTrailingNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

// checkNavBlock checks navigate's --block before anything is sent: a
// class list or none, on navigate only, and on a Tab target that is
// winuse's. It returns the classes to block (nil for none).
func (s Services) checkNavBlock(ctx context.Context, cmd string, a protocol.NavigateArgs) ([]string, *Error) {
	canon, err := protocol.ParseBlock(a.Block)
	if err != nil {
		return nil, Fail(cmd+": "+err.Error(), protocol.PageUsage(cmd))
	}
	if cmd != "navigate" {
		return nil, nothingSent(Fail(cmd+" takes no --block: the Tab keeps what it blocks through "+cmd,
			"to change it, run winuse navigate "+a.Window+" <url> --block images,fonts,media|none"))
	}
	bt := tabTargetOf(ctx)
	if bt == nil {
		return nil, nothingSent(Fail(fmt.Sprintf("navigate --block takes a Tab winuse opened, named as <window>:<tab>, not %s: the Tab a Window shows may be the user's", a.Window),
			blockTabHint(a.Window)))
	}
	if !s.assetBlocks.mayBlock(bt) {
		return nil, blockRefusal(bt)
	}
	return protocol.BlockList(canon), nil
}

// blockBeforeNavigate sets a Tab target's block on its session before the
// navigation, so the new document loads under it. A browser page through
// the extension has no session to block on. --block none on a Tab that
// blocks nothing sends nothing: Network was never turned on there.
func (s Services) blockBeforeNavigate(ctx context.Context, pw *pageWin, classes []string) *Error {
	if len(classes) == 0 && len(s.assetBlocks.classes(pw.tab.TargetID)) == 0 {
		return nil
	}
	if pw.session == "" {
		if pw.conn.Mode == browser.ModeExtension {
			return nothingSent(Fail(fmt.Sprintf("--block cannot reach the Tab of window %d while it shows a browser page (%s)", pw.w.ID, pw.tab.URL),
				"navigate it to a site first, then run navigate with --block again: winuse navigate "+pw.id()+" <url>"))
		}
		if rerr := pw.attach(ctx); rerr != nil {
			return nothingSent(rerr)
		}
	}
	if err := page.BlockAssets(ctx, pw.conn.Client, pw.session, classes); err != nil {
		return nothingSent(Fail(fmt.Sprintf("setting --block in the Tab of window %d failed: %v", pw.w.ID, err),
			"retry; run winuse browser status "+pw.name+" if it keeps failing"))
	}
	s.assetBlocks.set(pw.tab.TargetID, classes)
	return nil
}

// startNewTab readies a Tab tab new just opened on about:blank, then sends
// it to url ("" stays on about:blank): it blocks the classes (--block),
// and installs the exception listener (page/exceptions.go, bladebro-ideas
// 30), so the first page the Tab loads is blocked and captured too. A Tab
// that could not block is closed again, so nothing is left loading what
// the agent asked to block.
func (s Services) startNewTab(ctx context.Context, tw tabWindow, targetID, url string, classes []string) *Error {
	short := page.TabID(targetID)
	session, err := tw.reader.TabSession(ctx, tw.conn, targetID)
	if err == nil && len(classes) > 0 {
		err = page.BlockAssets(ctx, tw.conn.Client, session, classes)
	}
	switch {
	case err != nil && len(classes) > 0:
		if cerr := page.CloseTab(ctx, tw.conn.Client, targetID); cerr != nil {
			return Fail(fmt.Sprintf("tab %s opened, and setting --block in it failed (%v), and closing it again failed: %v", short, err, cerr),
				"close it: winuse tab close "+tw.id()+" "+short)
		}
		return Fail(fmt.Sprintf("setting --block in the new tab failed (%v), so winuse closed it again", err),
			"retry; run winuse browser status "+tw.name+" if it keeps failing")
	case err != nil:
		return Fail(fmt.Sprintf("tab %s opened on about:blank, and attaching it to send it to %s failed: %v", short, url, err),
			"read winuse tab list "+tw.id()+", then run winuse navigate "+tw.id()+":"+short+" "+url)
	}
	if len(classes) > 0 {
		s.assetBlocks.set(targetID, classes)
	}
	if url == "" {
		return nil
	}
	page.WatchLog(ctx, tw.conn.Client, session, targetID).Capture(ctx, session)
	_, err = page.Navigate(ctx, tw.conn.Client, session, page.NavURL(url))
	var nav *page.NavigateError
	if err == nil || errors.As(err, &nav) || errors.Is(err, page.ErrDownloadStarted) {
		return nil // the Tab shows the page, or the browser's error page
	}
	what := "opened"
	if len(classes) > 0 {
		what = "opened and blocks " + strings.Join(classes, ", ")
	}
	return Fail(fmt.Sprintf("tab %s %s, and sending it to %s failed: %v", short, what, url, err),
		"read winuse tab list "+tw.id()+", then run winuse navigate "+tw.id()+":"+short+" "+url)
}
