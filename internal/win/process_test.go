package win

import (
	"os"
	"testing"
)

func TestStringLangID(t *testing.T) {
	cases := []struct {
		lang, charset uint16
		want          string
	}{
		{0x0409, 0x04b0, "040904b0"},
		{0x0409, 0x04e4, "040904e4"},
		{0x0000, 0x04b0, "000004b0"},
		{0x0413, 0x1200, "04131200"},
	}
	for _, tc := range cases {
		if got := stringLangID(tc.lang, tc.charset); got != tc.want {
			t.Errorf("stringLangID(%#x, %#x) = %q, want %q", tc.lang, tc.charset, got, tc.want)
		}
	}
}

func TestTrimExt(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"notepad.exe", "notepad"},
		{"ms-edges.exe", "ms-edges"},
		{"noext", "noext"},
		// A name that is all extension keeps itself (filepath.Ext treats
		// the leading dot as part of the extension).
		{".hidden", ".hidden"},
	}
	for _, tc := range cases {
		if got := trimExt(tc.in); got != tc.want {
			t.Errorf("trimExt(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAppNameFallbacks(t *testing.T) {
	// No version resource exists for a made-up path: the exe base name is
	// the App name. (FileDescription itself needs a real PE file with a
	// version resource; the e2e run covers that.)
	apps := newAppNames()
	if got := apps.Name(`C:\made up dir\SomeApp.EXE`); got != "SomeApp" {
		t.Errorf("fallback App name = %q, want SomeApp", got)
	}
	if got := apps.Name(""); got != "" {
		t.Errorf("empty path must give empty App, got %q", got)
	}
}

// BenchmarkAppName is one App name lookup for system executables: read
// from the version resource (what every Collect paid per distinct exe
// when it built a fresh cache) and from the shared cache.
func BenchmarkAppName(b *testing.B) {
	dir := os.Getenv("SystemRoot")
	paths := []string{dir + `\explorer.exe`, dir + `\System32\notepad.exe`, dir + `\System32\cmd.exe`, dir + `\System32\svchost.exe`}
	b.Run("fresh", func(b *testing.B) {
		for b.Loop() {
			apps := newAppNames()
			for _, p := range paths {
				apps.Name(p)
			}
		}
	})
	b.Run("shared", func(b *testing.B) {
		for b.Loop() {
			for _, p := range paths {
				sharedNames.Name(p)
			}
		}
	})
}
