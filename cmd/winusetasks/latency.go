package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/latency"
	"github.com/hoangvu12/winuse/internal/tasksuite"
)

// latencyRefusal is what `latency` says without the user's start.
var latencyRefusal = `refusing to start: the latency A/B replaces the running winuse helper
(agents using winuse lose their Window States) and starts the fixture app,
whose window appears on the desktop.

Start it yourself, from your own terminal, by adding ` + tasksuite.UserStartFlag + `:

    winusetasks latency ` + tasksuite.UserStartFlag + `

Never pass ` + tasksuite.UserStartFlag + ` on an agent's behalf.`

// parseLatency reads latency's options. code is 2 with a message on a
// usage error.
func parseLatency(args []string) (opts latency.Options, iAmTheUser bool, msg string, code int) {
	opts = latency.Options{Schedule: latency.DefaultSchedule, Fixture: true}
	usageErr := func(format string, a ...any) (latency.Options, bool, string, int) {
		return opts, false, fmt.Sprintf(format, a...), 2
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		var value string
		hasValue := false
		if eq := strings.IndexByte(a, '='); eq >= 0 && strings.HasPrefix(a, "--") {
			value, hasValue, a = a[eq+1:], true, a[:eq]
		}
		need := func() (string, bool) {
			if hasValue {
				return value, value != ""
			}
			// A flag is never a value: "--a --i-am-the-user" is a usage error.
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return "", false
			}
			i++
			return args[i], true
		}
		number := func(min int) (int, bool) {
			v, ok := need()
			if !ok {
				return 0, false
			}
			n, err := strconv.Atoi(v)
			return n, err == nil && n >= min && n <= 1000000
		}
		switch a {
		case "--i-am-the-user":
			iAmTheUser = true
		case "--a", "--b", "--a-label", "--b-label", "--window", "--out":
			v, ok := need()
			if !ok || strings.TrimSpace(v) == "" {
				return usageErr("%s needs a value", a)
			}
			switch a {
			case "--a":
				opts.A.Source = v
			case "--b":
				opts.B.Source = v
			case "--a-label":
				opts.A.Label = v
			case "--b-label":
				opts.B.Label = v
			case "--window":
				opts.Windows = append(opts.Windows, v)
			case "--out":
				opts.Out = v
			}
		case "--huge-list":
			n, ok := number(1)
			if !ok {
				return usageErr("--huge-list needs a number of items above 0")
			}
			opts.HugeList = n
		case "--rounds", "--per-block":
			n, ok := number(1)
			if !ok || n > 100 {
				return usageErr("%s needs a number from 1 to 100", a)
			}
			if a == "--rounds" {
				opts.Schedule.Rounds = n
			} else {
				opts.Schedule.PerBlock = n
			}
		case "--warmup":
			n, ok := number(0)
			if !ok || n > 100 {
				return usageErr("--warmup needs a number from 0 to 100")
			}
			opts.Schedule.Warmup = n
		case "--no-fixture":
			opts.Fixture = false
		default:
			return usageErr("unknown latency option %q", args[i])
		}
	}
	for _, l := range []string{opts.A.Label, opts.B.Label} {
		if l != "" && strings.ContainsAny(l, " \t\\/:*?\"<>|") {
			return usageErr("a label is one word (%q)", l)
		}
	}
	la, lb := opts.A.Label, opts.B.Label
	if la == "" {
		la = "A"
	}
	if lb == "" {
		lb = "B"
	}
	if strings.EqualFold(la, lb) {
		return usageErr("the two arms need different labels")
	}
	if opts.HugeList > 0 && !opts.Fixture {
		return usageErr("--huge-list needs the fixture (drop --no-fixture)")
	}
	if !opts.Fixture && len(opts.Windows) == 0 {
		return usageErr("with --no-fixture, name a Window to read with --window")
	}
	return opts, iAmTheUser, "", 0
}

// latencyCmd runs the latency A/B. It refuses to start unless the user
// explicitly launched it.
func latencyCmd(args []string) int {
	opts, iAmTheUser, msg, code := parseLatency(args)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "winusetasks: %s\n\n%s", msg, usage)
		return code
	}
	if !iAmTheUser {
		fmt.Fprintf(os.Stderr, "winusetasks: %s\n", latencyRefusal)
		return 1
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	if opts.Repo, err = tasksuite.FindRepoRoot(wd); err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := latency.Run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "\nwinusetasks: %v\n", err)
		return 1
	}
	return 0
}

// latencyReportCmd prints a latency A/B's table again from its
// results.json (safe: it reads the file only).
func latencyReportCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "winusetasks: latency-report takes one results.json\n\n%s", usage)
		return 2
	}
	rec, err := latency.LoadRecord(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winusetasks: %v\n", err)
		return 1
	}
	fmt.Print(latency.Report([2]*latency.Arm{rec.Arms[0], rec.Arms[1]}, rec.Ops, rec.Samples, rec.Schedule))
	return 0
}
