package page

// The wall check (.scratch/bladebro-ideas ticket 05): block.js (a port of
// bladebro's detect_block.js, Apache-2.0, third_party/bladebro/LICENSE)
// names the wall a short challenge or block page shows. It is the one
// copy: BlockKind runs it alone after navigate, back, forward and reload,
// and read.go puts it into read.js, so `winuse read` and any later reader
// (extract) name walls the same way. It runs in an isolated world of the
// main frame, never sends Runtime.enable, and only reads: it never clicks,
// types or solves anything.

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed block.js
var blockJS string

// blockMarker is where read.js takes block.js.
const blockMarker = "/* winuse:block.js */ null"

// blockWorld names the isolated world BlockKind runs block.js in.
const blockWorld = "winuse-block"

// withBlockCheck puts block.js into a reader's script in place of its
// marker; a script without the marker is a bug, so it panics at start.
func withBlockCheck(js string) string {
	if !strings.Contains(js, blockMarker) {
		panic("page: a reader script lacks " + blockMarker)
	}
	return strings.Replace(js, blockMarker, blockJS, 1)
}

// BlockKind runs block.js on the main frame's document of session and
// returns the wall it names, "" for none (an ordinary page, or a value
// that is no name).
func BlockKind(ctx context.Context, c Caller, session string) (string, error) {
	ctxID, err := isolatedWorld(ctx, c, session, "", blockWorld)
	if err != nil {
		return "", err
	}
	res, err := c.CallSession(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": blockJS,
		"executionContextId":  ctxID,
		"returnByValue":       true,
		"silent":              true,
	})
	if err != nil {
		return "", staleOr(err)
	}
	var cr struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &cr); err != nil {
		return "", err
	}
	if cr.ExceptionDetails != nil {
		return "", fmt.Errorf("checking the Page for a wall failed: %s", cr.ExceptionDetails.message())
	}
	var kind string
	if json.Unmarshal(cr.Result.Value, &kind) != nil {
		return "", nil
	}
	return kind, nil
}

// isolatedWorld makes an isolated world named name in frameID (the
// session's main frame when "") and returns its execution context.
func isolatedWorld(ctx context.Context, c Caller, session, frameID, name string) (int, error) {
	if frameID == "" {
		res, err := c.CallSession(ctx, session, "Page.getFrameTree", nil)
		if err != nil {
			return 0, err
		}
		var ft struct {
			FrameTree frameTree `json:"frameTree"`
		}
		if err := json.Unmarshal(res, &ft); err != nil {
			return 0, err
		}
		frameID = ft.FrameTree.Frame.ID
	}
	res, err := c.CallSession(ctx, session, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": name})
	if err != nil {
		return 0, staleOr(err)
	}
	var w struct {
		ExecutionContextID int `json:"executionContextId"`
	}
	if json.Unmarshal(res, &w) != nil || w.ExecutionContextID == 0 {
		return 0, errors.New("the browser gave no isolated world to read the Page in")
	}
	return w.ExecutionContextID, nil
}
