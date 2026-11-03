// Package agent resolves which Agent a winuse command comes from
// (multi-agent ticket 02, ADR 0014 Decision 3). Several Agents can drive
// one helper at once; each request names its Agent so the helper can keep
// their Window States, cursors and lanes apart (later tickets).
//
// The identity is automatic: the CLI walks up its parent processes to the
// first one that is not a shell or a winuse binary, which is the Harness
// that ran the command (Claude Code, Codex, ...), and uses that process's
// id and start time. Each Harness session is its own process, so it is
// its own Agent. WINUSE_AGENT=<name> overrides it, for several Agents
// under one Harness process (a session's sub-agents), and labels the
// Agent Cursor. With neither, the request carries no identity and the
// helper files it under the shared default Agent, as before.
//
// Measured on the PC (2026-11-03): a command run by Claude Code's Bash
// tool has the parents powershell.exe, bash.exe (three), claude.exe; one
// run by its PowerShell tool has powershell.exe, cmd.exe, claude.exe.
// Both resolve to claude.exe.
package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// EnvVar names the override: the Agent's name, which is also its label.
const EnvVar = "WINUSE_AGENT"

// DefaultID is the helper's name for requests that carry no identity: one
// shared Agent, today's behaviour.
const DefaultID = "default"

// maxName caps an Agent name from WINUSE_AGENT, in runes.
const maxName = 64

// Identity is one Agent as a request carries it: ID tells Agents apart,
// Label is what the Agent Cursor and messages show. The zero value is no
// identity (the default Agent).
type Identity struct {
	ID    string
	Label string
}

// Proc is one process as the walk sees it.
type Proc struct {
	PID    uint32
	Parent uint32
	Exe    string // the executable's file name, such as "claude.exe"
}

// skipped are the processes between a Harness and winuse: shells and
// console hosts, and winuse itself. Lowercase, without ".exe".
var skipped = map[string]bool{
	"cmd": true, "powershell": true, "pwsh": true,
	"bash": true, "sh": true, "zsh": true, "dash": true,
	"conhost": true, "openconsole": true,
	"winuse": true,
}

// Skipped reports whether exe is a shell, a console host or winuse: a
// process the walk passes over.
func Skipped(exe string) bool {
	return skipped[exeBase(exe)]
}

// exeBase is an executable's file name in lowercase without ".exe".
func exeBase(exe string) string {
	return strings.TrimSuffix(strings.ToLower(exe), ".exe")
}

// FromEnv reads the override: the name, trimmed and cut to 64 runes, as
// both ID and label. An empty name, or one with control characters, is no
// override.
func FromEnv(value string) (Identity, bool) {
	name := strings.TrimSpace(value)
	if name == "" || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Identity{}, false
	}
	if r := []rune(name); len(r) > maxName {
		name = string(r[:maxName])
	}
	return Identity{ID: "name:" + name, Label: name}, true
}

// Walk finds the Agent's process: from self, up the parents, the first
// process that is not Skipped. created reads a process's start
// time (0 when unknown); a parent that started after its child is a
// reused process id, and the walk stops there with no identity, as it does
// at a missing parent or after 32 steps.
func Walk(self uint32, procs map[uint32]Proc, created func(pid uint32) int64) (Proc, int64, bool) {
	p, ok := procs[self]
	if !ok {
		return Proc{}, 0, false
	}
	childStart := created(self)
	for range 32 {
		parent, ok := procs[p.Parent]
		if !ok || p.Parent == 0 || p.Parent == p.PID {
			return Proc{}, 0, false
		}
		start := created(parent.PID)
		if start != 0 && childStart != 0 && start > childStart {
			return Proc{}, 0, false
		}
		if !Skipped(parent.Exe) {
			return parent, start, true
		}
		p, childStart = parent, start
	}
	return Proc{}, 0, false
}

// FromProcess is the automatic identity of the Agent's process: its id and
// start time tell it apart from a later process that reuses the id; the
// label is its name and id, such as "claude 17048".
func FromProcess(p Proc, start int64) Identity {
	return Identity{
		ID:    fmt.Sprintf("proc:%d:%d", p.PID, start),
		Label: fmt.Sprintf("%s %d", exeBase(p.Exe), p.PID),
	}
}

// Resolve is the calling Agent: WINUSE_AGENT when set, else the walk from
// this process, else no identity.
func Resolve() Identity {
	if id, ok := FromEnv(os.Getenv(EnvVar)); ok {
		return id
	}
	procs, err := snapshot()
	if err != nil {
		return Identity{}
	}
	p, start, ok := Walk(uint32(os.Getpid()), procs, processStart)
	if !ok {
		return Identity{}
	}
	return FromProcess(p, start)
}

// contextKey is the context key of a request's Agent.
type contextKey struct{}

// NewContext returns ctx carrying the Agent a request comes from.
func NewContext(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext is the Agent a request comes from, or the default Agent
// when ctx carries none.
func FromContext(ctx context.Context) Identity {
	if id, ok := ctx.Value(contextKey{}).(Identity); ok && id.ID != "" {
		return id
	}
	return Identity{ID: DefaultID, Label: DefaultID}
}

// Key is the store key of the Agent a request comes from: its ID, or
// DefaultID. Every per-Agent store (the Window States, the screenshot
// mappings) keys its entries by it.
func Key(ctx context.Context) string {
	return FromContext(ctx).ID
}
