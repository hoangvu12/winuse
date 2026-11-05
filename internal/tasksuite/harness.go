package tasksuite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HarnessSpec pins one Harness the Task Suite runs through: a fixed
// model and fixed limits, identical across every run of a Harness (the
// method: same Harness, model and prompt; only winuse changes).
type HarnessSpec struct {
	Name             string        // "claude" or "pi"
	ModelID          string        // the exact model id passed to the Harness
	MaxTurns         int           // claude only: the fixed turn limit
	DefaultTimeLimit time.Duration // per-task wall-clock limit
}

// ClaudeSpec is the primary run: Claude Code headless (`claude -p`) with
// Opus 5.5, per the ticket.
var ClaudeSpec = HarnessSpec{
	Name:             "claude",
	ModelID:          "claude-opus-5-5",
	MaxTurns:         40,
	DefaultTimeLimit: 15 * time.Minute,
}

// PiSpec is the text-only subset run: Pi headless with glm-5.3.
var PiSpec = HarnessSpec{
	Name:             "pi",
	ModelID:          "iroha/dashscope/glm-5.3",
	DefaultTimeLimit: 12 * time.Minute,
}

// SpecFor returns the spec for a harness name (claude is the default).
func SpecFor(name string) (HarnessSpec, error) {
	switch strings.ToLower(name) {
	case "", "claude":
		return ClaudeSpec, nil
	case "pi":
		return PiSpec, nil
	default:
		return HarnessSpec{}, fmt.Errorf("unknown harness %q (expected claude or pi)", name)
	}
}

// HarnessInvocation is the exact command the runner execs for every task
// of one Harness: the resolved executable and the fixed flags. The
// prompt is never an argument — it always travels on stdin, because a
// .cmd shim forwards arguments through cmd.exe, whose quoting cannot
// carry a multi-line prompt; flags only are safe everywhere.
type HarnessInvocation struct {
	Argv []string
}

// ResolveHarness finds the Harness's executable and builds the fixed
// argv. It prefers a native executable (.exe); an npm-style .cmd/.bat
// shim is unwrapped to its `node script.js` form so Go's argv quoting
// stays exact.
func ResolveHarness(spec HarnessSpec) (HarnessInvocation, error) {
	path := findOnPath(spec.Name)
	if path == "" {
		return HarnessInvocation{}, fmt.Errorf("%s was not found on PATH — the Task Suite needs the %s harness installed and on PATH", spec.Name, spec.Name)
	}
	base := []string{path}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe":
		return HarnessInvocation{Argv: append(base, harnessFlags(spec)...)}, nil
	case ".cmd", ".bat":
		node, script, ok := resolveNodeShim(path)
		if !ok {
			return HarnessInvocation{}, fmt.Errorf("%s resolved to %s, which is not a recognized npm shim: install the harness as a native .exe or a standard npm shim", spec.Name, path)
		}
		return HarnessInvocation{Argv: append([]string{node, script}, harnessFlags(spec)...)}, nil
	default:
		return HarnessInvocation{}, fmt.Errorf("%s resolved to %s, which is not executable (expected .exe, .cmd or .bat)", spec.Name, path)
	}
}

// harnessFlags are the Harness's fixed flags (cwd and stdin are set by
// the runner; the model, the turn limit and the output format are the
// fixed method).
func harnessFlags(spec HarnessSpec) []string {
	switch spec.Name {
	case "claude":
		flags := []string{
			"-p", // headless: run the prompt (read from stdin) and exit
			"--model", spec.ModelID,
			"--max-turns", strconv.Itoa(spec.MaxTurns),
			// One JSON event per line, the result (num_turns, usage, cost)
			// last: a run killed at its time limit still leaves the turns
			// and tokens it used. stream-json needs --verbose.
			"--output-format", "stream-json",
			"--verbose",
			// Unattended runs cannot answer permission prompts; the task
			// works only in its scratch folder, which contains the blast
			// radius. Documented in tasksuite/README.md.
			"--dangerously-skip-permissions",
		}
		return flags
	case "pi":
		return []string{
			"--mode", "json", // JSONL events on stdout: tool calls are countable
			"--no-session",
			"--model", spec.ModelID,
		}
	default:
		return nil
	}
}

