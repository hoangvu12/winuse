// Package version carries the winuse binary version.
package version

// Version is the winuse binary version. The CLI sends it with every request;
// a helper built from a different version reports itself stale, exits, and
// the CLI starts a current one (so a winuse upgrade never acts through the
// old helper; only the read-only status is answered across builds). Bump
// it per release. Build (build.go) tells two builds of one version apart.
const Version = "0.3.0"
