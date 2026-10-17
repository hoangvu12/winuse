package tasksuite

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// promptPreamble is the fixed framing sent before every task's body,
// identical for every task and every run — the method (borrowed from
// Cua Driver Bench) pins the Harness, the model and the prompt; only the
// task body and the run's scratch paths vary.
const promptPreamble = `You are completing a desktop task on this Windows machine. A Check
script will inspect the end result, so the task only counts when the
apps really end up in the required state.

- The winuse skill is installed in this project (.claude/skills/ and
  .agents/skills/): load it and follow it to observe and operate the
  apps' windows.
- Do the task through the apps' UIs. The shell may start the apps the
  task needs (the commands it gives you, or an app's own launch
  command); everything else — creating, editing, moving, renaming or
  deleting the task's files — must happen through the apps, never
  through the shell or code you write.
- The machine is yours while you work: nobody else is using the screen.
  If you cannot finish after reasonable retries, stop and report exactly
  what you tried and what failed.
- When the task is done, close the apps and windows you opened (their
  saved files must stay where the task put them), then reply with a
  short summary of the steps you took.

Task:
`

// RenderPrompt renders the full prompt for one task: the fixed preamble
// plus the task body with the run's paths templated in. The returned
// prompt is what the Harness receives on stdin.
func RenderPrompt(t Task, vars PromptVars) (string, error) {
	tmpl, err := template.New("task-" + t.ID).Option("missingkey=error").Parse(t.Body)
	if err != nil {
		return "", fmt.Errorf("task %s: prompt body is not a valid template: %w", t.ID, err)
	}
	var body bytes.Buffer
	if err := tmpl.Execute(&body, vars); err != nil {
		return "", fmt.Errorf("task %s: rendering the prompt: %w", t.ID, err)
	}
	rendered := body.String()
	if strings.Contains(rendered, "{{") {
		return "", fmt.Errorf("task %s: the rendered prompt still contains {{ — the body uses an unknown template variable", t.ID)
	}
	return promptPreamble + rendered, nil
}
