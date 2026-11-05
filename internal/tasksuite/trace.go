package tasksuite

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The kinds a winuse command's time is summed under: what the agent
// spent reading the desktop, acting on it, and waiting on it. The rest
// (status, skill, version, the browser connection's plumbing) is other.
const (
	KindObserve = "observe"
	KindAct     = "act"
	KindWait    = "wait"
	KindOther   = "other"
)

// TraceKinds is the fixed order the kinds are printed in.
var TraceKinds = []string{KindObserve, KindAct, KindWait, KindOther}

// observeCommands only read: Windows, Elements, Tabs, displays, the
// clipboard and the notifications. A command a trace line names is its
// wire name (the CLI's name for one that never reached the helper).
var observeCommands = map[string]bool{
	"list-windows":       true,
	"get-window":         true,
	"get-window-state":   true,
	"find":               true,
	"record-tree":        true,
	"zoom":               true,
	"list-apps":          true,
	"tab-list":           true,
	"list-displays":      true,
	"screenshot":         true,
	"clipboard-get":      true,
	"list-notifications": true,
}

// otherCommands neither observe nor act on the desktop.
var otherCommands = map[string]bool{
	"status":  true,
	"version": true,
	"skill":   true,
	"browser": true, // a CLI name only: its subcommands reach the helper as browser-*
}

// CommandKind is the kind a trace line's command is summed under.
// Everything not listed acts: the Actions, the Batch, the Window
// Actions, launching and closing Apps, the Page's navigation and eval,
// the tab and clipboard changes, the notifications' buttons.
func CommandKind(cmd string) string {
	switch {
	case cmd == "wait":
		return KindWait
	case observeCommands[cmd]:
		return KindObserve
	case otherCommands[cmd], strings.HasPrefix(cmd, "browser-"):
		return KindOther
	default:
		return KindAct
	}
}

// KindTotal is the commands of one kind in a trace and their summed
// durations.
type KindTotal struct {
	Commands int   `json:"commands"`
	MS       int64 `json:"ms"`
}

// TraceSummary is what one task run's winuse trace says about the
// agent's use of winuse: how many commands of each kind, how long they
// took (each command from its start to its exit, process start
// excluded), how many failed and with which codes, and how many took the
// foreground.
type TraceSummary struct {
	Commands int                  `json:"commands"`
	Actions  int                  `json:"actions"` // the act commands, a Batch counted as its steps
	Failed   int                  `json:"failed"`
	MS       int64                `json:"ms"`
	Kinds    map[string]KindTotal `json:"kinds"`
	Codes    map[string]int       `json:"codes,omitempty"` // failure codes (STALE_REF, ...) by count
	Headed   int                  `json:"headed"`          // commands that took a Headed route
	Skipped  int                  `json:"skipped,omitempty"`
}

// Kind returns one kind's total (zero when the trace has none).
func (s TraceSummary) Kind(kind string) KindTotal { return s.Kinds[kind] }

// traceRecord is the part of a winuse trace line (internal/cli/trace.go)
// the summary reads.
type traceRecord struct {
	Command    string          `json:"command"`
	Args       json.RawMessage `json:"args"`
	Routes     []string        `json:"routes"`
	OK         bool            `json:"ok"`
	Code       string          `json:"code"`
	DurationMS int64           `json:"duration_ms"`
}

// ParseTrace sums a winuse trace (one JSON line per command). Lines that
// are not trace lines are counted as skipped, never an error: the trace
// is evidence, and a torn last line (the agent killed mid-write) must not
// lose the rest.
func ParseTrace(r io.Reader) (TraceSummary, error) {
	sum := TraceSummary{Kinds: map[string]KindTotal{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec traceRecord
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Command == "" {
			sum.Skipped++
			continue
		}
		sum.add(rec)
	}
	return sum, sc.Err()
}

func (s *TraceSummary) add(rec traceRecord) {
	kind := CommandKind(rec.Command)
	k := s.Kinds[kind]
	k.Commands++
	k.MS += rec.DurationMS
	s.Kinds[kind] = k
	s.Commands++
	s.MS += rec.DurationMS
	if kind == KindAct {
		s.Actions += batchSteps(rec)
	}
	if !rec.OK {
		s.Failed++
		if rec.Code != "" {
			if s.Codes == nil {
				s.Codes = map[string]int{}
			}
			s.Codes[rec.Code]++
		}
	}
	for _, r := range rec.Routes {
		if strings.EqualFold(r, "Headed") {
			s.Headed++
			break
		}
	}
}

// batchSteps is how many Actions one act line stands for: a Batch's
// steps, one otherwise.
func batchSteps(rec traceRecord) int {
	if rec.Command != "batch" || len(rec.Args) == 0 {
		return 1
	}
	var args struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(rec.Args, &args) != nil || len(args.Steps) == 0 {
		return 1
	}
	return len(args.Steps)
}

// ReadTraceFiles sums a task's trace file together with the files it
// rolled over into (<name>.1<ext>, <name>.2<ext>, ...: winuse renames a
// full trace and starts a new one). A missing trace is an empty summary:
// the agent ran no winuse command. ok is false then.
func ReadTraceFiles(path string) (sum TraceSummary, ok bool, err error) {
	sum = TraceSummary{Kinds: map[string]KindTotal{}}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	files := []string{}
	for i := 1; ; i++ {
		rolled := fmt.Sprintf("%s.%d%s", base, i, ext)
		if _, err := os.Stat(rolled); err != nil {
			break
		}
		files = append(files, rolled)
	}
	files = append(files, path)
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return sum, ok, err
		}
		part, perr := ParseTrace(fh)
		fh.Close()
		if perr != nil {
			return sum, ok, perr
		}
		ok = true
		sum.merge(part)
	}
	return sum, ok, nil
}

func (s *TraceSummary) merge(o TraceSummary) {
	s.Commands += o.Commands
	s.Actions += o.Actions
	s.Failed += o.Failed
	s.MS += o.MS
	s.Headed += o.Headed
	s.Skipped += o.Skipped
	for k, v := range o.Kinds {
		t := s.Kinds[k]
		t.Commands += v.Commands
		t.MS += v.MS
		s.Kinds[k] = t
	}
	for c, n := range o.Codes {
		if s.Codes == nil {
			s.Codes = map[string]int{}
		}
		s.Codes[c] += n
	}
}
