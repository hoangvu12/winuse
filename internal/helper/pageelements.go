package helper

// Page Element Actions of browser merge ticket 06, on top of ticket 05's
// runPage pipeline (pageactions.go): hover, drag and upload on Page
// Elements, set-value on a Page's lists and check boxes, and what a click
// reports beyond ok (a file chooser it opened, a download it started, the
// browser's context menu it kept closed).
//
//	command                              Page Element                                       UIA Element
//	hover <el>                           CDP mouseMoved at its centre, Headless             Headed with --headed (pointer.go, desktop-parity 13)
//	drag <el> <el | @x,y>                CDP press, moves, release (HTML5 drags dropped     Headed with --headed (pointer.go, desktop-parity 13)
//	                                     through CDP drag events), Headless
//	set-value <select> <label|value>     the option set by the page's JS, input + change   (today's routes)
//	set-value <check box> on|off         a trusted click when the state differs, read back (today's routes)
//	upload <file input> <file>...        DOM.setFileInputFiles                              refused
//	upload <file>...                     answers the file chooser a click opened            -
//	click --button right                 the click, with Chrome's own context menu kept     (today's routes)
//	                                     closed when the page has no menu of its own
//
// The decisions are in the ticket's comment
// (.scratch/browser-merge/issues/06-page-only-actions.md).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// clickSettle bounds how long a Page click waits for a file chooser or a
// download event it may have caused.
const clickSettle = 25 * time.Millisecond

// chooserHold is how long file chooser interception outlasts a Page Action
// in a browser winuse launched or cloned: a page may open its chooser a
// moment after the click (after a fetch), later than clickSettle.
const chooserHold = 2 * time.Second

// holdChoosers turns file chooser interception on for the Tab of o for the
// length of one Action (runPage, runPageInput, a click with its settle) and
// returns the release to defer. Through the extension or the inspect
// toggle the Tab is in the user's own browser, so interception must not
// outlast winuse's Action: a file input the user clicks would open nothing
// (ticket 06). A browser winuse launched or cloned is winuse's own, and
// keeps it chooserHold longer, so a late chooser is held back too. A
// failed release is logged, not reported: the Action's outcome stands.
func (s Services) holdChoosers(ctx context.Context, w win.WindowInfo, o *page.Origin) func() {
	watch := page.WatchOf(o.Client, o.TopSession())
	return releaseChoosers(watch.Intercept(ctx), o.TopSession(), s.chooserLinger(ctx, w, o))
}

// chooserLinger is how long interception outlasts an Action on o's Tab:
// chooserHold in a browser winuse launched or cloned, none otherwise.
func (s Services) chooserLinger(ctx context.Context, w win.WindowInfo, o *page.Origin) time.Duration {
	if conn := s.originConn(ctx, w, o); conn != nil && (conn.Mode == browser.ModeLaunch || conn.Mode == browser.ModeClone) {
		return chooserHold
	}
	return 0
}

// releaseChoosers is holdChoosers' release: at once, or linger later.
func releaseChoosers(release func() error, session string, linger time.Duration) func() {
	off := func() {
		if err := release(); err != nil {
			log.Printf("turning file chooser interception off in %s: %v", session, err)
		}
	}
	return func() {
		if linger > 0 {
			time.AfterFunc(linger, off)
			return
		}
		off()
	}
}

// actRefusal is a Page Action's own refusal raised before any input: its
// message and hint as they are, plus "nothing was sent" (pageFailure).
type actRefusal struct{ msg, hint string }

func (e *actRefusal) Error() string { return e.msg }

// pageHover runs `hover <window> <element>` on a Page Element (elementHover
// routes it here): a mouse move to its centre, after the actionability
// checks.
func (s Services) pageHover(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget) (string, *Error) {
	return s.runPage(ctx, pageRun{
		noun: "hover", done: "hovered", verb: "hover", w: w, index: index, origin: o, target: target,
		aim: page.AimOptions{Scroll: true, Visible: true, HitTest: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return in.Hover(ctx, aim.Point)
		},
	})
}

