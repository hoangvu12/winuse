package tasksuite

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows/registry"
)

// RunOptions configure one Task Suite run.
type RunOptions struct {
	Harness   string        // "claude" (default: the full suite) or "pi" (the text-only subset)
	TaskIDs   []string      // run only these task IDs (all tasks otherwise)
	TimeLimit time.Duration // overrides every task's limit when > 0
	MaxTurns  int           // overrides the claude turn limit when > 0
	RepoRoot  string        // the winuse repository root; found from the cwd when empty
	RunRoot   string        // the run directory; %TEMP%\winuse-tasks-<stamp> when empty
	Out       io.Writer     // progress output; os.Stdout when nil
	Repeats   int           // runs per task and arm (1 when 0)
	// Arms are what the run measures: one arm (a plain run) or two (an
	// A/B run, its arms interleaved per task). None means one arm built
	// from RepoRoot with its SKILL.md.
	Arms []ArmOptions
}

// ArmOptions are one arm of a run: the winuse it exercises and the skill
// its agent loads.
type ArmOptions struct {
	Label  string // names the arm in the tables and its directories ("" in a plain run)
	Winuse string // "": built from RepoRoot; a directory holding go.mod: built from that tree; else a winuse.exe to copy
	Skill  string // "": RepoRoot's skills/winuse/SKILL.md; a SKILL.md, a directory holding one, or a tree
}

// CheckArms validates a run's arms before anything is built: at most two,
// labelled uniquely with names fit for directories (both, in an A/B
// run), and every path they name existing.
func CheckArms(arms []ArmOptions) error {
	if len(arms) > 2 {
		return fmt.Errorf("a run has at most two arms (A and B), not %d", len(arms))
	}
	seen := map[string]bool{}
	for _, a := range arms {
		if len(arms) == 2 || a.Label != "" {
			if !armLabelOK(a.Label) {
				return fmt.Errorf("arm label %q: use 1-32 letters, digits, '-', '_' or '.'", a.Label)
			}
			if seen[strings.ToLower(a.Label)] {
				return fmt.Errorf("arm label %q is used twice", a.Label)
			}
			seen[strings.ToLower(a.Label)] = true
		}
		if a.Winuse != "" {
			if _, err := os.Stat(a.Winuse); err != nil {
				return fmt.Errorf("arm %s: winuse %s: %w", a.Label, a.Winuse, err)
			}
		}
		if a.Skill != "" {
			if _, err := resolveSkillPath(a.Skill); err != nil {
				return fmt.Errorf("arm %s: skill: %w", a.Label, err)
			}
		}
	}
	return nil
}

