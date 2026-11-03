package helper

// No Headed Action without --headed (ADR 0013 rule 3, headless-max ticket
// 21). Every routing decision that ends in the Headed pipeline (runAction)
// asks headedOnly first: when the agent did not pass --headed, the Action
// is refused before anything is sent, with why it has no background route
// and the exact command to retry with --headed. winuse never takes the
// user's foreground, pointer or keyboard on its own.
//
// activate-window is not gated: the command itself asks for the
// foreground. Under --headed every route is today's. The Headless, Posted
// and Page routes are decided before the gate and never reach it.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/win"
)

// headedOnly is the refusal of an Action whose only route is Headed while
// the agent did not pass --headed. The Window resolves first, so a bad
// reference still fails as itself. why says what has no background route
// (it ends in "; nothing was sent"); retry builds the command after
// `winuse --headed ` from the resolved Window ID; alt is another way that
// needs no foreground (--post, --into), or "".
func (s Services) headedOnly(ref, why string, retry func(id string) string, alt string) *Error {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return rerr
	}
	return headedRefusal(w, why, retry, alt)
}

// headedRefusal is headedOnly on a resolved Window.
func headedRefusal(w win.WindowInfo, why string, retry func(id string) string, alt string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	hint := "pass --headed to do it with the real pointer and keyboard, which brings the Window to the foreground: winuse --headed " + retry(id)
	if alt != "" {
		hint = alt + "; or " + hint
	}
	return nothingSent(Fail(why, hint))
}

// pointToken renders an @x,y point the way the agent types it.
func pointToken(x, y int) string {
	return "@" + strconv.Itoa(x) + "," + strconv.Itoa(y)
}

// secondaryToken renders a Secondary Action for a retry, quoted when it
// has a space ("Scroll Down").
func secondaryToken(action string) string {
	if strings.Contains(action, " ") {
		return strconv.Quote(action)
	}
	return action
}

// clickOptions renders click's --count and --button for a retry, the
// defaults left out.
func clickOptions(button win.MouseButton, count int) string {
	var b strings.Builder
	if count > 1 {
		b.WriteString(" --count " + strconv.Itoa(count))
	}
	if button != win.MouseLeft {
		b.WriteString(" --button " + buttonWord(button))
	}
	return b.String()
}

// clickDescription names a click for a refusal: "a double click", "a
// right click".
func clickDescription(button win.MouseButton, count int) string {
	switch {
	case button != win.MouseLeft:
		return "a " + buttonWord(button) + " click"
	case count > 1:
		return "a " + clickWord(count) + " click"
	}
	return "a click"
}

// typeTextWhy says why plain type-text has no background route into the
// Window: route is the Window probe's reading (nil: no probe).
func typeTextWhy(w win.WindowInfo, text string, route *win.KeyTarget) string {
	switch {
	case route == nil:
		return fmt.Sprintf("type-text has no background route into window %d", w.ID)
	case route.Focus.HWND == 0:
		return fmt.Sprintf("type-text has no background route: window %d reports no focused control while it is in the background", w.ID)
	case route.Focus.IsEdit() && !route.Focus.Writable():
		return fmt.Sprintf("type-text has no background route: the focused edit control of window %d is disabled or read-only", w.ID)
	case route.Focus.IsEdit() && hasControlChars(text):
		return "type-text has no background route for this text: a newline, a tab or another control character is typed as a real key"
	}
	return fmt.Sprintf("type-text has no background route: the focused control of window %d (class %s) is not a classic edit control", w.ID, route.Focus.Class)
}

// typeTextAlt is plain type-text's ways without the foreground: --into a
// named field, or --post to the focused control.
func typeTextAlt(id string) string {
	return "to type without the foreground, name a classic edit box or Page field: winuse type-text " + id +
		" --into <element> <text>, or post the text to the focused control (best effort): winuse type-text " + id + " --post <text>"
}

// pressKeyWhy says why press-key has no background route.
func pressKeyWhy(w win.WindowInfo, chord keys.Chord) string {
	return fmt.Sprintf("press-key %s has no background route in window %d: only Return or Escape on a classic dialog is pressed without the foreground", chord, w.ID)
}

// pressKeyAlt offers Posted Keys for a key, or a chord the attached key
// state route can post (headless-max ticket 07).
func pressKeyAlt(id string, chord keys.Chord) string {
	if postedModifier(chord) {
		if _, err := win.PlanPostedChord(chord, ""); err != nil {
			return ""
		}
	}
	return "to post it to the focused control without the foreground (best effort): winuse press-key " + id + " " + chord.String() + " --post"
}
