package tasksuite

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcInfo is one process in a snapshot: its exe base name (lower-cased)
// and its parent's pid.
type ProcInfo struct {
	Name   string
	Parent uint32
}

// ProcSnapshot maps pid → process, the enumeration the cleanup works on.
type ProcSnapshot map[uint32]ProcInfo

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
		out[entry.ProcessID] = ProcInfo{
			Name:   strings.ToLower(windows.UTF16ToString(entry.ExeFile[:])),
			Parent: entry.ParentProcessID,
		}
		entry.Size = entrySize
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out, nil
}

// procTracker records which processes a task started. The machine keeps
// starting processes of its own while a task runs (browsers, sync
// clients, the user's tools), so "new since the task began" is not
// "started by the task": only descendants of the Harness count, and they
// are recorded while the run is live, because a child outlives the
// short-lived shell that started it and its parent pid then names a
// process that no longer exists.
type procTracker struct {
	mu     sync.Mutex
	before ProcSnapshot
	root   uint32            // the Harness process
	owned  map[uint32]string // pid → name of every descendant seen
}

func newProcTracker(before ProcSnapshot, root uint32) *procTracker {
	return &procTracker{before: before, root: root, owned: map[uint32]string{}}
}

// observe adds the snapshot's new descendants of the root (to a fixed
// point: a snapshot may list a child before its parent).
func (tr *procTracker) observe(snap ProcSnapshot) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for changed := true; changed; {
		changed = false
		for pid, p := range snap {
			if _, done := tr.owned[pid]; done || pid == tr.root {
				continue
			}
			if _, old := tr.before[pid]; old {
				continue
			}
			if _, parentOwned := tr.owned[p.Parent]; parentOwned || p.Parent == tr.root {
				tr.owned[pid] = p.Name
				changed = true
			}
		}
	}
}

// poll observes a snapshot every interval until stop is closed.
func (tr *procTracker) poll(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if snap, err := snapshotProcesses(); err == nil {
			tr.observe(snap)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// Owned returns a copy of the recorded descendants.
func (tr *procTracker) Owned() map[uint32]string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make(map[uint32]string, len(tr.owned))
	for pid, name := range tr.owned {
		out[pid] = name
	}
	return out
}

// TaskProcesses decides what the cleanup kills, from the snapshot before
// the Harness started, the snapshot after the Check, and the descendants
// recorded while it ran. A process is the task's when it is new and:
//
//   - a recorded descendant of the Harness; or
//   - runs this run's own winuse.exe (the detached helper — its parent,
//     a short-lived CLI, may never have been observed); or
//   - is one of the task's declared apps (appExes, e.g. "notepad.exe")
//     not spawned by a pre-existing process of the same app (an app
//     launched through the shell has no Harness ancestry, but the user's
//     own already-running copy of that app keeps its children).
//
// Everything else — whatever the machine started meanwhile — is left
// alone. explorer.exe and the runner itself never qualify. Sorted by pid.
func TaskProcesses(before, after ProcSnapshot, owned map[uint32]string, appExes map[string]bool,
	isRunBinary func(pid uint32) bool, selfPID uint32) (pids []uint32, names []string) {
	for pid, p := range after {
		if _, old := before[pid]; old || pid == selfPID || neverKill[p.Name] {
			continue
		}
		ownedName, isOwned := owned[pid]
		switch {
		case isOwned && ownedName == p.Name:
		case p.Name == "winuse.exe" && isRunBinary != nil && isRunBinary(pid):
		case appExes[p.Name] && !preexistingSameApp(before, p):
		default:
			continue
		}
		pids = append(pids, pid)
		names = append(names, p.Name)
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

// preexistingSameApp reports whether p's parent is a process of the same
// app that was already running before the task (the user's own copy).
func preexistingSameApp(before ProcSnapshot, p ProcInfo) bool {
	parent, ok := before[p.Parent]
	return ok && parent.Name == p.Name
}

// appExeNames maps a task's declared apps ("notepad", "msedge") to the
// exe base names the cleanup may match.
func appExeNames(apps []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range apps {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if !strings.HasSuffix(a, ".exe") {
			a += ".exe"
		}
		if !neverKill[a] {
			out[a] = true
		}
	}
	return out
}

// processPath returns a live process's full image path, lower-cased.
func processPath(pid uint32) (string, bool) {
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
	return strings.ToLower(strings.TrimSpace(windows.UTF16ToString(buf[:size]))), true
}

// processName re-reads a live process's exe base name, so a pid is only
// killed while it still is the process the snapshot saw (pids are
// reused; the diff window is minutes, but the check is cheap).
func processName(pid uint32) (string, bool) {
	full, ok := processPath(pid)
	if !ok {
		return "", false
	}
	base := full
	if i := strings.LastIndexByte(full, '\\'); i >= 0 {
		base = full[i+1:]
	}
	if i := strings.LastIndexByte(base, ':'); i >= 0 { // \\?\C:... prefixes
		base = base[i+1:]
	}
	return base, true
}

// KillTree kills a process tree (taskkill /T /F), used only for a Harness
// that exceeded its time limit — its own tree.
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

// CleanupTaskProcesses kills the processes TaskProcesses chose, one by
// one (never their trees: a tree can reach processes that are not the
// task's), verifying each pid still holds the name the snapshot saw.
func CleanupTaskProcesses(pids []uint32, names []string, logf func(string)) {
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
		out, err := exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(pid), 10), "/F").CombinedOutput()
		if logf == nil {
			continue
		}
		if err != nil {
			logf(fmt.Sprintf("cleanup: killing %s (pid %d): %v: %s", name, pid, err, strings.TrimSpace(string(out))))
		} else {
			logf(fmt.Sprintf("cleanup: killed %s (pid %d)", name, pid))
		}
	}
}
