package helper

// Modifiers on clicks and wheel scrolls (desktop-parity ticket 06):
// `click ... --modifiers ctrl,shift,alt` and `scroll @x,y ... --modifiers
// ctrl`. The routes, most headless first:
//
//   - a Page Element (click) or a point on a Page (scroll): CDP's
//     modifiers on the mouse events, Headless (pageactions.go);
//   - Ctrl+click or Shift+click on a list item whose list allows several
//     selected items: SelectionItem.AddToSelection, Headless, read back
//     (multiSelectClick; measured in probes round 4, section 6);
//   - with --post: the MK_* key state of the posted mouse messages, Ctrl
//     and Shift only (postclick.go);
//   - with --headed: the real modifier keys held with SendInput around the
//     click or wheel, released on every exit path (heldInput).
//
// Anything else is refused with nothing sent and the exact --headed retry
// (headless-max ticket 21). The point and Chromium message routes of
// headless-max 11 and 20 carry no modifiers, so they stay out.

import (
	"context"
	"errors"
	"fmt"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// MultiSelector is the element side of the multi-select route. The real
// implementation is *uia.Client; Services.Actions is type-asserted to it.
type MultiSelector interface {
	MultiSelect(ctx context.Context, hwnd uint64, index int, mode uia.MultiMode) (int, error)
}

var _ MultiSelector = (*uia.Client)(nil)

// modsOption renders --modifiers for a retry ("" for none).
func modsOption(m win.Modifiers) string {
	if m.None() {
		return ""
	}
	return " --modifiers " + m.String()
}

// heldInput runs input with the modifiers held as real keys (Headed only):
// pressed before, released after, on every exit path. A release that
// fails is reported, since a stuck modifier would reach the user's typing.
func (s Services) heldInput(m win.Modifiers, input func() error) error {
	if m.None() {
		return input()
	}
	release, err := s.HoldModifiers(m)
	if err != nil {
		return err
	}
	ierr := input()
	if rerr := release(); rerr != nil && ierr == nil {
		return rerr
	}
	return ierr
}

// multiSelectClick is Ctrl+click (toggle) or Shift+click (a range from the
// last selected item) on a list item, Headless. It applies to a left
// single click on an Element with exactly Ctrl or Shift; ok is false
// otherwise. An Element that is no item of a multi-select list is refused
// with nothing sent and the --headed retry.
func (s Services) multiSelectClick(ctx context.Context, ref string, target elementTarget, button win.MouseButton, count int, m win.Modifiers) (string, *Error, bool) {
	if target.point != nil || button != win.MouseLeft || count != 1 || m.Alt || m.Ctrl == m.Shift {
		return "", nil, false
	}
	ms, ok := s.Actions.(MultiSelector)
	actor, aok := s.Actions.(HeadlessActor)
	if !ok || !aok {
		return "", nil, false
	}
	mode, verb := uia.MultiToggle, "ctrl+click"
	if m.Shift {
		mode, verb = uia.MultiRange, "shift+click"
	}
	retry := func(id string) string { return "click " + id + " " + target.token() + modsOption(m) }
	out, rerr := s.runHeadless(ctx, ref, actor, headlessRun{
		cmd:    "click",
		verb:   verb,
		target: target,
		act: func(ctx context.Context, w win.WindowInfo, index int) error {
			_, err := ms.MultiSelect(ctx, w.ID, index, mode)
			return err
		},
		refusal: func(err error, w win.WindowInfo, index int) *Error {
			if errors.Is(err, uia.ErrNotMultiSelect) {
				return headedRefusal(w,
					fmt.Sprintf("click --modifiers %s has no background route on element %d: %v", m, index, err), retry, "")
			}
			return clickRefusal(err, w, index)
		},
	})
	return out, rerr, true
}

// modifierRefusal is the refusal of a click or scroll with modifiers that
// no Headless or Posted route takes, without --headed.
func (s Services) modifierRefusal(ref, cmd string, m win.Modifiers, retry func(id string) string) *Error {
	return s.headedOnly(ref,
		fmt.Sprintf("%s --modifiers %s has no background route here: only a Page, a multi-select list item (ctrl or shift) or --post on a classic control carry modifiers without the real keys", cmd, m),
		retry, "")
}
