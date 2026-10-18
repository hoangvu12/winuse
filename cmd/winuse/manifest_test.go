package main

import (
	"os"
	"strings"
	"testing"
)

// The per-monitor-v2 DPI manifest is the fix for Codex's 175%-scaling offset
// bug: it must stay embedded in the binary. The manifest is generated from
// winres/winres.json into this package's .syso files (go build links any
// *_windows_<arch>.syso in the package directory). This offline test guards
// against the file being deleted or regenerated without the DPI setting.
//
// Regenerate with: go run github.com/tc-hib/go-winres@latest make
// (see winres/winres.json).
func TestManifestSysoDeclaresPerMonitorV2(t *testing.T) {
	for _, name := range []string{"rsrc_windows_amd64.syso", "rsrc_windows_arm64.syso"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s: %v (regenerate from winres/winres.json, see README)", name, err)
			continue
		}
		for _, needle := range []string{
			`<dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">permonitorv2`,
			`<longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true`,
			`<requestedExecutionLevel level="asInvoker"`,
		} {
			if !strings.Contains(string(data), needle) {
				t.Errorf("%s: manifest missing %q", name, needle)
			}
		}
	}
}
