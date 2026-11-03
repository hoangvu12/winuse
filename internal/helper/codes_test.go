package helper

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// failureSite is one Fail call or Error literal in the helper's source:
// its message and hint with the literal text kept and every computed part
// (a %verb, a variable, a call) as "x".
type failureSite struct {
	pos       string
	msg, hint string
	coded     bool // an Error literal that sets its Code
}

var (
	formatVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
	intVerb    = regexp.MustCompile(`%[-+# 0-9.]*d`)
	// computedWords are the words a message keeps once its computed parts
	// and a leading "<command>: " are taken out.
	commandPrefix = regexp.MustCompile(`^[a-z-]+: `)
	literalWord   = regexp.MustCompile(`[A-Za-z][A-Za-z'-]*`)
)

// computed reports whether a message is mostly computed ("x: x",
// "batch: Action 1 (x): x", "x in window 1"): the runtime classifies its
// text, which the source does not show.
func computed(msg string) bool {
	n := 0
	for _, w := range literalWord.FindAllString(commandPrefix.ReplaceAllString(msg, ""), -1) {
		if w != "x" {
			n++
		}
	}
	return n < 3
}

// literalText renders an expression's literal text: string literals
// (fmt.Sprintf's format included) with every computed part as "x", or
// "1" for a number.
func literalText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if s, err := strconv.Unquote(e.Value); err == nil {
			return formatVerb.ReplaceAllString(intVerb.ReplaceAllString(s, "1"), "x")
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return literalText(e.X) + literalText(e.Y)
		}
	case *ast.ParenExpr:
		return literalText(e.X)
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(e.Args) > 0 {
			return literalText(e.Args[0])
		}
	}
	return "x"
}

// failureSites parses the helper's source for its failure sites.
func failureSites(t *testing.T) []failureSite {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var sites []failureSite
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "Fail" && len(n.Args) == 2 {
					sites = append(sites, failureSite{pos: fset.Position(n.Pos()).String(),
						msg: literalText(n.Args[0]), hint: literalText(n.Args[1])})
				}
			case *ast.CompositeLit:
				if id, ok := n.Type.(*ast.Ident); ok && id.Name == "Error" {
					s := failureSite{pos: fset.Position(n.Pos()).String(), msg: "x", hint: "x"}
					for _, el := range n.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						switch kv.Key.(*ast.Ident).Name {
						case "Msg":
							s.msg = literalText(kv.Value)
						case "Hint":
							s.hint = literalText(kv.Value)
						case "Code":
							s.coded = true
						}
					}
					sites = append(sites, s)
				}
			}
			return true
		})
	}
	return sites
}

// TestEveryFailureSiteHasACode is ticket 07's coverage test: each helper
// failure site whose message has literal text gets its code from it
// (protocol.CodeFor) or sets one. A site whose message is mostly computed
// (err.Error(), a variable, "<command>: " and an error) is classified at
// run time by the same rules, INTERNAL when nothing matches.
func TestEveryFailureSiteHasACode(t *testing.T) {
	sites := failureSites(t)
	if len(sites) < 300 {
		t.Fatalf("found only %d failure sites; the source scan is broken", len(sites))
	}
	for _, s := range sites {
		if s.coded || computed(s.msg) {
			continue
		}
		if _, ok := protocol.CodeFor(s.msg, s.hint, 1); !ok {
			t.Errorf("%s: no code matches %q (hint %q): add a rule to protocol/codes.go or set the Error's Code", s.pos, s.msg, s.hint)
		}
	}
}

