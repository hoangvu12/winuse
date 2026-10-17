package helper

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestCommandsRegistryHasDiscoveryCommands(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"list-windows", "list-apps", "launch-app"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
}

func TestListAppsRejectsArguments(t *testing.T) {
	// Garbage arguments are refused before touching the desktop.
	out, cmdErr := Commands()["list-apps"](context.Background(), json.RawMessage(`{"unexpected":1}`))
	if out != "" || cmdErr == nil {
		t.Fatalf("want refusal for non-empty args, got %q, %v", out, cmdErr)
	}
	if cmdErr.Msg != "list-apps takes no arguments" {
		t.Fatalf("error message: %q", cmdErr.Msg)
	}
}

func TestLaunchAppRequiresApp(t *testing.T) {
	// The app argument is validated before any process starts.
	cmds := Commands()
	for _, args := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"app":""}`), json.RawMessage(`{"app":"   "}`)} {
		out, cmdErr := cmds["launch-app"](context.Background(), args)
		if out != "" || cmdErr == nil {
			t.Fatalf("args %s: want refusal, got %q, %v", args, out, cmdErr)
		}
		if cmdErr.Msg != "app is required" {
			t.Fatalf("error message for args %s: %q", args, cmdErr.Msg)
		}
	}
	// Malformed JSON arguments are refused too, not panicked on.
	out, cmdErr := cmds["launch-app"](context.Background(), json.RawMessage(`not json`))
	if out != "" || cmdErr == nil || cmdErr.Msg != "app is required" {
		t.Fatalf("malformed args: got %q, %v", out, cmdErr)
	}
}

func TestLaunchPollBudgetStaysUnderRequestBudget(t *testing.T) {
	// The specific "did not expose a targetable window" failure must be
	// reported, not the generic request timeout: the poll budget stays
	// under launch-app's request budget.
	if launchPollBudget >= protocol.LaunchTimeout {
		t.Fatalf("poll budget %s must stay under %s", launchPollBudget, protocol.LaunchTimeout)
	}
}
