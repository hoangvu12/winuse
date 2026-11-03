package helper

// The Batch command (ticket 11): several Actions on one Window in a
// single call, run in order, stopping at the first failure and naming the
// failed step. Every step runs through the same command the standalone
// form uses — the same pipeline (runHeadless for a Headless step,
// runAction's activate-and-verify for a Headed one, ADR 0010), the same
// argument validation and the same error mapping — with the Batch's
// Window injected into its arguments, so a step behaves exactly as if the
// agent had sent it alone. A Batch sent with `headed` injects `headed`
// into every step that takes it, so the whole Batch runs the v1 way.
// Without it, a step with no Headless route is refused at its turn, before
// it sends anything, exactly as when sent alone (headless-max ticket 21);
// the Batch stops there, and the steps before it took effect.
//
// Codex has no batch tool (codex-surface.md §1: the model chains calls in
// a JS cell instead); winuse's Batch keeps Codex's output style — `ok`,
// or a one-line failure with a recovery hint — and the failed step is
// named: "batch stopped at Action N (<action>): <message>".
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
	"strings"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// batchUsageHint is the recovery hint for batch's argument failures.
const batchUsageHint = "usage: winuse batch <window> <action> [arguments] [then <action> [arguments]] ..."

// batch runs `winuse batch <window> <action> [arguments] [then <action>
// [arguments]]...`: the Actions run in order on the one Window, and the
// first failure stops the Batch with the failed Action named. Steps are the
// Action commands (protocol.BatchStepKinds); a Step's arguments are the
// standalone command's own, without the Window. A stopped Batch (the
// user's Esc press) keeps the stop's exit code (130) and message. A
// successful Batch prints `ok` with its steps' note lines after it (a
// Page step's dialog or new Tab); the Windows the Batch opened are diffed
// once around the whole Batch (opened.go).
//
// A Batch with a Headed step is one overlay bracket (finding 12; an
// all-Headless Batch shows no banner, ADR 0010): the indicator shows once
// over the Batch's Window for the whole run — like the Esc arm scope —
// and the steps' own brackets ride inside it (bracketOverlay) without
// hiding the indicator between Actions. A Window that does not resolve
// (the first step fails with the resolution error anyway) brackets
// nothing.
func batch(commands map[string]Command, s Services) Command {
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
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
		type prepared struct {
			kind string
			cmd  Command
			args json.RawMessage
		}
		steps := make([]prepared, 0, len(a.Steps))
		for i, st := range a.Steps {
			if st.Kind == "" {
				return "", Fail(fmt.Sprintf("batch: Action %d has no action", i+1), batchUsageHint)
			}
			if !batchableStep(st.Kind) {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) is not a batchable action; expected one of %s",
					i+1, st.Kind, strings.Join(protocol.BatchStepKinds, ", ")), batchUsageHint)
			}
			cmd, ok := commands[st.Kind]
			if !ok {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) is unavailable", i+1, st.Kind), batchUsageHint)
			}
			injected, err := withWindow(a.Window, st.Args)
			if err == nil && a.Headed && takesHeaded(st.Kind) {
				injected, err = withHeaded(injected)
			}
			if err != nil {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s) has invalid arguments: %v", i+1, st.Kind, err), batchUsageHint)
			}
			if postsHeaded(injected) {
				e := postFlagsError(st.Kind)
				return "", Fail(fmt.Sprintf("batch: Action %d (%s): %s", i+1, st.Kind, e.Msg), e.Hint)
			}
			if e := blockedStep(st.Kind, injected); e != nil {
				return "", Fail(fmt.Sprintf("batch: Action %d (%s): %s", i+1, st.Kind, e.Msg), e.Hint)
			}
			steps = append(steps, prepared{kind: st.Kind, cmd: cmd, args: injected})
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
		w, werr := resolveWindow(s, a.Window)
		var bw *win.WindowInfo
		if werr == nil {
			bw = &w
		}
		//
		// A step with no Headless route that is not `headed` never goes
		// Headed: its command refuses it at its turn with nothing sent
		// (headless-max ticket 21), so it does not make the Batch Headed.
		// activate-window is the one step that asks for the foreground
		// itself.
		allHeadless := true
		for _, st := range steps {
			if s.stepHeadless(st.kind, st.args, bw) {
				continue
			}
			if st.kind != "activate-window" && !stepHeaded(st.args) {
				continue
			}
			allHeadless = false
			break
		}

		// Run the steps in order, stopping at the first failure. The
		// failed step's message and hint are forwarded verbatim (its exit
		// code too, so an Esc stop keeps protocol.ExitEsc). The Batch is one
		// Esc scope around the loop — a press anywhere in it (during a
		// step, between steps, in the last step's tail) stops the Batch with
		// the exit-130 stop, and the remaining steps never run. With a
		// Headed step it is v1's swallowing scope and one overlay bracket
		// (finding 12), raised over the Batch's Window when it resolves, so
		// the indicator does not blink between Actions; all-Headless, it is
		// a passive scope and no banner.
		if allHeadless {
			defer armPassive(esc)()
		} else {
			esc.Arm()
			defer esc.Disarm()
			if bw != nil {
				s.showOverlay(*bw)
				defer s.hideOverlay()
			}
		}
		var notes strings.Builder // the steps' note lines (opened.go)
		for i, st := range steps {
			if esc.Stopped() {
				return "", escStopError()
			}
			// The request's deadline stops the Batch between Actions
			// (finding 10): the remaining Actions must not run, and the
			// server reports the request timeout centrally.
			if err := ctx.Err(); err != nil {
				return "", Fail(fmt.Sprintf("the batch's request budget expired before Action %d (%s); the Actions that ran took effect", i+1, st.kind),
					"re-observe the Window and continue in a fresh request")
			}
			out, err := st.cmd(ctx, st.args)
			notes.WriteString(stepNotes(out))
			if err != nil {
				return "", &Error{
					Msg:  fmt.Sprintf("batch stopped at Action %d (%s): %s", i+1, st.kind, err.Msg),
					Hint: err.Hint,
					Exit: err.Exit,
				}
			}
		}
		// A press in the last step's tail: the steps all ran, but the user's
		// stop must still surface (their Esc was swallowed, or in a passive
		// scope meant as a stop) rather than the Batch reporting success.
		if esc.Stopped() {
			return "", escStopError()
		}
		return withNotes(okOutput, notes.String()), nil
	}
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