// armLabelOK is what an arm label may hold: it names directories.
func armLabelOK(label string) bool {
	if label == "" || len(label) > 32 {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// resolveSkillPath finds the SKILL.md a skill option names: the file
// itself, a directory holding SKILL.md, or a tree's
// skills/winuse/SKILL.md.
func resolveSkillPath(p string) (string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return p, nil
	}
	for _, c := range []string{filepath.Join(p, "SKILL.md"), filepath.Join(p, "skills", "winuse", "SKILL.md")} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s holds no SKILL.md (nor skills\\winuse\\SKILL.md)", p)
}

// armState is one arm as the run uses it.
type armState struct {
	ArmRecord
	binDir string
	skill  []byte
}

// Run executes the Task Suite: every (selected) task, unattended, through
// the pinned Harness, each in its own scratch folder, each judged by its
// Check script, then the pass/fail table and the speed summary. With
// repeats, every task runs that many times per arm; with two arms (A/B),
// the arms alternate per task (ABBA), and a comparison table follows.
// It is user-started only — the caller must have passed
// CheckUserStarted (cmd/winusetasks enforces it).
func Run(opts RunOptions) error {
	started := time.Now()
	spec, err := SpecFor(opts.Harness)
	if err != nil {
		return err
	}
	if opts.MaxTurns > 0 {
		spec.MaxTurns = opts.MaxTurns
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	repeats := opts.Repeats
	if repeats <= 0 {
		repeats = 1
	}
	armOpts := opts.Arms
	if len(armOpts) == 0 {
		armOpts = []ArmOptions{{}}
	}
	if err := CheckArms(armOpts); err != nil {
		return err
	}

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoRoot, err = FindRepoRoot(wd)
		if err != nil {
			return err
		}
	}

	tasks, err := LoadTasks(TasksDir(repoRoot))
	if err != nil {
		return err
	}
	// Unknown ids are typos, not empty runs.
	known := map[string]bool{}
	for _, t := range tasks {
		known[t.ID] = true
	}
	for _, id := range opts.TaskIDs {
		if !known[id] {
			return fmt.Errorf("unknown task %q (see `winusetasks list`)", id)
		}
	}
	tasks = selectTasks(tasks, spec, opts.TaskIDs)
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks to run: the %s harness runs %s", spec.Name,
			specSelects(spec))
	}

	runRoot := opts.RunRoot
	if runRoot == "" {
		runRoot = filepath.Join(os.TempDir(), fmt.Sprintf("winuse-tasks-%s", started.Format("20060102-150405")))
	}
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		return fmt.Errorf("creating the run directory: %w", err)
	}

	inv, err := ResolveHarness(spec)
	if err != nil {
		return err
	}

	rs := &runState{
		spec:      spec,
		inv:       inv,
		repoRoot:  repoRoot,
		runRoot:   runRoot,
		timeLimit: opts.TimeLimit,
		split:     repeats > 1 || len(armOpts) > 1,
		out:       out,
	}
	rs.edgePath = resolveEdgePath()
	logf := func(msg string) { fmt.Fprintln(out, msg) }

	fmt.Fprintf(out, "winuse Task Suite %s — harness %s (%s), %d task(s) × %d repeat(s) × %d arm(s)\n",
		version.Version, spec.Name, spec.ModelID, len(tasks), repeats, len(armOpts))
	fmt.Fprintf(out, "Run directory: %s\n", runRoot)

	// Each arm's winuse binary goes into its own bin directory (prepended
	// to the Harness's PATH): built from this source tree unless the arm
	// names another tree or an exe, so a plain run always measures the
	// tree it was started from.
	var arms []*armState
	for _, ao := range armOpts {
		arm, err := prepareArm(repoRoot, runRoot, ao, len(armOpts) > 1)
		if err != nil {
			return err
		}
		arms = append(arms, arm)
		name := arm.Label
		if name == "" {
			name = "winuse"
		}
		fmt.Fprintf(out, "  %s: winuse %s (build %s), skill %s (sha256 %.12s)\n",
			name, arm.WinuseSource, shortBuild(arm.WinuseBuild), arm.SkillSource, arm.SkillSHA256)
	}
	fmt.Fprintln(out)

	// Fail fast, before taking over the screen: the Harness must be
	// logged in and the model must exist.
	if err := preflight(rs, spec); err != nil {
		return fmt.Errorf("harness preflight failed: %w", err)
	}

	var results []TaskResult
	total := len(tasks) * repeats * len(arms)
	n := 0
	for _, t := range tasks {
		// A missing app skips the task in every arm, once.
		if reason := rs.skipReason(t); reason != "" {
			for _, arm := range arms {
				n += repeats
				res := TaskResult{Task: t, Status: Skip, Reason: reason, Steps: -1, Arm: arm.Label, Repeat: 1, Usage: Usage{Turns: -1, CostUSD: -1}}
				results = append(results, res)
				logf(fmt.Sprintf("[%d/%d] %s", n, total, progressLine(res)))
				logf("        " + reason)
			}
			continue
		}
		for rep := 1; rep <= repeats; rep++ {
			// The arms interleave, ABBA, so drift on the machine (an
			// update, a warm cache) hits both.
			order := arms
			if rep%2 == 0 && len(arms) == 2 {
				order = []*armState{arms[1], arms[0]}
			}
			for _, arm := range order {
				n++
				res := rs.runTask(t, arm, rep)
				results = append(results, res)
				logf(fmt.Sprintf("[%d/%d] %s", n, total, progressLine(res)))
				if res.Reason != "" && res.Status != Pass {
					for _, line := range strings.Split(strings.TrimRight(res.Reason, "\n"), "\n") {
						if line != "" {
							logf("        " + line)
						}
					}
				}
			}
		}
	}

	table := FormatTable(results)
	summary := FormatSummary(results)
	if len(arms) == 2 {
		summary += "\n" + FormatComparison(results, arms[0].Label, arms[1].Label)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, table)
	fmt.Fprintln(out)
	fmt.Fprint(out, summary)

	rec := RunRecord{
		Schema:  RecordSchema,
		Started: started.UTC(),
		Harness: spec.Name,
		Model:   spec.ModelID,
		Winuse:  version.Version,
		Repeats: repeats,
		Results: results,
	}
	for _, a := range arms {
		rec.Arms = append(rec.Arms, a.ArmRecord)
	}
	if err := writeRunFiles(runRoot, rec, table, summary); err != nil {
		logf(fmt.Sprintf("writing the run's result files: %v", err))
	}
	fmt.Fprintf(out, "\nResults: %s\n", filepath.Join(runRoot, "results.json"))
	return nil
}

