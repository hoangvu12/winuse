package fixture

// The ElementProviderFromPoint shim (frompoint_amd64.s): the assembly thunk
// moves the double arguments into the integer registers the Go callback
// reads, then jumps to fromPointTarget.

func fromPointThunk()
func fromPointThunkPC() uintptr

// fromPointTarget is the Go callback the thunk jumps to; set once, before
// the vtable holding the thunk exists.
var fromPointTarget uintptr

// fromPointHasArgs reports whether rootElementProviderFromPoint receives
// the point (the doubles' bits) in its x and y arguments.
const fromPointHasArgs = true

// fromPointEntry returns the vtable entry for ElementProviderFromPoint.
func fromPointEntry() uintptr {
	fromPointTarget = newCallback(rootElementProviderFromPoint)
	return fromPointThunkPC()
}
