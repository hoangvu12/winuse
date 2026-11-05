package tasksuite

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// The speed metrics (speed ticket 01): per task and arm, the medians and
// spread over the repeats of what each run measured — the agent's wall
// time, the model's turns, tokens and cost, and the time winuse's
// commands took by kind — and, for an A/B run, the comparison of two
// arms. Pure logic over TaskResults, so a results file can be
// summarized again offline (`winusetasks report`).

// RecordSchema is the results file's schema version. 1 had no speed
// metrics, no arms and one run per task.
const RecordSchema = 2

// ArmRecord names what one arm of a run measured: its winuse build and
// its skill.
type ArmRecord struct {
	Label        string `json:"label"`
	WinuseSource string `json:"winuse_source"` // "tree" (built from the run's tree), a tree path or an exe path
	WinuseBuild  string `json:"winuse_build"`  // the exe's Go build id
	SkillSource  string `json:"skill_source"`  // "tree" or a SKILL.md path
	SkillSHA256  string `json:"skill_sha256"`  // over the skill folder: SKILL.md and references\<topic>.md (run.go skillHash)
}

// RunRecord is a run's results file (results.json).
type RunRecord struct {
	Schema  int          `json:"schema"`
	Started time.Time    `json:"started"`
	Harness string       `json:"harness"`
	Model   string       `json:"model"`
	Winuse  string       `json:"winuse"` // the runner's version
	Repeats int          `json:"repeats"`
	Arms    []ArmRecord  `json:"arms"`
	Results []TaskResult `json:"results"`
}

// LoadRunRecord reads a results file. A schema-1 file loads too: its
// runs have no metrics, one arm and one repeat.
func LoadRunRecord(path string) (RunRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RunRecord{}, err
	}
	var rec RunRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return RunRecord{}, fmt.Errorf("%s is not a Task Suite results file: %w", path, err)
	}
	if rec.Repeats == 0 {
		rec.Repeats = 1
	}
	return rec, nil
}

// runName is a row's run in the per-run table: its arm and repeat.
func runName(r TaskResult) string {
	rep := r.Repeat
	if rep == 0 {
		rep = 1
	}
	if r.Arm == "" {
		return fmt.Sprintf("r%d", rep)
	}
	return fmt.Sprintf("%s r%d", r.Arm, rep)
}

// Spread is the median and range of one metric over a task's repeats.
// N = 0: no run measured it.
type Spread struct {
	N      int
	Median float64
	Min    float64
	Max    float64
}

// SpreadOf summarizes values (in any order).
func SpreadOf(values []float64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	s := Spread{N: len(v), Min: v[0], Max: v[len(v)-1]}
	if len(v)%2 == 1 {
		s.Median = v[len(v)/2]
	} else {
		s.Median = (v[len(v)/2-1] + v[len(v)/2]) / 2
	}
	return s
}

// TaskArmStats is one task's runs in one arm, summarized.
type TaskArmStats struct {
	Task     string
	Arm      string
	Runs     int // every run, skipped and errored included
	Measured int // runs that reached the Check or the time limit
	Passed   int
	Skipped  bool   // the task was skipped (a missing app)
	Reason   string // why it was skipped, or the last error

	Wall         Spread // the agent's wall time, seconds
	Turns        Spread
	InputTokens  Spread
	OutputTokens Spread
	CostUSD      Spread
	APISeconds   Spread // time in model calls
	WinuseS      Spread // every winuse command's time, seconds
	Kinds        map[string]Spread
	Commands     Spread
	Actions      Spread
	Failed       Spread // failed winuse commands
	Headed       Spread
}

