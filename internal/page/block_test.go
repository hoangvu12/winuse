package page

// The wall check (.scratch/bladebro-ideas ticket 05) over a fake browser:
// block.js runs alone in an isolated world of the main frame, never through
// Runtime.evaluate or Runtime.enable, and read.js and extract.js carry the
// same check (one copy, put in by read.go and extract.go). What block.js
// finds on real pages is test/e2e's TestWallCheck.

import (
	"context"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestBlockKindRunsTheWallCheckInAnIsolatedWorld(t *testing.T) {
	f := &readCaller{reply: map[string]any{"result": map[string]any{"type": "string", "value": "Cloudflare"}}}
	kind, err := BlockKind(context.Background(), f, "S")
	if err != nil || kind != "Cloudflare" {
		t.Fatalf("got %q, %v; want Cloudflare", kind, err)
	}
	if got := f.params["Page.createIsolatedWorld"]["frameId"]; got != "MAIN" {
		t.Errorf("isolated world in frame %v, want the main frame", got)
	}
	call := f.params["Runtime.callFunctionOn"]
	if call["executionContextId"] != float64(42) || call["objectId"] != nil || call["returnByValue"] != true {
		t.Errorf("the check ran with %v, want by value in the isolated world 42", call)
	}
	if fn, _ := call["functionDeclaration"].(string); fn != blockJS {
		t.Errorf("the check sent %.80q, want block.js as it is", fn)
	}
	for _, m := range f.calls {
		if m == "Runtime.enable" || m == "Runtime.evaluate" {
			t.Errorf("sent %s: the check runs only in its isolated world", m)
		}
	}
}

func TestBlockKindOnAnOrdinaryPageOrAFailure(t *testing.T) {
	f := &readCaller{reply: map[string]any{"result": map[string]any{"type": "string", "value": ""}}}
	if kind, err := BlockKind(context.Background(), f, "S"); err != nil || kind != "" {
		t.Errorf("an ordinary page: %q, %v", kind, err)
	}
	f = &readCaller{reply: map[string]any{"result": map[string]any{"type": "object"},
		"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: boom"}}}}
	if kind, err := BlockKind(context.Background(), f, "S"); err == nil || kind != "" || !strings.Contains(err.Error(), "TypeError: boom") {
		t.Errorf("a throw: %q, %v", kind, err)
	}
	// Anything but a string names no wall.
	f = &readCaller{reply: map[string]any{"result": map[string]any{"type": "boolean", "value": true}}}
	if kind, _ := BlockKind(context.Background(), f, "S"); kind != "" {
		t.Errorf("a boolean: %q, want none", kind)
	}
}

func TestReadAndExtractCarryTheSameWallCheck(t *testing.T) {
	for name, js := range map[string]string{"read.js": readMarkdownJS, "extract.js": extractJS} {
		if !strings.Contains(js, blockJS) {
			t.Errorf("%s does not carry block.js", name)
		}
		if strings.Contains(js, blockMarker) {
			t.Errorf("%s still has the marker block.js replaces", name)
		}
		if strings.Count(js, "winuse:block */") != 1 {
			t.Errorf("%s carries block.js more than once", name)
		}
		if strings.Contains(js, "'rate-limit'") && strings.Count(js, "'rate-limit'") != strings.Count(blockJS, "'rate-limit'") {
			t.Errorf("%s has a wall check of its own beside block.js", name)
		}
	}
	if !strings.Contains(blockJS, "Apache License 2.0") || !strings.Contains(blockJS, "detect_block.js") {
		t.Error("block.js lost its attribution")
	}
	for _, kind := range protocol.WallKinds {
		if !strings.Contains(blockJS, "'"+kind+"'") {
			t.Errorf("block.js never names %s", kind)
		}
	}
}
