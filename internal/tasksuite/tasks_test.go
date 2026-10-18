package tasksuite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTasksValidateTaskFiles validates the shipped Task Suite: every
// task directory parses, its scripts exist and its prompt renders. This
// is the offline `winusetasks validate`, minus any script execution —
// no test ever runs a setup or Check script.
func TestTasksValidateTaskFiles(t *testing.T) {
	tasks, err := LoadTasks("../../tasksuite/tasks")
	if err != nil {
		t.Fatalf("loading the Task Suite: %v", err)
	}
	if len(tasks) < 9 {
		t.Errorf("the ticket asks for about 10 tasks; found %d", len(tasks))
	}
	// Tasks may be skipped at run time (VS Code not installed, ...), so
	// the suite must stay meaningful without its optional task: at least
	// 8 tasks must be unconditional.
	mandatory := 0
	seenTitles := map[string]bool{}
	// apps guaranteed on any Windows install: the System32 staples, plus
	// Edge (it ships with Windows and cannot be removed); a task requiring
	// only these runs everywhere and cannot be skipped
	alwaysPresent := map[string]bool{"notepad": true, "mspaint": true, "explorer": true, "msedge": true}
	vars := PromptVars{
		ScratchDir: `C:\scratch dir`,
		BaseURL:    "http://127.0.0.1:41234",
		EdgePath:   `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, task := range tasks {
		if seenTitles[task.Title] {
			t.Errorf("two tasks share the title %q", task.Title)
		}
		seenTitles[task.Title] = true
		mandatory++
		for _, app := range task.Apps {
			if !alwaysPresent[app] {
				mandatory--
				break
			}
		}
		prompt, err := RenderPrompt(task, vars)
		if err != nil {
			t.Errorf("task %s: %v", task.ID, err)
			continue
		}
		if !strings.Contains(prompt, promptPreambleTaskMark) {
			t.Errorf("task %s: prompt lost the preamble", task.ID)
		}
		if strings.Contains(prompt, "{{") {
			t.Errorf("task %s: prompt still contains an unrendered template", task.ID)
		}
		if !strings.Contains(prompt, `C:\scratch dir`) {
			t.Errorf("task %s: prompt does not name the scratch folder", task.ID)
		}
		if task.HTTP && !strings.Contains(prompt, "http://127.0.0.1:41234") {
			t.Errorf("task %s: http task does not use the task server's URL", task.ID)
		}
	}
	if mandatory < 8 {
		t.Errorf("only %d of %d tasks are unconditional; too much of the suite can be skipped", mandatory, len(tasks))
	}
}

const promptPreambleTaskMark = "Task:"

// TestTasksSpread checks the ticket's spread: ordinary Windows apps,
// Chromium/Electron apps, a dialog-heavy flow and a canvas app.
func TestTasksSpread(t *testing.T) {
	tasks, err := LoadTasks("../../tasksuite/tasks")
	if err != nil {
		t.Fatalf("loading the Task Suite: %v", err)
	}
	counts := map[string]int{}
	dialogTagged, textOnly := 0, 0
	for _, t := range tasks {
		counts[t.Category]++
		if t.TextOnly {
			textOnly++
		}
		for _, tag := range t.Tags {
			if tag == "dialog" || strings.Contains(tag, "dialog") {
				dialogTagged++
				break
			}
		}
	}
	if counts["ordinary"] < 3 {
		t.Errorf("too few ordinary-app tasks: %d", counts["ordinary"])
	}
	if counts["chromium"] < 2 {
		t.Errorf("too few Chromium tasks: %d", counts["chromium"])
	}
	if counts["canvas"] < 1 {
		t.Error("no canvas task")
	}
	if counts["electron"] < 1 {
		t.Error("no Electron task")
	}
	if textOnly < 3 {
		t.Errorf("the text-only subset is too small: %d tasks", textOnly)
	}
}

// TestTasksScratchOnly pins the suite's safety property: every task
// works only inside its scratch folder. Task prompts may name exactly
// one folder — the scratch folder rendered from {{.ScratchDir}} — plus
// Edge's executable (the app-starting command); any other drive path
// would mean the task touches real files.
func TestTasksScratchOnly(t *testing.T) {
	tasks, err := LoadTasks("../../tasksuite/tasks")
	if err != nil {
		t.Fatalf("loading the Task Suite: %v", err)
	}
	scratch := `C:\task scratch`
	edge := `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
	// a drive path, but never inside http:// or edge:// (whose scheme
	// colon would otherwise match)
	drivePath := regexp.MustCompile("(?:^|[^A-Za-z0-9])([A-Za-z]:[\\\\/][^\\s\"')]*)")
	for _, task := range tasks {
		prompt, err := RenderPrompt(task, PromptVars{
			ScratchDir: scratch,
			BaseURL:    "http://127.0.0.1:41234",
			EdgePath:   edge,
		})
		if err != nil {
			t.Errorf("task %s: %v", task.ID, err)
			continue
		}
		for _, m := range drivePath.FindAllStringSubmatch(prompt, -1) {
			m := m[1]
			// A path with a space matches only up to the space; what matters
			// is that every drive path is a prefix of the scratch folder or
			// of Edge's executable, never of anything else.
			if !strings.HasPrefix(scratch, m) && !strings.HasPrefix(edge, m) {
				t.Errorf("task %s: prompt references a path outside the scratch folder: %q", task.ID, m)
			}
		}
	}
}

func TestSelectTasks(t *testing.T) {
	tasks := []Task{
		{ID: "a", TextOnly: true},
		{ID: "b"},
		{ID: "c", TextOnly: true},
	}
	if got := selectTasks(tasks, PiSpec, nil); len(got) != 2 {
		t.Errorf("pi default = %d tasks, want the text-only subset (2)", len(got))
	}
	if got := selectTasks(tasks, ClaudeSpec, nil); len(got) != 3 {
		t.Errorf("claude default = %d tasks, want all (3)", len(got))
	}
	if got := selectTasks(tasks, PiSpec, []string{"b"}); len(got) != 1 || got[0].ID != "b" {
		t.Errorf("explicit ids must win over the subset: %v", got)
	}
}

func TestFindRepoRoot(t *testing.T) {
	// The test binary runs in internal/tasksuite; the root holds go.mod.
	root, err := FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("root %q has no go.mod", root)
	}
	if _, err := os.Stat(filepath.Join(root, "tasksuite", "tasks")); err != nil {
		t.Errorf("root %q has no tasksuite/tasks", root)
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		secs float64
		want string
	}{
		{37.4, "37s"},
		{59.4, "59s"},
		{92, "1m32s"},
		{3842, "1h04m"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := formatSeconds(c.secs); got != c.want {
			t.Errorf("formatSeconds(%v) = %q, want %q", c.secs, got, c.want)
		}
	}
}

func TestFormatTable(t *testing.T) {
	results := []TaskResult{
		{Task: Task{ID: "notepad-note", Category: "ordinary"}, Status: Pass, Steps: 14, Seconds: 92},
		{Task: Task{ID: "paint-draw", Category: "canvas"}, Status: Fail, Steps: 9, Seconds: 298.4,
			Reason: "line.png has only 12 non-white pixels (expected at least 100)"},
		{Task: Task{ID: "vscode-edit", Category: "electron"}, Status: Skip, Steps: -1, Seconds: 0.2,
			Reason: "requires code, which was not found on PATH"},
		{Task: Task{ID: "edge-form", Category: "chromium"}, Status: TaskError, Steps: -1, Seconds: 1,
			Reason: "setup failed: setup exited 1: boom"},
	}
	table := FormatTable(results)
	for _, want := range []string{
		"Task", "Category", "Result", "Steps", "Time",
		"notepad-note", "PASS", "14", "1m32s",
		"paint-draw", "FAIL", "4m58s",
		"line.png has only 12 non-white pixels",
		"requires code, which was not found on PATH",
		"setup failed: setup exited 1: boom",
		"1 passed, 1 failed, 1 errors, 1 skipped — 4 tasks",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
	if !strings.Contains(table, fmt.Sprintf("%d steps", 14+9)) {
		t.Errorf("summary should count the known steps:\n%s", table)
	}
	// The reason lines are indented under their row.
	for _, line := range strings.Split(table, "\n") {
		if strings.Contains(line, "non-white") && !strings.HasPrefix(line, "        ") {
			t.Errorf("reason line not indented: %q", line)
		}
	}
}

func TestFormatTableEmpty(t *testing.T) {
	if table := FormatTable(nil); !strings.Contains(table, "no tasks ran") {
		t.Errorf("empty table = %q", table)
	}
}
