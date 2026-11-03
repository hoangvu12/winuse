package helper

// The Agent behind each request (multi-agent ticket 02, ADR 0014 Decision
// 3). The CLI names it on the wire (protocol.Request.Agent, resolved by
// internal/agent); the server puts it on the command's context, where
// later tickets read it for the per-Agent Window States, cursors and
// lanes (AgentOf). A request with no identity belongs to the shared
// default Agent, as every request did before. The helper also remembers
// which Agents it served, for `winuse status`.

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/status"
)

// agentKey is the context key of the request's Agent.
type agentKey struct{}

// requestAgent is the Agent a request names, or the default Agent.
func requestAgent(req *protocol.Request) agent.Identity {
	if req.Agent == "" {
		return agent.Identity{ID: agent.DefaultID, Label: agent.DefaultID}
	}
	label := req.AgentLabel
	if label == "" {
		label = req.Agent
	}
	return agent.Identity{ID: req.Agent, Label: label}
}

// withAgent puts the request's Agent on its context.
func withAgent(ctx context.Context, a agent.Identity) context.Context {
	return context.WithValue(ctx, agentKey{}, a)
}

// AgentOf is the Agent a command runs for: the one its request named, or
// the default Agent.
func AgentOf(ctx context.Context) agent.Identity {
	if a, ok := ctx.Value(agentKey{}).(agent.Identity); ok {
		return a
	}
	return agent.Identity{ID: agent.DefaultID, Label: agent.DefaultID}
}

// maxAgentsSeen caps the Agents the helper remembers: the oldest goes
// first.
const maxAgentsSeen = 16

// agentLog remembers the Agents the helper served since it started: each
// one's label, last command and when it came.
type agentLog struct {
	mu   sync.Mutex
	seen map[string]seenAgent
}

type seenAgent struct {
	label   string
	command string
	at      time.Time
}

func newAgentLog() *agentLog {
	return &agentLog{seen: map[string]seenAgent{}}
}

// record notes one command of an Agent.
func (l *agentLog) record(a agent.Identity, command string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen[a.ID] = seenAgent{label: a.Label, command: command, at: at}
	if len(l.seen) <= maxAgentsSeen {
		return
	}
	oldest, first := "", true
	for id, s := range l.seen {
		if first || s.at.Before(l.seen[oldest].at) {
			oldest, first = id, false
		}
	}
	delete(l.seen, oldest)
}

// list is the remembered Agents for the status report, the most recent
// first, with how long ago each last command came.
func (l *agentLog) list(now time.Time) []status.Agent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]status.Agent, 0, len(l.seen))
	for id, s := range l.seen {
		out = append(out, status.Agent{Label: s.label, ID: id, Command: s.command, Ago: now.Sub(s.at)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ago != out[j].Ago {
			return out[i].Ago < out[j].Ago
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// tracked wraps a command so that each run records its Agent.
func (s Services) tracked(name string, cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		s.Agents.record(AgentOf(ctx), name, s.Now())
		return cmd(ctx, args)
	}
}
