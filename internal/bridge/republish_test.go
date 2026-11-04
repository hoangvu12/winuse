package bridge_test

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
)

// TestSameHostImage: a running host under a new name (the user renamed the
// running winuse.exe to replace it) is still the host its bridge file
// names; another program at a reused pid is not.
func TestSameHostImage(t *testing.T) {
	for _, c := range []struct {
		recorded, now string
		want          bool
	}{
		{`C:\w\winuse.exe`, `c:\W\WINUSE.EXE`, true},
		{`C:\w\winuse.exe`, `C:\w\winuse.old.exe`, true},
		{`C:\w\winuse.exe`, `C:\elsewhere\winuse-0.1.exe`, true},
		{`C:\w\winuse.exe`, `C:\Windows\notepad.exe`, false},
		{`C:\w\host.exe`, `C:\w\winuse.exe`, false},
	} {
		if got := bridge.SameHostImage(c.recorded, c.now); got != c.want {
			t.Errorf("SameHostImage(%q, %q) = %v", c.recorded, c.now, got)
		}
	}
}

// TestHostRepublishesItsBridgeFile: a bridge file deleted while the host
// runs comes back, so a helper finds the extension again.
func TestHostRepublishesItsBridgeFile(t *testing.T) {
	dir := t.TempDir()
	inR, inW := io.Pipe()
	cfg := bridge.HostConfig{Dir: dir, Origin: bridge.Origin, PID: 4100, Republish: 20 * time.Millisecond,
		Listen: func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }}
	done := make(chan error, 1)
	go func() { done <- bridge.ServeHost(cfg, inR, io.Discard) }()
	bridge.WriteFrame(inW, []byte(`{"hello":{"browser":"brave","instance":"ab12cd34","version":"0.2.0","build":"b1"}}`))
	path := filepath.Join(dir, "brave-ab12cd34.json")
	waitFor := func(exists bool) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); (err == nil) == exists {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}
	if !waitFor(true) {
		t.Fatal("the bridge file never appeared")
	}
	os.Remove(path)
	if !waitFor(true) {
		t.Fatal("the deleted bridge file was not written again")
	}
	inW.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the host did not exit")
	}
	if !waitFor(false) {
		t.Error("the bridge file outlived its host")
	}
}
