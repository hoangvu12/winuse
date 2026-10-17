package win

import (
	"errors"
	"strings"
	"testing"
)

func testWindows() []WindowInfo {
	return []WindowInfo{
		{ID: 101, App: "Notepad", ExePath: `C:\Windows\System32\notepad.exe`, Title: "Untitled - Notepad"},
		{ID: 202, App: "Google Chrome", ExePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, Title: "Profile 1 - Google Chrome"},
		{ID: 203, App: "Google Chrome", ExePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, Title: "DevTools - example.com"},
		{ID: 204, App: "Calculator", ExePath: `C:\Program Files\WindowsApps\Microsoft.WindowsCalculator_8wekyb3d8bbwe!App\CalculatorApp.exe`, Title: "Calculator"},
		{ID: 205, App: "Unknown", ExePath: "", Title: "NoExe"},
	}
}

func TestResolveWindowByID(t *testing.T) {
	w, err := ResolveWindow(testWindows(), "202")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if w.ID != 202 {
		t.Errorf("resolved Window ID %d, want 202", w.ID)
	}
}

func TestResolveWindowByIDNotFound(t *testing.T) {
	_, err := ResolveWindow(testWindows(), "999999")
	if err == nil {
		t.Fatal("unknown ID must fail")
	}
	var re *ResolveError
	if !errors.As(err, &re) {
		t.Fatalf("error type %T, want *ResolveError", err)
	}
	if !strings.Contains(re.Msg, "no open Window with ID 999999") {
		t.Errorf("message = %q", re.Msg)
	}
	if re.Hint == "" {
		t.Error("a resolution failure needs a recovery hint")
	}
}

func TestResolveWindowByAppDisplayName(t *testing.T) {
	w, err := ResolveWindow(testWindows(), "notepad")
	if err != nil {
		t.Fatalf("resolve by display name (case-insensitive): %v", err)
	}
	if w.ID != 101 {
		t.Errorf("resolved Window ID %d, want 101", w.ID)
	}
}

func TestResolveWindowByExeBaseName(t *testing.T) {
	for _, ref := range []string{"chrome", "chrome.exe", "CHROME.EXE"} {
		if _, err := ResolveWindow(testWindows(), ref); err == nil {
			t.Fatalf("Chrome has two Windows: %q must be ambiguous", ref)
		}
	}
	if w, err := ResolveWindow(testWindows()[:1], "notepad.exe"); err != nil || w.ID != 101 {
		t.Errorf("resolve by exe base name: %v, %v", w, err)
	}
}

func TestResolveWindowAppUniqueResolves(t *testing.T) {
	w, err := ResolveWindow(testWindows(), "Calculator")
	if err != nil {
		t.Fatalf("unique app must resolve: %v", err)
	}
	if w.ID != 204 {
		t.Errorf("resolved Window ID %d, want 204", w.ID)
	}
}

func TestResolveWindowAppAmbiguousListsCandidates(t *testing.T) {
	_, err := ResolveWindow(testWindows(), "Google Chrome")
	if err == nil {
		t.Fatal("an App with several Windows must not resolve")
	}
	var re *ResolveError
	if !errors.As(err, &re) {
		t.Fatalf("error type %T, want *ResolveError", err)
	}
	if !strings.Contains(re.Msg, `app "Google Chrome" has 2 open Windows`) {
		t.Errorf("message = %q", re.Msg)
	}
	// The hint lists the candidates in list-windows' format, so an agent
	// can pick a Window ID directly.
	for _, want := range []string{"202\tGoogle Chrome\tProfile 1 - Google Chrome", "203\tGoogle Chrome\tDevTools - example.com"} {
		if !strings.Contains(re.Hint, want) {
			t.Errorf("hint %q missing candidate %q", re.Hint, want)
		}
	}
}

func TestResolveWindowAppNoWindows(t *testing.T) {
	_, err := ResolveWindow(testWindows(), "Visual Studio Code")
	if err == nil {
		t.Fatal("an App with no Windows must fail")
	}
	var re *ResolveError
	if !errors.As(err, &re) {
		t.Fatalf("error type %T, want *ResolveError", err)
	}
	if !strings.Contains(re.Msg, `app "Visual Studio Code" has no open Windows`) {
		t.Errorf("message = %q", re.Msg)
	}
}

func TestResolveWindowEmptyRef(t *testing.T) {
	if _, err := ResolveWindow(testWindows(), "  "); err == nil {
		t.Fatal("an empty reference must fail")
	}
}

func TestResolveWindowTitleIsNotAMatch(t *testing.T) {
	// A Window is named by its Window ID or its App name — never its title.
	if _, err := ResolveWindow(testWindows(), "Untitled - Notepad"); err == nil {
		t.Fatal("a title must not resolve a Window")
	}
}

func TestAppExeName(t *testing.T) {
	cases := []struct {
		w    WindowInfo
		want string
	}{
		{WindowInfo{ExePath: `C:\Program Files\App\winword.exe`}, "winword.exe"},
		{WindowInfo{ExePath: `C:\Windows\System32\notepad.exe`, App: "Notepad"}, "notepad.exe"},
		{WindowInfo{App: "Packaged App"}, "Packaged App"}, // UWP path resolution failed
		{WindowInfo{}, "unknown"},
	}
	for _, c := range cases {
		if got := AppExeName(c.w); got != c.want {
			t.Errorf("AppExeName(%+v) = %q, want %q", c.w, got, c.want)
		}
	}
}
