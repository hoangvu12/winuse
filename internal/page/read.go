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
	"regexp"
	"strings"
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
	// (same registrable domain, SiteURL)
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
	md.Text = printLinks(md.Text, md.URL, o.FullURLs)
	return md, nil
}

// readLink is a link as read.js hands it back: \x01, its text, \x1f, its
// absolute URL, \x02. A URL never holds a control character (the URL
// parser escapes them) and read.js drops these three from the Page's text.
var readLink = regexp.MustCompile("\x01([^\x01\x02\x1f]*)\x1f([^\x01\x02\x1f]*)\x02")

// linkLike is a link text that looks like an address (example.com/...).
var linkLike = regexp.MustCompile(`(?i)^(https?://)?[\w.-]+\.[a-z]{2,}`)

// printLinks prints each link read.js handed back as Markdown, its URL
// shortened by SiteURL against the Page's own address (whole with
// fullURLs), so `winuse read` and the Page lines share one rule. A link
// whose text is its URL prints once.
func printLinks(text, pageURL string, fullURLs bool) string {
	out := readLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := readLink.FindStringSubmatch(m)
		tx, href := sub[1], sub[2]
		u := href
		if !fullURLs {
			u = SiteURL(href, pageURL)
		}
		if u == "" {
			return tx
		}
		bu := bareURL(u)
		if u == tx || bu != "" && strings.HasPrefix(bareURL(tx), bu) && linkLike.MatchString(tx) {
			return u
		}
		return "[" + tx + "](" + u + ")"
	})
	return linkMarks.Replace(out)
}

// linkMarks drops the marks of a link that did not print as one (a link
// inside another link's text).
var linkMarks = strings.NewReplacer("\x01", "", "\x02", "", "\x1f", "")

// bareURL drops a URL's scheme, "www." and trailing slashes or "…", to
// compare a link's text with its URL.
func bareURL(s string) string {
	for _, p := range []string{"https://", "http://"} {
		if cut, ok := strings.CutPrefix(s, p); ok {
			s = cut
			break
		}
	}
	s = strings.TrimPrefix(s, "www.")
	return strings.TrimRight(s, "/…")
}
