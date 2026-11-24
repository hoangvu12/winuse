package cli

// run (ADR 0023 amendment): `winuse run <window> <goal> [--set <text>]...
// [--max-steps N] [--model <m>]` keeps the wheel for a bounded task. Each
// step reads the Window, asks a Jev-compatible server for the next
// operation and its target in one request (internal/jev/run.go), and does
// that one thing, until the Window visibly shows the goal achieved. It
// hands back to the Agent on a sign-in, password or payment window, when
// it stalls, at --max-steps, and on the physical Esc key.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/jev"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const runUsage = "usage: winuse run <window> <goal> [--set <text>]... [--max-steps N] [--model <model>]"

// The run's bounds.
const (
	runDefaultSteps = 25
	runMaxSteps     = 100
	runStallMax     = 3 // steps in a row that changed nothing
	runWait         = 700 * time.Millisecond
	runScrollSteps  = 10 // a desktop pane's small increments: about most of a screen of rows in File Explorer, as a Page's 80%
)

// The settle after an Action: the next step reads the Window once it has
// stopped changing, not mid-load. A DuckDuckGo run read the results page
// before its results were there, clicked Search again and then the search
// box: two wasted steps of five.
const (
	runSettleMax   = 3 * time.Second        // give up waiting and read what is there
	runSettlePoll  = 150 * time.Millisecond // between two reads
	runSettleQuiet = 1 * time.Second        // an Action that visibly changed nothing settles here
	runTypeSettle  = 400 * time.Millisecond // typing: suggestions come a moment later
)

// readResult is one read of the Window.
type readResult struct {
	head  string
	els   []jev.Element
	print string
	page  bool // the root is a Page (document): it scrolls and loads
	// scrollBox is what SCROLL scrolls in a desktop App
	// (jev.ScrollContainer); nil on a Page and when nothing scrolls.
	scrollBox *jev.Element
}

// scrolls reports whether SCROLL can be offered on this read.
func (rr *readResult) scrolls() bool { return rr.page || rr.scrollBox != nil }

// read reads the Window whole and uncapped, a Page's off-screen Elements
// marked (jev.ParseState reads the mark): a step sends what is on screen,
// as jev-ultrafast does, plus the off-screen Elements its goal names
// (jev.InView). The read becomes the Agent's Window State, so its Element
// Indexes are the ones the next Action uses.
func (r *runner) read() (*readResult, error) {
	zero := 0
	resp, err := sendRequest("get-window-state", windowArgs{Window: r.a.window, MaxChars: &zero, MarkOffscreen: true}, hideNotes)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, finish(resp)
	}
	head, els := jev.ParseState(resp.Out)
	rr := &readResult{head: head, els: els, print: stateFingerprint(els),
		page: strings.Contains(resp.Out, "\n\t0 document")}
	if !rr.page {
		if box, ok := jev.ScrollContainer(els); ok {
			rr.scrollBox = &box
		}
	}
	return rr, nil
}

// settle waits after an Action until the Window has stopped changing:
// a Page's load first, then two reads in a row that agree. Reads that
// still match before (the Action's effect has not shown yet) go on until
// runSettleQuiet; runSettleMax bounds it all. It returns the last read.
func (r *runner) settle(before string, page, typed bool) (*readResult, error) {
	start := time.Now()
	deadline := start.Add(runSettleMax)
	if page {
		r.waitLoad(deadline)
	}
	last, err := r.read()
	if err != nil {
		return nil, err
	}
	for time.Now().Before(deadline) {
		if r.sleep(runSettlePoll) {
			return nil, escStop
		}
		if page {
			r.waitLoad(deadline)
		}
		cur, err := r.read()
		if err != nil {
			return nil, err
		}
		stable := cur.print == last.print
		last = cur
		since := time.Since(start)
		if stable && (typed && since < runTypeSettle) {
			continue
		}
		if stable && (cur.print != before || since >= runSettleQuiet) {
			break
		}
	}
	return last, nil
}

// waitLoad waits, until deadline at most, for a Page navigating to load.
// A Page that is not loading returns at once; a failure is ignored (the
// reads that follow see what there is).
func (r *runner) waitLoad(deadline time.Time) {
	ms := int(time.Until(deadline) / time.Millisecond)
	if ms < 100 {
		return
	}
	_, _ = sendRequest("wait", protocol.WaitArgs{Window: r.a.window, For: "load", TimeoutMS: ms}, nil)
}

// runArgs is run's parsed command line.
type runArgs struct {
	window, goal string
	values       []string
	maxSteps     int
	model        string
}

var quoted = regexp.MustCompile(`"([^"]+)"`)

