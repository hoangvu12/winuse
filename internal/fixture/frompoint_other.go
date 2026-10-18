//go:build !amd64

package fixture

// Without the amd64 thunk the callback cannot read its double arguments,
// so ElementProviderFromPoint reports "no element at point" (legal:
// pRetVal is optional-out) and UIA falls back to the window root.
const fromPointHasArgs = false

func fromPointEntry() uintptr { return newCallback(rootElementProviderFromPoint) }
