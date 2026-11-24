package helper

// `winuse read` (bladebro-ideas ticket 25) over the fake launched Chrome
// (winRig) and 05's Page Elements (pageRig): the Markdown with its header,
// --urls, the cut line, --out, the wall note, --root on a Page Element,
// and the refusals (a native Window, a UIA Element). No browser runs.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// readPage answers read.js's call with md, and records its options and
// where it ran.
type readPage struct {
	md      map[string]any
	opts    []map[string]any
	objects []string
}

func (p *readPage) handle(s *cdptest.Server, other func(fn string) any) {
	s.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"executionContextId": 9}, nil
	})
	s.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var call struct {
			Fn       string `json:"functionDeclaration"`
			ObjectID string `json:"objectId"`
			Args     []struct {
				Value map[string]any `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &call)
		if !strings.Contains(call.Fn, "bladebro") {
			return map[string]any{"result": map[string]any{"type": "object", "value": other(call.Fn)}}, nil
		}
		p.opts = append(p.opts, call.Args[0].Value)
		p.objects = append(p.objects, call.ObjectID)
		return map[string]any{"result": map[string]any{"type": "object", "value": p.md}}, nil
	})
}

func TestReadPrintsThePagesMainContent(t *testing.T) {
	r := newWinRig(t)
	p := &readPage{md: map[string]any{"markdown": "# Docs\n\nSee [the guide](/guide).", "main": "article#story", "title": "Docs", "url": "https://docs.test/"}}
	p.handle(r.browser.Srv, func(string) any { return nil })

	out, e := r.run(t, "read", protocol.ReadArgs{Window: "301"})
	if e != nil {
		t.Fatalf("read: %+v", e)
	}
	want := "Page: Docs - https://docs.test/ (main content: article#story; --root <element> reads another part)\n\n# Docs\n\nSee [the guide](/guide).\n"
	if out != want {
		t.Errorf("read printed\n%q\nwant\n%q", out, want)
	}
	if len(p.opts) != 1 || p.opts[0]["urls"] != "short" || p.objects[0] != "" {
		t.Errorf("read.js ran with %v on %v, want short URLs on the document", p.opts, p.objects)
	}
	if strings.Contains(out, "[0]") || len(r.cursor.labels) != 0 {
		t.Errorf("read showed Element Indexes or the cursor: %q %v", out, r.cursor.labels)
	}

	if _, e := r.run(t, "read", protocol.ReadArgs{Window: "301", URLs: "full"}); e != nil || p.opts[1]["urls"] != "full" {
		t.Errorf("--urls full: %v %+v", p.opts, e)
	}
	if _, e := r.run(t, "read", protocol.ReadArgs{Window: "301", URLs: "long"}); e == nil || !strings.Contains(e.Msg, "--urls") {
		t.Errorf("--urls long: %+v", e)
	}
	r.passiveOnly(t)
}

func TestReadCutsAtMaxCharsAndSaysSo(t *testing.T) {
	r := newWinRig(t)
	body := strings.Repeat("word ", 100) // 500 characters
	p := &readPage{md: map[string]any{"markdown": body, "main": "main", "title": "Docs", "url": "https://docs.test/"}}
	p.handle(r.browser.Srv, func(string) any { return nil })

	n := 120
	out, e := r.run(t, "read", protocol.ReadArgs{Window: "301", MaxChars: &n})
	if e != nil {
		t.Fatalf("read: %+v", e)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if last != "(cut at 120 of 500 characters; --max-chars raises the cut, 0 lifts it, --out <file> writes it whole)" {
		t.Errorf("the cut line is %q", last)
	}
	if got := lines[2]; got != strings.TrimSpace(body[:120]) {
		t.Errorf("the Markdown printed is %q", got)
	}

	// The default cap is the other reads' (28000 characters).
	p.md["markdown"] = strings.Repeat("x", defaultMaxChars+10)
	out, _ = r.run(t, "read", protocol.ReadArgs{Window: "301"})
	if !strings.Contains(out, "(cut at 28000 of 28010 characters") {
		t.Errorf("no default cut: %d characters", len(out))
	}
	zero := 0
	out, _ = r.run(t, "read", protocol.ReadArgs{Window: "301", MaxChars: &zero})
	if strings.Contains(out, "cut at") {
		t.Error("--max-chars 0 still cut")
	}
}

func TestReadOutWritesTheWholeMarkdown(t *testing.T) {
	r := newWinRig(t)
	body := strings.Repeat("word ", 10000)
	p := &readPage{md: map[string]any{"markdown": body, "main": "main", "title": "Docs", "url": "https://docs.test/", "block": "Cloudflare"}}
	p.handle(r.browser.Srv, func(string) any { return nil })
	file := filepath.Join(t.TempDir(), "page.md")

	out, e := r.run(t, "read", protocol.ReadArgs{Window: "301", Out: file})
	if e != nil {
		t.Fatalf("read --out: %+v", e)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != body+"\n" {
		t.Fatalf("the file holds %d characters, %v", len(data), err)
	}
	want := "Page: Docs - https://docs.test/ (main content: main; --root <element> reads another part)\n" +
		"Markdown: " + file + " (50000 characters)\n" +
		"Note: the Page looks like a Cloudflare wall; ask the user to handle it\n"
	if out != want {
		t.Errorf("read --out printed\n%q\nwant\n%q", out, want)
	}
	n := 10
	if _, e := r.run(t, "read", protocol.ReadArgs{Window: "301", Out: file, MaxChars: &n}); e == nil {
		t.Error("--out with --max-chars was accepted")
	}
}

func TestReadRefusesANativeWindow(t *testing.T) {
	r := newWinRig(t)
	_, e := r.run(t, "read", protocol.ReadArgs{Window: "101"})
	if e == nil || !strings.Contains(e.Msg, "window 101 is not a browser Window") || !strings.Contains(e.Hint, "winuse get-window-state 101") {
		t.Errorf("a native Window: %+v", e)
	}
}

func TestReadRootRendersAPageElement(t *testing.T) {
	r := newPageRig(t)
	p := &readPage{md: map[string]any{"markdown": "- one\n- two", "main": "ul", "scoped": true}}
	p.handle(r.srv, func(fn string) any {
		if strings.Contains(fn, "isConnected") {
			return true
		}
		return nil
	})
	var resolved []map[string]any
	r.srv.Handle("DOM.resolveNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		resolved = append(resolved, p)
		return map[string]any{"object": map[string]any{"type": "object", "objectId": "node-obj"}}, nil
	})

	out, e := r.run(t, "read", protocol.ReadArgs{Window: "301", Root: "8"})
	if e != nil {
		t.Fatalf("read --root 8: %+v", e)
	}
	if out != "Element 8 (ul):\n\n- one\n- two\n" {
		t.Errorf("read --root printed %q", out)
	}
	last := resolved[len(resolved)-1]
	if last["backendNodeId"] != float64(80) || last["executionContextId"] != float64(9) || p.objects[0] != "node-obj" {
		t.Errorf("read.js ran on %v / %v, want node 80 in the isolated world", last, p.objects)
	}
	r.noForeground(t)

	// A UIA Element of the frame has no Page subtree.
	_, e = r.run(t, "read", protocol.ReadArgs{Window: "301", Root: "2"})
	if e == nil || !strings.Contains(e.Msg, "element 2 is not in the Page") || !strings.Contains(e.Hint, "get-window-state 301 --root 2") {
		t.Errorf("a UIA Element: %+v", e)
	}
	if _, e = r.run(t, "read", protocol.ReadArgs{Window: "301", Root: "row 3"}); e == nil || !strings.Contains(e.Msg, "not an Element Index") {
		t.Errorf("--root \"row 3\": %+v", e)
	}
}
