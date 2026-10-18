package tasksuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTask writes a task directory with the given task.md, setup.ps1
// and check.ps1 (scripts are never executed by any test).
func writeTask(t *testing.T, dir, md string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"setup.ps1", "check.ps1"} {
		if err := os.WriteFile(filepath.Join(dir, script), []byte("# script\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const minimalTask = `---
title: A task
category: ordinary
---

Do the thing in "{{.ScratchDir}}".
`

func TestParseTaskMinimal(t *testing.T) {
	task, err := parseTask("t1", minimalTask)
	if err != nil {
		t.Fatalf("parseTask: %v", err)
	}
	if task.Title != "A task" || task.Category != "ordinary" {
		t.Errorf("title/category = %q/%q", task.Title, task.Category)
	}
	if task.TextOnly || task.HTTP || task.TimeLimit != 0 {
		t.Errorf("defaults wrong: text=%v http=%v limit=%v", task.TextOnly, task.HTTP, task.TimeLimit)
	}
	if !strings.Contains(task.Body, "Do the thing") {
		t.Errorf("body = %q", task.Body)
	}
}

func TestParseTaskAllFields(t *testing.T) {
	md := `---
title: Full task
category: chromium
tags: dialog, settings
apps: msedge, code
text: yes
http: yes
time-limit: 17m
---

Body with {{.BaseURL}} and "{{.EdgePath}}".
`
	task, err := parseTask("t2", md)
	if err != nil {
		t.Fatalf("parseTask: %v", err)
	}
	if !task.TextOnly || !task.HTTP {
		t.Errorf("text/http not parsed")
	}
	if task.TimeLimit != 17*time.Minute {
		t.Errorf("time-limit = %v", task.TimeLimit)
	}
	if strings.Join(task.Tags, ",") != "dialog,settings" {
		t.Errorf("tags = %v", task.Tags)
	}
	if strings.Join(task.Apps, ",") != "msedge,code" {
		t.Errorf("apps = %v", task.Apps)
	}
}

func TestParseTaskErrors(t *testing.T) {
	cases := []struct {
		name, md, want string
	}{
		{"no front-matter", "just a body\n", "front-matter"},
		{"unclosed", "---\ntitle: x\ncategory: ordinary\n", "closed"},
		{"no title", "---\ncategory: ordinary\n---\nbody\n", "title"},
		{"no category", "---\ntitle: x\n---\nbody\n", "category"},
		{"bad category", "---\ntitle: x\ncategory: exotic\n---\nbody\n", "category"},
		{"empty body", "---\ntitle: x\ncategory: ordinary\n---\n\n", "body"},
		{"unknown key", "---\ntitle: x\ncategory: ordinary\nseverity: high\n---\nbody\n", "unknown"},
		{"bad text", "---\ntitle: x\ncategory: ordinary\ntext: maybe\n---\nbody\n", "yes or no"},
		{"bad time", "---\ntitle: x\ncategory: ordinary\ntime-limit: soon\n---\nbody\n", "duration"},
		{"base url without http", "---\ntitle: x\ncategory: ordinary\n---\nGo to {{.BaseURL}}\n", "BaseURL"},
		{"edge path without msedge", "---\ntitle: x\ncategory: ordinary\napps: code\n---\nRun {{.EdgePath}}\n", "EdgePath"},
		{"not key value", "---\ntitle: x\ncategory: ordinary\njusttext\n---\nbody\n", "key: value"},
		{"duplicate key", "---\ntitle: x\ntitle: y\ncategory: ordinary\n---\nbody\n", "twice"},
	}
	for _, c := range cases {
		_, err := parseTask("t", c.md)
		if err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

func TestParseTaskCRLF(t *testing.T) {
	md := strings.ReplaceAll(minimalTask, "\n", "\r\n")
	task, err := parseTask("t3", md)
	if err != nil {
		t.Fatalf("parseTask with CRLF: %v", err)
	}
	if task.Title != "A task" {
		t.Errorf("title = %q", task.Title)
	}
}

func TestLoadTaskScripts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a-task")
	writeTask(t, dir, minimalTask)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if task.ID != "a-task" || task.Dir != dir {
		t.Errorf("id/dir = %q/%q", task.ID, task.Dir)
	}
	if task.SetupScript() != filepath.Join(dir, "setup.ps1") || task.CheckScript() != filepath.Join(dir, "check.ps1") {
		t.Errorf("script paths wrong: %q %q", task.SetupScript(), task.CheckScript())
	}

	// A missing Check script is a task-definition bug.
	os.Remove(filepath.Join(dir, "check.ps1"))
	if _, err := LoadTask(dir); err == nil || !strings.Contains(err.Error(), "check.ps1") {
		t.Errorf("missing check.ps1 not reported: %v", err)
	}
}

func TestLoadTasksFixedOrder(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"c-task", "a-task", "b-task"} {
		writeTask(t, filepath.Join(root, id), minimalTask)
	}
	tasks, err := LoadTasks(root)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	got := []string{}
	for _, t := range tasks {
		got = append(got, t.ID)
	}
	if strings.Join(got, ",") != "a-task,b-task,c-task" {
		t.Errorf("tasks not in fixed id order: %v", got)
	}
}