// TestEveryDeliveryPhraseIsMapped checks the phrases the helper's
// failures use for delivery (SKILL.md's result phrases) against
// protocol.DeliveryOf, at every failure site that says one.
func TestEveryDeliveryPhraseIsMapped(t *testing.T) {
	phrases := []struct {
		re       *regexp.Regexp
		delivery string
	}{
		{regexp.MustCompile(`may have (partly )?taken effect`), protocol.DeliveryUncertain},
		{regexp.MustCompile(`nothing was (sent|started|changed|added)`), protocol.DeliveryNotDelivered},
		{regexp.MustCompile(`was delivered|do not repeat`), protocol.DeliveryDeliveredUnverified},
	}
	seen := map[string]bool{}
	for _, s := range failureSites(t) {
		for _, p := range phrases {
			text := s.msg + "\n" + s.hint
			if !p.re.MatchString(text) {
				continue
			}
			seen[p.delivery] = true
			if d, _ := protocol.DeliveryOf(s.msg, s.hint); d == "" {
				t.Errorf("%s: %q (hint %q) says %s but maps to no delivery", s.pos, s.msg, s.hint, p.delivery)
			}
			break
		}
	}
	for _, p := range phrases {
		if !seen[p.delivery] {
			t.Errorf("no failure site says %s; the scan or the phrase is out of date", p.delivery)
		}
	}
}

// TestHandleStampsEveryFailure is the other half of the coverage test:
// whatever way a request fails, its Response reaches the CLI with a code;
// the site's own code wins over its message's; an Esc stop and a spent
// budget take theirs whatever the command said.
func TestHandleStampsEveryFailure(t *testing.T) {
	commands := map[string]Command{
		"stale": func(context.Context, json.RawMessage) (string, *Error) {
			return "", nothingSent(Fail("element 3 is not available in the cached Window State for window 5", "run winuse get-window-state 5 again"))
		},
		"coded": func(context.Context, json.RawMessage) (string, *Error) {
			return "", &Error{Msg: "something unusual failed", Exit: 1, Code: protocol.CodeNotFound}
		},
		"esc": func(context.Context, json.RawMessage) (string, *Error) {
			e := escStopError()
			e.Code = protocol.CodeUsage
			return "", e
		},
		"slow": func(ctx context.Context, _ json.RawMessage) (string, *Error) {
			<-ctx.Done()
			return "", Fail("element 1 is minimized", "")
		},
		"computed": func(context.Context, json.RawMessage) (string, *Error) {
			return "", Fail(errors.New("HRESULT 0x80004005").Error(), "")
		},
		"ok": echoCommand("done\n"),
	}
	s := New("0.1.0", time.Hour, 50*time.Millisecond, commands, discardLogger())
	cases := []struct {
		req                   *protocol.Request
		code, delivery, retry string
	}{
		{testRequest("stale"), protocol.CodeStaleRef, protocol.DeliveryNotDelivered, protocol.RetrySafe},
		{testRequest("coded"), protocol.CodeNotFound, "", ""},
		{testRequest("esc"), protocol.CodeStopped, protocol.DeliveryUncertain, protocol.RetryUnsafe},
		{testRequest("slow"), protocol.CodeTimeout, protocol.DeliveryUncertain, protocol.RetryUnsafe},
		{testRequest("computed"), protocol.CodeInternal, "", ""},
		{testRequest("nope"), protocol.CodeUsage, protocol.DeliveryNotDelivered, protocol.RetrySafe},
		{&protocol.Request{V: protocol.Version + 1, Ver: "0.1.0", Cmd: "ok"}, protocol.CodeInternal, "", ""},
		{&protocol.Request{V: protocol.Version, Ver: "9.9.9", Cmd: "ok"}, protocol.CodeInternal, "", ""},
	}
	for _, tc := range cases {
		resp := s.handle(tc.req)
		if resp.OK {
			t.Fatalf("%s: OK, want a failure", tc.req.Cmd)
		}
		if resp.Code != tc.code || resp.Delivery != tc.delivery || resp.Retry != tc.retry {
			t.Errorf("%s (%q): code %q delivery %q retry %q, want %q %q %q",
				tc.req.Cmd, resp.Err, resp.Code, resp.Delivery, resp.Retry, tc.code, tc.delivery, tc.retry)
		}
	}
	if resp := s.handle(testRequest("ok")); !resp.OK || resp.Code != "" || resp.Delivery != "" || resp.Retry != "" {
		t.Errorf("a success carries no code: %+v", resp)
	}
}
