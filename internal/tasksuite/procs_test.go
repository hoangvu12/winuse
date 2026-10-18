package tasksuite

import (
	"reflect"
	"testing"
)

func p(name string, parent uint32) ProcInfo { return ProcInfo{Name: name, Parent: parent} }

func TestTrackerRecordsOnlyHarnessDescendants(t *testing.T) {
	before := ProcSnapshot{4: p("system", 0), 10: p("explorer.exe", 4), 20: p("brave.exe", 10)}
	tr := newProcTracker(before, 100)

	// The Harness (100) starts a shell (110) that starts the winuse CLI
	// (120); meanwhile the user's browser spawns a renderer (200) and the
	// list shows a grandchild before its parent.
	tr.observe(ProcSnapshot{
		4: p("system", 0), 10: p("explorer.exe", 4), 20: p("brave.exe", 10),
		100: p("claude.exe", 1), 120: p("winuse.exe", 110), 110: p("pwsh.exe", 100),
		200: p("brave.exe", 20),
	})
	// The shell and the CLI exit; a child of the dead CLI (the helper,
	// 130) is still recognised because the CLI was recorded.
	tr.observe(ProcSnapshot{
		4: p("system", 0), 10: p("explorer.exe", 4), 20: p("brave.exe", 10),
		100: p("claude.exe", 1), 130: p("winuse.exe", 120), 200: p("brave.exe", 20),
	})

	want := map[uint32]string{110: "pwsh.exe", 120: "winuse.exe", 130: "winuse.exe"}
	if got := tr.Owned(); !reflect.DeepEqual(got, want) {
		t.Errorf("owned = %v, want %v", got, want)
	}
}

func TestTaskProcessesLeavesTheMachinesOwnNewProcessesAlone(t *testing.T) {
	// Found live: the first Task Suite run killed brave.exe, roboco.exe,
	// node.exe, smartscreen.exe … — processes the machine started while
	// the task ran. Only the task's processes may go.
	before := ProcSnapshot{
		10: p("explorer.exe", 4), 20: p("brave.exe", 10), 30: p("roboco.exe", 10),
		40: p("notepad.exe", 10), // the user's own Notepad
	}
	after := ProcSnapshot{
		10: p("explorer.exe", 4), 20: p("brave.exe", 10), 30: p("roboco.exe", 10), 40: p("notepad.exe", 10),
		// the machine's own new processes
		201: p("brave.exe", 20), 202: p("roboco.exe", 30), 203: p("node.exe", 30),
		204: p("smartscreen.exe", 900), 205: p("conhost.exe", 203),
		206: p("notepad.exe", 40), // a child of the user's pre-existing Notepad
		// the task's
		301: p("winuse.exe", 777),   // the detached helper: parent never observed
		302: p("notepad.exe", 950),  // launched through the shell
		303: p("pwsh.exe", 100),     // recorded descendant
		304: p("explorer.exe", 100), // never, even as a descendant
	}
	owned := map[uint32]string{303: "pwsh.exe", 304: "explorer.exe"}
	isRunBinary := func(pid uint32) bool { return pid == 301 }
	pids, names := TaskProcesses(before, after, owned, appExeNames([]string{"notepad"}), isRunBinary, 1)

	wantPids := []uint32{301, 302, 303}
	wantNames := []string{"winuse.exe", "notepad.exe", "pwsh.exe"}
	if !reflect.DeepEqual(pids, wantPids) || !reflect.DeepEqual(names, wantNames) {
		t.Errorf("task processes = %v %v, want %v %v", pids, names, wantPids, wantNames)
	}
}

func TestTaskProcessesSkipsReusedPidsAndOtherWinuseBuilds(t *testing.T) {
	before := ProcSnapshot{}
	after := ProcSnapshot{
		50: p("calc.exe", 1),   // recorded as pwsh.exe: the pid was reused
		60: p("winuse.exe", 1), // a winuse.exe that is not this run's build
	}
	owned := map[uint32]string{50: "pwsh.exe"}
	pids, _ := TaskProcesses(before, after, owned, nil, func(uint32) bool { return false }, 0)
	if len(pids) != 0 {
		t.Errorf("pids = %v, want none", pids)
	}
}

func TestTaskProcessesSortedByPid(t *testing.T) {
	after := ProcSnapshot{90: p("b.exe", 1), 10: p("a.exe", 1), 50: p("c.exe", 1)}
	owned := map[uint32]string{90: "b.exe", 10: "a.exe", 50: "c.exe"}
	pids, _ := TaskProcesses(ProcSnapshot{}, after, owned, nil, nil, 0)
	if !reflect.DeepEqual(pids, []uint32{10, 50, 90}) {
		t.Errorf("pids not sorted: %v", pids)
	}
}

func TestAppExeNames(t *testing.T) {
	got := appExeNames([]string{"notepad", "MSEdge", "code.exe", "explorer", " "})
	want := map[string]bool{"notepad.exe": true, "msedge.exe": true, "code.exe": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appExeNames = %v, want %v", got, want)
	}
}
