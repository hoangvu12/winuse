// This file parses winuse listing output. It carries NO e2e build tag on
// purpose: it is pure string logic with no desktop access, so the default
// offline `go test ./...` runs it — a safety net for the parsing the
// tag-gated tests rely on. Everything that touches the desktop stays
// behind the e2e tag (see the package comment).

package e2e

import (
	"strings"
	"testing"
)

// appBlock finds the list-apps block for the App with the given name and
// returns its header fields (name, state, last used, use count, id) plus
// its Window lines (id, title pairs). App lines never start with a tab;
// Window lines do.
func appBlock(stdout, appName string) (header []string, windows [][2]string, ok bool) {
	collecting := false
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			collecting = false
			current := strings.SplitN(line, "\t", 5)
			if len(current) == 5 && strings.EqualFold(current[0], appName) {
				header = current
				ok = true
				collecting = true
			}
			continue
		}
		if collecting {
			fields := strings.SplitN(strings.TrimPrefix(line, "\t"), "\t", 2)
			if len(fields) == 2 {
				windows = append(windows, [2]string{fields[0], fields[1]})
			}
		}
	}
	return header, windows, ok
}

func TestAppBlock(t *testing.T) {
	listing := "Microsoft Edge\trunning\t-\t0\tC:\\Program Files\\Edge\\msedge.exe\n" +
		"\t197024\tSome page - Microsoft Edge\n" +
		"\t197100\tSave As\n" +
		"winusefixture\trunning\t-\t0\tC:\\temp\\winuse-e2e-1\\winusefixture.exe\n" +
		"\t456789\tWinuse Fixture\n" +
		"Visual Studio Code\tnot running\t2026-02-01T10:00:00+07:00\t12\tC:\\Users\\u\\AppData\\...\\Code.lnk\n"

	header, windows, ok := appBlock(listing, "winusefixture")
	if !ok {
		t.Fatalf("the winusefixture block was not found:\n%s", listing)
	}
	if len(header) != 5 || header[0] != "winusefixture" || header[1] != "running" || header[4] != `C:\temp\winuse-e2e-1\winusefixture.exe` {
		t.Fatalf("header: %q", header)
	}
	if len(windows) != 1 || windows[0][0] != "456789" || windows[0][1] != "Winuse Fixture" {
		t.Fatalf("windows: %+v", windows)
	}

	// Case-insensitive App name; a block that is not last in the listing.
	header, windows, ok = appBlock(listing, "MICROSOFT EDGE")
	if !ok || header[1] != "running" {
		t.Fatalf("case-insensitive lookup failed: %q, %v", header, ok)
	}
	if len(windows) != 2 || windows[1][0] != "197100" {
		t.Fatalf("windows: %+v", windows)
	}

	// A not-running App has no Window lines.
	header, windows, ok = appBlock(listing, "Visual Studio Code")
	if !ok || header[1] != "not running" || len(windows) != 0 {
		t.Fatalf("not-running block: %q, %+v, %v", header, windows, ok)
	}

	// Unknown App.
	if _, _, ok := appBlock(listing, "Nothing"); ok {
		t.Fatal("an unknown App must not be found")
	}
}
