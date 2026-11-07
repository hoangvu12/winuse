package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// Every native failure a Headless Action's pattern call can end in maps
// to its typed delivery (reliability-council 02): a refusal before the
// call sent nothing, a failed write (the RPC and CO HRESULTs included, as
// uia's callErr now maps them) may have taken effect, a failed read-back
// was delivered. The text is the one headlessFailure always built.
func TestHeadlessFailuresCarryTheirDelivery(t *testing.T) {
	w := win.WindowInfo{ID: 101}
	index := 3
	run := headlessRun{cmd: "click", target: elementTarget{index: &index}, refusal: clickRefusal}
	staleHR := fmt.Errorf("%w: the Element is no longer available (HRESULT 0x80040201)", uia.ErrStale)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"UIA_E_ELEMENTNOTAVAILABLE", staleHR, protocol.DeliveryNotDelivered},
		{"RPC_E_DISCONNECTED", &uia.WriteError{What: "invoking the Element", HR: uia.HRESULT(-0x7FFEFEF8)}, protocol.DeliveryUncertain},
		{"CO_E_OBJNOTCONNECTED", &uia.WriteError{What: "invoking the Element", HR: uia.HRESULT(-0x7FFBFE03)}, protocol.DeliveryUncertain},
		{"RPC_E_SERVERFAULT", &uia.WriteError{What: "invoking the Element", HR: uia.HRESULT(-0x7FFEFEFB)}, protocol.DeliveryUncertain},
		{"a timed-out message", &uia.WriteError{What: "selecting the combo's item (CB_SETCURSEL)", Err: errors.New("timed out")}, protocol.DeliveryUncertain},
		{"a read-back", &uia.VerifyError{Action: "Toggle", Expected: "a changed toggle state", Observed: "off"}, protocol.DeliveryDeliveredUnverified},
		{"no click route", uia.ErrNoClickRoute, protocol.DeliveryNotDelivered},
		{"Raise", uia.ErrRaiseHeadless, protocol.DeliveryNotDelivered},
		{"disabled", uia.ErrDisabled, protocol.DeliveryNotDelivered},
		{"no state", uia.ErrNoState, protocol.DeliveryNotDelivered},
		{"not settable", uia.ErrNotSettable, protocol.DeliveryNotDelivered},
		{"select by name", &uia.SelectItemError{}, protocol.DeliveryNotDelivered},
	}
	for _, tc := range cases {
		if got := nativeDelivery(tc.err); got != tc.want {
			t.Errorf("%s: nativeDelivery = %q, want %q", tc.name, got, tc.want)
		}
		e := headlessFailure(tc.err, w, index, run)
		if e.Delivery != tc.want {
			t.Errorf("%s: headlessFailure(%q).Delivery = %q, want %q", tc.name, e.Msg, e.Delivery, tc.want)
		}
		// The typed value agrees with the text: the phrases are unchanged.
		if text, _ := protocol.DeliveryOf(e.Msg, e.Hint); text != tc.want {
			t.Errorf("%s: the text %q (hint %q) says %q, the type %q", tc.name, e.Msg, e.Hint, text, tc.want)
		}
		if got := SentNothing(e); got != (tc.want == protocol.DeliveryNotDelivered) {
			t.Errorf("%s: SentNothing = %v", tc.name, got)
		}
	}
}