// findOnPath searches PATH for name with Windows' extension preference
// (.exe, then .cmd, then .bat), skipping extensionless files (an sh
// script named `pi` sits next to `pi.cmd` and is not executable).
func findOnPath(name string) string {
	if strings.ContainsRune(name, os.PathSeparator) || strings.ContainsRune(name, '/') {
		return "" // only bare names are searched
	}
	dirs := filepath.SplitList(os.Getenv("PATH"))
	exts := []string{".exe", ".cmd", ".bat"}
	for _, ext := range exts {
		for _, dir := range dirs {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, name+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// shimScriptRe finds the quoted script path inside an npm-generated
// shim (`"%dp0%\node_modules\pkg\dist\cli.js"` and friends).
var shimScriptRe = regexp.MustCompile(`"([^"]+\.(?:js|mjs|cjs))"`)

// resolveNodeShim unwraps an npm .cmd/.bat shim: it returns the node
// executable and the script the shim runs. %dp0% / %~dp0% (the shim's
// directory) is expanded in both the script path and node's.
func resolveNodeShim(shimPath string) (node, script string, ok bool) {
	raw, err := os.ReadFile(shimPath)
	if err != nil {
		return "", "", false
	}
	dir := filepath.Dir(shimPath)
	m := shimScriptRe.FindStringSubmatch(string(raw))
	if m == nil {
		return "", "", false
	}
	script = m[1]
	script = expandDp0(script, dir)
	if _, err := os.Stat(script); err != nil {
		return "", "", false
	}
	// npm shims prefer the node.exe next to the shim (%dp0%\node.exe),
	// falling back to node on PATH.
	node = filepath.Join(dir, "node.exe")
	if st, err := os.Stat(node); err == nil && !st.IsDir() {
		return node, script, true
	}
	if p := findOnPath("node"); p != "" {
		return p, script, true
	}
	return "", "", false
}

func expandDp0(s, dir string) string {
	// npm shims spell the shim directory %dp0% / %~dp0 (optionally with a
	// trailing backslash); expand every spelling, then clean the path.
	r := strings.NewReplacer(
		`%~dp0\`, dir+`\`,
		`%~dp0`, dir,
		`%dp0%\`, dir+`\`,
		`%dp0%`, dir,
		`%dp0\`, dir+`\`,
		`%dp0`, dir,
	)
	return filepath.Clean(r.Replace(s))
}

// HarnessOutcome is what one finished Harness run reports: the steps
// the agent took and its final reply, plus a note when the Harness
// itself misbehaved.
type HarnessOutcome struct {
	Steps     int    // claude: num_turns; pi: tool calls; -1 = unknown
	FinalText string // the agent's final reply
	Err       string // a Harness-level note (still measured by the Check)
	Usage     Usage  // the model's turns, tokens and cost
}

// Usage is what the model spent on one task run, as the Harness reports
// it. InputTokens counts every prompt token, cached or not; the cache
// split is kept beside it.
type Usage struct {
	Turns               int     `json:"turns"` // model turns; -1 = unknown
	InputTokens         int64   `json:"input_tokens"`
	UncachedInputTokens int64   `json:"uncached_input_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheWriteTokens    int64   `json:"cache_write_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CostUSD             float64 `json:"cost_usd"` // the Harness's own estimate; -1 = unknown
	APIMS               int64   `json:"api_ms"`   // time in model calls (claude's duration_api_ms); 0 = unknown
	Partial             bool    `json:"partial"`  // no final result (a killed run): summed from the messages seen
	Known               bool    `json:"known"`    // the Harness reported tokens or cost at all
}

// tokenUsage is the usage block of a claude result or assistant message.
type tokenUsage struct {
	Input      int64 `json:"input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	Output     int64 `json:"output_tokens"`
}

func (u *Usage) addTokens(t tokenUsage) {
	u.UncachedInputTokens += t.Input
	u.CacheReadTokens += t.CacheRead
	u.CacheWriteTokens += t.CacheWrite
	u.InputTokens += t.Input + t.CacheRead + t.CacheWrite
	u.OutputTokens += t.Output
}

// ParseClaudeResult parses `claude -p --output-format stream-json`
// stdout (one event per line, the result object last) or the older
// `--output-format json` forms (a single result object, possibly
// pretty-printed, or one array of every event): num_turns, the reply,
// and the usage, cost and API time. Leading noise is tolerated. With no
// result object (the run was killed at its time limit), the usage is
// summed from the assistant messages streamed so far and marked
// partial, and the error still says there was no result.
func ParseClaudeResult(stdout string) (HarnessOutcome, error) {
	objs := claudeResultObjects(stdout)
	if len(objs) == 0 {
		out := HarnessOutcome{Steps: -1, Usage: claudeStreamedUsage(stdout)}
		return out, fmt.Errorf("no result JSON in claude output")
	}
	last := objs[len(objs)-1]
	out := HarnessOutcome{Steps: -1, Usage: Usage{Turns: -1, CostUSD: -1}}
	if n, ok := last["num_turns"].(float64); ok {
		out.Steps = int(n)
		out.Usage.Turns = int(n)
	}
	if raw, ok := last["usage"]; ok {
		var t tokenUsage
		if b, err := json.Marshal(raw); err == nil && json.Unmarshal(b, &t) == nil {
			out.Usage.addTokens(t)
			out.Usage.Known = true
		}
	}
	if c, ok := last["total_cost_usd"].(float64); ok {
		out.Usage.CostUSD = c
		out.Usage.Known = true
	}
	if ms, ok := last["duration_api_ms"].(float64); ok {
		out.Usage.APIMS = int64(ms)
	}
	if r, ok := last["result"].(string); ok {
		out.FinalText = r
	}
	if isErr, ok := last["is_error"].(bool); ok && isErr {
		if sub, ok := last["subtype"].(string); ok {
			out.Err = "claude reported an error result: " + sub
		} else {
			out.Err = "claude reported an error result"
		}
	}
	return out, nil
}

// claudeResultObjects decodes the type:"result" JSON objects in the
// output: first the whole output as one object (claude pretty-prints
// its result) or as one array of events (what --verbose with
// --output-format json prints), then one object per line.
func claudeResultObjects(stdout string) []map[string]any {
	var found []map[string]any
	whole := strings.TrimSpace(stdout)
	if strings.HasPrefix(whole, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(whole), &obj); err == nil && obj["type"] == "result" {
			found = append(found, obj)
		}
	}
	if strings.HasPrefix(whole, "[") {
		var events []map[string]any
		if err := json.Unmarshal([]byte(whole), &events); err == nil {
			for _, ev := range events {
				if ev["type"] == "result" {
					found = append(found, ev)
				}
			}
		}
	}
	if len(found) == 0 {
		for _, line := range strings.Split(stdout, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				continue
			}
			if obj["type"] == "result" {
				found = append(found, obj)
			}
		}
	}
	return found
}

// claudeStreamedUsage sums the assistant messages of a stream-json
// output that has no result (a killed run): one turn per message id,
// each message's usage counted once (claude streams a message once per
// content block, every copy with the same id and usage). The cost is
// unknown; a message's output count may be a streaming placeholder.
func claudeStreamedUsage(stdout string) Usage {
	u := Usage{Turns: -1, CostUSD: -1}
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message *struct {
				ID    string      `json:"id"`
				Usage *tokenUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "assistant" || ev.Message == nil {
			continue
		}
		if ev.Message.ID != "" {
			if seen[ev.Message.ID] {
				continue
			}
			seen[ev.Message.ID] = true
		}
		if u.Turns < 0 {
			u.Turns = 0
		}
		u.Turns++
		if ev.Message.Usage != nil {
			u.addTokens(*ev.Message.Usage)
			u.Known = true
		}
	}
	if u.Turns >= 0 {
		u.Partial = true
	}
	return u
}

// piUsage is the usage block pi puts on an assistant message.
type piUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Cost       *struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// ParsePiEvents parses `pi --mode json` stdout: one JSON object per
// line. Steps are the tool executions; the final reply is the last
// assistant message. Turns are the assistant messages, and their usage
// blocks (when pi sends them) are summed into the tokens and cost.
func ParsePiEvents(stdout string) (HarnessOutcome, error) {
	out := HarnessOutcome{Steps: 0, Usage: Usage{Turns: 0}}
	cost, costKnown := 0.0, false
	sawEvent := false
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message *struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Usage   *piUsage        `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		sawEvent = true
		switch ev.Type {
		case "tool_execution_start":
			out.Steps++
		case "message_end":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				out.FinalText = extractText(ev.Message.Content)
				out.Usage.Turns++
				if u := ev.Message.Usage; u != nil {
					out.Usage.addTokens(tokenUsage{Input: u.Input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output})
					out.Usage.Known = true
					if u.Cost != nil {
						cost += u.Cost.Total
						costKnown = true
					}
				}
			}
		case "agent_settled":
			// aborted runs are surfaced through the exit code; nothing to do
		}
	}
	if !sawEvent {
		return HarnessOutcome{Steps: -1, Usage: Usage{Turns: -1, CostUSD: -1}}, fmt.Errorf("no JSON events in pi output")
	}
	out.Usage.CostUSD = -1
	if costKnown {
		out.Usage.CostUSD = cost
	}
	return out, nil
}

// extractText pulls the text out of a message content value: either a
// plain string or an array of blocks.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" {
				texts = append(texts, b.Text)
			}
		}
		return strings.Join(texts, "")
	}
	return ""
}
