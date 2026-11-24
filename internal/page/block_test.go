package page

// The wall check (.scratch/bladebro-ideas ticket 05) over a fake browser:
// block.js runs alone in an isolated world of the main frame, never through
// Runtime.evaluate or Runtime.enable, and read.js carries the same check
// (one copy, put in by read.go). What block.js finds on real pages is
// test/e2e's TestWallCheck.

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

func TestReadCarriesTheSameWallCheck(t *testing.T) {
	if !strings.Contains(readMarkdownJS, blockJS) {
		t.Error("read.js does not carry block.js")
	}
	if strings.Contains(readMarkdownJS, blockMarker) {
		t.Error("read.js still has the marker block.js replaces")
	}
	if strings.Count(readMarkdownJS, "winuse:block */") != 1 {
		t.Error("read.js carries block.js more than once, or a second wall check")
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
