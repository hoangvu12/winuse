package latency

// Offline tests: the plan, the ABBA schedule with a fake caller, the
// statistics, the report, the results file, the arm sources and the zip
// export. Nothing here starts winuse, the fixture or a helper.

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPlan(t *testing.T) {
	ops := Plan(101, 202, []string{"notepad"})
	var names []string
	for _, op := range ops {
		names = append(names, op.Name)
	}
	want := []string{"list-windows",
		"fixture get-window-state", "fixture --skeleton", "fixture find --role button",
		"huge list get-window-state", "huge list --skeleton",
		"notepad get-window-state", "notepad --skeleton", "notepad find --role button"}
	if !slices.Equal(names, want) {
		t.Fatalf("ops %q\nwant %q", names, want)
	}
	if got := strings.Join(ops[2].Args, " "); got != "get-window-state 101 --skeleton" {
		t.Errorf("skeleton args %q", got)
	}
	if got := strings.Join(ops[8].Args, " "); got != "find notepad --role button --count" {
		t.Errorf("find args %q", got)
	}
	// Every call only reads.
	for _, op := range ops {
		switch op.Args[0] {
		case "list-windows", "get-window-state", "find":
		default:
			t.Errorf("%s is not a read", op.Args[0])
		}
		if slices.Contains(op.Args, "--screenshot") {
			t.Errorf("%s takes a screenshot", op.Name)
		}
	}
	if ops := Plan(0, 0, nil); len(ops) != 1 {
		t.Errorf("no targets: %v", ops)
	}
}

func TestOrderIsABBA(t *testing.T) {
	var got []int
	for r := 0; r < 4; r++ {
		o := Order(r)
		got = append(got, o[0], o[1])
	}
	if !slices.Equal(got, []int{0, 1, 1, 0, 0, 1, 1, 0}) {
		t.Errorf("order %v", got)
	}
}

// fakeCaller records every call and answers with a time per arm.
type fakeCaller struct {
	calls []string // "<arm> <args>"
	took  map[string]time.Duration
	fail  func(arm string, args []string) bool
	err   error
}

func (f *fakeCaller) call(_ context.Context, arm *Arm, args []string) (time.Duration, int, string, error) {
	f.calls = append(f.calls, arm.Label+" "+strings.Join(args, " "))
	if f.err != nil {
		return 0, 0, "", f.err
	}
	if f.fail != nil && f.fail(arm.Label, args) {
		return time.Millisecond, 1, "error: window 101 not found", nil
	}
	return f.took[arm.Label], 0, "", nil
}

func testArms() [2]*Arm {
	return [2]*Arm{{Label: "A", Desc: "main~1 (1234567890)", Build: "aaaaaaaaaaaaaaaa"}, {Label: "B", Desc: "HEAD (abcdefabcd)", Build: "bbbbbbbbbbbbbbbb"}}
}

func TestMeasureRunsBlocksInABBAOrder(t *testing.T) {
	ops := Plan(101, 0, nil) // 4 ops
	f := &fakeCaller{took: map[string]time.Duration{"A": 100 * time.Millisecond, "B": 80 * time.Millisecond}}
	s := Schedule{Rounds: 2, PerBlock: 3, Warmup: 1}
	var progress []string
	samples, err := Measure(context.Background(), testArms(), ops, s, f.call, func(l string) { progress = append(progress, l) })
	if err != nil {
		t.Fatal(err)
	}
	// Per block: the helper start, then (1+3) passes of 4 ops.
	perBlock := 1 + 4*4
	if len(f.calls) != 4*perBlock {
		t.Fatalf("%d calls, want %d", len(f.calls), 4*perBlock)
	}
	// Blocks: A, B (round 1), B, A (round 2); each starts with list-windows.
	for blk, arm := range []string{"A", "B", "B", "A"} {
		first := f.calls[blk*perBlock]
		if first != arm+" list-windows" {
			t.Errorf("block %d starts with %q, want %q", blk, first, arm+" list-windows")
		}
		for _, c := range f.calls[blk*perBlock : (blk+1)*perBlock] {
			if !strings.HasPrefix(c, arm+" ") {
				t.Errorf("block %d (%s) ran %q", blk, arm, c)
			}
		}
	}
	// The warm-up pass is not timed: 2 rounds x 3 passes x 4 ops per arm.
	if len(samples) != 2*2*3*4 {
		t.Fatalf("%d samples", len(samples))
	}
	for _, sm := range samples {
		if (sm.Arm == "A" && sm.Millis != 100) || (sm.Arm == "B" && sm.Millis != 80) || sm.Round < 1 || sm.Round > 2 {
			t.Errorf("sample %+v", sm)
		}
	}
	if len(progress) != 4 || !strings.HasPrefix(progress[2], "round 2/2, B: 17 calls") {
		t.Errorf("progress %q", progress)
	}
}

