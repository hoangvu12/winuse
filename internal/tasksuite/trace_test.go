package tasksuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandKind(t *testing.T) {
	cases := map[string]string{
		"get-window-state":         KindObserve,
		"list-windows":             KindObserve,
		"find":                     KindObserve,
		"screenshot":               KindObserve,
		"tab-list":                 KindObserve,
		"clipboard-get":            KindObserve,
		"click":                    KindAct,
		"batch":                    KindAct,
		"type-text":                KindAct,
		"set-value":                KindAct,
		"perform-secondary-action": KindAct,
		"launch-app":               KindAct,
		"close-window":             KindAct,
		"navigate":                 KindAct,
		"eval":                     KindAct,
		"tab-new":                  KindAct,
		"wait":                     KindWait,
		"status":                   KindOther,
		"skill":                    KindOther,
		"browser-connect":          KindOther,
	}
	for cmd, want := range cases {
		if got := CommandKind(cmd); got != want {
			t.Errorf("CommandKind(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestParseTrace(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sum, err := ParseTrace(f)
	if err != nil {
		t.Fatalf("ParseTrace: %v", err)
	}
	if sum.Commands != 12 || sum.Skipped != 2 {
		t.Errorf("commands/skipped = %d/%d, want 12/2 (a junk line and a torn last line)", sum.Commands, sum.Skipped)
	}
	want := map[string]KindTotal{
		KindObserve: {Commands: 4, MS: 180 + 650 + 610 + 300},
		KindAct:     {Commands: 6, MS: 1400 + 120 + 2600 + 850 + 700 + 400},
		KindWait:    {Commands: 1, MS: 900},
		KindOther:   {Commands: 1, MS: 40},
	}
	for k, w := range want {
		if got := sum.Kind(k); got != w {
			t.Errorf("kind %s = %+v, want %+v", k, got, w)
		}
	}
	if sum.MS != 8750 {
		t.Errorf("total ms = %d, want 8750", sum.MS)
	}
	if sum.Actions != 8 {
		t.Errorf("actions = %d, want 8 (the Batch counts its 3 steps)", sum.Actions)
	}
	if sum.Failed != 1 || sum.Codes["STALE_REF"] != 1 {
		t.Errorf("failed = %d, codes = %v", sum.Failed, sum.Codes)
	}
	if sum.Headed != 1 {
		t.Errorf("headed = %d, want 1", sum.Headed)
	}
}

func TestReadTraceFilesRolledOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	// winuse renamed the full trace to trace.1.jsonl, then trace.2.jsonl.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("trace.1.jsonl", `{"command":"get-window-state","ok":true,"duration_ms":500}`+"\n")
	write("trace.2.jsonl", `{"command":"click","ok":true,"routes":["Headless"],"duration_ms":900}`+"\n")
	write("trace.jsonl", `{"command":"wait","ok":true,"duration_ms":300}`+"\n")
	sum, ok, err := ReadTraceFiles(path)
	if err != nil || !ok {
		t.Fatalf("ReadTraceFiles: ok=%v err=%v", ok, err)
	}
	if sum.Commands != 3 || sum.MS != 1700 {
		t.Errorf("summed %d commands, %d ms; want 3, 1700", sum.Commands, sum.MS)
	}
	if sum.Kind(KindObserve).MS != 500 || sum.Kind(KindAct).MS != 900 || sum.Kind(KindWait).MS != 300 {
		t.Errorf("kinds = %+v", sum.Kinds)
	}
}

func TestReadTraceFilesMissing(t *testing.T) {
	sum, ok, err := ReadTraceFiles(filepath.Join(t.TempDir(), "trace.jsonl"))
	if err != nil || ok || sum.Commands != 0 {
		t.Errorf("a missing trace = %+v ok=%v err=%v; want an empty summary, not found", sum, ok, err)
	}
}

func TestTaskEnvLeavesTracingToTheHarness(t *testing.T) {
	// The user's own WINUSE_TRACE never reaches the task's setup and
	// Check scripts; the runner sets the Harness's own.
	t.Setenv("WINUSE_TRACE", `C:\mine\trace.jsonl`)
	rs := &runState{}
	env := rs.taskEnv(&armState{binDir: `C:\run\bin`}, `C:\run\scratch`, "")
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "WINUSE_TRACE=") {
			t.Errorf("task env keeps %q", e)
		}
	}
	found := false
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			found = true
			if !strings.HasPrefix(e[len("PATH="):], `C:\run\bin;`) {
				t.Errorf("PATH does not start with the arm's bin: %q", e)
			}
		}
	}
	if !found {
		t.Error("no PATH in the task env")
	}
}
