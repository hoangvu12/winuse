// Package version carries the winuse binary version.
package version

// Version is the winuse binary version. The CLI sends it with every request;
// a helper built from a different version reports itself stale, exits, and
// the CLI starts a current one (so a winuse upgrade never talks to the old
// helper). Bump it per release.
const Version = "0.1.0"
