package page

// Extract is `winuse extract` on a Page (ticket 26): extract.js, ported
// from bladebro (Apache-2.0, third_party/bladebro), run in winuse's own
// isolated world of the frame, so the Page's scripts never see it and it
// never sees theirs. It needs no Runtime.enable: Page.createIsolatedWorld
// hands back the world's context id, and Runtime.evaluate (the whole Page)
// or DOM.resolveNode plus Runtime.callFunctionOn (a --root node) run in it.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/hoangvu12/winuse/internal/extract"
)

//go:embed extract.js
var extractSource string

// extractJS is extract.js with block.js, the shared wall check, in it.
var extractJS = withBlockCheck(extractSource)

// extractWorld is the isolated world extract.js runs in.
const extractWorld = "winuse-extract"

// ExtractLimit is --limit's default on a Page, and ExtractMaxLimit its
// ceiling.
const (
	ExtractLimit    = 50
	ExtractMaxLimit = 1000
)

// ExtractOptions are extract's flags: As is "" (auto), "list", "table" or
// "jsonld"; Limit is the most rows printed (0: ExtractLimit).
type ExtractOptions struct {
	As    string `json:"as,omitempty"`
	Limit int    `json:"limit"`
}

// ExtractModes are --as's values.
var ExtractModes = []string{"list", "table", "jsonld"}

// Extract runs extract.js in the Page of session (the Tab's), or with root
// on that node (its own session and frame: an iframe's node runs in that
// frame), and returns what it found.
func Extract(ctx context.Context, c Caller, session string, root *Origin, o ExtractOptions) (extract.Result, error) {
	if o.Limit <= 0 {
		o.Limit = ExtractLimit
	}
	args, _ := json.Marshal(o)
	var res json.RawMessage
	var err error
	if root != nil {
		var world int
		if world, err = isolatedWorld(ctx, c, root.SessionID, root.FrameID, extractWorld); err != nil {
			return extract.Result{}, err
		}
		res, err = callIn(ctx, c, root.SessionID, world, root.BackendNodeID, extractJS, o)
	} else {
		var world int
		if world, err = isolatedWorld(ctx, c, session, "", extractWorld); err != nil {
			return extract.Result{}, err
		}
		res, err = c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{
			"expression": "(" + extractJS + ").call(null, " + string(args) + ")",
			"contextId":  world, "returnByValue": true, "silent": true,
		})
		if err == nil {
			res, err = resultValue(res)
		}
	}
	if err != nil {
		return extract.Result{}, staleOr(err)
	}
	var got struct {
		Kind      string            `json:"kind"`
		Container string            `json:"container"`
		Label     string            `json:"label"`
		Rows      []json.RawMessage `json:"rows"`
		Found     int               `json:"found"`
		AtLeast   bool              `json:"at_least"`
		PageSays  *extract.Claim    `json:"page_says"`
		Wall      string            `json:"wall"`
	}
	if err := json.Unmarshal(res, &got); err != nil {
		return extract.Result{}, fmt.Errorf("reading extract's answer: %v", err)
	}
	r := extract.Result{Kind: got.Kind, Container: got.Container, Label: got.Label, Found: got.Found,
		AtLeast: got.AtLeast, Limit: o.Limit, PageSays: got.PageSays, Wall: got.Wall}
	for _, row := range got.Rows {
		var b bytes.Buffer
		if json.Compact(&b, row) == nil {
			r.Rows = append(r.Rows, b.Bytes())
		}
	}
	return r, nil
}

// callIn resolves a node in the world and calls fn on it with arg.
func callIn(ctx context.Context, c Caller, session string, world int, backendNodeID int64, fn string, arg any) (json.RawMessage, error) {
	res, err := c.CallSession(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID, "executionContextId": world, "objectGroup": objectGroup})
	if err != nil {
		return nil, err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if json.Unmarshal(res, &rn) != nil || rn.Object.ObjectID == "" {
		return nil, fmt.Errorf("%w: the node no longer resolves", ErrStale)
	}
	defer c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObject", map[string]any{"objectId": rn.Object.ObjectID})
	res, err = c.CallSession(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"objectId": rn.Object.ObjectID, "functionDeclaration": fn, "arguments": []any{map[string]any{"value": arg}},
		"returnByValue": true, "silent": true,
	})
	if err != nil {
		return nil, err
	}
	return resultValue(res)
}

// resultValue is a Runtime answer's value, or the exception it threw.
func resultValue(res json.RawMessage) (json.RawMessage, error) {
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if e := r.ExceptionDetails; e != nil {
		msg := e.Text
		if e.Exception != nil && e.Exception.Description != "" {
			msg = e.Exception.Description
		}
		return nil, fmt.Errorf("extract failed in the Page: %s", msg)
	}
	return r.Result.Value, nil
}
