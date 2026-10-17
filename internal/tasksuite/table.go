package tasksuite

import (
	"fmt"
	"strings"
)

// Status is one task's outcome.
type Status string

const (
	// Pass: the Check script found the required end state.
	Pass Status = "PASS"
	// Fail: the task ran but the end state is wrong (or the time limit
	// ran out).
	Fail Status = "FAIL"
	// TaskError: the run could not be measured (setup failed, the
	// Harness crashed or produced no result).
	TaskError Status = "ERROR"
	// Skip: a requirement was unmet (an app missing).
	Skip Status = "SKIP"
)

// TaskResult is one task's measured outcome: what the ticket's table
// needs — pass/fail, steps and time — plus why, for anything not passed.
type TaskResult struct {
	Task       Task
	Status     Status
	Steps      int     // -1: unknown (a Harness crash, a claude timeout)
	Seconds    float64 // wall clock of the measured part (setup→check)
	Reason     string  // the Check's failure line, or the timeout/error note
	ScratchDir string
}

// FormatTable renders the pass/fail table with steps and time, plus the
// failure reasons under their rows and a summary line. Plain text, no
// box drawing: it must survive any terminal and a redirected file.
func FormatTable(results []TaskResult) string {
	if len(results) == 0 {
		return "no tasks ran\n"
	}
	headers := []string{"Task", "Category", "Result", "Steps", "Time"}
	rows := make([][]string, len(results))
	for i, r := range results {
		steps := "-"
		if r.Steps >= 0 {
			steps = fmt.Sprintf("%d", r.Steps)
		}
		rows[i] = []string{r.Task.ID, r.Task.Category, string(r.Status), steps, formatSeconds(r.Seconds)}
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
		}
		b.WriteByte('\n')
	}
	writeRow(headers)
	for i, row := range rows {
		writeRow(row)
		if r := results[i]; r.Status != Pass && r.Reason != "" {
			for _, line := range strings.Split(strings.TrimRight(r.Reason, "\n"), "\n") {
				if line == "" {
					continue
				}
				fmt.Fprintf(&b, "        %s\n", line)
			}
		}
	}

	var pass, fail, errs, skip int
	var seconds float64
	var steps int
	knownSteps := 0
	for _, r := range results {
		seconds += r.Seconds
		if r.Steps >= 0 {
			steps += r.Steps
			knownSteps++
		}
		switch r.Status {
		case Pass:
			pass++
		case Fail:
			fail++
		case TaskError:
			errs++
		case Skip:
			skip++
		}
	}
	fmt.Fprintf(&b, "\n%d passed, %d failed, %d errors, %d skipped — %d tasks, %s total",
		pass, fail, errs, skip, len(results), formatSeconds(seconds))
	if knownSteps > 0 {
		fmt.Fprintf(&b, ", %d steps", steps)
	}
	b.WriteByte('\n')
	return b.String()
}

// formatSeconds renders a duration the way a person reads one: 37s,
// 3m12s, 1h02m.
func formatSeconds(s float64) string {
	total := int(s + 0.5)
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	if total < 60*60 {
		return fmt.Sprintf("%dm%02ds", total/60, total%60)
	}
	return fmt.Sprintf("%dh%02dm", total/3600, (total%3600)/60)
}
