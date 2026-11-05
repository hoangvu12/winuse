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
  report <results.json>      print a run's speed summary (and A/B comparison)
                             again from its results file (no desktop access)

run options:
  --harness <claude|pi>      claude (default): every task, Opus 5.5;
                             pi: the text-only subset, glm-5.3
  --time-limit <duration>    per-task wall-clock limit (default: 15m claude, 12m pi)
  --max-turns <n>            claude only (default 40)
  --repeats <n>              runs per task (and per arm); the summary gives
                             medians and ranges (default 1)
  --winuse <exe|tree>        the winuse the run measures: a winuse.exe (copied
                             into the run directory) or a source tree (built
                             there); default: built from this tree
  --skill <file|dir|tree>    the SKILL.md the agent loads; default: this tree's
  --label <name>             arm A's name in an A/B run (default A)
  --b-winuse <exe|tree>      an A/B run: arm B's winuse (default: arm A's)
  --b-skill <file|dir|tree>  an A/B run: arm B's skill (default: arm A's)
  --b-label <name>           arm B's name (default B)
                             Any --b-* option makes the run A/B: every task runs
                             in both arms, interleaved ABBA, and a comparison
                             table follows the summary.
  --i-am-the-user            the explicit user start: you are the human at the
                             machine, starting the run yourself. Agents never
                             pass this. See tasksuite/README.md first.

examples:
  winusetasks list
  winusetasks run --i-am-the-user
  winusetasks run --i-am-the-user --harness pi
  winusetasks run --i-am-the-user paint-draw
  winusetasks run --i-am-the-user --repeats 3
  winusetasks run --i-am-the-user --repeats 3 --b-winuse ..\winuse-wt\speed-waits --b-label waits
  winusetasks report <run directory>\results.json
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
	case "report":
		return reportCmd(args[1:])
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
	armFlags := map[string]string{}
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
		case "--repeats":
			v, ok := need("repeats")
			if !ok {
				fmt.Fprintf(os.Stderr, "winusetasks: --repeats needs a number\n\n%s", usage)
				return 2
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > 20 {
				fmt.Fprintf(os.Stderr, "winusetasks: --repeats %q is not a number from 1 to 20\n", v)
				return 2
			}
			opts.Repeats = n
		case "--winuse", "--skill", "--label", "--b-winuse", "--b-skill", "--b-label":
			v, ok := need(a)
			if !ok || strings.TrimSpace(v) == "" {
				fmt.Fprintf(os.Stderr, "winusetasks: %s needs a value\n\n%s", a, usage)
				return 2
			}
			armFlags[a] = v
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "winusetasks: unknown option %q\n\n%s", a, usage)
				return 2
			}
			ids = append(ids, a)
		}
	}
	opts.Arms = armsOf(armFlags)
	if err := tasksuite.CheckArms(opts.Arms); err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 2
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

// armsOf turns the arm options into the run's arms: none for a plain run
// from this tree, one when --winuse, --skill or --label is given, two
// when any --b-* option is (arm B inherits what it does not override).
func armsOf(flags map[string]string) []tasksuite.ArmOptions {
	a := tasksuite.ArmOptions{Label: flags["--label"], Winuse: flags["--winuse"], Skill: flags["--skill"]}
	_, bw := flags["--b-winuse"]
	_, bs := flags["--b-skill"]
	_, bl := flags["--b-label"]
	if !bw && !bs && !bl {
		if a == (tasksuite.ArmOptions{}) {
			return nil
		}
		return []tasksuite.ArmOptions{a}
	}
	if a.Label == "" {
		a.Label = "A"
	}
	b := a
	b.Label = "B"
	if v, ok := flags["--b-label"]; ok {
		b.Label = v
	}
	if v, ok := flags["--b-winuse"]; ok {
		b.Winuse = v
	}
	if v, ok := flags["--b-skill"]; ok {
		b.Skill = v
	}
	return []tasksuite.ArmOptions{a, b}
}

// reportCmd prints a results file's speed summary again, and the A/B
// comparison when the run had two arms (safe: it reads the file only).
func reportCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "winusetasks: report takes one results.json\n\n%s", usage)
		return 2
	}
	rec, err := tasksuite.LoadRunRecord(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	fmt.Printf("Task Suite run %s — harness %s (%s), %d repeat(s)\n", rec.Started.Local().Format("2006-01-02 15:04"), rec.Harness, rec.Model, rec.Repeats)
	for _, a := range rec.Arms {
		name := a.Label
		if name == "" {
			name = "winuse"
		}
		fmt.Printf("  %s: winuse %s (build %.12s), skill %s (sha256 %.12s)\n", name, a.WinuseSource, a.WinuseBuild, a.SkillSource, a.SkillSHA256)
	}
	fmt.Println()
	fmt.Print(tasksuite.FormatTable(rec.Results))
	fmt.Println()
	fmt.Print(tasksuite.FormatSummary(rec.Results))
	if arms := tasksuite.Arms(rec.Results); len(arms) == 2 {
		fmt.Println()
		fmt.Print(tasksuite.FormatComparison(rec.Results, arms[0], arms[1]))
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
