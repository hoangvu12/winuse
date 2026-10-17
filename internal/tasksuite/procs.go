package tasksuite

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcSnapshot maps pid → exe base name (lower-cased), the before/after
// pair the cleanup diff works on.
type ProcSnapshot map[uint32]string

// neverKill lists processes the cleanup must never touch even when they
// are new: killing the shell restarts the user's desktop (all Explorer
// windows belong to the one pre-existing explorer.exe; the task's
// Explorer window is closed by the agent, never by the runner).
var neverKill = map[string]bool{
	"explorer.exe": true,
}

// snapshotProcesses lists the machine's processes once. Read-only: no
// input, no windows, no capture — it is the same enumeration
// list-windows performs.
func snapshotProcesses() (ProcSnapshot, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	entrySize := uint32(unsafe.Sizeof(windows.ProcessEntry32{}))
	var entry windows.ProcessEntry32
	entry.Size = entrySize
	out := ProcSnapshot{}
	if err := windows.Process32First(snap, &entry); err != nil {
		return out, nil // an empty snapshot is a valid (degraded) result
	}
	for {
		out[entry.ProcessID] = strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		entry.Size = entrySize
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out, nil
}

// NewProcesses computes which processes a task started: everything in
// after that is not in before (minus the never-kill list and the runner
// itself). Deterministic and pure — the offline tests cover it.
func NewProcesses(before, after ProcSnapshot, selfPID uint32) (pids []uint32, names []string) {
	for pid, name := range after {
		if _, old := before[pid]; old {
			continue
		}
		if pid == selfPID {
			continue
		}
		if neverKill[name] {
			continue
		}
		pids = append(pids, pid)
		names = append(names, name)
	}
	// stable order: kill in pid order, so logs (and tests) are
	// reproducible.
	for i := 1; i < len(pids); i++ {
		for j := i; j > 0 && pids[j] < pids[j-1]; j-- {
			pids[j], pids[j-1] = pids[j-1], pids[j]
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return pids, names
}

// processName re-reads a live process's exe base name, so a pid is only
// killed while it still is the process the snapshot saw (pids are
// reused; the diff window is minutes, but the check is cheap).
func processName(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	full := windows.UTF16ToString(buf[:size])
	base := full
	if i := strings.LastIndexByte(full, '\\'); i >= 0 {
		base = full[i+1:]
	}
	if i := strings.LastIndexByte(base, ':'); i >= 0 { // \\?\C:... prefixes
		base = base[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(base)), true
}

// KillTree kills a process tree (taskkill /T /F), used for a Harness
// that exceeded its time limit.
func KillTree(pid uint32, logf func(string)) {
	out, err := exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(pid), 10), "/T", "/F").CombinedOutput()
	if logf != nil {
		if err != nil {
			logf(fmt.Sprintf("cleanup: killing pid %d: %v: %s", pid, err, strings.TrimSpace(string(out))))
		} else {
			logf(fmt.Sprintf("cleanup: killed the process tree of pid %d", pid))
		}
	}
}

// CleanupNewProcesses kills the processes a task left behind (apps the
// agent started, winuse's helper), verifying each pid still holds the
// name the snapshot saw. The user's own processes were in `before` and
// are never touched.
func CleanupNewProcesses(pids []uint32, names []string, logf func(string)) {
	for i, pid := range pids {
		name, ok := processName(pid)
		if !ok {
			continue // already gone
		}
		if name != names[i] {
			if logf != nil {
				logf(fmt.Sprintf("cleanup: pid %d is now %s, not %s — leaving it alone", pid, name, names[i]))
			}
			continue
		}
		if neverKill[name] {
			continue
		}
		KillTree(pid, logf)
		if logf != nil {
			logf(fmt.Sprintf("cleanup: killed %s (pid %d)", name, pid))
		}
	}
}
