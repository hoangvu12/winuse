# 02: release.yml publishes SHA256SUMS

Status: done (release.yml writes dist/SHA256SUMS; first in v0.4.0)

`winuse update` (01) verifies what it downloads. Have the release job write
`dist/SHA256SUMS` (`sha256sum winuse_windows_*.exe`) and attach it with the
binaries.

Releases before this one have no `SHA256SUMS`. For those, `winuse update
--to` installs without verifying and prints a note saying so. It does not
refuse, because Fleet pins 0.3.0 first.
