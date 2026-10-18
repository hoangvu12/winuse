package tasksuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecFor(t *testing.T) {
	if _, err := SpecFor(""); err != nil {
		t.Errorf("claude must be the default: %v", err)
	}
	spec, err := SpecFor("pi")
	if err != nil || spec.Name != "pi" {
		t.Errorf("pi spec: %v %v", spec, err)
	}
	if _, err := SpecFor("vscode"); err == nil {
		t.Error("an unknown harness must fail")
	}
}

// TestSpecsPinTheMethod pins the fixed models and limits: the ticket's
// claude -p with Opus 5.5 and Pi with glm-5.3, under fixed time and turn
// limits.
func TestSpecsPinTheMethod(t *testing.T) {
	if ClaudeSpec.ModelID != "claude-opus-5-5" {
		t.Errorf("claude model = %q", ClaudeSpec.ModelID)
	}
	if ClaudeSpec.MaxTurns <= 0 || ClaudeSpec.DefaultTimeLimit <= 0 {
		t.Errorf("claude limits not fixed: turns=%d time=%v", ClaudeSpec.MaxTurns, ClaudeSpec.DefaultTimeLimit)
	}
	if PiSpec.ModelID != "iroha/dashscope/glm-5.3" {
		t.Errorf("pi model = %q", PiSpec.ModelID)
	}
	if PiSpec.DefaultTimeLimit <= 0 {
		t.Errorf("pi time limit not fixed: %v", PiSpec.DefaultTimeLimit)
	}
}

func TestFindOnPathPrefersExeAndSkipsExtensionless(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	// an extensionless `pi` (an sh script) sits next to `pi.cmd` — the
	// extensionless file must never win
	for _, name := range []string{"pi", "pi.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := findOnPath("pi"); got != filepath.Join(dir, "pi.cmd") {
		t.Errorf("findOnPath(pi) = %q, want pi.cmd", got)
	}
	// and .exe beats .cmd
	if err := os.WriteFile(filepath.Join(dir, "fake.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.cmd"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := findOnPath("fake"); got != filepath.Join(dir, "fake.exe") {
		t.Errorf("findOnPath(fake) = %q, want fake.exe", got)
	}
	if got := findOnPath("missing"); got != "" {
		t.Errorf("findOnPath(missing) = %q", got)
	}
}

const npmShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
)
endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@scope\pi\dist\cli.js" %*
`

func TestResolveNodeShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "pi.cmd")
	if err := os.WriteFile(shim, []byte(npmShim), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "node_modules", "@scope", "pi", "dist", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// node.exe lives next to the shim (nvm4w / npm layouts)
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	node, got, ok := resolveNodeShim(shim)
	if !ok {
		t.Fatal("npm shim not recognized")
	}
	if node != filepath.Join(dir, "node.exe") || got != script {
		t.Errorf("resolveNodeShim = %q, %q", node, got)
	}
}

func TestResolveNodeShimWithoutLocalNode(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "h.cmd")
	body := strings.ReplaceAll(npmShim, `node_modules\@scope\pi\dist\cli.js`, `h.js`)
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "h.js"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// no node.exe next to the shim; node must come from PATH
	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "node.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", nodeDir)
	node, script, ok := resolveNodeShim(shim)
	if !ok {
		t.Fatal("shim without local node not recognized")
	}
	if node != filepath.Join(nodeDir, "node.exe") || script != filepath.Join(dir, "h.js") {
		t.Errorf("resolveNodeShim = %q, %q", node, script)
	}
}

func TestResolveHarnessClaudeExe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := ResolveHarness(ClaudeSpec)
	if err != nil {
		t.Fatalf("ResolveHarness: %v", err)
	}
	if inv.Argv[0] != exe {
		t.Errorf("argv[0] = %q", inv.Argv[0])
	}
	joined := strings.Join(inv.Argv, " ")
	for _, want := range []string{"-p", "--model claude-opus-5-5", "--max-turns 40", "--output-format json", "--dangerously-skip-permissions"} {
		if !strings.Contains(joined, want) {
			t.Errorf("claude argv missing %q: %v", want, inv.Argv)
		}
	}
}

func TestResolveHarnessPiUnwrapsShim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	shim := filepath.Join(dir, "pi.cmd")
	if err := os.WriteFile(shim, []byte(npmShim), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "node_modules", "@scope", "pi", "dist", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := ResolveHarness(PiSpec)
	if err != nil {
		t.Fatalf("ResolveHarness: %v", err)
	}
	if inv.Argv[0] != filepath.Join(dir, "node.exe") || inv.Argv[1] != script {
		t.Errorf("pi argv does not run node with the shim's script: %v", inv.Argv)
	}
	joined := strings.Join(inv.Argv, " ")
	for _, want := range []string{"--mode json", "--no-session", "--model iroha/dashscope/glm-5.3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("pi argv missing %q: %v", want, inv.Argv)
		}
	}
	// the prompt never travels as an argument (stdin only)
	for _, a := range inv.Argv[2:] {
		if strings.ContainsAny(a, " \n") {
			t.Errorf("flag %q contains whitespace — it must survive a .cmd shim's quoting", a)
		}
	}
}

func TestResolveHarnessMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ResolveHarness(ClaudeSpec); err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("missing harness not reported: %v", err)
	}
}

func TestParseClaudeResult(t *testing.T) {
	stdout := `some leading noise
{"type":"result","subtype":"success","is_error":false,"num_turns":14,"duration_ms":92000,"result":"Saved the note.","session_id":"abc","total_cost_usd":0.42}
`
	out, err := ParseClaudeResult(stdout)
	if err != nil {
		t.Fatalf("ParseClaudeResult: %v", err)
	}
	if out.Steps != 14 {
		t.Errorf("steps = %d", out.Steps)
	}
	if out.FinalText != "Saved the note." {
		t.Errorf("final text = %q", out.FinalText)
	}
	if out.Err != "" {
		t.Errorf("unexpected error note: %q", out.Err)
	}
}

func TestParseClaudeResultPrettyPrinted(t *testing.T) {
	// claude -p --output-format json may pretty-print its single result
	// object across lines; the parser must still find it.
	stdout := `{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "num_turns": 7,
  "result": "Done.",
  "total_cost_usd": 0.1
}`
	out, err := ParseClaudeResult(stdout)
	if err != nil {
		t.Fatalf("ParseClaudeResult: %v", err)
	}
	if out.Steps != 7 || out.FinalText != "Done." {
		t.Errorf("steps/text = %d/%q", out.Steps, out.FinalText)
	}
}

func TestParseClaudeResultVerboseEventArray(t *testing.T) {
	// Found live (the first Task Suite run failed its preflight): with
	// --verbose, --output-format json prints one JSON array of every
	// event, the result object last.
	stdout := `[{"type":"system","subtype":"init","session_id":"s"},` +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ready"}]}},` +
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"ready"}]`
	out, err := ParseClaudeResult(stdout)
	if err != nil {
		t.Fatalf("ParseClaudeResult: %v", err)
	}
	if out.Steps != 1 || out.FinalText != "ready" || out.Err != "" {
		t.Errorf("outcome = %+v", out)
	}
}

func TestParseClaudeResultErrorResult(t *testing.T) {
	stdout := `{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":3,"result":""}`
	out, err := ParseClaudeResult(stdout)
	if err != nil {
		t.Fatalf("ParseClaudeResult: %v", err)
	}
	if out.Err == "" {
		t.Error("an is_error result must be noted")
	}
}