// progressLine is a finished run's line in the progress output.
func progressLine(r TaskResult) string {
	name := r.Task.ID
	if r.Arm != "" || r.Repeat > 1 {
		name += " " + runName(r)
	}
	turns := "-"
	if r.Usage.Turns >= 0 {
		turns = fmt.Sprintf("%d", r.Usage.Turns)
	} else if r.Steps >= 0 {
		turns = fmt.Sprintf("%d", r.Steps)
	}
	line := fmt.Sprintf("%-28s %s  %s turns, %s", name, r.Status, turns, formatSeconds(r.Seconds))
	if r.measured() {
		line += fmt.Sprintf(" (agent %s, winuse %s in %d commands", fmtSecs(r.HarnessSeconds), fmtSecs(float64(r.Trace.MS)/1000), r.Trace.Commands)
		if r.Usage.Known && r.Usage.CostUSD >= 0 {
			line += ", " + fmtCost(r.Usage.CostUSD)
		}
		line += ")"
	}
	return line
}

func shortBuild(id string) string {
	if id == "" {
		return "unknown"
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// prepareArm puts an arm's winuse.exe into its bin directory and reads
// its skill. A tree is built with `go build` into the run directory; an
// exe is copied, so the run's cleanup, which kills the run's own winuse
// processes by their path, never touches an installed winuse.
func prepareArm(repoRoot, runRoot string, ao ArmOptions, named bool) (*armState, error) {
	arm := &armState{ArmRecord: ArmRecord{Label: ao.Label}}
	if named && arm.Label == "" {
		return nil, fmt.Errorf("an A/B run's arms need labels")
	}
	arm.binDir = filepath.Join(runRoot, "bin")
	if named {
		arm.binDir = filepath.Join(runRoot, "bin-"+ao.Label)
	}
	exe := filepath.Join(arm.binDir, "winuse.exe")
	switch src := ao.Winuse; {
	case src == "":
		arm.WinuseSource = "tree"
		if err := buildWinuse(repoRoot, arm.binDir); err != nil {
			return nil, err
		}
	case isTree(src):
		abs, _ := filepath.Abs(src)
		arm.WinuseSource = abs
		if err := buildWinuse(abs, arm.binDir); err != nil {
			return nil, fmt.Errorf("arm %s: %w", ao.Label, err)
		}
	default:
		abs, _ := filepath.Abs(src)
		arm.WinuseSource = abs
		if err := copyFile(abs, exe); err != nil {
			return nil, fmt.Errorf("arm %s: copying %s: %w", ao.Label, abs, err)
		}
	}
	arm.WinuseBuild = version.ExeBuild(exe)

	skillPath := filepath.Join(repoRoot, "skills", "winuse", "SKILL.md")
	arm.SkillSource = "tree"
	if ao.Skill != "" {
		p, err := resolveSkillPath(ao.Skill)
		if err != nil {
			return nil, fmt.Errorf("arm %s: skill: %w", ao.Label, err)
		}
		skillPath, _ = filepath.Abs(p)
		arm.SkillSource = skillPath
	}
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("arm %s: reading the skill: %w", ao.Label, err)
	}
	arm.skill = raw
	sum := sha256.Sum256(raw)
	arm.SkillSHA256 = hex.EncodeToString(sum[:])
	return arm, nil
}

// isTree reports whether a winuse option names a source tree (a
// directory holding go.mod) rather than an exe.
func isTree(p string) bool {
	st, err := os.Stat(filepath.Join(p, "go.mod"))
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// selectTasks narrows the suite: explicit IDs win; the pi harness
// defaults to the text-only subset.
func selectTasks(tasks []Task, spec HarnessSpec, ids []string) []Task {
	if len(ids) == 0 {
		if spec.Name == "pi" {
			var text []Task
			for _, t := range tasks {
				if t.TextOnly {
					text = append(text, t)
				}
			}
			return text
		}
		return tasks
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []Task
	for _, t := range tasks {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// runState carries what every task of one run shares.
type runState struct {
	spec      HarnessSpec
	inv       HarnessInvocation
	repoRoot  string
	runRoot   string
	edgePath  string        // empty: Edge not installed; msedge tasks skip
	timeLimit time.Duration // > 0: overrides every task's limit
	split     bool          // repeats or arms: each run gets <task>\<arm>-r<n>
	out       io.Writer
}

// specSelects describes what a harness runs, for empty-selection errors.
func specSelects(spec HarnessSpec) string {
	if spec.Name == "pi" {
		return "only the text-only subset — no task is marked `text: yes`"
	}
	return "every task"
}

// skipReason is why a task cannot run on this machine ("" when it can):
// an app it requires is missing.
func (rs *runState) skipReason(t Task) string {
	for _, app := range t.Apps {
		if strings.EqualFold(app, "msedge") {
			if rs.edgePath == "" {
				return "requires Microsoft Edge (msedge.exe was not found)"
			}
			continue
		}
		if findOnPath(app) == "" {
			return fmt.Sprintf("requires %s, which was not found on PATH", app)
		}
	}
	return ""
}

// taskDir is one run's directory: <task> in a plain run, or
// <task>\<arm>-r<n> with repeats or arms.
func (rs *runState) taskDir(t Task, arm *armState, rep int) string {
	if !rs.split {
		return filepath.Join(rs.runRoot, t.ID)
	}
	label := arm.Label
	if label == "" {
		label = "run"
	}
	return filepath.Join(rs.runRoot, t.ID, fmt.Sprintf("%s-r%d", label, rep))
}

// TraceFileName is the winuse trace a task run's Harness writes
// (WINUSE_TRACE), in the run's directory: redacted by default, so it is
// safe to keep with the results.
const TraceFileName = "trace.jsonl"

// runTask runs one task end to end: scratch folder, setup, Harness under
// its time limit, Check, cleanup. A task's outcome is always a result —
// only the run's plumbing above can fail hard.
func (rs *runState) runTask(t Task, arm *armState, rep int) TaskResult {
	started := time.Now()
	taskDir := rs.taskDir(t, arm, rep)
	scratch := filepath.Join(taskDir, "scratch")
	res := TaskResult{Task: t, Status: TaskError, Steps: -1, ScratchDir: scratch, Arm: arm.Label, Repeat: rep, Usage: Usage{Turns: -1, CostUSD: -1}}

	if err := os.MkdirAll(scratch, 0o755); err != nil {
		res.Reason = fmt.Sprintf("creating the scratch folder: %v", err)
		res.Seconds = time.Since(started).Seconds()
		return res
	}
	// The skill the driven agent loads: the arm's, copied into the
	// scratch folder, so every run starts from the same guidance,
	// independent of what is installed user-wide.
	if err := installSkill(arm.skill, scratch); err != nil {
		res.Reason = fmt.Sprintf("installing the skill into the scratch folder: %v", err)
		res.Seconds = time.Since(started).Seconds()
		return res
	}

	baseURL := ""
	var server *Server
	if t.HTTP {
		var err error
		server, err = StartServer(scratch)
		if err != nil {
			res.Reason = fmt.Sprintf("starting the task server: %v", err)
			res.Seconds = time.Since(started).Seconds()
			return res
		}
		baseURL = server.BaseURL()
	}
	env := rs.taskEnv(arm, scratch, baseURL)

	// The prompt: the fixed preamble plus the task body, with this run's
	// paths templated in. A body that cannot render is a task definition
	// bug, not a measurement.
	prompt, perr := RenderPrompt(t, PromptVars{ScratchDir: scratch, BaseURL: baseURL, EdgePath: rs.edgePath})
	if perr != nil {
		res.Reason = fmt.Sprintf("rendering the prompt: %v", perr)
		if server != nil {
			server.Close()
		}
		res.Seconds = time.Since(started).Seconds()
		return res
	}
	rs.writeTaskFile(taskDir, "prompt.txt", prompt, "")

	// Setup: prepare the scratch folder. The Check may not exist without
	// it, so a setup failure is an error, never a failed task.
	setupStarted := time.Now()
	setupOut, setupErr, setupExit, setupTimedOut, setupRunErr := runScript(t.SetupScript(), scratch, env, time.Minute)
	res.SetupSeconds = time.Since(setupStarted).Seconds()
	rs.writeTaskFile(taskDir, "setup-output.txt", setupOut, setupErr)
	if setupRunErr == nil && setupTimedOut {
		setupRunErr = fmt.Errorf("setup timed out")
	}
	if setupRunErr == nil && setupExit != 0 {
		setupRunErr = fmt.Errorf("setup exited %d: %s", setupExit, firstLine(setupOut, setupErr))
	}
	if setupRunErr != nil {
		res.Reason = fmt.Sprintf("setup failed: %v", setupRunErr)
		if server != nil {
			server.Close()
		}
		res.Seconds = time.Since(started).Seconds()
		return res
	}

	// What runs before the Harness: everything the task starts must be
	// gone when it ends.
	before, _ := snapshotProcesses()

	limit := rs.spec.DefaultTimeLimit
	if t.TimeLimit > 0 {
		limit = t.TimeLimit
	}
	if rs.timeLimit > 0 { // the explicit --time-limit beats both
		limit = rs.timeLimit
	}

	stdoutF, stderrF, err := openTaskOutputs(taskDir)
	if err != nil {
		res.Reason = fmt.Sprintf("opening the run's output files: %v", err)
		res.Seconds = time.Since(started).Seconds()
		return res
	}
	// Only the Harness (and the winuse commands its agent runs) is
	// traced: the setup and Check scripts are the runner's, not the
	// agent's.
	tracePath := filepath.Join(taskDir, TraceFileName)
	harnessEnv := append([]string{}, env...)
	setEnv(&harnessEnv, "WINUSE_TRACE", tracePath)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := win.NoConsoleWindow(exec.Command(rs.inv.Argv[0], rs.inv.Argv[1:]...))
	cmd.Dir = scratch
	cmd.Env = harnessEnv
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = io.MultiWriter(stdoutF, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderrF, &stderrBuf)
	var timedOut bool
	var tracker *procTracker
	harnessStarted := time.Now()
	runErr := cmd.Start()
	if runErr == nil {
		// Record the Harness's descendants while it runs: only they (and
		// the task's own apps) are the cleanup's to kill.
		tracker = newProcTracker(before, uint32(cmd.Process.Pid))
		stopPoll := make(chan struct{})
		pollDone := make(chan struct{})
		go func() { tracker.poll(250*time.Millisecond, stopPoll); close(pollDone) }()
		defer func() { close(stopPoll); <-pollDone }()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case runErr = <-done:
		case <-time.After(limit):
			KillTree(uint32(cmd.Process.Pid), nil)
			runErr = <-done // reap the tree we just killed
			timedOut = true
		}
	}
	res.HarnessSeconds = time.Since(harnessStarted).Seconds()
	stdoutF.Close()
	stderrF.Close()
	if server != nil {
		server.Close()
	}

	var outcome HarnessOutcome
	if rs.spec.Name == "claude" {
		outcome, err = ParseClaudeResult(stdoutBuf.String())
	} else {
		outcome, err = ParsePiEvents(stdoutBuf.String())
	}
	res.Usage = outcome.Usage
	if sum, found, terr := ReadTraceFiles(tracePath); terr == nil {
		res.Trace, res.TraceFound = sum, found
	} else {
		fmt.Fprintf(rs.out, "        reading the winuse trace: %v\n", terr)
	}

	switch {
	case timedOut:
		res.Status = Fail
		res.Reason = fmt.Sprintf("time limit exceeded (%s)", limit)
		res.Steps = outcome.Steps // pi streams events live; claude has no result yet
		if res.Steps < 0 && outcome.Usage.Partial {
			res.Steps = outcome.Usage.Turns // claude's streamed messages so far
		}
	case runErr != nil && !isExitStatus(runErr):
		res.Reason = fmt.Sprintf("running %s: %v", rs.spec.Name, runErr)
	case err != nil:
		res.Reason = fmt.Sprintf("%s exited without a usable result: %v: %s",
			rs.spec.Name, err, firstLine(stdoutBuf.String(), stderrBuf.String()))
	default:
		res.Steps = outcome.Steps
		res.Status = Fail // until the Check says otherwise
		checkStarted := time.Now()
		res.Reason = rs.runCheck(t, scratch, env, taskDir)
		res.CheckSeconds = time.Since(checkStarted).Seconds()
		if res.Reason == "" {
			res.Status = Pass
			res.Reason = ""
		} else if outcome.Err != "" {
			res.Reason = res.Reason + " (harness note: " + outcome.Err + ")"
		}
	}

	// Cleanup: what the task started goes away (apps, the winuse helper);
	// processes the machine started meanwhile are not the task's and are
	// left alone (see TaskProcesses).
	if after, aerr := snapshotProcesses(); aerr == nil {
		owned := map[uint32]string{}
		if tracker != nil {
			tracker.observe(after)
			owned = tracker.Owned()
		}
		runBin := strings.ToLower(filepath.Join(arm.binDir, "winuse.exe"))
		isRunBinary := func(pid uint32) bool {
			p, ok := processPath(pid)
			return ok && strings.HasSuffix(p, runBin)
		}
		pids, names := TaskProcesses(before, after, owned, appExeNames(t.Apps), isRunBinary, uint32(os.Getpid()))
		CleanupTaskProcesses(pids, names, func(msg string) {
			fmt.Fprintf(rs.out, "        %s\n", msg)
		})
	}
	res.Seconds = time.Since(started).Seconds()
	return res
}

// runCheck runs the task's Check script and returns "" when the task
// passed, or the failure reason the table prints.
func (rs *runState) runCheck(t Task, scratch string, env []string, taskDir string) string {
	checkOut, checkErr, exit, timedOut, err := runScript(t.CheckScript(), scratch, env, 2*time.Minute)
	rs.writeTaskFile(taskDir, "check-output.txt", checkOut, checkErr)
	if err != nil {
		return fmt.Sprintf("running the Check script: %v", err)
	}
	if timedOut {
		return "the Check script timed out"
	}
	if exit == 0 {
		return ""
	}
	if reason := firstLine(checkOut, checkErr); reason != "" {
		return reason
	}
	return fmt.Sprintf("check exited %d", exit)
}

// taskEnv is the environment every child of the task gets: the arm's
// winuse binary first on PATH, a helper that outlives slow agent
// thinking, and the task's paths.
func (rs *runState) taskEnv(arm *armState, scratch, baseURL string) []string {
	env := append([]string{}, os.Environ()...)
	setEnv(&env, "PATH", arm.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	setEnv(&env, "WINUSE_HELPER_IDLE", "10m")
	setEnv(&env, "WINUSE_TASK_SCRATCH", scratch)
	setEnv(&env, "WINUSE_TASK_URL", baseURL)
	// A trace the user set for their own winuse is not the run's.
	unsetEnv(&env, "WINUSE_TRACE")
	return env
}

// preflight proves the Harness answers with the pinned model before any
// screen is touched.
func preflight(rs *runState, spec HarnessSpec) error {
	prompt := "Reply with the single word: ready."
	cmd := win.NoConsoleWindow(exec.Command(rs.inv.Argv[0], rs.inv.Argv[1:]...))
	cmd.Dir = rs.runRoot
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil && !isExitStatus(err) {
			return fmt.Errorf("%v (%s)", err, firstLine(stdout.String(), stderr.String()))
		}
	case <-time.After(90 * time.Second):
		KillTree(uint32(cmd.Process.Pid), nil)
		<-done
		return fmt.Errorf("%s did not answer within 90s — is it logged in and is the model %s available?", spec.Name, spec.ModelID)
	}
	var err error
	if spec.Name == "claude" {
		_, err = ParseClaudeResult(stdout.String())
	} else {
		_, err = ParsePiEvents(stdout.String())
	}
	if err != nil {
		return fmt.Errorf("%v (stderr: %s) — is %s logged in / configured, and is the model %s available?", err, firstLine("", stderr.String()), spec.Name, spec.ModelID)
	}
	return nil
}

// runScript runs a task's PowerShell script in the scratch folder.
func runScript(script, dir string, env []string, timeout time.Duration) (stdout, stderr string, exit int, timedOut bool, err error) {
	cmd := win.NoConsoleWindow(exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script))
	cmd.Dir = dir
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		return "", "", 0, false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case werr := <-done:
		if exitErr, ok := werr.(*exec.ExitError); ok {
			return out.String(), errOut.String(), exitErr.ExitCode(), false, nil
		}
		return out.String(), errOut.String(), 0, false, werr
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return out.String(), errOut.String(), 0, true, nil
	}
}

// buildWinuse compiles the winuse binary an arm measures, from a source
// tree, into the run's bin directory.
func buildWinuse(srcRoot, binDir string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(binDir, "winuse.exe")
	cmd := win.NoConsoleWindow(exec.Command("go", "build", "-o", exe, "./cmd/winuse"))
	cmd.Dir = srcRoot
	cmd.Env = append(append([]string{}, os.Environ()...), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building winuse in %s: %v: %s", srcRoot, err, firstLine(string(out), ""))
	}
	return nil
}

// installSkill writes an arm's SKILL.md into both harnesses'
// project-skill locations inside the scratch folder.
func installSkill(raw []byte, scratch string) error {
	for _, dest := range []string{
		filepath.Join(scratch, ".claude", "skills", "winuse", "SKILL.md"),
		filepath.Join(scratch, ".agents", "skills", "winuse", "SKILL.md"),
	} {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeRunFiles records the run: the per-run table, the speed summary
// (and the A/B comparison) and the machine-readable results file, for
// comparing runs across winuse versions and skills.
func writeRunFiles(runRoot string, rec RunRecord, table, summary string) error {
	if err := os.WriteFile(filepath.Join(runRoot, "table.txt"), []byte(table), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runRoot, "summary.txt"), []byte(summary), 0o644); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runRoot, "results.json"), raw, 0o644)
}

func (rs *runState) writeTaskFile(taskDir, name, stdout, stderr string) {
	content := "stdout:\n" + stdout + "\nstderr:\n" + stderr + "\n"
	_ = os.WriteFile(filepath.Join(taskDir, name), []byte(content), 0o644)
}

func openTaskOutputs(taskDir string) (io.WriteCloser, io.WriteCloser, error) {
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, nil, err
	}
	outF, err := os.Create(filepath.Join(taskDir, "harness-stdout.txt"))
	if err != nil {
		return nil, nil, err
	}
	errF, err := os.Create(filepath.Join(taskDir, "harness-stderr.txt"))
	if err != nil {
		outF.Close()
		return nil, nil, err
	}
	return outF, errF, nil
}

