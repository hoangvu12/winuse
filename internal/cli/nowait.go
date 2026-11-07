package cli

// The global --no-wait flag (reliability-council 06): an Action refused
// for a reason that may pass on its own (a disabled Element, a modal
// dialog over its Window, a name:/id: reference that matches nothing yet)
// waits up to about 2 s for it to pass and then runs again; --no-wait
// fails at once, as before. It is placed like --headed (headed.go): before
// the command or anywhere after it outside literal text. Commands that do
// not wait accept it and ignore it, a Batch among them: its steps never
// wait. The request carries it (protocol.Request.NoWait).

// noWaitFlag is the global flag's spelling.
const noWaitFlag = "--no-wait"

// activeNoWait is the running command's --no-wait, which runRequest puts
// on its request.
var activeNoWait bool

// extractNoWait removes every --no-wait among the leading flags (where
// hoistGlobalFlags put the ones after the command) and reports whether one
// was given.
func extractNoWait(args []string) (rest []string, on bool) {
	rest = make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		n := leadingFlag(args[i:])
		if n == 0 {
			break
		}
		if args[i] == noWaitFlag {
			on = true
		} else {
			rest = append(rest, args[i:i+n]...)
		}
		i += n
	}
	return append(rest, args[i:]...), on
}
