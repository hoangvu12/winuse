package tasksuite

import (
	"strings"
	"testing"
)

func TestRenderPromptIncludesPreambleAndVars(t *testing.T) {
	task := Task{ID: "t", Title: "T", Category: "ordinary", Body: `Save the file in "{{.ScratchDir}}" and open {{.BaseURL}}.`}
	vars := PromptVars{ScratchDir: `C:\tmp\scratch dir`, BaseURL: "http://127.0.0.1:41234"}
	prompt, err := RenderPrompt(task, vars)
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	for _, want := range []string{
		"You are completing a desktop task",
		"winuse skill",
		"everything else — creating, editing, moving, renaming or",
		`C:\tmp\scratch dir`,
		"http://127.0.0.1:41234",
		"Task:",
		`Save the file in "C:\tmp\scratch dir" and open http://127.0.0.1:41234.`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestRenderPromptSamePreambleEveryTask(t *testing.T) {
	// The method pins the prompt: the preamble is byte-identical for
	// every task; only the body differs.
	a, err := RenderPrompt(Task{ID: "a", Body: "A"}, PromptVars{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderPrompt(Task{ID: "b", Body: "B"}, PromptVars{})
	if err != nil {
		t.Fatal(err)
	}
	if a[:len(a)-len("A")] != b[:len(b)-len("B")] {
		t.Error("the prompt preamble differs between tasks")
	}
}

func TestRenderPromptRejectsUnknownTemplateVariable(t *testing.T) {
	task := Task{ID: "t", Body: "Use {{.SurpriseDir}}."}
	if _, err := RenderPrompt(task, PromptVars{}); err == nil {
		t.Error("an unknown template variable must fail rendering")
	}
}