// parseRunArgs parses run's line: the Window, then the goal's words, with
// the flags anywhere. Quoted strings inside the goal become text values
// too, before the --set ones.
func parseRunArgs(args []string) (runArgs, error) {
	a := runArgs{maxSteps: runDefaultSteps}
	var pos, sets []string
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		switch name {
		case "--set", "--max-steps", "--model":
			if !inline {
				if i+1 >= len(args) {
					return a, fmt.Errorf("%s needs a value", name)
				}
				i++
				value = args[i]
			}
			switch name {
			case "--set":
				if value == "" {
					return a, errors.New("--set needs a text")
				}
				sets = append(sets, value)
			case "--max-steps":
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > runMaxSteps {
					return a, fmt.Errorf("--max-steps takes 1 to %d, got %q", runMaxSteps, value)
				}
				a.maxSteps = n
			default:
				if strings.TrimSpace(value) == "" {
					return a, errors.New("--model needs a model name")
				}
				a.model = value
			}
			continue
		}
		if strings.HasPrefix(args[i], "--") {
			return a, fmt.Errorf("unknown flag %s", args[i])
		}
		pos = append(pos, args[i])
	}
	if len(pos) < 2 {
		return a, errors.New("run takes a window and a goal")
	}
	a.window = pos[0]
	a.goal = strings.TrimSpace(strings.Join(pos[1:], " "))
	if a.goal == "" {
		return a, errors.New("the goal is empty")
	}
	for _, m := range quoted.FindAllStringSubmatch(a.goal, -1) {
		a.values = append(a.values, m[1])
	}
	a.values = append(a.values, sets...)
	return a, nil
}

// runCmd runs `winuse run`.
func runCmd(args []string, headed bool) int {
	a, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, runUsage)
		return 2
	}
	cfg, err := jev.Load(os.Getenv)
	if err != nil && cfg.Key == "" {
		reportError(os.Stderr, noJev("run", err))
		return 1
	}
	if a.model != "" {
		cfg.Model = a.model
	}
	if cfg.Key == "" {
		reportError(os.Stderr, noJev("run", nil))
		return 1
	}
	// The physical Esc key stops the run between steps too, not only
	// during an Action (the helper watches those). Passive: the key still
	// reaches the user's App.
	esc, escErr := win.NewEscWatcher()
	if escErr == nil {
		defer esc.Close()
		esc.ArmPassive()
		defer esc.DisarmPassive()
	}
	r := &runner{a: a, cfg: cfg, headed: headed, w: os.Stdout, stopped: esc.Stopped,
		jev: &jev.Client{BaseURL: cfg.BaseURL, Key: cfg.Key}}
	if escErr != nil {
		fmt.Fprintf(r.w, "Note: Esc is watched only during each Action (%v)\n", escErr)
	}
	if err := r.run(); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// runner is one run's loop state.
type runner struct {
	a       runArgs
	cfg     jev.Config
	headed  bool
	w       io.Writer
	stopped func() bool
	jev     *jev.Client
}

var escStop = &exitError{msg: "run: stopped by the physical Escape key; the step in progress did nothing more",
	hint: "the user took over: ask how to proceed", code: 130}

