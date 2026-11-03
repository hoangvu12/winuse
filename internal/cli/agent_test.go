package cli

// Every request names the calling Agent (multi-agent ticket 02): the
// identity internal/agent resolves goes on the wire as agent and
// agent_label, and a request with no identity carries neither.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
)

// withCallingAgent replaces the resolved Agent for one test.
func withCallingAgent(t *testing.T, id agent.Identity) {
	t.Helper()
	old := callingAgent
	callingAgent = func() agent.Identity { return id }
	t.Cleanup(func() { callingAgent = old })
}

func TestRequestNamesTheCallingAgent(t *testing.T) {
	withCallingAgent(t, agent.Identity{ID: "proc:17048:30", Label: "claude 17048"})
	req, err := Request("click", map[string]int{"index": 3})
	if err != nil {
		t.Fatal(err)
	}
	if req.Agent != "proc:17048:30" || req.AgentLabel != "claude 17048" {
		t.Errorf("request agent = %q, label %q", req.Agent, req.AgentLabel)
	}
	raw, _ := json.Marshal(req)
	if !strings.Contains(string(raw), `"agent":"proc:17048:30","agent_label":"claude 17048"`) {
		t.Errorf("wire request = %s, want agent and agent_label", raw)
	}

	withCallingAgent(t, agent.Identity{})
	req, err = Request("list-windows", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(req)
	if strings.Contains(string(raw), `"agent`) {
		t.Errorf("a request with no identity = %s, want no agent fields (the default Agent)", raw)
	}
}
