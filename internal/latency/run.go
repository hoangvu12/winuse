package latency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options configure one latency A/B.
type Options struct {
	Repo     string // the repository: refs resolve here, the fixture builds from its tree
	A, B     Arm    // Label and Source; an empty Source is the default (A: merge-base of HEAD and main, B: HEAD)
	Schedule Schedule
	Fixture  bool     // start the fixture app and read it
	HugeList int      // also open the fixture's huge-list Window with this many items (0: no)
	Windows  []string // Windows of real Apps to read too: Window IDs or App names
	Out      string   // the results folder ("": a new temp folder)
	Stdout   io.Writer
}

// Notice is what the A/B prints before it starts anything: that it
// replaces the running helper, what it opens and what it reads.
func Notice(o Options, helperPID uint32, helperPath string, helperRunning bool) string {
	var b strings.Builder
	b.WriteString("winusetasks latency: times read-only winuse calls on two builds.\n\nBefore it starts:\n")
	b.WriteString("- One winuse helper serves every winuse command in this Windows session, and a\n" +
		"  command of another build makes it exit. ")
	if helperRunning {
		fmt.Fprintf(&b, "The helper running now (pid %d, %s) exits at the\n  A/B's first command", helperPID, helperPath)
	} else {
		b.WriteString("No helper is running now; one that starts while\n  the A/B runs exits at its next command")
	}
	b.WriteString(": agents using winuse lose their Window States and\n" +
		"  must read again. Every switch between the two builds restarts the helper.\n" +
		"  At the end the A/B stops its own helper; the installed winuse starts its\n" +
		"  own again on its next command.\n")
	if o.Fixture {
		b.WriteString("- It starts the fixture app (its window appears and may take the foreground)")
		if o.HugeList > 0 {
			fmt.Fprintf(&b, "\n  with its huge-list Window (%d items)", o.HugeList)
		}
		b.WriteString(", and closes it at the end.\n")
	}
	b.WriteString("- It only reads: list-windows, get-window-state, --skeleton and find")
	var on []string
	if o.Fixture {
		on = append(on, "the fixture")
	}
	on = append(on, o.Windows...)
	if len(on) > 0 {
		b.WriteString(", on " + strings.Join(on, ", "))
	}
	b.WriteString(".\n  No clicks, keys or screenshots.\n")
	b.WriteString("- Leave the machine idle while it runs: other load shows in the times.\n\n")
	return b.String()
}

// Run builds both arms, starts the fixture, measures, and writes
// results.json and summary.txt to the results folder. It stops its own
// helper, closes the fixture and removes the builds however it ends.
func Run(ctx context.Context, o Options) (err error) {
	out := o.Stdout
	if out == nil {
		out = os.Stdout
	}
	pid, path, running := RunningHelper()
	fmt.Fprint(out, Notice(o, pid, path, running))

	dir := o.Out
	if dir == "" {
		if dir, err = os.MkdirTemp("", "winuse-latency-"); err != nil {
			return err
		}
	} else if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dir = longPath(dir)
	build := filepath.Join(dir, "build")
	defer func() {
		if stopped, serr := StopHelperUnder(build); serr != nil {
			fmt.Fprintf(out, "Could not stop the A/B's helper: %v\n", serr)
		} else if stopped {
			fmt.Fprintln(out, "Stopped the A/B's helper. The next winuse command starts its own helper again.")
		}
		if rerr := os.RemoveAll(build); rerr != nil {
			fmt.Fprintf(out, "Could not remove the builds in %s: %v\n", build, rerr)
		}
	}()

	a, b := o.A, o.B
	if a.Label == "" {
		a.Label = "A"
	}
	if b.Label == "" {
		b.Label = "B"
	}
	mergeBase := a.Source == ""
	if mergeBase {
		if a.Source, err = MergeBase(o.Repo); err != nil {
			return err
		}
	}
	if b.Source == "" {
		b.Source = "HEAD"
	}
	for _, arm := range []*Arm{&a, &b} {
		fmt.Fprintf(out, "Building %s from %s...\n", arm.Label, arm.Source)
		if err := PrepareArm(o.Repo, filepath.Join(build, arm.Label), arm); err != nil {
			return err
		}
	}
	if mergeBase {
		a.Source, a.Desc = "merge-base", fmt.Sprintf("merge-base of HEAD and main (%.10s)", a.Source)
	}
	fmt.Fprintf(out, "%s: %s, build %.12s\n%s: %s, build %.12s\n", a.Label, a.Desc, a.Build, b.Label, b.Desc, b.Build)
	if a.Build == b.Build {
		fmt.Fprintln(out, "Warning: both arms are the same build; they share one helper, and the A/B measures noise.")
	}

	var fixMain, fixHuge uint64
	if o.Fixture {
		fmt.Fprintln(out, "Building and starting the fixture app...")
		exe, err := BuildFixture(o.Repo, build)
		if err != nil {
			return err
		}
		fix, err := StartFixture(exe, build, o.HugeList)
		if err != nil {
			return err
		}
		defer fix.Close()
		fixMain, fixHuge = fix.Main, fix.HugeList
	}
	ops := Plan(fixMain, fixHuge, o.Windows)
	if len(ops) == 1 {
		fmt.Fprintln(out, "Nothing to read but list-windows: name a Window with --window, or leave the fixture on.")
	}

	started := time.Now()
	arms := [2]*Arm{&a, &b}
	samples, merr := Measure(ctx, arms, ops, o.Schedule, Call, func(line string) { fmt.Fprintln(out, line) })
	rec := Record{Schema: RecordSchema, Started: started.UTC(), Arms: arms[:], Schedule: o.Schedule, Ops: ops, Samples: samples}
	summary := Report(arms, ops, samples, o.Schedule)
	if merr != nil {
		summary += fmt.Sprintf("\nThe A/B stopped early: %v. The table holds the samples so far.\n", merr)
	}
	if werr := writeResults(dir, rec, summary); werr != nil {
		return werr
	}
	fmt.Fprintf(out, "\n%s\nResults: %s\n", summary, dir)
	return merr
}

func writeResults(dir string, rec Record, summary string) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.txt"), []byte(summary), 0o644)
}

// LoadRecord reads a results.json back, for `winusetasks latency-report`.
func LoadRecord(path string) (Record, error) {
	var rec Record
	data, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("%s: %w", path, err)
	}
	if rec.Schema != RecordSchema || len(rec.Arms) != 2 {
		return rec, fmt.Errorf("%s: not a latency A/B results file (schema %d, %d arms)", path, rec.Schema, len(rec.Arms))
	}
	return rec, nil
}
