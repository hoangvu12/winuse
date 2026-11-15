package cli

// pick (ADR 0023): `winuse pick <window> <description> [--click |
// --set-value <text>] [--model <m>]` reads the Window State inside winuse,
// asks a Jev-compatible System One server which Element matches the
// description, and prints that Element's line (and acts on it), so the
// Window State never enters the Agent's context and the decision costs no
// model turn. The read replaces the Agent's Window State of that Window,
// as get-window-state does, so the printed Element Index works afterwards.
//
// `winuse jev [status] | set ... | clear | check` keeps the server, key
// and model (internal/jev).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/jev"
	"github.com/hoangvu12/winuse/internal/protocol"
)

const pickUsage = "usage: winuse pick <window> <description> [--click | --set-value <text>] [--model <model>]"

// pickBudget bounds the Jev request, retries included.
const pickBudget = 30 * time.Second

// pickArgs is pick's parsed command line.
type pickArgs struct {
	window, desc string
	click        bool
	setValue     *string
	model        string
}

// parsePickArgs parses pick's line: the Window, then the description's
// words (joined with spaces), with the flags anywhere.
func parsePickArgs(args []string) (pickArgs, error) {
	var a pickArgs
	var pos []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		name, value, inline := strings.Cut(tok, "=")
		switch name {
		case "--click":
			if inline {
				return a, errors.New("--click is a flag (no value)")
			}
			a.click = true
			continue
		case "--set-value", "--model":
			if !inline {
				if i+1 >= len(args) {
					return a, fmt.Errorf("%s needs a value", name)
				}
				i++
				value = args[i]
			}
			if name == "--model" {
				if strings.TrimSpace(value) == "" {
					return a, errors.New("--model needs a model name")
				}
				a.model = value
			} else {
				v := value
				a.setValue = &v
			}
			continue
		}
		if strings.HasPrefix(tok, "--") {
			return a, fmt.Errorf("unknown flag %s", tok)
		}
		pos = append(pos, tok)
	}
	if a.click && a.setValue != nil {
		return a, errors.New("pass --click or --set-value, not both")
	}
	if activeObserve != nil && !a.click && a.setValue == nil {
		return a, errors.New("--observe goes with --click or --set-value: a pick alone acts on nothing")
	}
	if len(pos) < 2 {
		return a, errors.New("pick takes a window and a description of the Element")
	}
	a.window = pos[0]
	a.desc = strings.TrimSpace(strings.Join(pos[1:], " "))
	if a.desc == "" {
		return a, errors.New("the description is empty")
	}
	return a, nil
}

