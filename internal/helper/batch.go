package helper

// The Batch command (ticket 11): several Actions on one Window in a
// single call, run in order, stopping at the first failure and naming the
// failed step. Every step runs through the same command the standalone
// form uses — the same runAction pipeline (window resolution, locked
// desktop and administrator refusals, activate-and-verify), the same
// argument validation and the same error mapping — with the Batch's
// Window injected into its arguments, so a step behaves exactly as if the
// agent had sent it alone.
//
// Codex has no batch tool (codex-surface.md §1: the model chains calls in
// a JS cell instead); winuse's Batch keeps Codex's output style — `ok`,
// or a one-line failure with a recovery hint — and the failed step is
// named: "batch stopped at step N (<action>): <message>".
//
// Ticket 12 (Esc stop) aborts a running Batch here: the run loop below is
// the between-steps point (the Esc check at its top, before each step),
// and a stopped step's own input pipeline is where mid-step input aborts;
// remaining steps must not run after a stop. The request's deadline
// stops the Batch at the same point (finding 10): a timed-out request
// keeps no work running. The whole Batch is one armed
// Esc scope around its steps' Actions, so a press between steps — or in
// the last step's tail — is latched and stops the Batch, and the physical
// key is swallowed for the Batch's duration.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// batchUsageHint is the recovery hint for batch's argument failures.
const batchUsageHint = "usage: winuse batch <window> <action> [arguments] [then <action> [arguments]] ..."

// batch runs `winuse batch <window> <action> [arguments] [then <action>
// [arguments]]...`: the Actions run in order on the one Window, and the
// first failure stops the Batch with the failed step named. Steps are the
// Action commands (protocol.BatchStepKinds); a step's arguments are the
// standalone command's own, without the Window. A stopped Batch (the
// user's Esc press) keeps the stop's exit code (130) and message.
func batch(commands map[string]Command, esc EscWatch) Command {
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
			return "", Fail("batch: at least one action step is required", batchUsageHint)
		}

		// Prepare every step before any input is sent: a step's Kind must
		// be a batchable Action (protocol.BatchStepKinds) and its
		// arguments well-formed JSON with the Batch's Window injected —
		// the same rule press-key's key validation follows, so a
		// malformed Batch never acts. A step's value-level validation
		// (key names, secondary actions, element availability) still runs
		// at its turn: it may depend on the live Window, and the failure
		// then names its step.
		type prepared struct {
			kind string
			cmd  Command
			args json.RawMessage
		}
		steps := make([]prepared, 0, len(a.Steps))
		for i, st := range a.Steps {
			if st.Kind == "" {
				return "", Fail(fmt.Sprintf("batch: step %d has no action", i+1), batchUsageHint)
			}
			if !batchableStep(st.Kind) {
				return "", Fail(fmt.Sprintf("batch: step %d (%s) is not a batchable action; expected one of %s",
					i+1, st.Kind, strings.Join(protocol.BatchStepKinds, ", ")), batchUsageHint)
			}
			cmd, ok := commands[st.Kind]
			if !ok {
				return "", Fail(fmt.Sprintf("batch: step %d (%s) is unavailable", i+1, st.Kind), batchUsageHint)
			}
			injected, err := withWindow(a.Window, st.Args)
			if err != nil {
				return "", Fail(fmt.Sprintf("batch: step %d (%s) has invalid arguments: %v", i+1, st.Kind, err), batchUsageHint)
			}
			steps = append(steps, prepared{kind: st.Kind, cmd: cmd, args: injected})
		}

		// Run the steps in order, stopping at the first failure. The
		// failed step's message and hint are forwarded verbatim (its exit
		// code too, so an Esc stop keeps protocol.ExitEsc). The Batch is one
		// armed Esc scope around the loop — a press anywhere in it (during a
		// step, between steps, in the last step's tail) stops the Batch with
		// the exit-130 stop, and the remaining steps never run.
		esc.Arm()
		defer esc.Disarm()
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
			_, err := st.cmd(ctx, st.args)
			if err != nil {
				return "", &Error{
					Msg:  fmt.Sprintf("batch stopped at step %d (%s): %s", i+1, st.kind, err.Msg),
					Hint: err.Hint,
					Exit: err.Exit,
				}
			}
		}
		// A press in the last step's tail: the steps all ran, but the user's
		// stop must still surface (their Esc was swallowed) rather than the
		// Batch reporting success.
		if esc.Stopped() {
			return "", escStopError()
		}
		return okOutput, nil
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