// Summarize groups results by task and arm, in the order they first
// appear.
func Summarize(results []TaskResult) []TaskArmStats {
	type key struct{ task, arm string }
	var order []key
	groups := map[key][]TaskResult{}
	for _, r := range results {
		k := key{r.Task.ID, r.Arm}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	out := make([]TaskArmStats, 0, len(order))
	for _, k := range order {
		out = append(out, summarizeGroup(k.task, k.arm, groups[k]))
	}
	return out
}

func summarizeGroup(task, arm string, runs []TaskResult) TaskArmStats {
	st := TaskArmStats{Task: task, Arm: arm, Runs: len(runs), Kinds: map[string]Spread{}}
	var wall, turns, in, out, cost, api, winuse, cmds, acts, failed, headed []float64
	kinds := map[string][]float64{}
	for _, r := range runs {
		switch r.Status {
		case Skip:
			st.Skipped, st.Reason = true, r.Reason
		case TaskError:
			st.Reason = r.Reason
		}
		if !r.measured() {
			continue
		}
		st.Measured++
		if r.Status == Pass {
			st.Passed++
		}
		// A schema-1 run has no Harness time or usage: its whole time and
		// its steps stand in.
		if r.HarnessSeconds > 0 {
			wall = append(wall, r.HarnessSeconds)
		} else {
			wall = append(wall, r.Seconds)
		}
		if r.Usage.Turns > 0 {
			turns = append(turns, float64(r.Usage.Turns))
		} else if r.Steps >= 0 {
			turns = append(turns, float64(r.Steps))
		}
		if r.Usage.Known {
			in = append(in, float64(r.Usage.InputTokens))
			out = append(out, float64(r.Usage.OutputTokens))
		}
		if r.Usage.CostUSD >= 0 && r.Usage.Known {
			cost = append(cost, r.Usage.CostUSD)
		}
		if r.Usage.APIMS > 0 {
			api = append(api, float64(r.Usage.APIMS)/1000)
		}
		if r.TraceFound || r.Trace.Commands > 0 {
			winuse = append(winuse, float64(r.Trace.MS)/1000)
			cmds = append(cmds, float64(r.Trace.Commands))
			acts = append(acts, float64(r.Trace.Actions))
			failed = append(failed, float64(r.Trace.Failed))
			headed = append(headed, float64(r.Trace.Headed))
			for _, k := range TraceKinds {
				kinds[k] = append(kinds[k], float64(r.Trace.Kind(k).MS)/1000)
			}
		}
	}
	st.Wall, st.Turns = SpreadOf(wall), SpreadOf(turns)
	st.InputTokens, st.OutputTokens, st.CostUSD = SpreadOf(in), SpreadOf(out), SpreadOf(cost)
	st.APISeconds, st.WinuseS = SpreadOf(api), SpreadOf(winuse)
	st.Commands, st.Actions, st.Failed, st.Headed = SpreadOf(cmds), SpreadOf(acts), SpreadOf(failed), SpreadOf(headed)
	for k, v := range kinds {
		st.Kinds[k] = SpreadOf(v)
	}
	return st
}

// Arms lists the arm labels in the order the results first use them.
func Arms(results []TaskResult) []string {
	var arms []string
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.Arm] {
			seen[r.Arm] = true
			arms = append(arms, r.Arm)
		}
	}
	return arms
}

// FormatSummary renders the speed summary: per arm, one row per task with
// the pass count and the medians over its repeats, the wall time and the
// turns with their range. Plain text, like FormatTable.
func FormatSummary(results []TaskResult) string {
	stats := Summarize(results)
	if len(stats) == 0 {
		return "no tasks ran\n"
	}
	var b strings.Builder
	for _, arm := range Arms(results) {
		var rows [][]string
		var skipped []string
		for _, st := range stats {
			if st.Arm != arm {
				continue
			}
			if st.Skipped {
				skipped = append(skipped, st.Task)
				continue
			}
			rows = append(rows, []string{
				st.Task,
				fmt.Sprintf("%d/%d", st.Passed, st.Runs),
				spreadCell(st.Wall, fmtSecs),
				spreadCell(st.Turns, fmtCount),
				medianCell(st.InputTokens, fmtTokens),
				medianCell(st.OutputTokens, fmtTokens),
				medianCell(st.CostUSD, fmtCost),
				medianCell(st.APISeconds, fmtSecs),
				medianCell(st.WinuseS, fmtSecs),
				kindsCell(st),
				medianCell(st.Commands, fmtCount) + "/" + medianCell(st.Actions, fmtCount),
				medianCell(st.Failed, fmtCount),
				medianCell(st.Headed, fmtCount),
			})
		}
		title := "Speed"
		if arm != "" {
			title = "Speed, arm " + arm
		}
		fmt.Fprintf(&b, "%s: medians over the repeats, [min-max] for wall and turns\n", title)
		if len(rows) > 0 {
			writeTable(&b, []string{"Task", "Pass", "Wall", "Turns", "In tok", "Out tok", "Cost", "API", "winuse", "obs/act/wait", "cmds/acts", "fail", "headed"}, rows)
		}
		if len(skipped) > 0 {
			fmt.Fprintf(&b, "skipped: %s\n", strings.Join(skipped, ", "))
		}
		b.WriteByte('\n')
	}
	b.WriteString("Wall: the agent's time (Harness start to exit). API: time in model calls. winuse: the agent's\n" +
		"winuse commands summed, split into observe/act/wait seconds. cmds/acts: commands and Actions (a Batch\n" +
		"counts its steps). fail: failed winuse commands. headed: commands that took the foreground.\n")
	return b.String()
}

