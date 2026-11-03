package helper

// The Batch command (ticket 11): several Actions on one Window in a
// single call, run in order. Every step runs through the same command the
// standalone form uses — the same pipeline (runHeadless for a Headless
// step, runAction's activate-and-verify for a Headed one, ADR 0010), the
// same argument validation and the same error mapping — with the Batch's
// Window injected into its arguments, so a step behaves exactly as if the
// agent had sent it alone. A Batch sent with `headed` injects `headed`
// into every step that takes it, so the whole Batch runs the v1 way.
// Without it, a step with no Headless route is refused at its turn, before
// it sends anything, exactly as when sent alone (headless-max ticket 21).
//
// Like agent-desktop's batch (desktop-parity ticket 20), a failed step
// does not stop the Batch: the next step runs, uncertain failures too
// ("may have taken effect", "delivered; do not repeat it"). Each step's
// output is printed under its number, "Action N (<action>): <first
// line>" with the rest indented two spaces, and a failed step's line is
// "failed: <message>" with its hint. A Batch with a failed step then
// fails as a whole (exit 1), naming every failed step and marking the
// uncertain ones, and its step lines travel as Error.Out. `stop_on_error`
// keeps the old rule: the first failure stops the Batch, as "batch
// stopped at Action N (<action>): <message>" with the step's hint. An Esc
// stop and the request's deadline always stop it.
//
// Codex has no batch tool (codex-surface.md §1: the model chains calls in
// a JS cell instead); winuse's Batch keeps Codex's output style — the
// steps' output, or a one-line failure with a recovery hint.
//
// Ticket 12 (Esc stop) aborts a running Batch here: the run loop below is
// the between-steps point (the Esc check at its top, before each step),
// and a stopped step's own input pipeline is where mid-step input aborts;
// remaining steps must not run after a stop. The request's deadline
// stops the Batch at the same point (finding 10): a timed-out request
// keeps no work running. The whole Batch is one Esc scope around its
// steps' Actions, so a press between steps — or in the last step's tail —
// is latched and stops the Batch. The scope's kind is decided before the
// first step runs (headless spec, "Esc in Headless scopes"): a Batch with
// any Headed step is one swallowing scope with the banner for its whole
// run, exactly as in v1, Headless steps included; an all-Headless Batch
// is one passive scope — the user's Esc stops it but reaches their app —
// with no banner (the Agent Cursor shows each step).

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// batchUsageHint is the recovery hint for batch's argument failures.
const batchUsageHint = "usage: winuse batch <window> <action> [arguments] [then <action> [arguments]] ..."

