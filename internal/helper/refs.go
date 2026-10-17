package helper

// The element-side half of ticket 11's Element References: the
// ElementResolver interface the Action commands type-assert from
// Services.Actions (the way ticket 05's ElementPointer and ticket 07's
// ElementActor work), and the shared failure mapping every
// Element-taking Action reuses. The pure matching lives in
// internal/wstate (seam 2); the live-Element store the resolution reads
// is the uia Client's.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// elementTarget is the parsed Element Reference of one Element-taking
// Action (finding 16's shared front half): exactly one form is set — an
// Element Index, a name:/id: reference, or an @x,y point of the Window's
// latest screenshot. What the point means is the Action's own step:
// click/scroll/drag/hover aim input at the point itself, set-value and
// perform-secondary-action act on the Element the UIA worker finds under
// it.
type elementTarget struct {
	index *int            // the Element Index form
	ref   string          // the name:<text> / id:<AutomationId> form
	point *protocol.Point // the @x,y coordinate form
}

// elementRefRequiredHint is the shared recovery hint when no Element
// Reference was supplied at all.
const elementRefRequiredHint = "read a Window's Elements with winuse get-window-state <window>, then act on one by its Element Index, name: or id: reference, or @x,y from its latest screenshot"

// parseElementTarget guards and validates the Element Reference forms of
// one Element-taking Action: exactly one of an Element Index, a name:/id:
// reference or @x,y coordinates must be present (several are a usage
// failure naming the command; so are a negative index and negative
// coordinates). It runs before the Action pipeline, so a bad target never
// activates anything.
func parseElementTarget(cmd string, index *int, ref string, point *protocol.Point, usage string) (elementTarget, *Error) {
	if n := countRefForms(point != nil, index != nil, ref != ""); n > 1 {
		return elementTarget{}, Fail(fmt.Sprintf("%s: pass an element index, a name:/id: reference or @x,y coordinates, not several", cmd), usage)
	}
	if index == nil && ref == "" && point == nil {
		return elementTarget{}, Fail(fmt.Sprintf("%s: an element index, a name:/id: reference or @x,y coordinates are required", cmd), elementRefRequiredHint)
	}
	if index != nil && *index < 0 {
		return elementTarget{}, Fail(fmt.Sprintf("%s: the element index must be a non-negative integer", cmd), usage)
	}
	if point != nil && (point.X < 0 || point.Y < 0) {
		return elementTarget{}, Fail(fmt.Sprintf("%s: the @x,y coordinates must be non-negative integers", cmd), usage)
	}
	return elementTarget{index: index, ref: ref, point: point}, nil
}

// resolve turns the target into the Element Index the Action's step acts
// on, inside the Action pipeline (the Window is resolved): a name:/id:
// reference resolves against the Window's latest Window State (exactly
// one match, never a guess); an Element Index stays; the point form is
// left to the Action's own step (it is not an index).
func (t elementTarget) resolve(s Services, w win.WindowInfo) (int, *Error) {
	if t.ref != "" {
		return resolveElementRef(s, w, t.ref)
	}
	if t.index != nil {
		return *t.index, nil
	}
	return -1, nil
}

// ElementResolver resolves a name:/id: Element Reference against a
// Window's latest Window State to its Element Index (ticket 11). The
// real implementation is *uia.Client; Services.Actions is type-asserted
// to it, so tests fake it like the click pipeline's ElementPointer.
type ElementResolver interface {
	// ResolveElementRef resolves ref ("name:<text>" or
	// "id:<AutomationId>") against hwnd's latest Window State. Exactly one
	// match resolves to its Element Index; several matches and none are
	// errors carrying the candidates, never a guess.
	ResolveElementRef(hwnd uint64, ref string) (int, error)
}

// resolveElementRef is the shared step body every Element-taking Action
// runs for a name:/id: reference: one match becomes the Element Index the
// Action acts on, exactly as if the agent had passed it. Resolution
// failures follow ticket 03's candidate pattern: ambiguity lists the
// matching Elements' lines, a miss points at a fresh Window State read.
func resolveElementRef(s Services, w win.WindowInfo, ref string) (int, *Error) {
	resolver, ok := s.Actions.(ElementResolver)
	if !ok {
		return 0, Fail("element references are unavailable: no UIA client", "")
	}
	index, err := resolver.ResolveElementRef(w.ID, ref)
	if err != nil {
		return 0, refError(err, w)
	}
	return index, nil
}

// refError maps a reference-resolution failure to the user-facing line
// and recovery hint, in the shared Codex style.
func refError(err error, w win.WindowInfo) *Error {
	var ambiguous *wstate.AmbiguousRefError
	if errors.As(err, &ambiguous) {
		return Fail(fmt.Sprintf("%s in the cached Window State for window %d", err.Error(), w.ID),
			"candidates:\n"+strings.Join(ambiguous.Candidates, "\n"))
	}
	var missing *wstate.NoMatchRefError
	if errors.As(err, &missing) {
		return Fail(fmt.Sprintf("%s in the cached Window State for window %d", err.Error(), w.ID),
			"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+
				" and use an Element Index, name: or id: reference from that Window State (name: and id: match the tree's name and ID: columns exactly)")
	}
	var bad *wstate.BadRefError
	if errors.As(err, &bad) {
		return Fail(err.Error(),
			"an Element Reference is an Element Index, name:<text>, id:<AutomationId> or @x,y (the latest screenshot's pixels)")
	}
	if errors.Is(err, uia.ErrNoState) {
		return Fail(fmt.Sprintf("no cached Window State is available for window %d", w.ID),
			"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+
				" to read the Window's Elements before acting on them")
	}
	return Fail(err.Error(), "")
}
