//go:build e2e

// The clipboard tests (desktop-parity ticket 01): clipboard-set, then
// clipboard-get, for text, an image and files, through the real winuse
// binary, with Windows PowerShell as a second program on the other side
// (it reads what winuse wrote, and writes a bitmap winuse reads). The
// clipboard is the user's: the test keeps their text and puts it back
// afterwards, and skips when the clipboard holds anything but text, which
// it could not put back. Every write also lands in the clipboard history
// (Win+V) when that is on.

package e2e

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows"
)

var (
	e2eUser32                   = windows.NewLazySystemDLL("user32.dll")
	procE2EOpenClipboard        = e2eUser32.NewProc("OpenClipboard")
	procE2ECloseClipboard       = e2eUser32.NewProc("CloseClipboard")
	procE2EEnumClipboardFormats = e2eUser32.NewProc("EnumClipboardFormats")
)

// The text family Windows keeps beside CF_UNICODETEXT: CF_TEXT,
// CF_OEMTEXT, CF_UNICODETEXT and CF_LOCALE. A clipboard holding only these
// is plain text, which the test can put back.
var textFamily = map[uintptr]bool{1: true, 7: true, 13: true, 16: true}

// clipboardFormats lists the clipboard's formats, on one locked thread
// (the clipboard is opened and closed by one thread).
func clipboardFormats(t *testing.T) []uintptr {
	t.Helper()
	type result struct {
		formats []uintptr
		err     error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var r result
		for i := 0; ; i++ {
			if ok, _, _ := procE2EOpenClipboard.Call(0); ok != 0 {
				break
			}
			if i == 20 {
				r.err = fmt.Errorf("OpenClipboard kept failing")
				done <- r
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		for f := uintptr(0); ; {
			f, _, _ = procE2EEnumClipboardFormats.Call(f)
			if f == 0 {
				break
			}
			r.formats = append(r.formats, f)
		}
		procE2ECloseClipboard.Call()
		done <- r
	}()
	r := <-done
	if r.err != nil {
		t.Fatalf("listing the clipboard's formats: %v", r.err)
	}
	return r.formats
}

// keepUserClipboard saves the user's clipboard text and puts it back when
// the test ends (an empty clipboard is emptied again). It skips the test
// when the clipboard holds a format beyond plain text.
func keepUserClipboard(t *testing.T) {
	t.Helper()
	formats := clipboardFormats(t)
	for _, f := range formats {
		if !textFamily[f] {
			t.Skipf("the clipboard holds a format other than text (%d among %v), which the test could not put back; copy some plain text, or clear the clipboard, and rerun", f, formats)
		}
	}
	clip := win.NewClipboard()
	saved, hadText := "", len(formats) > 0
	if hadText {
		got, err := clip.Get(win.ClipText, 5*time.Second)
		if err != nil {
			t.Fatalf("saving the user's clipboard text: %v", err)
		}
		saved = got.Text
	}
	t.Logf("saved the user's clipboard: %d characters of text (empty: %v)", len(saved), !hadText)
	t.Cleanup(func() {
		var err error
		if hadText {
			err = clip.Set(win.ClipContent{Format: win.ClipText, Text: saved}, 5*time.Second)
		} else {
			err = clip.Clear(5 * time.Second)
		}
		if err != nil {
			t.Errorf("PUTTING BACK THE USER'S CLIPBOARD FAILED: %v", err)
			return
		}
		t.Log("put the user's clipboard back")
	})
}

// powershell runs a Windows PowerShell command (STA, so the
// System.Windows.Forms clipboard calls work) and returns its output.
func powershell(t *testing.T, script string) string {
	t.Helper()
	cmd := win.NoConsoleWindow(exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Sta", "-Command", script))
	// No console window of its own, which could take the foreground.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("powershell %q: %v\n%s", script, err, out)
	}
	return strings.TrimRight(string(out), "\r\n")
}

// clip runs winuse with the e2e idle and fails the test on a non-zero exit.
func clip(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("winuse %q: exit %d\nstdout: %s\nstderr: %s", args, exit, stdout, stderr)
	}
	return stdout
}

func TestClipboard(t *testing.T) {
	optIn(t)
	keepUserClipboard(t)
	fg := win.ForegroundWindow()
	// The long form of the temp folder: PowerShell prints file paths long
	// (C:\Users\HP MEDIA\... rather than C:\Users\HPMEDI~1\...).
	dir := longPath(t, t.TempDir())

	t.Run("text", func(t *testing.T) {
		text := fmt.Sprintf("winuse e2e xin chào 🙂 --headed %d", time.Now().UnixNano())
		if out := clip(t, "clipboard-set", text); out != "ok\n" {
			t.Errorf("clipboard-set printed %q, want ok", out)
		}
		if got := clip(t, "clipboard-get"); got != text {
			t.Errorf("clipboard-get = %q, want %q exactly", got, text)
		}
		if got := powershell(t, "Get-Clipboard -Raw"); !strings.HasPrefix(got, "winuse e2e") {
			t.Errorf("PowerShell's Get-Clipboard = %q; another program must see the text", got)
		}
		multi := "line one\r\nline two"
		clip(t, "clipboard-set", multi)
		if got := clip(t, "clipboard-get", "--format", "text"); got != multi {
			t.Errorf("multi-line text = %q, want %q", got, multi)
		}
	})

	t.Run("image", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 6, 4))
		src.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
		src.SetNRGBA(5, 3, color.NRGBA{0, 0, 255, 255})
		in := filepath.Join(dir, "in.png")
		writePNG(t, in, src)
		clip(t, "clipboard-set", "--image", in)

		out := filepath.Join(dir, "out.png")
		got := clip(t, "clipboard-get", "--format", "image", "--out", out)
		if got != out+" (6x4 px)\n" {
			t.Errorf("clipboard-get --format image printed %q", got)
		}
		img := readPNG(t, out)
		if c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); c != (color.NRGBA{255, 0, 0, 255}) {
			t.Errorf("pixel 0,0 = %v, want red", c)
		}
		// The CF_DIB winuse writes beside the PNG is what .NET reads.
		if wh := powershell(t, "Add-Type -AssemblyName System.Windows.Forms; $i = [System.Windows.Forms.Clipboard]::GetImage(); \"$($i.Width)x$($i.Height)\""); wh != "6x4" {
			t.Errorf("PowerShell read the image as %q, want 6x4", wh)
		}

		_, stderr, exit := runWinuse(t, []string{"clipboard-get"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 1 || !strings.Contains(stderr, "the clipboard holds no text") || !strings.Contains(stderr, "--format image") {
			t.Errorf("clipboard-get of an image as text: exit %d, stderr %q", exit, stderr)
		}

		// A bitmap another program put there (CF_BITMAP, no PNG) reads back
		// through the CF_DIB Windows synthesizes.
		powershell(t, "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; "+
			"$b = New-Object System.Drawing.Bitmap 7,5; $b.SetPixel(0,0,[System.Drawing.Color]::FromArgb(255,0,200,0)); "+
			"[System.Windows.Forms.Clipboard]::SetImage($b)")
		got = clip(t, "clipboard-get", "--format", "image", "--out", out)
		if !strings.HasSuffix(got, " (7x5 px)\n") {
			t.Errorf("the .NET bitmap read back as %q, want 7x5", got)
		}
		img = readPNG(t, out)
		if c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); c.G != 200 || c.R != 0 || c.A != 255 {
			t.Errorf("the .NET bitmap's pixel 0,0 = %v, want opaque green 200", c)
		}

		// Without --out the PNG goes to %TEMP%\winuse.
		got = clip(t, "clipboard-get", "--format", "image")
		path := strings.TrimSuffix(got, " (7x5 px)\n")
		if !strings.Contains(path, `\winuse\winuse-clipboard-`) {
			t.Errorf("default image path %q", path)
		}
		os.Remove(path)
	})

	t.Run("files", func(t *testing.T) {
		a := filepath.Join(dir, "a.txt")
		b := filepath.Join(dir, "b c.txt")
		for _, p := range []string{a, b} {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		clip(t, "clipboard-set", "--file", a, b)
		if got := clip(t, "clipboard-get", "--format", "files"); got != a+"\n"+b+"\n" {
			t.Errorf("clipboard-get --format files = %q", got)
		}
		if got := powershell(t, "(Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }) -join '|'"); got != a+"|"+b {
			t.Errorf("PowerShell's file list = %q, want %q", got, a+"|"+b)
		}

		_, stderr, exit := runWinuse(t, []string{"clipboard-set", "--file", filepath.Join(dir, "gone.txt")}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 1 || !strings.Contains(stderr, "nothing was sent") {
			t.Errorf("a missing file: exit %d, stderr %q", exit, stderr)
		}
		if got := clip(t, "clipboard-get", "--format", "files"); got != a+"\n"+b+"\n" {
			t.Errorf("the refused write changed the clipboard: %q", got)
		}
	})

	t.Run("clear", func(t *testing.T) {
		clip(t, "clipboard-set", "to be cleared")
		if out := clip(t, "clipboard-clear"); out != "ok\n" {
			t.Errorf("clipboard-clear printed %q", out)
		}
		_, stderr, exit := runWinuse(t, []string{"clipboard-get"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 1 || !strings.Contains(stderr, "the clipboard is empty") {
			t.Errorf("clipboard-get after clear: exit %d, stderr %q", exit, stderr)
		}
	})

	if now := win.ForegroundWindow(); now != fg {
		t.Errorf("the foreground moved from %d to %d; the clipboard commands need no foreground", fg, now)
	}
}

func longPath(t *testing.T, path string) string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		t.Fatalf("GetLongPathName(%s): %v", path, err)
	}
	return windows.UTF16ToString(buf[:n])
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return img
}
