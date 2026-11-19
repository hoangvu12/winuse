package browser

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/win"
)

// TestAnyInspectWindow: connect waits for the toggle only while a top-level
// window of that browser shows the inspect page; another browser's window
// or an owned popup with that title does not count.
func TestAnyInspectWindow(t *testing.T) {
	brave := `C:\Users\u\AppData\Local\BraveSoftware\Brave-Browser\Application\brave.exe`
	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	inspect := "Inspect with Chrome Developer Tools - Brave"
	for _, c := range []struct {
		name string
		ws   []win.WindowInfo
		want bool
	}{
		{"the user's tabs only", []win.WindowInfo{{ID: 1, ExePath: brave, Title: "YouTube - Brave"}}, false},
		{"an inspect window", []win.WindowInfo{{ID: 1, ExePath: brave, Title: "YouTube - Brave"}, {ID: 2, ExePath: brave, Title: inspect}}, true},
		{"the image matches case-insensitively", []win.WindowInfo{{ID: 2, ExePath: `C:\B\BRAVE.EXE`, Title: inspect}}, true},
		{"another browser's inspect window", []win.WindowInfo{{ID: 2, ExePath: chrome, Title: "Inspect with Chrome Developer Tools - Google Chrome"}}, false},
		{"an owned popup", []win.WindowInfo{{ID: 3, ExePath: brave, Title: inspect, Owner: 1}}, false},
	} {
		if got := anyInspectWindow(c.ws, "brave.exe"); got != c.want {
			t.Errorf("%s: anyInspectWindow = %v, want %v", c.name, got, c.want)
		}
	}
}