// batch runs `winuse batch <window> <action> [arguments] [then <action>
// [arguments]]...`: the Actions run in order on the one Window, past a
// failed step unless `stop_on_error`. Steps are the Action commands
// (protocol.BatchStepKinds); a Step's arguments are the standalone
// command's own, without the Window. A stopped Batch (the user's Esc
// press) keeps the stop's exit code (130) and message. The output is the
// steps' lines (stepLines), each step's note lines included (a Page
// step's dialog); the Windows the Batch opened are diffed once around the
// whole Batch (opened.go), after a failed step too. A Window that does
// not resolve fails the Batch before any step: every step would fail on
// it.
//
// A Batch with a Headed step is one overlay bracket (finding 12; an
// all-Headless Batch shows no banner, ADR 0010): the indicator shows once
// over the Batch's Window for the whole run — like the Esc arm scope —
// and the steps' own brackets ride inside it (bracketOverlay) without
// hiding the indicator between Actions.
func batch(commands map[string]Command, s Services) Command {
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	return func(ctx context.Context, args json.RawMessage) (result string, failure *Error) {
		var a protocol.BatchArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("batch: "+err.Error(), batchUsageHint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("batch: a window reference is required", batchUsageHint)
		}
		if len(a.Steps) == 0 {
			return "", Fail("batch: at least one Action is required", batchUsageHint)
		}

		// Prepare every step before any input is sent: a step's Kind must
		// be a batchable Action (protocol.BatchStepKinds) and its
		// arguments well-formed JSON with the Batch's Window injected —
		// the same rule press-key's key validation follows, so a
		// malformed Batch never acts. A step's value-level validation
		// (key names, secondary actions, element availability) still runs
		// at its turn: it may depend on the live Window, and the failure
		// then names its step. A posted step that is headed, by its own
		// flag or the Batch's, is refused here too (postFlagsError), and so
		// is a press-key step with a Blocked Combo and no `force` of its own
		// (blockedStep), so the steps before it never act.
		//
		// A step may name its own Window (desktop-parity ticket 24): it
		// resolves here with the Batch's, so a step's Window that does not
		// resolve, or whose ID another Window took (reusedID), fails the
		// Batch before any step. An `in new` step's Window is the one the
		// step before it opened, known only at its turn: it cannot be the
		// first step, and it takes no Element Index or @x,y point, which
		// only a Window State or screenshot of that Window could give.
		steps := make([]batchStep, 0, len(a.Steps))
		for i, st := range a.Steps {
			if st.Kind == "" {
				return "", Fail(fmt.Sprintf("batch: Action %d has no action", i+1), batchUsageHint)
			}
			if st.InNew && (i == 0 || st.Window != "") {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s): `in new` needs an Action before it and names no other Window", i+1, st.Kind), batchUsageHint)
			}
			if st.InNew && indexedStep(st.Args) {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) is in a new Window, which has no Window State or screenshot yet; nothing was sent", i+1, st.Kind),
					"name its Element with name: or id:, which resolve in a fresh read, or send a key or a Window Action")
			}
			if !batchableStep(st.Kind) {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) is not a batchable action; expected one of %s",
					i+1, st.Kind, strings.Join(protocol.BatchStepKinds, ", ")), batchUsageHint)
			}
			cmd, ok := commands[st.Kind]
			if !ok {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) is unavailable", i+1, st.Kind), batchUsageHint)
			}
			window := a.Window
			if st.Window != "" {
				window = st.Window
			}
			injected, err := withWindow(window, st.Args)
			if err == nil && a.Headed && takesHeaded(st.Kind) {
				injected, err = withHeaded(injected)
			}
			if err != nil {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) has invalid arguments: %v", i+1, st.Kind, err), batchUsageHint)
			}
			if postsHeaded(injected) {
				e := postFlagsError(st.Kind)
				if st.Kind == "click" || st.Kind == "scroll" {
					e = postMouseFlagsError(st.Kind) // postclick.go
				}
				return "", Fail(fmt.Sprintf("batch: Action %d (%s): %s", i+1, st.Kind, e.Msg), e.Hint)
			}
			if e := blockedStep(st.Kind, injected); e != nil {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s): %s", i+1, st.Kind, e.Msg), e.Hint)
			}
			steps = append(steps, batchStep{kind: st.Kind, cmd: cmd, args: injected, own: st.Window, inNew: st.InNew})
		}

		// The Batch's scope, decided up front by the same predicate each
		// step's command routes on (headlessStep): Headless only when every
		// step is. The Batch's Window resolves once here. press-key and
		// type-text route by the live Window (ADR 0011, Decision 4), so each
		// keyboard step probes it (Services.KeyRoute) right here, before the
		// scope is picked, and probes it again when the step runs.
		//
		// The race: an earlier step can move the focus or close the dialog,
		// so a keyboard step can route differently when it runs than when
		// the scope was picked. Both directions stay safe. A step that turns
		// Headed inside a passive scope arms its own swallowing scope and
		// banner for its run (runAction; a swallowing scope wins over a
		// passive one), and a step that turns Headless inside a swallowing
		// scope runs Headless under the banner, as a Headless click does in
		// a mixed Batch. Each step still decides before it sends anything.
		// A Window that does not resolve fails the Batch here, with nothing
		// sent: run on, every step would fail on it.
		w, werr := resolveWindow(s, a.Window)
		if werr != nil {
			return "", &Error{Msg: "batch: " + werr.Msg, Hint: werr.Hint, Exit: werr.Exit}
		}
		bw := &w
		for i := range steps {
			st := &steps[i]
			if st.inNew {
				continue // its Window opens during the Batch
			}
			st.w = w
			if st.own == "" {
				continue
			}
			sw, serr := resolveWindow(s, st.own)
			if serr == nil {
				serr = s.reusedID(sw.ID)
			}
			if serr != nil {
				return "", &Error{Msg: fmt.Sprintf("batch: Action %d (%s): %s", i+1, st.kind, serr.Msg), Hint: serr.Hint, Exit: serr.Exit}
			}
			st.w = sw
		}
		//
		// A step with no Headless route that is not `headed` never goes
		// Headed: its command refuses it at its turn with nothing sent
		// (headless-max ticket 21), so it does not make the Batch Headed.
		// activate-window is the one step that asks for the foreground
		// itself. An `in new` step cannot be probed before its Window
		// opens: only its flag (inNewHeaded) makes it Headed.
		allHeadless := true
		for _, st := range steps {
			if st.inNew {
				if inNewHeaded(st.kind, st.args) {
					allHeadless = false
					break
				}
				continue
			}
			if s.stepHeadless(ctx, st.kind, st.args, &st.w) {
				continue
			}
			if st.kind != "activate-window" && !stepHeaded(st.args) {
				continue
			}
			allHeadless = false
			break
		}

		// Run the steps in order, each step's output under its number. A
		// failed step's line forwards its message and hint verbatim, and
		// the next step runs; with `stop_on_error` the first failure stops
		// the Batch instead, forwarding the step's exit code too. The Batch
		// is one Esc scope around the loop — a press anywhere in it (during
		// a step, between steps, in the last step's tail) stops the Batch
		// with the exit-130 stop, and the remaining steps never run. With a
		// Headed step it is v1's swallowing scope and one overlay bracket
		// (finding 12), raised over the Batch's Window, so the indicator
		// does not blink between Actions; all-Headless, it is a passive
		// scope and no banner. Every way out keeps the lines of the steps
		// that ran (Error.Out).
		if allHeadless {
			defer armPassive(esc)()
		} else {
			// Polite real input (polite.go): one save before the first
			// step and one give-back after the last, not one per step.
			var run *politeRun
			ctx, run = s.beginBatchPolite(ctx)
			defer func() {
				note := s.giveBack(run)
				switch {
				case failure == nil:
					result = withNotes(result, note)
				case failure.Out != "":
					failure.Out = withNotes(failure.Out, note)
				}
			}()
			esc.Arm()
			defer esc.Disarm()
			s.showOverlay(*bw)
			defer s.hideOverlay()
		}
		var lines strings.Builder // the steps' output, under their numbers
		var failed []batchFailure
		stop := func(e *Error) (string, *Error) {
			e.Out = lines.String()
			return "", e
		}
		var opened *Error // why the next `in new` step has no Window
		for i := range steps {
			st := &steps[i]
			if esc.Stopped() {
				return stop(stopErrorFor(esc))
			}
			// The request's deadline stops the Batch between Actions
			// (finding 10): the remaining Actions must not run, and the
			// server reports the request timeout centrally.
			if err := ctx.Err(); err != nil {
				return stop(Fail(fmt.Sprintf("the batch's request budget expired before Action %d (%s); the Actions that ran took effect", i+1, st.kind),
					"re-observe the Window and continue in a fresh request"))
			}
			var err *Error
			if st.inNew {
				err = opened // the Window the step before opened, or why there is none
				if err == nil {
					err = s.inNewWindow(ctx, st)
				}
			}
			// The next step acts in the Window this one opens: snapshot
			// the Windows of this step's App before it runs (opened.go).
			next := i+1 < len(steps) && steps[i+1].inNew
			var before openedBefore
			listed := false
			if next && err == nil {
				before, listed = s.snapshotWindow(st.w)
			}
			if err == nil && st.w.ID != w.ID {
				err = s.stepHung(ctx, st)
			}
			out := ""
			if err == nil {
				out, err = st.cmd(ctx, st.args)
			}
			if next {
				steps[i+1].w, opened = s.openedByStep(ctx, esc, i+1, err, before, listed)
			}
			if err != nil && (a.StopOnError || err.Exit == protocol.ExitEsc) {
				return stop(&Error{
					Msg:  fmt.Sprintf("batch stopped at Action %d (%s): %s", i+1, st.kind, err.Msg),
					Hint: err.Hint,
					Exit: err.Exit,
				})
			}
			if err != nil {
				lines.WriteString(stepLines(i+1, st.label(), failedStepOutput(err)))
				failed = append(failed, batchFailure{n: i + 1, kind: st.kind, window: st.w.ID, uncertain: uncertainFailure(err)})
				continue
			}
			lines.WriteString(stepLines(i+1, st.label(), out))
		}
		// A press in the last step's tail: the steps all ran, but the user's
		// stop must still surface (their Esc was swallowed, or in a passive
		// scope meant as a stop) rather than the Batch reporting success.
		if esc.Stopped() {
			return stop(stopErrorFor(esc))
		}
		if len(failed) > 0 {
			return stop(batchReport(len(steps), failed))
		}
		return lines.String(), nil
	}
}

