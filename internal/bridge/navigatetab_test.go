package bridge

import (
	"strings"
	"testing"
)

// TestNavigateTabOnlyChangesTheURL: Winuse.navigateTab passes the refusal
// rule like the bridge's other methods, and background.js answers it with
// chrome.tabs.update of the url alone (the foreground rule is checked for
// the whole file by TestExtensionIdentity's never list).
func TestNavigateTabOnlyChangesTheURL(t *testing.T) {
	if code, _ := Refusal(MethodNavigateTab, ""); code != 0 {
		t.Errorf("%s refused with %d", MethodNavigateTab, code)
	}
	if code, _ := Refusal(MethodNavigateTab, "T1"); code != CodeRefused {
		t.Errorf("%s on a session: code %d", MethodNavigateTab, code)
	}
	js, err := extensionFiles.ReadFile("extension/background.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, want := range []string{
		`if (m.method === "` + MethodNavigateTab + `") return navigateTab(m.params || {});`,
		"await chrome.tabs.update(tab.id, { url: p.url });",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("background.js lacks %q", want)
		}
	}
}
