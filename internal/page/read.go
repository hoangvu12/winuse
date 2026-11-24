package page

// `winuse read` (.scratch/bladebro-ideas ticket 25): a Page's main content,
// or one Page Element's subtree, as Markdown. read.js (derived from
// bladebro's markdown.js, Apache-2.0, third_party/bladebro/LICENSE) runs
// in an isolated world of the node's frame, so the Page's own scripts
// neither see it nor change what it uses; nothing in the Page changes.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed read.js
var readSource string

// readMarkdownJS is read.js with block.js, the shared wall check, in it.
var readMarkdownJS = withBlockCheck(readSource)

// readWorld names the isolated world read.js runs in.
const readWorld = "winuse-read"

// ReadOptions shape read.js's Markdown.
type ReadOptions struct {
	// FullURLs prints links (and image URLs) whole; otherwise same-site
	// links print as paths, others as a shortened domain and path, and
	// images keep only their alt text.
	FullURLs bool
}

// Markdown is what read.js read.
type Markdown struct {
	Text   string `json:"markdown"`
	Main   string `json:"main"`   // the element rendered, as tag#id or tag.class
	Scoped bool   `json:"scoped"` // an Element given was rendered, not the picked main content
	// Block names the wall a short Page looks like (Cloudflare, DataDome,
	// PerimeterX, reCAPTCHA, Akamai, rate-limit), "" for none.
	Block string `json:"block"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ReadMarkdown runs read.js in an isolated world of frameID (the session's
// main frame when "") over session: on the node backendNodeID when it is
// not 0, else on the frame's document, whose main content it picks. A
// node that is gone is ErrStale.
func ReadMarkdown(ctx context.Context, c Caller, session, frameID string, backendNodeID int64, o ReadOptions) (Markdown, error) {
	worldID, err := isolatedWorld(ctx, c, session, frameID, readWorld)
	if err != nil {
		return Markdown{}, err
	}
	urls := "short"
	if o.FullURLs {
		urls = "full"
	}
	call := map[string]any{
		"functionDeclaration": readMarkdownJS,
		"arguments":           []any{map[string]any{"value": map[string]any{"urls": urls}}},
		"returnByValue":       true,
		"silent":              true,
	}
	if backendNodeID != 0 {
		res, err := c.CallSession(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID, "executionContextId": worldID, "objectGroup": objectGroup})
		if err != nil {
			return Markdown{}, staleOr(err)
		}
		var rn struct {
			Object struct {
				ObjectID string `json:"objectId"`
			} `json:"object"`
		}
		if json.Unmarshal(res, &rn) != nil || rn.Object.ObjectID == "" {
			return Markdown{}, fmt.Errorf("%w: the node no longer resolves", ErrStale)
		}
		defer c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObject", map[string]any{"objectId": rn.Object.ObjectID})
		call["objectId"] = rn.Object.ObjectID
	} else {
		call["executionContextId"] = worldID
	}
	res, err := c.CallSession(ctx, session, "Runtime.callFunctionOn", call)
	if err != nil {
		return Markdown{}, staleOr(err)
	}
	var cr struct {
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
	if err := json.Unmarshal(res, &cr); err != nil {
		return Markdown{}, err
	}
	if cr.ExceptionDetails != nil {
		msg := cr.ExceptionDetails.Text
		if cr.ExceptionDetails.Exception != nil && cr.ExceptionDetails.Exception.Description != "" {
			msg = cr.ExceptionDetails.Exception.Description
		}
		return Markdown{}, fmt.Errorf("reading the Page failed: %s", msg)
	}
	var md Markdown
	if err := json.Unmarshal(cr.Result.Value, &md); err != nil {
		return Markdown{}, fmt.Errorf("reading the Page returned no Markdown: %v", err)
	}
	return md, nil
}
