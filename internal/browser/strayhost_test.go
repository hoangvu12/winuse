package browser

// The upgrade path (background-tabs live check 3): the user replaced
// winuse.exe while a native host of the old binary ran, renamed to
// winuse.old.exe. Its bridge file was taken for a dead host's and deleted,
// so no winuse reached the extension, whose port the old host kept. ext
// status now ends such a host (one its browser started that no bridge file
// names), waits for the extension to connect again, and says so.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
)

func TestExtStatusEndsAStrayHostAndWaitsForTheExtension(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.installedFor("chrome", true, true)
	f.process("chrome", 100)
	chromeExe := f.sys.Env.CandidatePaths()["chrome"][0]
	var ended []int
	f.sys.NativeHosts = func() []bridge.HostProcess {
		return []bridge.HostProcess{
			{PID: 77, Exe: `C:\Users\u\AppData\Local\winuse\winuse.old.exe`, BrowserPID: 500, BrowserExe: chromeExe},
			{PID: 78, Exe: `C:\x\winuse.exe`, BrowserPID: 9, BrowserExe: `C:\Program Files\Other\other.exe`}, // another browser's
		}
	}
	f.sys.EndHost = func(pid int) error {
		ended = append(ended, pid)
		// The extension's port closes; it starts the registered host again.
		go f.connect("chrome", "ab12cd34", nil)
		return nil
	}
	out, err := NewManager(f.sys).ExtStatus(context.Background(), "chrome")
	if err != nil {
		t.Fatalf("ext status: %v", err)
	}
	if len(ended) != 1 || ended[0] != 77 {
		t.Errorf("ended %v, want the chrome host 77 only", ended)
	}
	if !strings.HasPrefix(out, "ok chrome connected through the winuse extension") ||
		!strings.Contains(out, `note: ended a winuse native host no winuse could reach (pid 77, C:\Users\u\AppData\Local\winuse\winuse.old.exe), which held the extension in Chrome; it connected again`) {
		t.Errorf("out = %q", out)
	}
}

func TestExtStatusNamesAStrayHostThatDidNotComeBack(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.installedFor("chrome", true, true)
	f.process("chrome", 100)
	f.sys.HostReconnectWait = 200 * time.Millisecond
	f.sys.NativeHosts = func() []bridge.HostProcess {
		return []bridge.HostProcess{{PID: 77, Exe: `C:\w\winuse.old.exe`, BrowserPID: 500, BrowserExe: f.sys.Env.CandidatePaths()["chrome"][0]}}
	}
	f.sys.EndHost = func(int) error { return nil }
	_, err := NewManager(f.sys).ExtStatus(context.Background(), "chrome")
	p := problemOf(t, err, ExtStrayHost)
	if !strings.Contains(p.Sentence(), `held by a native host of an older winuse that no winuse could reach (pid 77, C:\w\winuse.old.exe); winuse ended it`) ||
		!strings.Contains(p.Next(), "winuse browser ext status chrome --wait 60") {
		t.Errorf("problem = %q / %q", p.Sentence(), p.Next())
	}
}

// TestExtStatusLeavesAHostWithABridgeFile: a host a bridge file names is
// the one winuse talks to; it is never ended.
func TestExtStatusLeavesAHostWithABridgeFile(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.installedFor("chrome", true, true)
	ext := f.connect("chrome", "ab12cd34", &bridgetest.Extension{})
	f.sys.NativeHosts = func() []bridge.HostProcess {
		return []bridge.HostProcess{{PID: ext.Info.PID, Exe: `C:\w\winuse.exe`, BrowserPID: 500, BrowserExe: f.sys.Env.CandidatePaths()["chrome"][0]}}
	}
	f.sys.EndHost = func(pid int) error {
		t.Errorf("ended host %d", pid)
		return nil
	}
	if _, err := NewManager(f.sys).ExtStatus(context.Background(), "chrome"); err != nil {
		t.Fatal(err)
	}
}