// notPageElement refuses an Element form that only Page Elements take,
// naming the point form instead. hintFmt takes the Window id twice.
func (s Services) notPageElement(ctx context.Context, ref string, target elementTarget, cmd, hintFmt string) *Error {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return rerr
	}
	index, rerr := target.resolve(ctx, s, w)
	if rerr != nil {
		return nothingSent(rerr)
	}
	id := strconv.FormatUint(w.ID, 10)
	return nothingSent(Fail(fmt.Sprintf("%s takes an element only in a browser Window's Page, and element %d of window %d is not in one", cmd, index, w.ID),
		fmt.Sprintf(hintFmt, id, id)))
}

// pageDrag runs `drag <window> <element> <element | @x,y>` from a Page
// Element (elementDrag routes it here): to another Element of the same
// Page, or to the point (x, y) of the Window's latest screenshot inside
// that Page, held drop at the end before the drop.
func (s Services) pageDrag(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, from elementTarget, to *elementTarget, x, y int, drop time.Duration) (string, *Error) {
	id := strconv.FormatUint(w.ID, 10)
	var toOrigin *page.Origin
	toIndex := -1
	if to != nil {
		_, ti, to2, ok := s.pageRoute(ctx, id, *to)
		if !ok || to2.Client != o.Client || to2.TargetID != o.TargetID {
			return "", nothingSent(Fail(fmt.Sprintf("a drag from element %d ends in the same Page, and the target %s is not in it", index, to.token()),
				"drag to an element of that Page, or to @x,y inside it (run winuse get-window-state "+id+" --screenshot)"))
		}
		toOrigin, toIndex = to2, ti
	}
	return s.runPage(ctx, pageRun{
		noun: "drag", done: "dragged", verb: "drag", w: w, index: index, origin: o, target: from,
		aim: page.AimOptions{Scroll: true, Visible: true, HitTest: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			end, err := s.dragEnd(ctx, w, o, aim, toOrigin, toIndex, x, y)
			if err != nil {
				return err
			}
			_, err = in.Drag(ctx, aim.Point, end, drop)
			return err
		},
	})
}

// dragEnd is where a drag ends, in CSS px of the Tab's viewport: the
// target Element's centre (checked, not scrolled: scrolling would move the
// start), or the screenshot point mapped into the Page.
func (s Services) dragEnd(ctx context.Context, w win.WindowInfo, from *page.Origin, aim page.Aim, to *page.Origin, toIndex, x, y int) (page.Point, error) {
	id := strconv.FormatUint(w.ID, 10)
	if to != nil {
		if rerr := checkPageOrigin(ctx, w, toIndex, to); rerr != nil {
			return page.Point{}, &actRefusal{rerr.Msg, rerr.Hint}
		}
		end, err := to.Aim(ctx, page.AimOptions{Visible: true})
		var na *page.NotActionableError
		if errors.As(err, &na) {
			return page.Point{}, &actRefusal{fmt.Sprintf("the drag cannot end at element %d: %s", toIndex, na.Error()),
				"scroll so both elements show (winuse scroll " + id + " " + strconv.Itoa(toIndex) + " into-view moves the start too), or drag to @x,y"}
		}
		if err != nil {
			return page.Point{}, err
		}
		return end.Point, nil
	}
	o, at, ok := s.pagePoint(ctx, w, x, y)
	if !ok || o.Client != from.Client || o.TargetID != from.TargetID {
		return page.Point{}, &actRefusal{fmt.Sprintf("the point @%d,%d is not on the Page of window %d", x, y, w.ID),
			"pick a point inside the Page from a fresh screenshot: winuse get-window-state " + id + " --screenshot"}
	}
	l, t, _, _, err := s.pageDocTopLeft(ctx, w, o)
	if err != nil {
		return page.Point{}, err
	}
	return page.ToCSS(l, t, aim.Rect.DPR, at.Left, at.Top), nil
}

