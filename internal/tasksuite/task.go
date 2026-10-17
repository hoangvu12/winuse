// Package tasksuite runs winuse's Task Suite: the fixed set of real
// desktop tasks (tasksuite/tasks/) whose unattended, Harness-driven runs
// measure whether winuse is as good as Codex Computer Use. The runner is
// maintainer tooling, not shipped with winuse.exe; it is started only by
// the user (see guard.go) because a run takes over the screen.
//
// This package keeps the Task Suite's pure logic — task-file parsing,
// prompt rendering, the opt-in refusal, harness argv and output parsing,
// the result table, the task server and the process-cleanup diff —
// offline-testable, per the spec's testing decisions; run.go wires them
// to real processes and is exercised only by user-started runs.
package tasksuite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TasksDirName is the directory (under the repository root) holding the
// Task Suite: tasks live as one directory per task.
const TasksDirName = "tasksuite"

// Categories the Task Suite spreads its tasks across (the ticket:
// ordinary Windows apps, Chromium/Electron apps, a dialog-heavy flow and
// a canvas app).
var Categories = map[string]bool{
	"ordinary": true, // Notepad, File Explorer, ...
	"chromium": true, // Edge (isolated profile) and other Chromium apps
	"electron": true, // VS Code and other Electron apps
	"canvas":   true, // apps whose tree is sparse (Paint)
}

// Task is one Task Suite task: a fixed prompt (task.md body), a setup
// script that prepares the scratch folder, and a Check script that
// inspects the end result. A task never touches anything outside its
// scratch folder.
type Task struct {
	ID        string        // the task directory's name; the fixed run order key
	Title     string        // one line, shown in the table and `list`
	Category  string        // one of Categories
	Tags      []string      // free-form (dialog, drag, context-menu, ...)
	Apps      []string      // apps that must exist, or the task is skipped
	TextOnly  bool          // eligible for the text-only (Pi) subset
	HTTP      bool          // needs the task server; {{.BaseURL}} resolves
	TimeLimit time.Duration // 0: the harness's default per-task limit
	Body      string        // the prompt body, after the front-matter
	Dir       string        // absolute path of the task directory
}

// SetupScript is the absolute path of the script that prepares the
// scratch folder before the Harness runs. It runs with the scratch
// folder as its working directory.
func (t Task) SetupScript() string { return filepath.Join(t.Dir, "setup.ps1") }

// CheckScript is the absolute path of the Check script: it inspects the
// end result (files, settings, app state in the scratch folder) and
// exits 0 only when the task passed. It never asks a model.
func (t Task) CheckScript() string { return filepath.Join(t.Dir, "check.ps1") }

// TaskFile is the per-directory file holding the prompt and its
// front-matter.
const TaskFile = "task.md"

// LoadTasks loads every task under dir (one directory per task) sorted
// by ID, so every run works the tasks in the same fixed order.
func LoadTasks(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading tasks directory %s: %w", dir, err)
	}
	var tasks []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := LoadTask(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found under %s", dir)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// LoadTask loads one task directory.
func LoadTask(dir string) (Task, error) {
	id := filepath.Base(dir)
	raw, err := os.ReadFile(filepath.Join(dir, TaskFile))
	if err != nil {
		return Task{}, fmt.Errorf("task %s: reading %s: %w", id, TaskFile, err)
	}
	t, err := parseTask(id, string(raw))
	if err != nil {
		return Task{}, fmt.Errorf("task %s: %w", id, err)
	}
	t.Dir = dir
	for _, script := range []struct{ name, path string }{
		{"setup.ps1", t.SetupScript()},
		{"check.ps1", t.CheckScript()},
	} {
		if _, err := os.Stat(script.path); err != nil {
			return Task{}, fmt.Errorf("task %s: missing %s (every task needs a setup and a Check script)", id, script.name)
		}
	}
	return t, nil
}

// parseTask parses a task.md: a `---` front-matter block of `key: value`
// lines, then the prompt body. The grammar is deliberately tiny — the
// skill's front-matter style — so no YAML dependency is needed.
func parseTask(id, raw string) (Task, error) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return Task{}, fmt.Errorf("%s must start with a `---` front-matter block", TaskFile)
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	var front, body string
	if end < 0 {
		// tolerate a final `---` with no trailing newline
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			rest = strings.TrimSuffix(rest, "\n---")
		} else {
			return Task{}, fmt.Errorf("%s front-matter is not closed with a `---` line", TaskFile)
		}
	}
	front, body = rest[:end], strings.TrimPrefix(rest[end+len("\n---\n"):], "\n")

	t := Task{ID: id}
	seen := map[string]bool{}
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Task{}, fmt.Errorf("front-matter line %q is not `key: value`", line)
		}
		key = strings.TrimSpace(key)
		if seen[key] {
			return Task{}, fmt.Errorf("front-matter key %q appears twice", key)
		}
		seen[key] = true
		value = strings.TrimSpace(value)
		switch key {
		case "title":
			if value == "" {
				return Task{}, fmt.Errorf("title is empty")
			}
			t.Title = value
		case "category":
			if !Categories[value] {
				return Task{}, fmt.Errorf("category %q is not one of: %s", value, categoryList())
			}
			t.Category = value
		case "tags":
			t.Tags = splitList(value)
		case "apps":
			t.Apps = splitList(value)
		case "text":
			b, err := parseYesNo(value)
			if err != nil {
				return Task{}, fmt.Errorf("text: %v", err)
			}
			t.TextOnly = b
		case "http":
			b, err := parseYesNo(value)
			if err != nil {
				return Task{}, fmt.Errorf("http: %v", err)
			}
			t.HTTP = b
		case "time-limit":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				return Task{}, fmt.Errorf("time-limit %q is not a positive Go duration (e.g. 15m)", value)
			}
			t.TimeLimit = d
		default:
			return Task{}, fmt.Errorf("unknown front-matter key %q", key)
		}
	}
	if t.Title == "" {
		return Task{}, fmt.Errorf("title is required")
	}
	if t.Category == "" {
		return Task{}, fmt.Errorf("category is required (one of: %s)", categoryList())
	}
	if strings.TrimSpace(body) == "" {
		return Task{}, fmt.Errorf("the prompt body is empty")
	}
	// A body that names a variable it cannot have fails at load time,
	// not mid-run: BaseURL exists only for server tasks, EdgePath only
	// when the task requires Edge.
	for _, miss := range []struct {
		tmpl, what string
		ok         func() bool
	}{
		{"{{.BaseURL}}", "http: yes", func() bool { return t.HTTP }},
		{"{{.EdgePath}}", "apps: msedge", func() bool {
			for _, a := range t.Apps {
				if strings.EqualFold(a, "msedge") {
					return true
				}
			}
			return false
		}},
	} {
		if strings.Contains(body, miss.tmpl) && !miss.ok() {
			return Task{}, fmt.Errorf("the body uses %s but the front-matter lacks %s", miss.tmpl, miss.what)
		}
	}
	t.Body = body
	return t, nil
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseYesNo(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "yes", "true":
		return true, nil
	case "no", "false", "":
		return false, nil
	default:
		return false, fmt.Errorf("expected yes or no, got %q", v)
	}
}

func categoryList() string {
	var names []string
	for c := range Categories {
		names = append(names, c)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// PromptVars are the values templated into a task's prompt body. Scratch
// is the task's scratch folder; BaseURL is the task server's root (only
// for http tasks); EdgePath is Microsoft Edge's executable (only
// resolved when the task requires msedge).
type PromptVars struct {
	ScratchDir string
	BaseURL    string
	EdgePath   string
}
