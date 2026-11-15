// Package latency is the latency A/B (learn-from 07): it builds two winuse
// revisions, by default the merge-base of HEAD with main and HEAD, and
// times the same read-only winuse calls on each, on the fixture app and on
// Windows the user names, then reports p50 and p95 per operation and the
// change. `winusetasks latency` runs it; only the user starts it.
//
// One helper serves every winuse command of a user's session, and a
// command of another build makes the running helper exit (desktop-parity
// 25). So the two builds cannot be called in turn one command at a time:
// each would restart the helper. The A/B runs in blocks instead: in each
// round each build runs one block (A then B, then B then A: ABBA), and a
// block starts its build's helper with a call that is not timed, then runs
// warm-up passes that are not timed either, then the timed passes.
//
// This file is the part that touches nothing: the plan, the schedule, the
// statistics and the report. build.go builds the revisions, live.go starts
// the fixture and the processes.
package latency

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// Arm is one build under test.
type Arm struct {
	Label  string `json:"label"`  // "A" and "B" unless the user names them
	Source string `json:"source"` // what the user named: a git ref, a source tree or a winuse.exe
	Desc   string `json:"desc"`   // what it resolved to (a ref with its commit, a path)
	Exe    string `json:"-"`      // the winuse.exe the arm runs
	Build  string `json:"build"`  // the exe's build id (version.ExeBuild)
}

// Op is one read-only winuse call the A/B times.
type Op struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// Plan lists the operations: list-windows, then reads of the fixture's main
// Window (0: no fixture), of its huge-list Window (0: not started), and of
// each Window the user named (a Window ID or an App name). Every one only
// reads: no Action, no screenshot, nothing that moves the foreground.
func Plan(fixture, hugeList uint64, windows []string) []Op {
	ops := []Op{{Name: "list-windows", Args: []string{"list-windows"}}}
	reads := func(prefix, w string, find bool) {
		ops = append(ops,
			Op{Name: prefix + " get-window-state", Args: []string{"get-window-state", w}},
			Op{Name: prefix + " --skeleton", Args: []string{"get-window-state", w, "--skeleton"}})
		if find {
			ops = append(ops, Op{Name: prefix + " find --role button", Args: []string{"find", w, "--role", "button", "--count"}})
		}
	}
	if fixture != 0 {
		reads("fixture", fmt.Sprint(fixture), true)
	}
	if hugeList != 0 {
		reads("huge list", fmt.Sprint(hugeList), false)
	}
	for _, w := range windows {
		reads(w, w, true)
	}
	return ops
}

// Schedule is how many times each operation runs.
type Schedule struct {
	Rounds   int `json:"rounds"`    // each round runs one block per arm, in ABBA order
	PerBlock int `json:"per_block"` // timed passes over every operation in a block
	Warmup   int `json:"warmup"`    // untimed passes before them
}

// DefaultSchedule gives 30 timed samples per operation and arm.
var DefaultSchedule = Schedule{Rounds: 6, PerBlock: 5, Warmup: 1}

// Order is the arm order of round r: A then B in even rounds, B then A in
// odd ones (ABBA), so a drift in the machine's load does not favour one.
func Order(round int) [2]int {
	if round%2 == 0 {
		return [2]int{0, 1}
	}
	return [2]int{1, 0}
}

// Sample is one timed call.
type Sample struct {
	Arm    string  `json:"arm"`
	Op     string  `json:"op"`
	Round  int     `json:"round"`
	Millis float64 `json:"ms"`
	Exit   int     `json:"exit"`
	Err    string  `json:"err,omitempty"` // the first line of a failure's stderr
}

// Caller runs one winuse call of arm and returns its wall time, its exit
// code and, on a failure, the first line of its stderr. An error means the
// call could not run at all, which ends the A/B.
type Caller func(ctx context.Context, arm *Arm, args []string) (took time.Duration, exit int, firstErr string, err error)

// startHelper is the untimed call that opens a block: it makes the running
// helper of the other build exit and starts this build's.
var startHelper = []string{"list-windows"}