// checkUploadFiles refuses a file list before anything is sent: every path
// absolute (the CLI resolves them), present, and a file.
func checkUploadFiles(files []string) *Error {
	if len(files) == 0 {
		return Fail("upload: at least one file is required", uploadUsageHint)
	}
	for _, f := range files {
		if !filepath.IsAbs(f) {
			return Fail(fmt.Sprintf("upload: the path %q is not absolute", f), uploadUsageHint)
		}
		fi, err := os.Stat(f)
		switch {
		case err != nil:
			return nothingSent(Fail(fmt.Sprintf("the file %s does not exist or cannot be read", f), "check the path"))
		case fi.IsDir():
			return nothingSent(Fail(fmt.Sprintf("%s is a folder; upload takes files", f), "name the files in it"))
		}
	}
	return nil
}

// pageUpload runs `winuse upload <window> [<element>] <file>...`: a file
// input Element gets the files (DOM.setFileInputFiles), or, without an
// Element or with one that is no file input, the file chooser a click
// opened (and the browser held back) is answered.
func pageUpload(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.UploadArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("upload: "+err.Error(), uploadUsageHint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("upload: a window reference is required", uploadUsageHint)
		}
		if rerr := checkUploadFiles(a.Files); rerr != nil {
			return "", rerr
		}
		if a.ElementIndex == nil && a.Ref == "" {
			return s.uploadToChooser(ctx, a)
		}
		target, rerr := parseElementTarget("upload", a.ElementIndex, a.Ref, nil, uploadUsageHint)
		if rerr != nil {
			return "", rerr
		}
		w, index, o, ok := s.pageRoute(ctx, a.Window, target)
		if !ok && tabTargetOf(ctx) != nil {
			_, _, _, refused := s.pageResolve(ctx, a.Window, target) // a Tab target's own refusal
			return "", refused
		}
		if !ok {
			return "", s.notPageElement(ctx, a.Window, target, "upload", "upload gives files to a file input of a Page: read winuse get-window-state %s for one, or click the Page's upload button and run winuse upload %s <file>...")
		}
		id := strconv.FormatUint(w.ID, 10)
		watch := page.WatchOf(o.Client, o.TopSession())
		var node int64
		return s.runPage(ctx, pageRun{
			noun: "upload", done: "given files", verb: "upload", w: w, index: index, origin: o, target: target,
			check: func(ctx context.Context) error {
				fi, err := o.ReadFileInput(ctx)
				if err != nil {
					return err
				}
				ch, _ := watch.Chooser()
				switch {
				case fi.File && fi.Disabled:
					return &page.NotActionableError{Why: page.WhyDisabled}
				case fi.File:
					node = o.BackendNodeID
					if len(a.Files) > 1 && !fi.Multiple {
						return &actRefusal{fmt.Sprintf("element %d takes one file, and %d were given", index, len(a.Files)), "upload one file"}
					}
				case ch != nil:
					node = ch.BackendNodeID
					if len(a.Files) > 1 && !ch.Multiple {
						return &actRefusal{fmt.Sprintf("the file chooser takes one file, and %d were given", len(a.Files)), "upload one file"}
					}
				default:
					return &actRefusal{fmt.Sprintf("element %d is not a file input, and no file chooser is waiting", index),
						"click it first: when it opens a file chooser, winuse holds the chooser back, then run winuse upload " + id + " <file>..."}
				}
				return nil
			},
			// A file input is often hidden behind a styled button: no
			// visibility check, and the Agent Cursor goes where it is.
			aim: page.AimOptions{Scroll: true},
			act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
				session := o.SessionID
				if node != o.BackendNodeID {
					session = o.TopSession() // the chooser's input, in the Tab's session
				}
				if err := page.SetFiles(ctx, o.Client, session, node, a.Files); err != nil {
					return err
				}
				in.Sent++ // the files reached the Page (setFileInputFiles fires input and change)
				watch.TakeChooser()
				return nil
			},
		})
	}
}