// pickCmd runs `winuse pick`.
func pickCmd(args []string, headed bool) int {
	a, err := parsePickArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, pickUsage)
		return 2
	}
	cfg, err := jev.Load(os.Getenv)
	if err != nil && cfg.Key == "" {
		reportError(os.Stderr, noJev("pick", err))
		return 1
	}
	if a.model != "" {
		cfg.Model = a.model
	}
	if err := runPick(a, cfg, headed, os.Stdout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// noKeyError is pick without a key: nothing was read or sent.
var noKeyError = noJev("pick", nil)

// noJev is pick or run with no Jev to ask (no key, or settings that do not
// load): nothing was read or sent, and the Agent does the work itself, as
// it does without Jev at all. Jev is an accelerator, never a requirement.
func noJev(cmd string, cause error) *exitError {
	msg := cmd + " needs a Jev API key; nothing was read or sent"
	if cause != nil {
		msg = fmt.Sprintf("%s: the Jev settings do not load (%v); nothing was read or sent", cmd, cause)
	}
	yourself := "read the Window with get-window-state and pick the Element yourself"
	if cmd == "run" {
		yourself = "do the task yourself: read the Window with get-window-state and act on its Elements"
	}
	return &exitError{msg: msg, code: 1,
		hint: yourself + "; set Jev up only if the user asks (winuse jev set --key <key>, TypeSafe's or a Jev-compatible server's with --base-url)"}
}

// runPick reads the Window, asks Jev, prints the pick and acts on it.
func runPick(a pickArgs, cfg jev.Config, headed bool, w io.Writer) error {
	if cfg.Key == "" {
		return noKeyError
	}
	zero := 0 // the whole State: it never reaches the Agent, so no cap
	resp, err := sendRequest("get-window-state", windowArgs{Window: a.window, MaxChars: &zero}, nil)
	if err != nil {
		return err
	}
	if !resp.OK {
		return finish(resp)
	}
	head, els := jev.ParseState(resp.Out)
	if len(els) == 0 {
		return &exitError{
			msg:  fmt.Sprintf("pick: the Window State of %s lists no Elements to pick from; nothing was sent", a.window),
			hint: "read it with get-window-state --screenshot: its content may be drawn (a canvas) or not built yet",
			code: 1,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickBudget)
	defer cancel()
	client := &jev.Client{BaseURL: cfg.BaseURL, Key: cfg.Key}
	answer, err := client.SystemOne(ctx, jev.PickRequest(cfg.Model, head, els, a.desc))
	if err != nil {
		return &exitError{
			msg:  fmt.Sprintf("pick: %s did not answer: %v", cfg.BaseURL, err),
			hint: "nothing was clicked; read the Window with get-window-state and pick the Element yourself (winuse jev check tests the server and key)",
			code: 1,
		}
	}
	ranked := jev.Ranked(answer, els)
	if len(ranked) == 0 || ranked[0].P < jev.MinP {
		printCandidates(w, ranked)
		return &exitError{
			msg:  fmt.Sprintf("pick: no Element matches %q with p >= %.1f; nothing was clicked", a.desc, jev.MinP),
			hint: "describe the control as the Window shows it (\"the Save button\", not \"save my work\"), or read the Window with get-window-state and pick the Element yourself",
			code: 1,
		}
	}
	top := ranked[0]
	fmt.Fprintf(w, "ok %d %s  p=%.2f\n", top.Index, top.Line, top.P)
	idx := top.Index
	switch {
	case a.click:
		return runWithArgs("click", protocol.ClickArgs{Window: a.window, ElementIndex: &idx, Headed: headed})
	case a.setValue != nil:
		return runWithArgs("set-value", protocol.SetValueArgs{Window: a.window, ElementIndex: &idx, Value: *a.setValue, Headed: headed})
	}
	return nil
}

// printCandidates lists the likeliest Elements of a pick that was not
// sure enough, so the Agent can choose among them.
func printCandidates(w io.Writer, ranked []jev.Candidate) {
	var shown []jev.Candidate
	for _, c := range ranked {
		if len(shown) == 3 || c.P < 0.05 {
			break
		}
		shown = append(shown, c)
	}
	if len(shown) == 0 {
		return
	}
	fmt.Fprintln(w, "candidates:")
	for _, c := range shown {
		fmt.Fprintf(w, "  %d %s  p=%.2f\n", c.Index, c.Line, c.P)
	}
}

const jevUsage = "usage: winuse jev [status] | jev set [--key <key> | --key -] [--base-url <url>] [--model <model>] | jev clear | jev check"

// jevCmd runs `winuse jev`: the settings pick uses.
func jevCmd(args []string) int {
	file, err := jev.ConfigFile(os.Getenv)
	if err != nil {
		reportError(os.Stderr, err)
		return 1
	}
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		if len(args) > 1 {
			break
		}
		stored, err := jev.ReadStored(file)
		if err != nil {
			reportError(os.Stderr, err)
			return 1
		}
		printJevStatus(os.Stdout, jev.Resolve(stored, os.Getenv))
		return 0
	case "set":
		return jevSet(file, args[1:], os.Stdin)
	case "clear":
		if len(args) > 1 {
			break
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			reportError(os.Stderr, err)
			return 1
		}
		fmt.Println("ok cleared the stored Jev settings; pick is off unless TYPESAFE_API_KEY is set")
		return 0
	case "check":
		if len(args) > 1 {
			break
		}
		return jevCheck()
	}
	fmt.Fprintln(os.Stderr, jevUsage)
	return 2
}

// printJevStatus prints the resolved settings, the key masked.
func printJevStatus(w io.Writer, c jev.Config) {
	fmt.Fprintf(w, "server %s (%s)\n", c.BaseURL, c.BaseURLFrom)
	fmt.Fprintf(w, "model %s (%s)\n", c.Model, c.ModelFrom)
	if c.Key == "" {
		fmt.Fprintln(w, "key none: pick is off; set one with winuse jev set --key <key>")
		return
	}
	fmt.Fprintf(w, "key %s (%s)\n", jev.Mask(c.Key), c.KeyFrom)
}

// jevSet merges the given settings into the stored ones. --key - reads
// the key from stdin, keeping it out of the command line.
func jevSet(file string, args []string, stdin io.Reader) int {
	stored, err := jev.ReadStored(file)
	if err != nil {
		reportError(os.Stderr, err)
		return 1
	}
	changed := false
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if name != "--key" && name != "--base-url" && name != "--model" {
			fmt.Fprintf(os.Stderr, "winuse: unknown argument %s\n%s\n", args[i], jevUsage)
			return 2
		}
		if !inline {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "winuse: %s needs a value\n%s\n", name, jevUsage)
				return 2
			}
			i++
			value = args[i]
		}
		if name == "--key" && value == "-" {
			line, err := bufio.NewReader(stdin).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				reportError(os.Stderr, err)
				return 1
			}
			value = strings.TrimPrefix(line, "\ufeff") // PowerShell's pipe can lead with a BOM
		}
		value = strings.TrimSpace(value)
		switch name {
		case "--key":
			if value == "" || strings.ContainsAny(value, " \t") {
				fmt.Fprintln(os.Stderr, "winuse: that does not look like an API key")
				return 2
			}
			stored.Key = value
		case "--base-url":
			if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
				fmt.Fprintf(os.Stderr, "winuse: --base-url needs an http(s) URL, got %q\n", value)
				return 2
			}
			stored.BaseURL = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(value, "/"), "/v1"), "/")
		case "--model":
			if value == "" {
				fmt.Fprintln(os.Stderr, "winuse: --model needs a model name")
				return 2
			}
			stored.Model = value
		}
		changed = true
	}
	if !changed {
		fmt.Fprintln(os.Stderr, jevUsage)
		return 2
	}
	if err := jev.WriteStored(file, stored); err != nil {
		reportError(os.Stderr, err)
		return 1
	}
	fmt.Print("ok stored; ")
	printJevStatus(os.Stdout, jev.Resolve(stored, os.Getenv))
	return 0
}

