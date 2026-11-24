package page

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// readCaller is a fake browser for ReadMarkdown: it answers the frame tree,
// the isolated world, the node and the call, and records what was sent.
type readCaller struct {
	calls   []string
	params  map[string]map[string]any
	resolve error // DOM.resolveNode's failure
	reply   any   // Runtime.callFunctionOn's answer
}

func (f *readCaller) CallSession(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	var p map[string]any
	b, _ := json.Marshal(params)
	json.Unmarshal(b, &p)
	if f.params == nil {
		f.params = map[string]map[string]any{}
	}
	f.params[method] = p
	var out any
	switch method {
	case "Page.getFrameTree":
		out = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "MAIN", "loaderId": "L1"}}}
	case "Page.createIsolatedWorld":
		out = map[string]any{"executionContextId": 42}
	case "DOM.resolveNode":
		if f.resolve != nil {
			return nil, f.resolve
		}
		out = map[string]any{"object": map[string]any{"objectId": "obj-1"}}
	case "Runtime.callFunctionOn":
		out = f.reply
	}
	return json.Marshal(out)
}

func mdReply(md map[string]any) any {
	return map[string]any{"result": map[string]any{"type": "object", "value": md}}
}

func TestReadMarkdownRunsInAnIsolatedWorldOfTheMainFrame(t *testing.T) {
	f := &readCaller{reply: mdReply(map[string]any{"markdown": "# Hi", "main": "article#story", "title": "Hi", "url": "https://a.test/"})}
	md, err := ReadMarkdown(context.Background(), f, "S", "", 0, ReadOptions{})
	if err != nil || md.Text != "# Hi" || md.Main != "article#story" || md.Scoped {
		t.Fatalf("got %+v, %v", md, err)
	}
	if got := f.params["Page.createIsolatedWorld"]["frameId"]; got != "MAIN" {
		t.Errorf("isolated world in frame %v, want the main frame", got)
	}
	call := f.params["Runtime.callFunctionOn"]
	if call["executionContextId"] != float64(42) || call["objectId"] != nil {
		t.Errorf("the call ran in %v (object %v), want the isolated world 42", call["executionContextId"], call["objectId"])
	}
	if !strings.Contains(call["functionDeclaration"].(string), "Apache License 2.0") {
		t.Error("read.js lost its attribution")
	}
	if opts := call["arguments"].([]any)[0].(map[string]any)["value"].(map[string]any); opts["urls"] != "short" {
		t.Errorf("urls %v, want short", opts["urls"])
	}
	for _, m := range f.calls {
		if m == "Runtime.enable" || m == "Runtime.evaluate" {
			t.Errorf("sent %s: read runs only in its isolated world", m)
		}
	}
}

func TestReadMarkdownOnANodeResolvesItInTheIsolatedWorld(t *testing.T) {
	f := &readCaller{reply: mdReply(map[string]any{"markdown": "| a |", "main": "table", "scoped": true})}
	md, err := ReadMarkdown(context.Background(), f, "S", "F2", 77, ReadOptions{FullURLs: true})
	if err != nil || !md.Scoped {
		t.Fatalf("got %+v, %v", md, err)
	}
	if f.params["Page.createIsolatedWorld"]["frameId"] != "F2" {
		t.Errorf("isolated world in %v, want the node's frame F2", f.params["Page.createIsolatedWorld"]["frameId"])
	}
	rn := f.params["DOM.resolveNode"]
	if rn["backendNodeId"] != float64(77) || rn["executionContextId"] != float64(42) {
		t.Errorf("resolveNode %v, want node 77 in world 42", rn)
	}
	call := f.params["Runtime.callFunctionOn"]
	if call["objectId"] != "obj-1" {
		t.Errorf("called on %v, want the node", call["objectId"])
	}
	if opts := call["arguments"].([]any)[0].(map[string]any)["value"].(map[string]any); opts["urls"] != "full" {
		t.Errorf("urls %v, want full", opts["urls"])
	}
	if f.calls[len(f.calls)-1] != "Runtime.releaseObject" {
		t.Errorf("the node was not released: %v", f.calls)
	}
}

func TestReadMarkdownFailures(t *testing.T) {
	f := &readCaller{resolve: &cdp.Error{Code: -32000, Message: "No node with given id found"}}
	if _, err := ReadMarkdown(context.Background(), f, "S", "F1", 9, ReadOptions{}); !errors.Is(err, ErrStale) {
		t.Errorf("a gone node: %v, want ErrStale", err)
	}
	f = &readCaller{reply: map[string]any{"result": map[string]any{"type": "object"},
		"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: boom"}}}}
	if _, err := ReadMarkdown(context.Background(), f, "S", "", 0, ReadOptions{}); err == nil || !strings.Contains(err.Error(), "TypeError: boom") {
		t.Errorf("a throw: %v", err)
	}
}
