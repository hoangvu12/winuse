package bridge

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/win"
)

// maxHostLog bounds host.log; a longer one starts over.
const maxHostLog = 256 << 10

// HostMain is the native host's entry point: cmd/winuse calls it when the
// browser starts winuse with the extension's origin (IsHostLaunch). dir is
// the bridges directory. It returns the process exit code. It refuses
// inside a go test binary, so no test publishes a bridge file in the real
// %LOCALAPPDATA% or listens as a real host.
func HostMain(origin, dir string) int {
	if win.LiveDesktopGuard() != nil {
		return 1
	}
	os.MkdirAll(dir, 0o755)
	logPath := filepath.Join(dir, "host.log")
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > maxHostLog {
		os.Remove(logPath)
	}
	log, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if log != nil {
		defer log.Close()
	}
	exe, _ := os.Executable()
	cfg := HostConfig{
		Dir:     dir,
		Origin:  origin,
		PID:     os.Getpid(),
		Exe:     exe,
		Browser: ParentBrowser,
		Listen:  func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") },
	}
	if log != nil {
		cfg.Log = log
	}
	if err := ServeHost(cfg, os.Stdin, os.Stdout); err != nil {
		if log != nil {
			(&host{cfg: cfg}).logf("%v", err)
		}
		return 1
	}
	return 0
}

// LastLogLine is host.log's last line in dir, which names the failure
// when the browser did start winuse but the host gave up; "" when there
// is none.
func LastLogLine(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "host.log"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// shells are the processes Chromium starts a native host through on
// Windows (cmd.exe /d /c "host" ...); the browser is above them.
var shells = map[string]bool{"cmd.exe": true, "conhost.exe": true}

// ParentBrowser names the process that started this host: the parent,
// past the cmd.exe Chromium launches hosts through.
func ParentBrowser() (int, string) {
	parents := processParents()
	pid := uint32(os.Getppid())
	for range 4 {
		exe, _ := ProcessImage(pid)
		if exe == "" || !shells[strings.ToLower(filepath.Base(exe))] {
			return int(pid), exe
		}
		next, ok := parents[pid]
		if !ok || next == 0 {
			return int(pid), exe
		}
		pid = next
	}
	exe, _ := ProcessImage(pid)
	return int(pid), exe
}

// processParents maps every process to its parent, from a Toolhelp
// snapshot.
func processParents() map[uint32]uint32 {
	out := map[uint32]uint32{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[e.ProcessID] = e.ParentProcessID
	}
	return out
}

// ProcessImage is a process's full image path.
func ProcessImage(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// stillActive is GetExitCodeProcess's answer for a running process.
const stillActive = 259

// HostAlive reports whether a bridge file's host still runs: its pid is a
// live process with the recorded image (a reused pid is some other
// program).
func HostAlive(info Info) bool {
	if info.PID <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(info.PID))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return false
	}
	if info.Exe == "" {
		return true
	}
	image, err := ProcessImage(uint32(info.PID))
	return err == nil && SameHostImage(info.Exe, image)
}

// NativeHosts lists the winuse native hosts running now: every winuse
// process other than this one whose parent, past the cmd.exe Chromium
// starts hosts through, is not a winuse process (the helper's parent is
// the CLI or a shell, never a browser). Each names that parent as its
// browser.
func NativeHosts() []HostProcess {
	parents := processParents()
	self := uint32(os.Getpid())
	var out []HostProcess
	for pid, parent := range parents {
		if pid == self || pid == 0 {
			continue
		}
		exe, err := ProcessImage(pid)
		if err != nil || !isWinuseImage(exe) {
			continue
		}
		bpid := parent
		bexe, _ := ProcessImage(bpid)
		for range 3 {
			if bexe == "" || !shells[strings.ToLower(filepath.Base(bexe))] {
				break
			}
			bpid = parents[bpid]
			bexe, _ = ProcessImage(bpid)
		}
		if bexe == "" || isWinuseImage(bexe) || shells[strings.ToLower(filepath.Base(bexe))] {
			continue
		}
		out = append(out, HostProcess{PID: int(pid), Exe: exe, BrowserPID: int(bpid), BrowserExe: bexe})
	}
	return out
}

// EndHost ends a native host process. Its browser closes the extension's
// port, and the extension starts a new host from the registered binary.
func EndHost(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}
