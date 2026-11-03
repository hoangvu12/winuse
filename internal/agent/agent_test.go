package agent

import (
	"strings"
	"testing"
)

// tree is the process tree measured under Claude Code's Bash tool, plus a
// second Harness, with winuse at the bottom.
func tree() map[uint32]Proc {
	return map[uint32]Proc{
		5540:  {PID: 5540, Parent: 4, Exe: "explorer.exe"},
		19040: {PID: 19040, Parent: 5540, Exe: "zeron.exe"},
		17048: {PID: 17048, Parent: 19040, Exe: "claude.exe"},
		25392: {PID: 25392, Parent: 17048, Exe: "bash.exe"},
		16564: {PID: 16564, Parent: 25392, Exe: "bash.exe"},
		7184:  {PID: 7184, Parent: 16564, Exe: "PowerShell.EXE"},
		900:   {PID: 900, Parent: 7184, Exe: "winuse.exe"},
		// A second Harness: Codex, through cmd.
		3000: {PID: 3000, Parent: 5540, Exe: "codex.exe"},
		3100: {PID: 3100, Parent: 3000, Exe: "cmd.exe"},
		3200: {PID: 3200, Parent: 3100, Exe: "winuse.exe"},
		// A winuse whose parent is gone.
		4000: {PID: 4000, Parent: 4444, Exe: "winuse.exe"},
	}
}

// started fakes the start times: every process started after its parent
// unless the test says otherwise.
func started(over map[uint32]int64) func(uint32) int64 {
	return func(pid uint32) int64 {
		if t, ok := over[pid]; ok {
			return t
		}
		return 1000 + int64(pid%1000) // a constant per process
	}
}

func TestWalkFindsTheFirstProcessThatIsNoShell(t *testing.T) {
	order := map[uint32]int64{5540: 10, 19040: 20, 17048: 30, 25392: 40, 16564: 50, 7184: 60, 900: 70, 3000: 31, 3100: 41, 3200: 51}
	p, start, ok := Walk(900, tree(), started(order))
	if !ok || p.PID != 17048 || p.Exe != "claude.exe" || start != 30 {
		t.Fatalf("Walk(winuse under claude) = %+v, %d, %v, want claude.exe 17048 started 30", p, start, ok)
	}
	if got := FromProcess(p, start); got.ID != "proc:17048:30" || got.Label != "claude 17048" {
		t.Errorf("FromProcess = %+v", got)
	}
	p, _, ok = Walk(3200, tree(), started(order))
	if !ok || p.PID != 3000 {
		t.Fatalf("Walk(winuse under codex) = %+v, %v, want codex.exe 3000", p, ok)
	}
}

func TestWalkGivesUpWithoutAnIdentity(t *testing.T) {
	order := map[uint32]int64{5540: 10, 19040: 20, 17048: 30, 25392: 40, 16564: 50, 7184: 60, 900: 70}
	if _, _, ok := Walk(4000, tree(), started(order)); ok {
		t.Error("a missing parent must give no identity")
	}
	if _, _, ok := Walk(1, tree(), started(order)); ok {
		t.Error("an unknown self must give no identity")
	}
	// The process id 17048 was reused by a process that started after
	// its child bash: the chain is broken there.
	reused := map[uint32]int64{5540: 10, 19040: 20, 17048: 99, 25392: 40, 16564: 50, 7184: 60, 900: 70}
	if p, _, ok := Walk(900, tree(), started(reused)); ok {
		t.Errorf("a reused parent id must give no identity, got %+v", p)
	}
	// Unknown start times are not judged.
	unknown := func(uint32) int64 { return 0 }
	if p, _, ok := Walk(900, tree(), unknown); !ok || p.PID != 17048 {
		t.Errorf("Walk with unknown start times = %+v, %v, want claude.exe", p, ok)
	}
	// A loop of shells ends after 32 steps.
	loop := map[uint32]Proc{1: {PID: 1, Parent: 2, Exe: "winuse.exe"}, 2: {PID: 2, Parent: 3, Exe: "cmd.exe"}, 3: {PID: 3, Parent: 2, Exe: "bash.exe"}}
	if _, _, ok := Walk(1, loop, unknown); ok {
		t.Error("a loop of shells must give no identity")
	}
}

func TestSkipped(t *testing.T) {
	for _, exe := range []string{"cmd.exe", "PowerShell.EXE", "pwsh.exe", "bash.exe", "sh.exe", "conhost.exe", "OpenConsole.exe", "winuse.exe"} {
		if !Skipped(exe) {
			t.Errorf("%s must be skipped", exe)
		}
	}
	for _, exe := range []string{"claude.exe", "codex.exe", "node.exe", "explorer.exe", "WindowsTerminal.exe"} {
		if Skipped(exe) {
			t.Errorf("%s must not be skipped: it can be the Harness", exe)
		}
	}
}

func TestFromEnv(t *testing.T) {
	if id, ok := FromEnv("  reviewer "); !ok || id.ID != "name:reviewer" || id.Label != "reviewer" {
		t.Errorf("FromEnv(reviewer) = %+v, %v", id, ok)
	}
	for _, v := range []string{"", "   ", "a\tb", "line\nbreak"} {
		if id, ok := FromEnv(v); ok {
			t.Errorf("FromEnv(%q) = %+v, want no override", v, id)
		}
	}
	long := strings.Repeat("é", 100)
	if id, ok := FromEnv(long); !ok || len([]rune(id.Label)) != 64 {
		t.Errorf("FromEnv(100 runes) label has %d runes, want 64", len([]rune(id.Label)))
	}
}

func TestResolvePrefersTheOverride(t *testing.T) {
	t.Setenv(EnvVar, "builder")
	if got := Resolve(); got.ID != "name:builder" || got.Label != "builder" {
		t.Errorf("Resolve with %s=builder = %+v", EnvVar, got)
	}
}
