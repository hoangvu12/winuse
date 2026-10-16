# Build our own Windows layer instead of using Cua Driver

We write winuse's Windows automation layer ourselves rather than building on Cua Driver (trycua/cua, MIT), the most capable open-source Windows computer-use driver. Cua Driver is built around background operation, which we dropped in favour of Codex-style foreground control; it is large (the Windows platform crate alone is ~1.6 MB of Rust source across 64 files, a ~33 MB download), ships telemetry, and is a fast-moving pre-release (three releases in Oct 13–16, 2026). Our goal is narrower: Codex's tool surface at Codex's quality, so a small focused layer is cheaper to own than a large dependency.

## Consequences

- Ideas (and MIT-licensed code where useful) may be borrowed from Cua Driver, e.g. foreground guarding and DPI handling; its AGPL `cua-perception` extension and FSL-licensed Driver Bench must not be copied.