// uploadToChooser answers the file chooser waiting in the Window's Tab.
func (s Services) uploadToChooser(ctx context.Context, a protocol.UploadArgs) (string, *Error) {
	return s.runPageWindow(ctx, a.Window, pageWindowRun{
		acts: true, target: s.pageDocRect, label: "upload",
		act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
			id := pw.id()
			ch, _ := pw.watch.Chooser()
			if ch == nil {
				return "", false, nothingSent(Fail(fmt.Sprintf("window %d has no file chooser waiting", pw.w.ID),
					"click the Page's upload button first (winuse holds the chooser back), or name its file input: winuse upload "+id+" <element> <file>..."))
			}
			if len(a.Files) > 1 && !ch.Multiple {
				return "", false, nothingSent(Fail(fmt.Sprintf("the file chooser takes one file, and %d were given", len(a.Files)), "upload one file"))
			}
			if err := page.SetFiles(ctx, pw.conn.Client, pw.session, ch.BackendNodeID, a.Files); err != nil {
				if errors.Is(err, page.ErrStale) {
					pw.watch.TakeChooser()
					return "", false, nothingSent(Fail("the file input that opened the chooser is gone", "click the Page's upload button again, then run winuse upload "+id+" <file>..."))
				}
				return "", false, nothingSent(Fail("giving the files to the Page failed: "+err.Error(), "read winuse get-window-state "+id))
			}
			pw.watch.TakeChooser()
			return okOutput, true, nil
		},
	})
}

// pageSetChoice is set-value on a Page's <select> or check box (handled
// true); anything else is left to set-value's text route (handled false).
func (s Services) pageSetChoice(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, value string) (string, *Error, bool) {
	if rerr := s.pagePrelude(ctx, w, o); rerr != nil {
		return "", nothingSent(rerr), true
	}
	ch, err := o.ReadChoice(ctx)
	if err != nil || ch.Kind == "" {
		return "", nil, false // not a choice, or unreadable: set-value's own path reports it
	}
	id := strconv.FormatUint(w.ID, 10)
	if ch.Kind == "select" {
		pick := -1
		out, rerr := s.runPage(ctx, pageRun{
			noun: "choice", done: "set", verb: "select", w: w, index: index, origin: o, target: target,
			check: func(ctx context.Context) error {
				if ch.Disabled {
					return &page.NotActionableError{Why: page.WhyDisabled}
				}
				i, err := page.PickOption(ch.Options, value)
				if err != nil {
					return &actRefusal{fmt.Sprintf("cannot set element %d: %s", index, err.Error()), "name one option by its label or value"}
				}
				if ch.Options[i].Disabled {
					return &actRefusal{fmt.Sprintf("cannot set element %d: the option %q is disabled", index, ch.Options[i].Label), "pick another option"}
				}
				pick = i
				return nil
			},
			aim: page.AimOptions{Scroll: true, Visible: true},
			act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
				if ch.Options[pick].Selected && !ch.Multiple {
					return nil // already chosen: nothing to send
				}
				err := o.SelectOption(ctx, pick)
				var verify *page.VerifyError
				if err == nil || errors.As(err, &verify) {
					in.Sent++ // the page's own change event went out
				}
				return err
			},
		})
		return out, rerr, true
	}
	if ch.Kind == "picker" {
		// A date, time or colour input: the value set from the page's JS, as
		// its popup would (ADR 0017: the popup is drawn on the user's screen).
		out, rerr := s.runPage(ctx, pageRun{
			noun: "value", done: "set", verb: "set value", w: w, index: index, origin: o, target: target,
			check: func(ctx context.Context) error {
				if ch.Disabled {
					return &page.NotActionableError{Why: page.WhyDisabled}
				}
				return nil
			},
			aim: page.AimOptions{Scroll: true},
			act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
				if ch.Value == value {
					return nil // already that value: nothing to send
				}
				err := o.SetPicker(ctx, ch, value)
				var verify *page.VerifyError
				if err == nil || errors.As(err, &verify) {
					in.Sent++ // the page's own input and change events went out
				}
				return err
			},
		})
		return out, rerr, true
	}
	want, perr := page.ParseOnOff(value)
	if perr != nil {
		return "", nothingSent(Fail(fmt.Sprintf("cannot set element %d: %s", index, perr.Error()), "winuse set-value "+id+" "+target.token()+" on (or off)")), true
	}
	if ch.Kind == "radio" && !want {
		return "", nothingSent(Fail(fmt.Sprintf("element %d is a radio button, which turns off when another one of its group is chosen", index),
			"set the other radio button on instead")), true
	}
	verb, word := "check", "on"
	if !want {
		verb, word = "uncheck", "off"
	}
	out, rerr := s.runPage(ctx, pageRun{
		noun: "click", done: "clicked", verb: verb, w: w, index: index, origin: o, target: target,
		check: func(ctx context.Context) error {
			if ch.Disabled {
				return &page.NotActionableError{Why: page.WhyDisabled}
			}
			return nil
		},
		aim: page.AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			if ch.Checked == want && !ch.Mixed {
				return nil // already in that state: nothing to send
			}
			if err := in.Click(ctx, aim.Point, page.ButtonLeft, 1); err != nil {
				return err
			}
			after, err := o.ReadChoice(ctx)
			if err != nil {
				return &page.VerifyError{Msg: "the click was delivered, but the check box could not be read back"}
			}
			if after.Checked != want || after.Mixed {
				return &page.VerifyError{Msg: fmt.Sprintf("the click was delivered, but element %d is not %s afterwards (the page may handle its clicks elsewhere, such as on its label)", index, word)}
			}
			return nil
		},
	})
	return out, rerr, true
}