// run is the loop: read, decide, act, until done or handed back.
func (r *runner) run() error {
	start := time.Now()
	values := make([]jev.Value, len(r.a.values))
	for i, v := range r.a.values {
		values[i] = jev.Value{ID: "v" + strconv.Itoa(i+1), Text: v}
	}
	fmt.Fprintf(r.w, "run: %q in %s (%d text values, %s at %s)\n", r.a.goal, r.a.window, len(values), r.cfg.Model, r.cfg.BaseURL)
	var history []jev.RunStep
	var prevLines map[string]bool
	failed := map[int]bool{} // refused Actions' Elements, until the Window changes
	prevPrint, lastPrint, stalls := "", "", 0
	var settled *readResult // the read the settle after an Action ended on
	var settleTook time.Duration
	for step := 1; step <= r.a.maxSteps; step++ {
		if r.stopped() {
			return escStop
		}
		stepStart := time.Now()
		cur := settled
		readTook := settleTook
		settled = nil
		if cur == nil {
			var err error
			if cur, err = r.read(); err != nil {
				return err
			}
			readTook = time.Since(stepStart)
		}
		head, els, print := cur.head, cur.els, cur.print
		if print != lastPrint {
			clear(failed) // a changed Window may make them work
		}
		lastPrint = print
		if n := len(history); n > 0 && prevPrint != "" {
			changed := print != prevPrint
			history[n-1].PageChanged = &changed
			if changed {
				stalls = 0
			} else if !strings.HasPrefix(history[n-1].Action, "WAIT") {
				stalls++
			}
		}
		prevPrint = print
		if stalls >= runStallMax {
			return &exitError{msg: fmt.Sprintf("run: stalled: %d steps in a row changed nothing (step %d)", stalls, step),
				hint: "take over: read the Window with get-window-state", code: 1}
		}
		in := jev.RunInput{Goal: r.a.goal, Head: head, Els: els, Values: values, History: history,
			Scroll: cur.scrolls(), Prev: prevLines, Failed: failed}
		prevLines = lineSet(els)
		req, ops := jev.RunRequest(r.cfg.Model, in)
		dreq := jev.DangerRequest(r.cfg.Model, in)
		asked := time.Now()
		type answered struct {
			resp *jev.Response
			err  error
		}
		dch := make(chan answered, 1)
		go func() { resp, err := r.ask(dreq); dch <- answered{resp, err} }()
		answer, err := r.ask(req)
		dres := <-dch
		if err != nil {
			return err
		}
		if dres.err != nil {
			return dres.err
		}
		dumpStep(fmt.Sprintf("step-%02d", step), req, answer)
		dumpStep(fmt.Sprintf("step-%02d-danger", step), dreq, dres.resp)
		d, err := jev.Decide(answer, in, ops)
		if err == nil {
			d.Danger, err = jev.ReadDanger(dres.resp)
		}
		if err != nil {
			return &exitError{msg: fmt.Sprintf("run: step %d: %v", step, err), hint: "take over: read the Window with get-window-state", code: 1}
		}
		if d.Op == jev.OpDone && d.Danger < jev.DangerAt {
			// A DONE is confirmed by a request of its own (jev.AchievedRequest).
			creq := jev.AchievedRequest(r.cfg.Model, in)
			cresp, err := r.ask(creq)
			if err != nil {
				return err
			}
			dumpStep(fmt.Sprintf("step-%02d-achieved", step), creq, cresp)
			if err := jev.ConfirmDone(&d, cresp); err != nil {
				return &exitError{msg: fmt.Sprintf("run: step %d: %v", step, err), hint: "take over: read the Window with get-window-state", code: 1}
			}
		}
		timing := fmt.Sprintf("  (read %.1f s, jev %.1f s)", readTook.Seconds(), time.Since(asked).Seconds())
		if d.Danger >= jev.DangerAt {
			return &exitError{msg: fmt.Sprintf("run: stopped before step %d: the window asks for a sign-in, a password or a payment (danger=%.2f); nothing was done in this step", step, d.Danger),
				hint: "hand it to the user: read the Window and ask them to sign in or pay themselves", code: 1}
		}
		if d.Stall != "" {
			fmt.Fprintf(r.w, "%d %s%s\n", step, d.Stall, timing)
			history = append(history, jev.RunStep{Action: d.Stall})
			stalls++
			prevPrint = "" // this step did nothing to compare
			continue
		}
		switch d.Op {
		case jev.OpDone:
			fmt.Fprintf(r.w, "%d DONE%s\n", step, timing)
			fmt.Fprintf(r.w, "ok done after %d steps in %.1f s (achieved=%.2f)\n", step-1, time.Since(start).Seconds(), d.Achieved)
			return nil
		case jev.OpBlocked:
			return &exitError{msg: fmt.Sprintf("run: blocked at step %d: no offered operation can make progress (p=%.2f)", step, d.OpP),
				hint: "take over: read the Window with get-window-state", code: 1}
		case jev.OpWait:
			fmt.Fprintf(r.w, "%d WAIT%s\n", step, timing)
			history = append(history, jev.RunStep{Action: "WAIT"})
			stalls++
			if r.sleep(runWait) {
				return escStop
			}
			continue
		}
		label, act := r.action(d, cur)
		fmt.Fprintf(r.w, "%d %s%s\n", step, label, timing)
		aresp, err := sendRequest(act.cmd, act.args, nil)
		if err != nil {
			return err
		}
		if out := strings.TrimRight(aresp.Out, "\n"); out != "" {
			fmt.Fprintln(r.w, "  "+strings.ReplaceAll(out, "\n", "\n  "))
		}
		if !aresp.OK {
			if aresp.Exit == 130 {
				return finish(aresp)
			}
			fmt.Fprintf(r.w, "  failed: %s\n", aresp.Err)
			if d.Target != nil {
				failed[d.Target.Index] = true // not offered again until the Window changes
			}
			history = append(history, jev.RunStep{Action: label + " failed: " + aresp.Err})
			stalls++
			prevPrint = "" // counted once, here
			continue
		}
		if d.Value != nil {
			d.Value.Used = true
		}
		history = append(history, jev.RunStep{Action: label})
		settleStart := time.Now()
		if settled, err = r.settle(print, cur.page, d.Op == jev.OpType); err != nil {
			return err
		}
		settleTook = time.Since(settleStart)
	}
	return &exitError{msg: fmt.Sprintf("run: no DONE within %d steps; the window does not show the goal achieved", r.a.maxSteps),
		hint: "take over: read the Window with get-window-state, or run again with a larger --max-steps", code: 1}
}