// jevCheck sends one small question to the server and reports the model
// that answered and how long the round trip took.
func jevCheck() int {
	cfg, err := jev.Load(os.Getenv)
	if err != nil && cfg.Key == "" {
		reportError(os.Stderr, err)
		return 1
	}
	if cfg.Key == "" {
		reportError(os.Stderr, noKeyError)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickBudget)
	defer cancel()
	client := &jev.Client{BaseURL: cfg.BaseURL, Key: cfg.Key}
	start := time.Now()
	resp, err := client.SystemOne(ctx, jev.Request{
		Model: cfg.Model,
		State: `Window: "Calculator". 1 button Plus. 2 button Equals.`,
		Questions: map[string]jev.Question{"where": {
			Type: "choice", Instructions: "Which element adds two numbers?",
			Criteria: map[string]any{"1": "button Plus", "2": "button Equals"}}},
	})
	if err != nil {
		reportError(os.Stderr, &exitError{msg: fmt.Sprintf("jev check: %s: %v", cfg.BaseURL, err), hint: "winuse jev status shows the server, model and key in use", code: 1})
		return 1
	}
	fmt.Printf("ok %s answered in %d ms (%s)\n", resp.Model, time.Since(start).Milliseconds(), cfg.BaseURL)
	return 0
}
