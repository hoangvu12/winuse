package page

// The wall check (.scratch/bladebro-ideas ticket 05) over a fake browser:
// wall.js runs alone in an isolated world of the main frame, never through
// Runtime.evaluate or Runtime.enable, and read.js and extract.js carry the
// same check (one copy, put in by read.go and extract.go). What wall.js
// finds on real pages is test/e2e's TestWallCheck.

import (
	"context"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestWallKindRunsTheWallCheckInAnIsolatedWorld(t *testing.T) {
	f := &readCaller{reply: map[string]any{"result": map[string]any{"type": "string", "value": "Cloudflare"}}}
	kind, err := WallKind(context.Background(), f, "S")
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
	if fn, _ := call["functionDeclaration"].(string); fn != wallJS {
		t.Errorf("the check sent %.80q, want wall.js as it is", fn)
	}
	for _, m := range f.calls {
		if m == "Runtime.enable" || m == "Runtime.evaluate" {
			t.Errorf("sent %s: the check runs only in its isolated world", m)
		}
	}
}

// After a Page Action that navigated, the check runs once the new document
// has its body: the watch saw the commit, when the body may still be on
// its way (bladebro-ideas 05).
func TestWallKindOnceLoadedWaitsForTheBody(t *testing.T) {
	f := &readCaller{reply: map[string]any{"result": map[string]any{"type": "string", "value": "DataDome"}}}
	kind, err := WallKindOnceLoaded(context.Background(), f, "S")
	if err != nil || kind != "DataDome" {
		t.Fatalf("got %q, %v; want DataDome", kind, err)
	}
	call := f.params["Runtime.callFunctionOn"]
	fn, _ := call["functionDeclaration"].(string)
	if call["awaitPromise"] != true || !strings.Contains(fn, wallJS) || !strings.Contains(fn, "DOMContentLoaded") {
		t.Errorf("the check ran with %v, want wall.js awaited until DOMContentLoaded", call)
	}
}

func TestWallKindOnAnOrdinaryPageOrAFailure(t *testing.T) {
	f := &readCaller{reply: map[string]any{"result": map[string]any{"type": "string", "value": ""}}}
	if kind, err := WallKind(context.Background(), f, "S"); err != nil || kind != "" {
		t.Errorf("an ordinary page: %q, %v", kind, err)
	}
	f = &readCaller{reply: map[string]any{"result": map[string]any{"type": "object"},
		"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: boom"}}}}
	if kind, err := WallKind(context.Background(), f, "S"); err == nil || kind != "" || !strings.Contains(err.Error(), "TypeError: boom") {
		t.Errorf("a throw: %q, %v", kind, err)
	}
	// Anything but a string names no wall.
	f = &readCaller{reply: map[string]any{"result": map[string]any{"type": "boolean", "value": true}}}
	if kind, _ := WallKind(context.Background(), f, "S"); kind != "" {
		t.Errorf("a boolean: %q, want none", kind)
	}
}

func TestReadAndExtractCarryTheSameWallCheck(t *testing.T) {
	for name, js := range map[string]string{"read.js": readMarkdownJS, "extract.js": extractJS} {
		if !strings.Contains(js, wallJS) {
			t.Errorf("%s does not carry wall.js", name)
		}
		if strings.Contains(js, wallMarker) {
			t.Errorf("%s still has the marker wall.js replaces", name)
		}
		if strings.Count(js, "winuse:wall */") != 1 {
			t.Errorf("%s carries wall.js more than once", name)
		}
		if strings.Contains(js, "'rate-limit'") && strings.Count(js, "'rate-limit'") != strings.Count(wallJS, "'rate-limit'") {
			t.Errorf("%s has a wall check of its own beside wall.js", name)
		}
	}
	if !strings.Contains(wallJS, "Apache License 2.0") || !strings.Contains(wallJS, "detect_block.js") {
		t.Error("wall.js lost its attribution")
	}
	for _, kind := range protocol.WallKinds {
		if !strings.Contains(wallJS, "'"+kind+"'") {
			t.Errorf("wall.js never names %s", kind)
		}
	}
}
