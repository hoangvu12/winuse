package tasksuite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/version"
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
}

// Run executes the Task Suite: every (selected) task, unattended, through
// the pinned Harness, each in its own scratch folder, each judged by its
// Check script, then the pass/fail table. It is user-started only — the
// caller must have passed CheckUserStarted (cmd/winusetasks enforces it).
func Run(opts RunOptions) error {
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
		runRoot = filepath.Join(os.TempDir(), fmt.Sprintf("winuse-tasks-%s", time.Now().Format("20060102-150405")))
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
		out:       out,
	}
	rs.edgePath = resolveEdgePath()
	logf := func(msg string) { fmt.Fprintln(out, msg) }

	fmt.Fprintf(out, "winuse Task Suite %s — harness %s (%s), %d task(s)\n", version.Version, spec.Name, spec.ModelID, len(tasks))
	fmt.Fprintf(out, "Run directory: %s\n\n", runRoot)

	// Build the winuse binary the tasks will exercise, from this source
	// tree, into the run's bin directory (prepended to the Harness's
	// PATH): a run always measures the tree it was started from.
	rs.binDir = filepath.Join(runRoot, "bin")
	if err := buildWinuse(repoRoot, rs.binDir); err != nil {
		return err
	}

	// Fail fast, before taking over the screen: the Harness must be
	// logged in and the model must exist.
	if err := preflight(rs, spec); err != nil {
		return fmt.Errorf("harness preflight failed: %w", err)
	}

	var results []TaskResult
	for i, t := range tasks {
		res := rs.runTask(t)
		results = append(results, res)
		steps, dur := "-", formatSeconds(res.Seconds)
		if res.Steps >= 0 {
			steps = fmt.Sprintf("%d", res.Steps)
		}
		logf(fmt.Sprintf("[%d/%d] %-22s %s  %s steps, %s", i+1, len(tasks), t.ID, res.Status, steps, dur))
		if res.Reason != "" && res.Status != Pass {
			for _, line := range strings.Split(strings.TrimRight(res.Reason, "\n"), "\n") {
				if line != "" {
					logf("        " + line)
				}
			}
		}
	}

	table := FormatTable(results)
	fmt.Fprintln(out)
	fmt.Fprint(out, table)

	if err := writeRunFiles(runRoot, spec, results, table); err != nil {
		logf(fmt.Sprintf("writing the run's result files: %v", err))
	}
	return nil
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
	binDir    string
	edgePath  string        // empty: Edge not installed; msedge tasks skip
	timeLimit time.Duration // > 0: overrides every task's limit
	out       io.Writer
}

// specSelects describes what a harness runs, for empty-selection errors.
func specSelects(spec HarnessSpec) string {
	if spec.Name == "pi" {
		return "only the text-only subset — no task is marked `text: yes`"
	}
	return "every task"
}