// stepLines renders one step's output under its number: the first line
// after "Action N (<kind>): ", every other line indented two spaces, so a
// line at the left margin always starts a step (or is a note the whole
// Batch added, opened.go). Output with no text reads ok.
func stepLines(n int, kind, out string) string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		out = strings.TrimSuffix(okOutput, "\n")
	}
	var b strings.Builder
	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			fmt.Fprintf(&b, "Action %d (%s): %s\n", n, kind, line)
			continue
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// failedStepOutput is a failed step's text under its number: "failed:"
// with the step's message, then its hint.
func failedStepOutput(e *Error) string {
	out := "failed: " + e.Msg
	if e.Hint != "" {
		out += "\nhint: " + e.Hint
	}
	return out
}

// batchFailure is one step a Batch ran past.
type batchFailure struct {
	n         int
	kind      string
	window    uint64 // the step's Window; 0 for an `in new` step whose Window was not found
	uncertain bool   // the step may have acted (uncertainFailure)
}

// uncertainFailure reports whether a failed step may have acted: its
// message or hint says the Action may have taken effect, or was delivered
// and must not be repeated (SKILL.md's result phrases).
func uncertainFailure(e *Error) bool {
	text := e.Msg + " " + e.Hint
	return strings.Contains(text, "may have taken effect") || strings.Contains(text, "do not repeat")
}