// resolveEdgePath finds Microsoft Edge through its App Paths registry
// entry (read-only). Empty means Edge is not installed.
func resolveEdgePath() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	path, _, err := k.GetStringValue("")
	if err != nil || path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// FindRepoRoot walks up from dir to the directory holding go.mod (the
// winuse repository root).
func FindRepoRoot(dir string) (string, error) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find the winuse repository root (no go.mod above %s) — run winusetasks from the repository", dir)
		}
		dir = parent
	}
}

// TasksDir is the directory the Task Suite's tasks live in under a
// repository root.
func TasksDir(root string) string {
	return filepath.Join(root, TasksDirName, "tasks")
}

func setEnv(env *[]string, key, value string) {
	for i, e := range *env {
		if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(key)+"=") {
			(*env)[i] = key + "=" + value
			return
		}
	}
	*env = append(*env, key+"="+value)
}

func unsetEnv(env *[]string, key string) {
	out := (*env)[:0]
	for _, e := range *env {
		if !strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(key)+"=") {
			out = append(out, e)
		}
	}
	*env = out
}

// firstLine returns the first non-empty, trimmed line of the outputs —
// the reason a script or a Harness failure prints.
func firstLine(a, b string) string {
	for _, s := range []string{a, b} {
		for _, line := range strings.Split(s, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return ""
}

func isExitStatus(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}