// runTask runs one task end to end: scratch folder, setup, Harness under
// its time limit, Check, cleanup. A task's outcome is always a result —
// only the run's plumbing above can fail hard.
func (rs *runState) runTask(t Task) TaskResult {
	started := time.Now()
	taskDir := filepath.Join(rs.runRoot, t.ID)
	scratch := filepath.Join(taskDir, "scratch")
	res := TaskResult{Task: t, Status: TaskError, Steps: -1, ScratchDir: scratch}

	// Requirement check first: a missing app is a skip, not a failure.
	for _, app := range t.Apps {
		if strings.EqualFold(app, "msedge") {
			if rs.edgePath == "" {
				res.Status, res.Reason = Skip, "requires Microsoft Edge (msedge.exe was not found)"
				res.Seconds = time.Since(started).Seconds()
				return res
			}
			continue
		}
		if findOnPath(app) == "" {
			res.Status = Skip
			res.Reason = fmt.Sprintf("requires %s, which was not found on PATH", app)
			res.Seconds = time.Since(started).Seconds()
			return res
		}
	}

	if err := os.MkdirAll(scratch, 0o755); err != nil {
		res.Reason = fmt.Sprintf("creating the scratch folder: %v", err)
		res.Seconds = time.Since(started).Seconds()
		return res
	}
	// The skill the driven agent loads: copied from this source tree into
	// the scratch folder, so every run starts from the same guidance,
	// independent of what is installed user-wide.
	if err := copySkill(rs.repoRoot, scratch); err != nil {
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
	env := rs.taskEnv(scratch, baseURL)

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
	setupOut, setupErr, setupExit, setupTimedOut, setupRunErr := runScript(t.SetupScript(), scratch, env, time.Minute)
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
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.Command(rs.inv.Argv[0], rs.inv.Argv[1:]...)
	cmd.Dir = scratch
	cmd.Env = env
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = io.MultiWriter(stdoutF, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(stderrF, &stderrBuf)
	var timedOut bool
	var tracker *procTracker
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

	switch {
	case timedOut:
		res.Status = Fail
		res.Reason = fmt.Sprintf("time limit exceeded (%s)", limit)
		res.Steps = outcome.Steps // pi streams events live; claude has no result yet
	case runErr != nil && !isExitStatus(runErr):
		res.Reason = fmt.Sprintf("running %s: %v", rs.spec.Name, runErr)
	case err != nil:
		res.Reason = fmt.Sprintf("%s exited without a usable result: %v: %s",
			rs.spec.Name, err, firstLine(stdoutBuf.String(), stderrBuf.String()))
	default:
		res.Steps = outcome.Steps
		res.Status = Fail // until the Check says otherwise
		res.Reason = rs.runCheck(t, scratch, env, taskDir)
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
		runBin := strings.ToLower(filepath.Join(rs.binDir, "winuse.exe"))
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

// taskEnv is the environment every child of the task gets: the run's
// winuse binary first on PATH, a helper that outlives slow agent
// thinking, and the task's paths.
func (rs *runState) taskEnv(scratch, baseURL string) []string {
	env := append([]string{}, os.Environ()...)
	setEnv(&env, "PATH", rs.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	setEnv(&env, "WINUSE_HELPER_IDLE", "10m")
	setEnv(&env, "WINUSE_TASK_SCRATCH", scratch)
	setEnv(&env, "WINUSE_TASK_URL", baseURL)
	return env
}

// preflight proves the Harness answers with the pinned model before any
// screen is touched.
func preflight(rs *runState, spec HarnessSpec) error {
	prompt := "Reply with the single word: ready."
	cmd := exec.Command(rs.inv.Argv[0], rs.inv.Argv[1:]...)
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
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
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

// buildWinuse compiles the winuse binary the run measures.
func buildWinuse(repoRoot, binDir string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(binDir, "winuse.exe")
	cmd := exec.Command("go", "build", "-o", exe, "./cmd/winuse")
	cmd.Dir = repoRoot
	cmd.Env = append(append([]string{}, os.Environ()...), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building winuse: %v: %s", err, firstLine(string(out), ""))
	}
	return nil
}

// copySkill writes SKILL.md into both harnesses' project-skill
// locations inside the scratch folder.
func copySkill(repoRoot, scratch string) error {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "SKILL.md"))
	if err != nil {
		return err
	}
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

// writeRunFiles records the run: the table and a machine-readable
// results file, for comparing runs across winuse versions.
func writeRunFiles(runRoot string, spec HarnessSpec, results []TaskResult, table string) error {
	if err := os.WriteFile(filepath.Join(runRoot, "table.txt"), []byte(table), 0o644); err != nil {
		return err
	}
	rec := struct {
		Started time.Time    `json:"started"`
		Harness string       `json:"harness"`
		Model   string       `json:"model"`
		Winuse  string       `json:"winuse"`
		Results []TaskResult `json:"results"`
	}{
		Started: time.Now().UTC(),
		Harness: spec.Name,
		Model:   spec.ModelID,
		Winuse:  version.Version,
		Results: results,
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
		if strings.HasPrefix(e, key+"=") {
			(*env)[i] = key + "=" + value
			return
		}
	}
	*env = append(*env, key+"="+value)
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
