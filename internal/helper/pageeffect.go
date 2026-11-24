package helper

// What a Page click or key did (.scratch/bladebro-ideas ticket 06). A
// Headless Page click (any button or count) and a Page press-key arm an
// effect watch (page.ArmEffect) just before their input and read it once
// the input was delivered and the Page settled, by page.Settle's rule (up
// to page.EffectBound); --observe then takes that settle (recordSettled).
// The Action stays `ok`, since its input was delivered, and gets at most
// one note:
//
//	Note: the Page navigated to <url>
//	Note: the Page is navigating to <url> and had not loaded when winuse looked; winuse wait <window> load waits for it
//	Note: the Page changed (text only)
//	Note: no change seen in the Page[; element N is now covered by <x> at its centre | ; element N is gone from the Page]; the click was delivered, so read the Page before repeating it
//
// A navigation that landed adds the wall Note (bladebro-ideas 05) after
// its own when the new Page looks like a challenge or block wall.
//
// A change to elements, attributes, a control's state, the focus or a
// scroll prints no note, nor does a watch that could not see (unknown).
// Text-only changes print no counts: the number of text nodes means nothing
// to the agent. Under --observe the diff shows the change, so only a
// navigation is noted. Nothing is ever sent again because no change was
// seen: that would break single dispatch. --json carries the effect as its
// `effect` field (navigated, changed, text, none or unknown), recorded on
// the request's context and put on the response by the server.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/hoangvu12/winuse/internal/page"
)

// effectRecord is the request's slot for the effect its Action had.
type effectRecord struct {
	mu   sync.Mutex
	kind string
}

type effectRecordKey struct{}

// withEffectRecord gives a request its effect slot.
func withEffectRecord(ctx context.Context) (context.Context, *effectRecord) {
	r := &effectRecord{}
	return context.WithValue(ctx, effectRecordKey{}, r), r
}

// recordEffect records the Action's effect for the response; outside a
// request it does nothing.
func recordEffect(ctx context.Context, kind string) {
	if r, ok := ctx.Value(effectRecordKey{}).(*effectRecord); ok {
		r.mu.Lock()
		r.kind = kind
		r.mu.Unlock()
	}
}

func (r *effectRecord) get() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kind
}

// observing reports whether the Action runs under --observe (observe.go's
// wrapper gives it the opened-Window record).
func observing(ctx context.Context) bool {
	_, ok := ctx.Value(openedRecordKey{}).(*openedRecord)
	return ok
}

// pageEffect is one Page Action's watch: wrap its act, then note what the
// watch saw.
type pageEffect struct {
	noun  string // "click" or "key"
	index int    // the clicked Element's index, -1 for a key
	ran   bool   // the input went out
	e     page.Effect
	o     *page.Origin // the Page the watch was armed on, for the wall check
}

// arm starts the watch on the Tab of o, at the click point (nil for a key).
func (pe *pageEffect) arm(ctx context.Context, o *page.Origin, at *page.Point) *page.EffectWatch {
	pe.o = o
	actx, cancel := context.WithTimeout(ctx, pageProbeBudget)
	defer cancel()
	return page.ArmEffect(actx, o.Client, o.TopSession(), at)
}

// settle reads the watch after the act: only a delivered input with no
// failure is read; anything else closes it, and a delivered input whose
// act failed (a dialog it opened) is unknown.
func (pe *pageEffect) settle(ctx context.Context, w *page.EffectWatch, in *page.Input, err error) {
	if in.Sent == 0 {
		w.Close()
		return
	}
	pe.ran = true
	if err != nil {
		w.Close()
		pe.e = page.Effect{Kind: page.EffectUnknown}
		return
	}
	pe.e = w.Read(ctx)
	if r, ok := w.Settled(); ok {
		recordSettled(ctx, pe.o.Client, pe.o.TopSession(), r)
	}
}

// wrapAim wraps a click's act.
func (pe *pageEffect) wrapAim(o *page.Origin, act func(ctx context.Context, in *page.Input, aim page.Aim) error) func(ctx context.Context, in *page.Input, aim page.Aim) error {
	return func(ctx context.Context, in *page.Input, aim page.Aim) error {
		at := aim.Point
		w := pe.arm(ctx, o, &at)
		err := act(ctx, in, aim)
		pe.settle(ctx, w, in, err)
		return err
	}
}

// wrapKeys wraps a key's act.
func (pe *pageEffect) wrapKeys(o *page.Origin, act func(ctx context.Context, in *page.Input) error) func(ctx context.Context, in *page.Input) error {
	return func(ctx context.Context, in *page.Input) error {
		w := pe.arm(ctx, o, nil)
		err := act(ctx, in)
		pe.settle(ctx, w, in, err)
		return err
	}
}

// finish records the effect of an Action that succeeded and adds its note
// to a plain `ok`. An output that already holds a note (a dialog, a file
// chooser, a download) keeps it alone.
func (pe *pageEffect) finish(ctx context.Context, windowID uint64, out string, rerr *Error) (string, *Error) {
	if rerr != nil || !pe.ran {
		return out, rerr
	}
	recordEffect(ctx, pe.e.Kind)
	// A navigation that landed gets the wall check (bladebro-ideas 05), its
	// Note after the effect's.
	wall := ""
	if pe.e.Kind == page.EffectNavigated && !pe.e.Pending && pe.o != nil && pe.o.Client != nil {
		wall = pageWallNote(ctx, pe.o.Client, pe.o.TopSession())
	}
	if out != okOutput {
		return out + wall, nil
	}
	note := pe.note(windowID)
	if note == "" || (observing(ctx) && pe.e.Kind != page.EffectNavigated) {
		return out + wall, nil
	}
	return okWith(note) + wall, nil
}

// note is the effect's note, "" for none.
func (pe *pageEffect) note(windowID uint64) string {
	e := pe.e
	switch e.Kind {
	case page.EffectNavigated:
		if e.Pending {
			to := ""
			if e.URL != "" {
				to = " to " + e.URL
			}
			return fmt.Sprintf("the Page is navigating%s and had not loaded when winuse looked; winuse wait %d load waits for it", to, windowID)
		}
		if e.URL == "" {
			return "the Page navigated"
		}
		return "the Page navigated to " + e.URL
	case page.EffectText:
		return "the Page changed (text only)"
	case page.EffectNone:
		var b strings.Builder
		b.WriteString("no change seen in the Page")
		if pe.index >= 0 {
			what := "element " + strconv.Itoa(pe.index)
			switch {
			case e.Gone:
				b.WriteString("; " + what + " is gone from the Page")
			case e.Blocker != "":
				// NotActionableError's wording: "it is covered by <x> at its centre".
				na := &page.NotActionableError{Why: page.WhyCovered, Blocker: e.Blocker}
				b.WriteString("; " + what + " " + strings.Replace(na.Error(), "it is ", "is now ", 1))
			}
		}
		b.WriteString("; the " + pe.noun + " was delivered, so read the Page before repeating it")
		return b.String()
	}
	return ""
}
