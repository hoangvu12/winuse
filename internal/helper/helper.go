package helper

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Defaults for the real helper (orchestrator decision: idle exit is
// 5 minutes of no requests). The next command restarts it transparently.
// Overridable with WINUSE_HELPER_IDLE (a Go duration like "90s", or a
// plain number of seconds).
const DefaultIdle = 5 * time.Minute

// logMaxSize truncates the log once it passes 1 MiB.
const logMaxSize = 1 << 20

// Main is the helper entry point (winuse __helper). It returns the process
// exit code.
func Main() int {
	logFile, err := openLog()
	if err == nil {
		defer logFile.Close()
		log.SetOutput(logFile)
	} else {
		log.Printf("opening helper log: %v", err)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("winuse helper %s starting (pid %d)", version.Version, os.Getpid())

	// Per-monitor DPI awareness v2 before any window call, so every later
	// coordinate is in physical pixels. The manifest covers the installed
	// binary; this covers go run. Already-set (manifest) is success.
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		log.Printf("setting per-monitor-v2 DPI awareness: %v", err)
	}

	// The helper must live in the interactive desktop session.
	if !win.HasInputDesktop() {
		log.Printf("no input desktop available: %v", win.ErrNoDesktop)
		fmt.Fprintln(os.Stderr, win.ErrNoDesktop)
		return 1
	}

	id, err := win.CurrentIdentity()
	if err != nil {
		log.Printf("reading process identity: %v", err)
		return 1
	}

	// Single instance per user/session/integrity level. The loser of a
	// start race exits quietly; the winner serves.
	releaseMutex, err := pipe.AcquireSingleton(id)
	if err != nil {
		if err == pipe.ErrAlreadyRunning {
			log.Printf("another helper is already running, exiting")
			return 0
		}
		log.Printf("acquiring helper mutex: %v", err)
		return 1
	}
	defer releaseMutex()

	ln, err := pipe.Listen(id)
	if err != nil {
		log.Printf("listening on pipe %s: %v", pipe.Name(id), err)
		return 1
	}

	idle, err := idleFromEnv(DefaultIdle)
	if err != nil {
		log.Printf("invalid WINUSE_HELPER_IDLE, using default %s: %v", DefaultIdle, err)
		idle = DefaultIdle
	}
	log.Printf("listening on %s (idle timeout %s)", pipe.Name(id), idle)

	server := New(version.Version, idle, protocol.RequestTimeout, Commands(), nil)
	err = server.Serve(ln)
	if err != nil {
		log.Printf("helper server: %v", err)
		return 1
	}
	log.Printf("helper exiting")
	return 0
}

// idleFromEnv parses WINUSE_HELPER_IDLE: a Go duration ("90s", "2m") or a
// plain number of seconds. Empty means the default.
func idleFromEnv(def time.Duration) (time.Duration, error) {
	v := os.Getenv("WINUSE_HELPER_IDLE")
	if v == "" {
		return def, nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	var secs int
	if _, err := fmt.Sscanf(v, "%d", &secs); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("WINUSE_HELPER_IDLE=%q is neither a duration nor a number of seconds", v)
}

// openLog opens %LOCALAPPDATA%\winuse\helper.log, truncating it once it
// passes logMaxSize. The helper is detached: stderr goes to NUL, so this
// file is the only diagnostic surface.
func openLog() (*os.File, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return nil, fmt.Errorf("LOCALAPPDATA is not set")
	}
	dir = filepath.Join(dir, "winuse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "helper.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > logMaxSize {
		os.Remove(path)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
