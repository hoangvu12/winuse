//go:build e2e

// Caret-aware typing (ticket 13 of headless-max): `type-text --into` a
// WPF TextBox, which has no classic edit control behind it, splices the
// text at the box's caret through its Text and Value patterns, with no
// keys and no foreground. The target is a WPF window the test starts
// itself through Windows PowerShell (shown without activation), whose
// TextBox writes its text to a file on every change. User-run only (see
// the package comment and test/e2e/README.md); an agent must never start
// them.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// wpfBoxScript shows a WPF window with one TextBox (AutomationId "box",
// text "hello world") without activating it, and writes the box's text
// to the state file on every change. Its arguments are the title and the
// state file.
const wpfBoxScript = `param($title, $state)
Add-Type -AssemblyName PresentationFramework
$w = New-Object System.Windows.Window
$w.Title = $title
$w.Width = 420; $w.Height = 140
$w.ShowActivated = $false
$tb = New-Object System.Windows.Controls.TextBox
$tb.Text = "hello world"
[System.Windows.Automation.AutomationProperties]::SetAutomationId($tb, "box")
$tb.Add_TextChanged({ [System.IO.File]::WriteAllText($state, $tb.Text, [System.Text.Encoding]::UTF8) })
$w.Content = $tb
$app = New-Object System.Windows.Application
[void]$app.Run($w)
`

// startWPFBox starts the WPF window and returns its Window ID and a
// reader of the box's last written text.
func startWPFBox(t *testing.T) (string, func() string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "box.ps1")
	if err := os.WriteFile(script, []byte(wpfBoxScript), 0o644); err != nil {
		t.Fatalf("writing the WPF script: %v", err)
	}
	state := filepath.Join(dir, "box.txt")
	title := "winuse e2e wpf box " + strconv.FormatInt(time.Now().UnixNano(), 36)
	cmd := win.NoConsoleWindow(exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", script, title, state))
	if err := cmd.Start(); err != nil {
		t.Skipf("starting Windows PowerShell: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	id := findWindowByTitle(t, title, 30*time.Second)
	read := func() string {
		b, _ := os.ReadFile(state)
		return strings.TrimPrefix(string(b), string(rune(0xFEFF))) // WriteAllText's UTF-8 BOM
	}
	return strconv.FormatUint(id, 10), read
}

// waitText polls the box's text until it is want.
func waitText(t *testing.T, read func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if read() == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the box holds %q, want %q", read(), want)
}

func TestTypeTextIntoAWPFBoxSplicesAtTheCaret(t *testing.T) {
	optIn(t)
	window, read := startWPFBox(t)
	cover := coverWindow(t, window)
	if _, stderr, exit := runGetWindowState(t, window); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	pointer := pointerPos(t)

	// The caret after "hello" (select-text with length 0), then the
	// splice: the text goes in there, read back, the caret after it.
	stdout, stderr, exit := runSelectText(t, window, "id:box", "5", "0")
	assertActionOK(t, "select-text (the caret after hello)", stdout, stderr, exit)
	res, overlaySeen := runWatched(t, []string{"type-text", window, "--into", "id:box", "xy😀"})
	assertActionOK(t, "type-text --into the WPF box", res.stdout, res.stderr, res.exit)
	if !strings.Contains(res.stdout, "no key events fired") {
		t.Errorf("output = %q, want the no-key-events note", res.stdout)
	}
	waitText(t, read, "helloxy😀 world")
	if overlaySeen {
		t.Error("the in-control banner was shown during the splice")
	}

	// The caret ended after the text: the next splice follows it.
	if strings.Contains(res.stdout, "the caret could not be placed") {
		t.Logf("MEASURED: the caret was not placed after the text in WPF: %q", res.stdout)
	} else {
		stdout, stderr, exit = runTypeText(t, []string{window, "--into", "id:box", "!"})
		assertActionOK(t, "type-text --into after the first splice", stdout, stderr, exit)
		waitText(t, read, "helloxy😀! world")
	}
	assertForeground(t, "type-text --into a WPF box", cover)
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v", pointer, got)
	}
}