// batchReport is the failure a Batch ends with after it ran past failed
// steps: how many failed, each one named, the uncertain ones marked, and
// a hint that sends the agent to the Window State before repeating an
// uncertain one: the State of each Window an uncertain step acted in.
func batchReport(total int, failed []batchFailure) *Error {
	names := make([]string, len(failed))
	var uncertain, reads []string
	for i, f := range failed {
		names[i] = fmt.Sprintf("Action %d (%s)", f.n, f.kind)
		if f.uncertain {
			names[i] = fmt.Sprintf("Action %d (%s, uncertain)", f.n, f.kind)
			uncertain = append(uncertain, strconv.Itoa(f.n))
			if read := fmt.Sprintf("winuse get-window-state %d", f.window); f.window != 0 && !slices.Contains(reads, read) {
				reads = append(reads, read)
			}
		}
	}
	msg := fmt.Sprintf("batch: %d of %d Actions failed: %s", len(failed), total, strings.Join(names, ", "))
	hint := "each failed Action's line in the output names its cause and hint; the other Actions took effect"
	if len(uncertain) > 0 {
		it := "it"
		if len(uncertain) > 1 {
			it = "them"
		}
		state := "the Window State"
		if len(reads) > 1 {
			state = "the Window States"
		}
		hint = fmt.Sprintf("%s may have taken effect: read %s (%s) before repeating %s; %s",
			actionsWord(uncertain), state, strings.Join(reads, "; "), it, hint)
	}
	return Fail(msg, hint)
}