// Delivery reads an *Error's own value first, then a native error's, then
// the text; SentNothing is true only for a proven refusal.
func TestDeliveryOfAnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"typed over its text", &Error{Msg: "element 3 is disabled; nothing was sent", Delivery: protocol.DeliveryUncertain}, protocol.DeliveryUncertain},
		{"an untyped Error's text", Fail("x", "the Action may have taken effect"), protocol.DeliveryUncertain},
		{"nothingSent", nothingSent(Fail("window 5 is minimized", "")), protocol.DeliveryNotDelivered},
		{"a wrapped native error", fmt.Errorf("step 2: %w", &uia.WriteError{What: "toggling", HR: uia.HRESULT(-0x7FFEFEFB)}), protocol.DeliveryUncertain},
		{"a plain error's text", errors.New("the clipboard kept changing; nothing was changed"), protocol.DeliveryNotDelivered},
		{"says nothing", errors.New("HRESULT 0x80004005"), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		if got := Delivery(tc.err); got != tc.want {
			t.Errorf("%s: Delivery = %q, want %q", tc.name, got, tc.want)
		}
	}
	if !SentNothing(queuedOutError()) || SentNothing(errors.New("HRESULT 0x80004005")) {
		t.Error("SentNothing: a queued-out request sent nothing; an unknown failure is not proven")
	}
}

// An Action's failure that says nothing about delivery is uncertain; its
// own delivery, its text's, and a usage error's (exit 2) are kept.
func TestActionDeliveredDefaultsToUncertain(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want string // the *Error's Delivery after the wrapper
	}{
		{"computed", Fail(errors.New("HRESULT 0x80004005").Error(), ""), protocol.DeliveryUncertain},
		{"coded", &Error{Msg: "something unusual failed", Exit: 1, Code: protocol.CodeNotFound}, protocol.DeliveryUncertain},
		{"typed", nothingSent(Fail("element 3 is disabled", "")), protocol.DeliveryNotDelivered},
		{"phrase", Fail("the edit control rejected the value", "the Action was delivered; do not repeat it"), ""},
		{"usage", &Error{Msg: "click: --count must be 1 or 2", Exit: 2}, ""},
	}
	for _, tc := range cases {
		shared := tc.err
		before := *shared
		_, got := actionDelivered(func(context.Context, json.RawMessage) (string, *Error) { return "", shared })(context.Background(), nil)
		if got.Delivery != tc.want || got.Msg != tc.err.Msg || got.Hint != tc.err.Hint || got.Code != tc.err.Code {
			t.Errorf("%s: %+v, want delivery %q and the same text and code", tc.name, got, tc.want)
		}
		if *shared != before {
			t.Errorf("%s: the command's own *Error was changed", tc.name)
		}
		resp := protocol.Response{Err: got.Msg, Hint: got.Hint, Exit: got.Exit}
		resp.Stamp(got.Code, got.Delivery)
		if resp.Delivery == "" || resp.Retry == "" {
			t.Errorf("%s: the Response has no delivery: %+v", tc.name, resp)
		}
	}
	if _, err := actionDelivered(echoCommand("ok\n"))(context.Background(), nil); err != nil {
		t.Errorf("a success: %+v", err)
	}
}

// Every Action in the registry leaves with a delivery, whatever it fails
// with. Offline and safe: the desktop lane is held, so each Action is
// refused at its lanes before its handler runs (a Window that does not
// exist takes the desktop lane); once by the lane wait's limit and once
// by its request's budget.
func TestEveryActionFailureHasADelivery(t *testing.T) {
	s := testServices(&fakeTrees{})
	s.lanes = newLaneManager()
	s.LaneWait = 10 * time.Millisecond
	s.lanes.lane(desktopLane) <- struct{}{} // another Action holds the desktop
	cmds := CommandsWith(s)
	var actions []string
	for name := range cmds {
		if name != "wait" && !observationOnlyCommand(name) {
			actions = append(actions, name)
		}
	}
	sort.Strings(actions)
	if len(actions) < 30 {
		t.Fatalf("found only %d Actions: %v", len(actions), actions)
	}
	args := json.RawMessage(`{"window":"999"}`)
	for _, name := range actions {
		for _, budget := range []time.Duration{time.Second, 5 * time.Millisecond} {
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			_, err := cmds[name](ctx, args)
			cancel()
			if err == nil {
				t.Errorf("%s: ran with the desktop held", name)
				continue
			}
			if Delivery(err) == "" {
				t.Errorf("%s (budget %s): %q (hint %q) has no delivery", name, budget, err.Msg, err.Hint)
			}
		}
	}
}
