package tasksuite

import "fmt"

// UserStartFlag is the flag that marks a Task Suite run as explicitly
// started by the user. The spec (Testing Decisions) and the ticket are
// binding: Task Suite runs take over the screen — the driven agents
// click, type, activate windows and take screenshots on the live
// desktop — so a run begins only when the user starts it.
//
// This is a refusal of convenience, not a security boundary: an agent
// could type the flag too, which is exactly why every agent-facing doc
// (the README, this message) says agents must never pass it. The flag
// exists so the default invocation refuses, and so "the run started"
// always means a human chose it.
const UserStartFlag = "--i-am-the-user"

// ErrNotUserStarted is the refusal every un-user-started run receives.
var ErrNotUserStarted = fmt.Errorf(`refusing to start: a Task Suite run takes over the screen —
the agents it drives will click, type, activate windows and take
screenshots on the live desktop while it runs.

Start the run yourself, from your own terminal, by adding %s:

    winusetasks run %s

Never pass %s on an agent's behalf. Task Suite runs are started
only by the user (spec: Testing Decisions).`, UserStartFlag, UserStartFlag, UserStartFlag)

// CheckUserStarted decides whether a run may start: only the explicit
// user start passes.
func CheckUserStarted(iAmTheUser bool) error {
	if iAmTheUser {
		return nil
	}
	return ErrNotUserStarted
}