// actionsWord names step numbers in prose: "Action 2", "Actions 2 and 4",
// "Actions 2, 4 and 5".
func actionsWord(ns []string) string {
	if len(ns) == 1 {
		return "Action " + ns[0]
	}
	return "Actions " + strings.Join(ns[:len(ns)-1], ", ") + " and " + ns[len(ns)-1]
}

// batchableStep reports whether kind is an Action a Batch step can be.
func batchableStep(kind string) bool {
	for _, k := range protocol.BatchStepKinds {
		if kind == k {
			return true
		}
	}
	return false
}

// withWindow injects the Batch's Window into one step's raw arguments,
// overlaying any window the step carries: a Batch acts on exactly one
// Window. Values pass through verbatim (raw JSON), so numbers and strings
// survive untouched.
func withWindow(window string, raw json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	w, err := json.Marshal(window)
	if err != nil {
		return nil, err
	}
	fields["window"] = w
	return json.Marshal(fields)
}

// postsHeaded reports whether one step's prepared arguments carry both
// `post` and `headed` (the Batch's headed already injected by withHeaded).
func postsHeaded(raw json.RawMessage) bool {
	var f struct {
		Post   bool `json:"post"`
		Headed bool `json:"headed"`
	}
	return json.Unmarshal(raw, &f) == nil && f.Post && f.Headed
}

// blockedStep is blockedComboError for one prepared step: a press-key step
// whose key parses to a Blocked Combo without the step's `force`. Other
// steps, and keys that do not parse (refused at their turn), pass.
func blockedStep(kind string, raw json.RawMessage) *Error {
	if kind != "press-key" {
		return nil
	}
	var a protocol.PressKeyArgs
	if json.Unmarshal(raw, &a) != nil {
		return nil
	}
	chord, err := keys.Parse(a.Key)
	if err != nil {
		return nil
	}
	return blockedComboError(chord, a.Force)
}

// withHeaded sets `headed: true` on one step's raw arguments (a Batch sent
// with `headed`, ADR 0010: --headed restores v1 behaviour for the whole
// Batch). Only steps whose command takes the flag get it (takesHeaded);
// the other Actions are always Headed.
func withHeaded(raw json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	fields["headed"] = json.RawMessage("true")
	return json.Marshal(fields)
}

// batchStep is one prepared Batch step.
type batchStep struct {
	kind  string
	cmd   Command
	args  json.RawMessage // the command's arguments, its Window and the Batch's headed injected
	own   string          // the step's own Window reference (desktop-parity ticket 24), "" for the Batch's
	inNew bool            // the step acts in the Window the step before it opened (`in new`)
	w     win.WindowInfo  // the step's Window, resolved; an `in new` step's once it opened
}

// label is the step's name in its output line: the Action, and the
// Window of a step that acts in its own ("press-key in 5678"; "in new"
// when the new Window was not found).
func (st batchStep) label() string {
	switch {
	case st.inNew && st.w.ID == 0:
		return st.kind + " in new"
	case st.inNew || st.own != "":
		return fmt.Sprintf("%s in %d", st.kind, st.w.ID)
	}
	return st.kind
}

// inNewWait bounds the wait for the Window a step opened, when the step
// after it is `in new`: longer than the opened-Window note's settle
// (openedSettle), since the agent says a Window will open; it stops at the
// first new Window.
const inNewWait = 2 * time.Second