func TestParseClaudeResultNoResult(t *testing.T) {
	if _, err := ParseClaudeResult("anything but json\n"); err == nil {
		t.Error("no result JSON must be an error")
	}
}

const piStream = `{"type":"session","version":3,"id":"uuid","cwd":"C:\\x"}
{"type":"agent_start"}
{"type":"turn_start"}
{"type":"message_start","message":{"role":"user","content":"do the task"}}
{"type":"message_end","message":{"role":"user","content":"do the task"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"I will start."}]}}
{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"winuse list-windows"}}
{"type":"tool_execution_end","toolCallId":"call_1","result":{"content":[]},"isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolcall","id":"call_1"}]}}
{"type":"tool_execution_start","toolCallId":"call_2","toolName":"read","args":{"path":"x"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"All done."}]}}
{"type":"agent_end","messages":[]}
{"type":"agent_settled","aborted":false}
`

func TestParsePiEvents(t *testing.T) {
	out, err := ParsePiEvents(piStream)
	if err != nil {
		t.Fatalf("ParsePiEvents: %v", err)
	}
	if out.Steps != 2 {
		t.Errorf("steps = %d, want the two tool executions", out.Steps)
	}
	if out.FinalText != "All done." {
		t.Errorf("final text = %q, want the last assistant message", out.FinalText)
	}
}

func TestParsePiEventsStringContent(t *testing.T) {
	stream := `{"type":"message_end","message":{"role":"assistant","content":"plain reply"}}
{"type":"agent_settled","aborted":false}
`
	out, err := ParsePiEvents(stream)
	if err != nil {
		t.Fatalf("ParsePiEvents: %v", err)
	}
	if out.FinalText != "plain reply" {
		t.Errorf("final text = %q", out.FinalText)
	}
}

func TestParsePiEventsNoEvents(t *testing.T) {
	if _, err := ParsePiEvents(""); err == nil {
		t.Error("an empty stream must be an error")
	}
}