// Measure runs the schedule and returns the timed samples. progress gets
// one line per block. A cancelled ctx stops it between calls with the
// samples so far.
func Measure(ctx context.Context, arms [2]*Arm, ops []Op, s Schedule, call Caller, progress func(string)) ([]Sample, error) {
	var out []Sample
	for r := 0; r < s.Rounds; r++ {
		for _, ai := range Order(r) {
			arm := arms[ai]
			start := time.Now()
			if _, _, _, err := call(ctx, arm, startHelper); err != nil {
				return out, err
			}
			for p := 0; p < s.Warmup+s.PerBlock; p++ {
				for _, op := range ops {
					if err := ctx.Err(); err != nil {
						return out, err
					}
					took, exit, firstErr, err := call(ctx, arm, op.Args)
					if err != nil {
						return out, err
					}
					if p < s.Warmup {
						continue
					}
					out = append(out, Sample{Arm: arm.Label, Op: op.Name, Round: r + 1, Millis: float64(took.Microseconds()) / 1000, Exit: exit, Err: firstErr})
				}
			}
			if progress != nil {
				progress(fmt.Sprintf("round %d/%d, %s: %d calls in %s", r+1, s.Rounds, arm.Label, 1+len(ops)*(s.Warmup+s.PerBlock), time.Since(start).Round(time.Millisecond)))
			}
		}
	}
	return out, nil
}

// Stats summarises one operation on one arm over its successful calls.
type Stats struct {
	N, Failed          int
	P25, P50, P75, P95 float64
	Min, Max           float64
}

// StatsOf computes an operation's statistics on one arm. A failed call
// (exit not 0) counts in Failed and not in the times.
func StatsOf(samples []Sample, arm, op string) Stats {
	var st Stats
	var ms []float64
	for _, s := range samples {
		if s.Arm != arm || s.Op != op {
			continue
		}
		if s.Exit != 0 {
			st.Failed++
			continue
		}
		ms = append(ms, s.Millis)
	}
	st.N = len(ms)
	if st.N == 0 {
		return st
	}
	slices.Sort(ms)
	st.Min, st.Max = ms[0], ms[len(ms)-1]
	st.P25, st.P50, st.P75, st.P95 = Percentile(ms, 25), Percentile(ms, 50), Percentile(ms, 75), Percentile(ms, 95)
	return st
}

// Percentile is the nearest-rank percentile p (0-100] of sorted values:
// the smallest value with at least p% of the values at or below it.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

// Report is the readable summary: the arms, the schedule and a table with
// each operation's p50 and p95 on both arms, B's change against A, and the
// failed calls. A * marks an operation whose middle halves (p25-p75) do not
// overlap: a change larger than the calls' own spread.
func Report(arms [2]*Arm, ops []Op, samples []Sample, s Schedule) string {
	var b strings.Builder
	a, bb := arms[0], arms[1]
	fmt.Fprintf(&b, "Latency A/B\n  %s: %s (build %.12s)\n  %s: %s (build %.12s)\n", a.Label, a.Desc, a.Build, bb.Label, bb.Desc, bb.Build)
	fmt.Fprintf(&b, "%d timed calls per operation and build (%d rounds of %d, ABBA, after %d untimed pass(es) per block).\n",
		s.Rounds*s.PerBlock, s.Rounds, s.PerBlock, s.Warmup)
	b.WriteString("Wall time of each winuse call, process start included. * = the p25-p75 ranges do not overlap.\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "operation\t%[1]s p50\t%[1]s p95\t%[2]s p50\t%[2]s p95\tp50 change\tp95 change\tfailed %[1]s/%[2]s\t\n", a.Label, bb.Label)
	for _, op := range ops {
		sa, sb := StatsOf(samples, a.Label, op.Name), StatsOf(samples, bb.Label, op.Name)
		mark := ""
		if sa.N > 0 && sb.N > 0 && (sb.P25 > sa.P75 || sb.P75 < sa.P25) {
			mark = " *"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d/%d\t\n", op.Name,
			ms(sa, sa.P50), ms(sa, sa.P95), ms(sb, sb.P50), ms(sb, sb.P95),
			change(sa, sb, sa.P50, sb.P50)+mark, change(sa, sb, sa.P95, sb.P95), sa.Failed, sb.Failed)
	}
	tw.Flush()
	return b.String()
}

func ms(st Stats, v float64) string {
	if st.N == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f ms", v)
}

// change is b's change against a: "+12 ms (+8.0%)".
func change(sa, sb Stats, a, b float64) string {
	if sa.N == 0 || sb.N == 0 {
		return "-"
	}
	d := b - a
	if a == 0 {
		return fmt.Sprintf("%+.0f ms", d)
	}
	return fmt.Sprintf("%+.0f ms (%+.1f%%)", d, d/a*100)
}

// Record is results.json: everything the report was made from.
type Record struct {
	Schema   int       `json:"schema"`
	Started  time.Time `json:"started"`
	Arms     []*Arm    `json:"arms"`
	Schedule Schedule  `json:"schedule"`
	Ops      []Op      `json:"ops"`
	Samples  []Sample  `json:"samples"`
}

// RecordSchema is Record's format version.
const RecordSchema = 1