// indexedStep reports whether a step's arguments name an Element Index or
// an @x,y point of a screenshot, which an `in new` step cannot.
func indexedStep(raw json.RawMessage) bool {
	var f struct {
		ElementIndex *int            `json:"element_index"`
		FromIndex    *int            `json:"from_index"`
		ToIndex      *int            `json:"to_index"`
		Point        *protocol.Point `json:"point"`
	}
	return json.Unmarshal(raw, &f) == nil && (f.ElementIndex != nil || f.FromIndex != nil || f.ToIndex != nil || f.Point != nil)
}

// inNewHeaded is an `in new` step's class before its Window exists:
// Headed when it asks for the foreground (activate-window) or carries
// headed. Without the flag, a step with no Headless route is refused at
// its turn with nothing sent (headless-max ticket 21).
func inNewHeaded(kind string, raw json.RawMessage) bool {
	return kind == "activate-window" || stepHeaded(raw)
}

// openedByStep finds the Window that Action n opened, for the `in new`
// step after it: the one new Window of its App (openedChanges) within
// inNewWait. err is Action n's failure; before and listed its snapshot
// (snapshotWindow). None, several, or a failed Action n refuses the `in
// new` step with nothing sent.
func (s Services) openedByStep(ctx context.Context, esc EscWatch, n int, err *Error, before openedBefore, listed bool) (win.WindowInfo, *Error) {
	hint := "run winuse list-windows, then name the Window with in <window ID> in a new Batch"
	switch {
	case err != nil:
		return win.WindowInfo{}, Fail(fmt.Sprintf("Action %d failed, so no Window it opened is known; nothing was sent", n), hint)
	case !listed:
		return win.WindowInfo{}, Fail(fmt.Sprintf("the Windows could not be listed to find the one Action %d opened; nothing was sent", n), hint)
	}
	opened := s.openedWithin(ctx, esc.Stopped, before, inNewWait)
	switch len(opened) {
	case 0:
		return win.WindowInfo{}, Fail(fmt.Sprintf("Action %d opened no Window within %s; nothing was sent", n, inNewWait), hint)
	case 1:
		return opened[0], nil
	}
	names := make([]string, len(opened))
	for i, w := range opened {
		names[i] = fmt.Sprintf("%d %q", w.ID, strings.Join(strings.Fields(w.Title), " "))
	}
	return win.WindowInfo{}, Fail(fmt.Sprintf("Action %d opened %d Windows (%s), and `in new` needs one; nothing was sent", n, len(opened), strings.Join(names, ", ")),
		"name the one to act in with in <window ID> in a new Batch")
}

// inNewWindow aims an `in new` step at the Window found for it. The Batch
// took its lanes up front (batchLanes): a new Window on another UI thread,
// or a step that goes to its Page, needs a lane the Batch does not hold,
// and is refused with nothing sent.
func (s Services) inNewWindow(ctx context.Context, st *batchStep) *Error {
	args, err := withWindow(strconv.FormatUint(st.w.ID, 10), st.args)
	if err != nil {
		return Fail(fmt.Sprintf("invalid arguments: %v", err), batchUsageHint)
	}
	if !s.lanesHeld(ctx, st.kind, args) {
		return Fail(fmt.Sprintf("window %d needs a lane this Batch did not take when it started (it runs on another UI thread than the Window that opened it, or the Action goes to its Page); nothing was sent", st.w.ID),
			fmt.Sprintf("send this Action on its own, or in a new Batch on window %d", st.w.ID))
	}
	st.args = args
	return nil
}

// stepHung is refuseHung (hung.go, multi-agent ticket 07) for a step in
// its own Window: the registry checks only the Batch's Window. A step
// that goes to the Page does not reach the Window's UI thread.
func (s Services) stepHung(ctx context.Context, st *batchStep) *Error {
	if s.WindowHung == nil || s.pageStep(ctx, st.kind, st.args, st.w) || !s.WindowHung(st.w.ID) {
		return nil
	}
	return hungWindowError(st.w.ID)
}
