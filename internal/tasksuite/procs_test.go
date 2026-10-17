package tasksuite

import "testing"

func TestNewProcessesDiff(t *testing.T) {
	before := ProcSnapshot{10: "explorer.exe", 20: "notepad.exe", 30: "userapp.exe"}
	after := ProcSnapshot{
		10: "explorer.exe", 20: "notepad.exe", 30: "userapp.exe", // pre-existing
		40: "msedge.exe", 50: "msedge.exe", 60: "winuse.exe", // the task's apps and helper
		70: "explorer.exe", // a NEW explorer pid — never killed
	}
	pids, names := NewProcesses(before, after, 0)
	if len(pids) != 3 {
		t.Fatalf("new processes = %v (%v), want 40, 50, 60", pids, names)
	}
	for i, want := range []struct {
		pid  uint32
		name string
	}{{40, "msedge.exe"}, {50, "msedge.exe"}, {60, "winuse.exe"}} {
		if pids[i] != want.pid || names[i] != want.name {
			t.Errorf("entry %d = %d/%s, want %d/%s", i, pids[i], names[i], want.pid, want.name)
		}
	}
}

func TestNewProcessesSkipsSelfAndExplorer(t *testing.T) {
	before := ProcSnapshot{1: "runner.exe"}
	after := ProcSnapshot{
		1:    "runner.exe",   // the runner itself
		2:    "explorer.exe", // the shell, even newly started
		3:    "notepad.exe",  // a task app
		9999: "whatever.exe", // would be killed
	}
	pids, _ := NewProcesses(before, after, 1)
	if len(pids) != 2 {
		t.Fatalf("pids = %v, want only 3 and 9999", pids)
	}
	for _, pid := range pids {
		if pid == 1 || pid == 2 {
			t.Errorf("pid %d must never be killed", pid)
		}
	}
}

func TestNewProcessesSortedByPid(t *testing.T) {
	before := ProcSnapshot{}
	after := ProcSnapshot{90: "b.exe", 10: "a.exe", 50: "c.exe"}
	pids, _ := NewProcesses(before, after, 0)
	if pids[0] != 10 || pids[1] != 50 || pids[2] != 90 {
		t.Errorf("pids not sorted: %v", pids)
	}
}