func TestMeasureRecordsFailuresAndStops(t *testing.T) {
	ops := Plan(101, 0, nil)
	f := &fakeCaller{took: map[string]time.Duration{"A": time.Millisecond, "B": time.Millisecond},
		fail: func(arm string, args []string) bool { return arm == "B" && args[0] == "find" }}
	samples, err := Measure(context.Background(), testArms(), ops, Schedule{Rounds: 1, PerBlock: 2}, f.call, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := StatsOf(samples, "B", "fixture find --role button")
	if st.Failed != 2 || st.N != 0 {
		t.Errorf("B find: %+v", st)
	}
	for _, sm := range samples {
		if sm.Exit != 0 && sm.Err != "error: window 101 not found" {
			t.Errorf("failure without its line: %+v", sm)
		}
	}

	// A call that cannot run ends the A/B.
	boom := errors.New("cannot start winuse.exe")
	if _, err := Measure(context.Background(), testArms(), ops, Schedule{Rounds: 1, PerBlock: 1}, (&fakeCaller{err: boom}).call, nil); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
	// A cancelled context stops between calls with what it has.
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	stopping := func(c context.Context, arm *Arm, args []string) (time.Duration, int, string, error) {
		if n++; n == 5 {
			cancel()
		}
		return time.Millisecond, 0, "", nil
	}
	samples, err = Measure(ctx, testArms(), ops, Schedule{Rounds: 2, PerBlock: 5}, stopping, nil)
	if !errors.Is(err, context.Canceled) || len(samples) != 4 {
		t.Errorf("cancelled: %v, %d samples", err, len(samples))
	}
}

func TestPercentile(t *testing.T) {
	var v []float64
	for i := 1; i <= 20; i++ {
		v = append(v, float64(i))
	}
	for p, want := range map[float64]float64{50: 10, 95: 19, 25: 5, 75: 15, 100: 20, 1: 1} {
		if got := Percentile(v, p); got != want {
			t.Errorf("p%v = %v, want %v", p, got, want)
		}
	}
	if Percentile(nil, 50) != 0 || Percentile([]float64{7}, 95) != 7 {
		t.Error("edge cases")
	}
}

func TestStatsOfLeavesFailuresOut(t *testing.T) {
	samples := []Sample{
		{Arm: "A", Op: "x", Millis: 30}, {Arm: "A", Op: "x", Millis: 10}, {Arm: "A", Op: "x", Millis: 20},
		{Arm: "A", Op: "x", Millis: 9999, Exit: 1}, {Arm: "B", Op: "x", Millis: 1}, {Arm: "A", Op: "y", Millis: 1},
	}
	st := StatsOf(samples, "A", "x")
	if st.N != 3 || st.Failed != 1 || st.P50 != 20 || st.Min != 10 || st.Max != 30 || st.P95 != 30 {
		t.Errorf("stats %+v", st)
	}
}

func TestReport(t *testing.T) {
	ops := []Op{{Name: "list-windows"}, {Name: "fixture get-window-state"}, {Name: "fixture --skeleton"}}
	var samples []Sample
	for i := 0; i < 10; i++ {
		samples = append(samples,
			Sample{Arm: "A", Op: "list-windows", Millis: 40 + float64(i)}, Sample{Arm: "B", Op: "list-windows", Millis: 41 + float64(i)},
			Sample{Arm: "A", Op: "fixture get-window-state", Millis: 200 + float64(i)}, Sample{Arm: "B", Op: "fixture get-window-state", Millis: 100 + float64(i)})
	}
	samples = append(samples, Sample{Arm: "B", Op: "fixture --skeleton", Exit: 1})
	out := Report(testArms(), ops, samples, Schedule{Rounds: 2, PerBlock: 5, Warmup: 1})
	for _, want := range []string{
		"A: main~1 (1234567890) (build aaaaaaaaaaaa)",
		"10 timed calls per operation and build (2 rounds of 5, ABBA, after 1 untimed pass(es) per block)",
		"A p50", "B p95", "failed A/B",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	line := func(prefix string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, prefix) {
				return strings.Join(strings.Fields(l), " ")
			}
		}
		t.Fatalf("no %q line:\n%s", prefix, out)
		return ""
	}
	// p50 of 200..209 is 204, of 100..109 is 104; p95 209 and 109.
	if got := line("fixture get-window-state"); got != "fixture get-window-state 204 ms 209 ms 104 ms 109 ms -100 ms (-49.0%) * -100 ms (-47.8%) 0/0" {
		t.Errorf("read line %q", got)
	}
	// Overlapping middles: no mark.
	if got := line("list-windows"); got != "list-windows 44 ms 49 ms 45 ms 50 ms +1 ms (+2.3%) +1 ms (+2.0%) 0/0" {
		t.Errorf("list line %q", got)
	}
	if got := line("fixture --skeleton"); got != "fixture --skeleton - - - - - - 0/1" {
		t.Errorf("skeleton line %q", got)
	}
}

func TestResultsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	arms := testArms()
	rec := Record{Schema: RecordSchema, Started: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), Arms: arms[:],
		Schedule: DefaultSchedule, Ops: Plan(101, 0, nil), Samples: []Sample{{Arm: "A", Op: "list-windows", Millis: 41.5}}}
	if err := writeResults(dir, rec, "summary"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRecord(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Arms[1].Desc != "HEAD (abcdefabcd)" || len(got.Ops) != 4 || got.Samples[0].Millis != 41.5 || got.Schedule != DefaultSchedule {
		t.Errorf("round trip %+v", got)
	}
	if s, _ := os.ReadFile(filepath.Join(dir, "summary.txt")); string(s) != "summary" {
		t.Errorf("summary %q", s)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"schema":2,"arms":[]}`), 0o644)
	if _, err := LoadRecord(bad); err == nil {
		t.Error("a Task Suite results file loaded as an A/B")
	}
}

func TestNotice(t *testing.T) {
	o := Options{Fixture: true, HugeList: 20000, Windows: []string{"notepad", "1442390"}}
	got := Notice(o, 4242, `C:\Users\u\winuse\winuse.exe`, true)
	for _, want := range []string{
		"The helper running now (pid 4242, C:\\Users\\u\\winuse\\winuse.exe) exits",
		"agents using winuse lose their Window States",
		"Every switch between the two builds restarts the helper",
		"starts its\n  own again on its next command",
		"starts the fixture app", "huge-list Window (20000 items)",
		"on the fixture, notepad, 1442390", "No clicks, keys or screenshots",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice lacks %q:\n%s", want, got)
		}
	}
	if got := Notice(Options{Windows: []string{"notepad"}}, 0, "", false); strings.Contains(got, "fixture") || !strings.Contains(got, "No helper is running now") {
		t.Errorf("no fixture, no helper:\n%s", got)
	}
}

func TestSourceKind(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "winuse.exe")
	os.WriteFile(exe, []byte("MZ"), 0o644)
	tree := filepath.Join(dir, "tree")
	os.MkdirAll(tree, 0o755)
	os.WriteFile(filepath.Join(tree, "go.mod"), []byte("module x\n"), 0o644)
	for src, want := range map[string]string{
		exe: kindExe, tree: kindTree, "HEAD": kindRef, "main~3": kindRef,
		filepath.Join(dir, "missing.exe"): kindRef, dir: kindRef,
	} {
		if got := sourceKind(src); got != want {
			t.Errorf("sourceKind(%q) = %s, want %s", src, got, want)
		}
	}
}

func TestPrepareArmCopiesAnExe(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mine.exe")
	os.WriteFile(src, []byte("MZ not really"), 0o644)
	arm := &Arm{Label: "A", Source: src}
	if err := PrepareArm(dir, filepath.Join(dir, "build", "A"), arm); err != nil {
		t.Fatal(err)
	}
	if arm.Exe != filepath.Join(dir, "build", "A", "winuse.exe") || arm.Desc != src || arm.Build == "" {
		t.Errorf("arm %+v", arm)
	}
	if b, _ := os.ReadFile(arm.Exe); string(b) != "MZ not really" {
		t.Errorf("copied %q", b)
	}
}

// The git side, read-only: this repository's merge-base with main, and a
// ref that names nothing. Skipped without git.
func TestGitRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo, _ := filepath.Abs("../..")
	if _, err := git(repo, "rev-parse", "--verify", "main^{commit}"); err != nil {
		t.Skip("no main branch here")
	}
	sha, err := MergeBase(repo)
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) {
		t.Errorf("merge-base %q, %v", sha, err)
	}
	err = PrepareArm(repo, t.TempDir(), &Arm{Label: "A", Source: "no-such-ref-for-latency-test"})
	if err == nil || !strings.Contains(err.Error(), "is not a winuse.exe, a source tree or a commit") {
		t.Errorf("bogus ref: %v", err)
	}
}

func TestUnzip(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, files map[string]string) string {
		p := filepath.Join(dir, name)
		f, _ := os.Create(p)
		zw := zip.NewWriter(f)
		for n, body := range files {
			w, _ := zw.Create(n)
			w.Write([]byte(body))
		}
		zw.Close()
		f.Close()
		return p
	}
	good := write("good.zip", map[string]string{"go.mod": "module x\n", "cmd/winuse/main.go": "package main\n"})
	out := filepath.Join(dir, "out")
	if err := unzip(good, out); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "cmd", "winuse", "main.go")); err != nil || string(b) != "package main\n" {
		t.Errorf("extracted %q, %v", b, err)
	}
	evil := write("evil.zip", map[string]string{"../escaped.txt": "x"})
	if err := unzip(evil, filepath.Join(dir, "out2")); err == nil {
		t.Error("an entry outside the folder was extracted")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Error("escaped.txt exists")
	}
}

// TestLongPathMatchesAnImagePath: a temp folder may come back in 8.3
// form; the helper's image path never does, so both are expanded.
func TestLongPathMatchesAnImagePath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "build", "A", "winuse.exe")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("MZ"), 0o644)
	long := longPath(exe)
	a, _ := os.Stat(exe)
	b, err := os.Stat(long)
	if err != nil || !os.SameFile(a, b) {
		t.Fatalf("longPath(%q) = %q, %v", exe, long, err)
	}
	if !under(long, longPath(filepath.Join(dir, "build"))) {
		t.Errorf("%q is not under the long build folder", long)
	}
	if longPath("") != "" || longPath(`C:\no\such\path`) != `C:\no\such\path` {
		t.Error("a missing path must come back as given")
	}
}

func TestUnder(t *testing.T) {
	for _, c := range []struct {
		path, dir string
		want      bool
	}{
		{`C:\Temp\winuse-latency-1\build\A\winuse.exe`, `C:\Temp\winuse-latency-1\build`, true},
		{`c:\temp\WINUSE-LATENCY-1\build\b\winuse.exe`, `C:\Temp\winuse-latency-1\build`, true},
		{`C:\Users\u\AppData\Local\winuse\winuse.exe`, `C:\Temp\winuse-latency-1\build`, false},
		{`C:\Temp\winuse-latency-1\buildx\winuse.exe`, `C:\Temp\winuse-latency-1\build`, false},
		{`C:\Temp\winuse-latency-1\build`, `C:\Temp\winuse-latency-1\build`, false},
		{"", `C:\x`, false},
	} {
		if got := under(c.path, c.dir); got != c.want {
			t.Errorf("under(%q, %q) = %v", c.path, c.dir, got)
		}
	}
}