// pageClickRun runs a Headless Page click (ticket 05's pageClick) and
// says what else it did: a file chooser it opened (held back by the
// browser), a download it started, or, for a right click, the browser's
// context menu it kept closed.
func (s Services) pageClickRun(ctx context.Context, run pageRun, o *page.Origin, button win.MouseButton) (string, *Error) {
	watch := page.WatchOf(o.Client, o.TopSession())
	_, choosers := watch.Chooser()
	watchChanged := watch.Changed()
	downloads := page.DownloadsOf(o.Client)
	before := downloads.Seq()
	downloadChanged := downloads.Changed()
	// The browser reports a chooser or a download as an event around the
	// click's last answer; give it a moment to arrive. This runs inside
	// the act, so file chooser interception (held by runPage around the
	// act) is still on for a chooser the click opens a moment late.
	press := run.act
	run.act = func(ctx context.Context, in *page.Input, aim page.Aim) error {
		err := press(ctx, in, aim)
		if err == nil && (watchChanged != nil || downloadChanged != nil) {
			select {
			case <-watchChanged:
			case <-downloadChanged:
			case <-time.After(clickSettle):
			}
		}
		return err
	}
	var menu page.ContextMenu
	guarded := false
	if button == win.MouseRight {
		click := run.act
		run.act = func(ctx context.Context, in *page.Input, aim page.Aim) error {
			guarded = o.GuardContextMenu(ctx) == nil
			err := click(ctx, in, aim)
			if guarded {
				m, gerr := o.ContextMenuResult(context.WithoutCancel(ctx))
				guarded = gerr == nil
				menu = m
			}
			return err
		}
	}
	out, rerr := s.runPage(ctx, run)
	if rerr != nil || out != okOutput {
		return out, rerr
	}
	id := strconv.FormatUint(run.w.ID, 10)
	var notes []string
	if guarded && menu.Fired && !menu.Page {
		notes = append(notes, "the page has no menu of its own there, so winuse kept the browser's context menu from opening")
	}
	if _, n := watch.Chooser(); n > choosers {
		notes = append(notes, "the click opened a file chooser, which the browser held back so nothing opened on the screen: answer it with winuse upload "+id+" <file>...")
	}
	for _, d := range downloads.Since(before) {
		notes = append(notes, fmt.Sprintf("the click started a download of %q: winuse wait %s download prints its path when it is done", d.Name, id))
	}
	if len(notes) == 0 {
		return out, nil
	}
	return okWith(notes...), nil
}