// FormatComparison renders the A/B table: per task, arm A's and arm B's
// medians and B's change, then the totals over the tasks both arms
// measured. A change is starred when B's median lies outside A's
// min-max range over its repeats: with few repeats, anything inside it
// is as likely noise as a change.
func FormatComparison(results []TaskResult, a, b string) string {
	byKey := map[string]TaskArmStats{}
	var tasks []string
	for _, st := range Summarize(results) {
		if _, ok := byKey["A\x00"+st.Task]; !ok && st.Arm == a {
			tasks = append(tasks, st.Task)
		}
		if st.Arm == a {
			byKey["A\x00"+st.Task] = st
		}
		if st.Arm == b {
			byKey["B\x00"+st.Task] = st
		}
	}
	type metric struct {
		name string
		get  func(TaskArmStats) Spread
		fmt  func(float64) string
	}
	metrics := []metric{
		{"Wall", func(s TaskArmStats) Spread { return s.Wall }, fmtSecs},
		{"Turns", func(s TaskArmStats) Spread { return s.Turns }, fmtCount},
		{"In tok", func(s TaskArmStats) Spread { return s.InputTokens }, fmtTokens},
		{"Cost", func(s TaskArmStats) Spread { return s.CostUSD }, fmtCost},
		{"winuse", func(s TaskArmStats) Spread { return s.WinuseS }, fmtSecs},
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "A/B: A = %s, B = %s; medians A -> B and B's change\n", a, b)
	headers := []string{"Task", "Pass A", "Pass B"}
	for _, m := range metrics {
		headers = append(headers, m.name)
	}
	var rows [][]string
	totalA := make([]float64, len(metrics))
	totalB := make([]float64, len(metrics))
	totalN := make([]int, len(metrics))
	passA, passB, runsA, runsB := 0, 0, 0, 0
	var skipped []string
	for _, task := range tasks {
		sa, sbb := byKey["A\x00"+task], byKey["B\x00"+task]
		if sa.Skipped || sbb.Skipped {
			skipped = append(skipped, task)
			continue
		}
		passA, passB, runsA, runsB = passA+sa.Passed, passB+sbb.Passed, runsA+sa.Runs, runsB+sbb.Runs
		row := []string{task, fmt.Sprintf("%d/%d", sa.Passed, sa.Runs), fmt.Sprintf("%d/%d", sbb.Passed, sbb.Runs)}
		for i, m := range metrics {
			va, vb := m.get(sa), m.get(sbb)
			if va.N == 0 || vb.N == 0 {
				row = append(row, "-")
				continue
			}
			totalA[i] += va.Median
			totalB[i] += vb.Median
			totalN[i]++
			star := ""
			if va.N > 1 && (vb.Median < va.Min || vb.Median > va.Max) {
				star = "*"
			}
			row = append(row, fmt.Sprintf("%s -> %s %s%s", m.fmt(va.Median), m.fmt(vb.Median), change(va.Median, vb.Median), star))
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		total := []string{"all tasks", fmt.Sprintf("%d/%d", passA, runsA), fmt.Sprintf("%d/%d", passB, runsB)}
		for i, m := range metrics {
			if totalN[i] == 0 {
				total = append(total, "-")
				continue
			}
			total = append(total, fmt.Sprintf("%s -> %s %s", m.fmt(totalA[i]), m.fmt(totalB[i]), change(totalA[i], totalB[i])))
		}
		rows = append(rows, total)
		writeTable(&sb, headers, rows)
	} else {
		sb.WriteString("no task was measured in both arms\n")
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&sb, "skipped: %s\n", strings.Join(skipped, ", "))
	}
	sb.WriteString("all tasks: the sums of the per-task medians. * = B's median is outside A's min-max over its repeats.\n")
	return sb.String()
}

// change is B's change from A, in percent.
func change(a, b float64) string {
	if a == 0 {
		if b == 0 {
			return "(0%)"
		}
		return "(new)"
	}
	return fmt.Sprintf("(%+.0f%%)", (b-a)/a*100)
}

func writeTable(b *strings.Builder, headers []string, rows [][]string) {
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
	write := func(cells []string) {
		line := ""
		for i, cell := range cells {
			if i > 0 {
				line += "  "
			}
			line += cell + strings.Repeat(" ", widths[i]-len(cell))
		}
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')
	}
	write(headers)
	for _, row := range rows {
		write(row)
	}
}

func spreadCell(s Spread, f func(float64) string) string {
	if s.N == 0 {
		return "-"
	}
	if s.N == 1 || s.Min == s.Max {
		return f(s.Median)
	}
	return fmt.Sprintf("%s [%s-%s]", f(s.Median), f(s.Min), f(s.Max))
}

func medianCell(s Spread, f func(float64) string) string {
	if s.N == 0 {
		return "-"
	}
	return f(s.Median)
}

func kindsCell(st TaskArmStats) string {
	if st.WinuseS.N == 0 {
		return "-"
	}
	parts := make([]string, 0, 3)
	for _, k := range []string{KindObserve, KindAct, KindWait} {
		parts = append(parts, fmt.Sprintf("%.1f", st.Kinds[k].Median))
	}
	return strings.Join(parts, "/")
}

func fmtSecs(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.1fs", s)
	}
	return formatSeconds(s)
}

func fmtCount(n float64) string {
	if n == math.Trunc(n) {
		return fmt.Sprintf("%d", int64(n))
	}
	return fmt.Sprintf("%.1f", n)
}

func fmtTokens(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0fk", n/1e3)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

func fmtCost(c float64) string { return fmt.Sprintf("$%.2f", c) }