// sentAction is a helper command a step sends.
type sentAction struct {
	cmd  string
	args any
}

// action is the command a decision sends and the line that names it.
func (r *runner) action(d jev.Decision, cur *readResult) (string, sentAction) {
	switch d.Op {
	case jev.OpClick:
		idx := d.Target.Index
		return fmt.Sprintf("CLICK %d %s  p=%.2f", idx, d.Target.Line, d.Target.P),
			sentAction{"click", protocol.ClickArgs{Window: r.a.window, ElementIndex: &idx, Headed: r.headed}}
	case jev.OpType:
		idx := d.Target.Index
		return fmt.Sprintf("TYPE_TEXT %d %s = %q  p=%.2f", idx, d.Target.Line, d.Value.Text, d.Target.P),
			sentAction{"set-value", protocol.SetValueArgs{Window: r.a.window, ElementIndex: &idx, Value: d.Value.Text, Headed: r.headed}}
	case jev.OpSelect:
		box, label := d.Box.Index, jev.OptionLabel(d.Target.Line)
		return fmt.Sprintf("SELECT %d %q in %d %s  p=%.2f", d.Target.Index, label, box, d.Box.Line, d.Target.P),
			sentAction{"set-value", protocol.SetValueArgs{Window: r.a.window, ElementIndex: &box, Value: label, Headed: r.headed}}
	}
	dir := "down"
	if d.Op == jev.OpScrollUp {
		dir = "up"
	}
	if cur.page {
		// A Page scrolls its window by most of a screen, as jev-ultrafast
		// does: scroll takes the list or page holding an Element, and the
		// document itself (Element 0) is refused on a Tab target.
		sign := ""
		if dir == "up" {
			sign = "-"
		}
		return d.Op, sentAction{"eval", protocol.EvalArgs{Window: r.a.window,
			Expression: "window.scrollBy(0, " + sign + "Math.round(innerHeight * 0.8)), 'ok'"}}
	}
	// A desktop App scrolls the pane holding most of its content
	// (jev.ScrollContainer), through its ScrollPattern: no pointer.
	box, steps := 0, runScrollSteps
	if cur.scrollBox != nil {
		box = cur.scrollBox.Index
	}
	return fmt.Sprintf("%s %d", d.Op, box), sentAction{"scroll", protocol.ScrollArgs{Window: r.a.window, ElementIndex: &box, Direction: dir, Steps: &steps, Headed: r.headed}}
}

// ask sends a step's request, giving up when the user presses Esc.
func (r *runner) ask(req jev.Request) (*jev.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pickBudget)
	defer cancel()
	type result struct {
		resp *jev.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := r.jev.SystemOne(ctx, req)
		done <- result{resp, err}
	}()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case res := <-done:
			if res.err != nil {
				return nil, &exitError{msg: fmt.Sprintf("run: %s did not answer: %v; nothing was done in this step", r.cfg.BaseURL, res.err),
					hint: "go on yourself: read the Window with get-window-state and act on its Elements (winuse jev check tests the server and key)", code: 1}
			}
			return res.resp, nil
		case <-tick.C:
			if r.stopped() {
				cancel()
				return nil, escStop
			}
		}
	}
}

// sleep waits d and reports whether the user pressed Esc meanwhile.
func (r *runner) sleep(d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if r.stopped() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return r.stopped()
}

// dumpStep writes a step's request and answer to WINUSE_JEV_DUMP (a
// folder), for judging the server's decisions; nothing without it.
func dumpStep(name string, req jev.Request, resp *jev.Response) {
	dir := os.Getenv("WINUSE_JEV_DUMP")
	if dir == "" {
		return
	}
	raw, err := json.MarshalIndent(map[string]any{"request": req, "response": resp}, "", "  ")
	if err == nil && os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(filepath.Join(dir, name+".json"), raw, 0o600)
	}
}

// lineSet is a read's Element lines, for telling which Elements are new.
func lineSet(els []jev.Element) map[string]bool {
	m := make(map[string]bool, len(els))
	for _, e := range els {
		m[e.Line] = true
	}
	return m
}

// stateFingerprint identifies what a read shows, to tell whether a step
// changed anything.
func stateFingerprint(els []jev.Element) string {
	var b strings.Builder
	for _, e := range els {
		b.WriteString(strconv.Itoa(e.Index))
		b.WriteByte(' ')
		b.WriteString(e.Line)
		b.WriteByte('\n')
	}
	return b.String()
}
