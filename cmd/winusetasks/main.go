// Command winusetasks runs winuse's Task Suite: the fixed set of real
// desktop tasks whose unattended, Harness-driven runs measure whether
// winuse is as good as Codex Computer Use. Maintainer tooling, built
// from this repository — not part of the shipped winuse binary.
//
// A run takes over the screen (the driven agents click, type, activate
// windows and take screenshots on the live desktop), so `run` refuses to
// start without --i-am-the-user: the Task Suite is started only by the
// user, from their own terminal. See tasksuite/README.md.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/tasksuite"
)

const usage = `usage: winusetasks <command> [options]

commands:
  list                       list the Task Suite's tasks (no desktop access)
  validate                   parse every task and report problems (no desktop access)
  run [options] [task ids]   run tasks unattended through a Harness
                             (takes over the screen; user-started only)

run options:
  --harness <claude|pi>      claude (default): every task, Opus 5.5;
                             pi: the text-only subset, glm-5.3
  --time-limit <duration>    per-task wall-clock limit (default: 15m claude, 12m pi)
  --max-turns <n>            claude only (default 40)
  --i-am-the-user            the explicit user start: you are the human at the
                             machine, starting the run yourself. Agents never
                             pass this. See tasksuite/README.md first.

examples:
  winusetasks list
  winusetasks run --i-am-the-user
  winusetasks run --i-am-the-user --harness pi
  winusetasks run --i-am-the-user paint-draw
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "list":
		return listCmd()
	case "validate":
		return validateCmd()
	case "run":
		return runCmd(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "winusetasks: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// tasksOf loads the suite for the safe (no-desktop) commands.
func tasksOf() ([]tasksuite.Task, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := tasksuite.FindRepoRoot(wd)
	if err != nil {
		return nil, err
	}
	return tasksuite.LoadTasks(tasksuite.TasksDir(root))
}

// listCmd lists the tasks (safe: it reads task files only).
func listCmd() int {
	tasks, err := tasksOf()
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	fmt.Printf("%-22s %-10s %-5s %s\n", "task", "category", "text", "title")
	for _, t := range tasks {
		text := "no"
		if t.TextOnly {
			text = "yes"
		}
		fmt.Printf("%-22s %-10s %-5s %s\n", t.ID, t.Category, text, t.Title)
	}
	fmt.Printf("\n%d tasks (%d text-only: run with --harness pi)\n", len(tasks), countText(tasks))
	return 0
}

// validateCmd parses every task and renders every prompt with sample
// paths (safe: no script is executed).
func validateCmd() int {
	tasks, err := tasksOf()
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	vars := tasksuite.PromptVars{
		ScratchDir: `C:\winuse-task-scratch`,
		BaseURL:    "http://127.0.0.1:41234",
		EdgePath:   `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	}
	ok := true
	for _, t := range tasks {
		if _, err := tasksuite.RenderPrompt(t, vars); err != nil {
			fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
			ok = false
			continue
		}
		fmt.Printf("ok  %s (%s)\n", t.ID, t.Title)
	}
	if !ok {
		return 1
	}
	fmt.Printf("\n%d tasks valid\n", len(tasks))
	return 0
}

// runCmd is the real run. It refuses to start unless the user explicitly
// launched it.
func runCmd(args []string) int {
	opts := tasksuite.RunOptions{Harness: "claude"}
	iAmTheUser := false
	var ids []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		var value string
		hasValue := false
		if eq := strings.IndexByte(a, '='); eq >= 0 && strings.HasPrefix(a, "--") {
			value, hasValue, a = a[eq+1:], true, a[:eq]
		}
		need := func(what string) (string, bool) {
			if hasValue {
				return value, true
			}
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		switch a {
		case "--i-am-the-user":
			iAmTheUser = true
		case "--harness":
			v, ok := need("harness")
			if !ok {
				fmt.Fprintf(os.Stderr, "winusetasks: --harness needs a value (claude or pi)\n\n%s", usage)
				return 2
			}
			opts.Harness = v
		case "--time-limit":
			v, ok := need("time limit")
			if !ok {
				fmt.Fprintf(os.Stderr, "winusetasks: --time-limit needs a duration value (e.g. 20m)\n\n%s", usage)
				return 2
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				fmt.Fprintf(os.Stderr, "winusetasks: --time-limit %q is not a positive duration\n", v)
				return 2
			}
			opts.TimeLimit = d
		case "--max-turns":
			v, ok := need("max turns")
			if !ok {
				fmt.Fprintf(os.Stderr, "winusetasks: --max-turns needs a number\n\n%s", usage)
				return 2
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				fmt.Fprintf(os.Stderr, "winusetasks: --max-turns %q is not a positive number\n", v)
				return 2
			}
			opts.MaxTurns = n
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "winusetasks: unknown option %q\n\n%s", a, usage)
				return 2
			}
			ids = append(ids, a)
		}
	}
	if err := tasksuite.CheckUserStarted(iAmTheUser); err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	opts.TaskIDs = ids
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	root, err := tasksuite.FindRepoRoot(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	opts.RepoRoot = root
	if err := tasksuite.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "\nwinusetasks: %v\n", err)
		return 1
	}
	return 0
}

func countText(tasks []tasksuite.Task) int {
	n := 0
	for _, t := range tasks {
		if t.TextOnly {
			n++
		}
	}
	return n
}
