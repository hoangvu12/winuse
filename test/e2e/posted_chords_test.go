//go:build e2e

// Posted chords through attached key state (ticket 07 of headless-max):
// `press-key <window> <chord> --post` holds the chord's modifiers for the
// target Window's thread only and posts the key. Two targets, as measured
// in probes round 4: the fixture's classic Win32 accelerator
// (TranslateAccelerator, no activation pair) and a WPF KeyBinding (with
// the activation pair) in a window the test starts through Windows
// PowerShell. Both covered; the foreground must not move and the user's
// own Control key must not read as down. User-run only (see the package
// comment and test/e2e/README.md); an agent must never start them.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/fixture"
)

var procChordGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")

// userControlDown reports whether the user's Control key reads as down.
func userControlDown() bool {
	r, _, _ := procChordGetAsyncKeyState.Call(0x11)
	return r&0x8000 != 0
}

// runPostedChord invokes `winuse press-key <window> <chord> --post`.
func runPostedChord(t *testing.T, window, chord string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"press-key", window, chord, "--post"}, "WINUSE_HELPER_IDLE="+e2eIdle)
}

func TestPostedChordFiresAClassicAccelerator(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	stdout, stderr, exit := runPostedChord(t, window, fixture.AcceleratorChord)
	assertActionOK(t, "press-key "+fixture.AcceleratorChord+" --post", stdout, stderr, exit)
	if !strings.HasPrefix(stdout, "ok\nNote: posted") {
		t.Errorf("output = %q, want the posted output", stdout)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Accelerators == 1 })
	assertForeground(t, "a posted chord into the fixture", cover)
	if userControlDown() {
		t.Error("the user's Control key reads as down after the posted chord")
	}
}

// wpfKeyScript shows a WPF window without activating it, with a KeyBinding
// Ctrl+S that appends "saved" to the state file. Its arguments are the
// title and the state file.
const wpfKeyScript = `param($title, $state)
Add-Type -AssemblyName PresentationFramework
$w = New-Object System.Windows.Window
$w.Title = $title
$w.Width = 420; $w.Height = 140
$w.ShowActivated = $false
$cmd = New-Object System.Windows.Input.RoutedCommand
$w.CommandBindings.Add((New-Object System.Windows.Input.CommandBinding($cmd, { [System.IO.File]::AppendAllText($state, "saved") }))) | Out-Null
$w.InputBindings.Add((New-Object System.Windows.Input.KeyBinding($cmd, [System.Windows.Input.Key]::S, [System.Windows.Input.ModifierKeys]::Control))) | Out-Null
$app = New-Object System.Windows.Application
[void]$app.Run($w)
`

func TestPostedChordFiresAWPFKeyBinding(t *testing.T) {
	optIn(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "keys.ps1")
	if err := os.WriteFile(script, []byte(wpfKeyScript), 0o644); err != nil {
		t.Fatalf("writing the WPF script: %v", err)
	}
	state := filepath.Join(dir, "keys.txt")
	title := "winuse e2e wpf keys " + strconv.FormatInt(time.Now().UnixNano(), 36)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-File", script, title, state)
	if err := cmd.Start(); err != nil {
		t.Skipf("starting Windows PowerShell: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	window := strconv.FormatUint(findWindowByTitle(t, title, 30*time.Second), 10)
	cover := coverWindow(t, window)

	stdout, stderr, exit := runPostedChord(t, window, "Control_L+s")
	assertActionOK(t, "press-key Control_L+s --post into WPF", stdout, stderr, exit)
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(state)
		if string(b) == "saved" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the WPF KeyBinding did not fire: state %q", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	assertForeground(t, "a posted chord into WPF", cover)
	if userControlDown() {
		t.Error("the user's Control key reads as down after the posted chord")
	}
}
