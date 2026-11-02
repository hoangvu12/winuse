package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// Origin is where an Element Index of the Page points: a DOM node in one
// session of one Tab, in one document generation. The helper keeps one per
// Page Element in its live-Element store, beside the UIA Elements of the
// frame (uia.Client.SplicePage), and ticket 05 routes Page Actions on it.
type Origin struct {
	Browser  string      // the browser's short name
	Client   *cdp.Client // the Browser Connection the Page was read over
	TargetID string      // the Tab's page target
	// SessionID is the flattened session the node lives in: the Tab's own
	// session, or a cross-origin iframe's auto-attached one.
	SessionID     string
	FrameID       string // the node's frame
	LoaderID      string // the frame's document generation when the Page was read
	BackendNodeID int64
	// Owner is set for a node of an iframe read over its own session: the
	// iframe element in the Tab's session, whose box offsets the node's.
	Owner *Owner
	// DocIndex is the Element Index of the UIA document Element the Page
	// hangs under (the Agent Cursor transform's origin, ticket 05).
	DocIndex int
}

// Owner is the iframe element a cross-origin iframe's document sits in.
type Owner struct {
	SessionID     string
	BackendNodeID int64
	FrameID       string
	LoaderID      string
}

// Caller is the CDP side an Origin is checked over; *cdp.Client is one.
type Caller interface {
	CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
}

// caller is the Origin's own connection, refusing one that ended.
func (o Origin) caller() (Caller, error) {
	if o.Client == nil {
		return nil, fmt.Errorf("%w: no Browser Connection", ErrStale)
	}
	if !o.Client.Alive() {
		return nil, fmt.Errorf("%w: the Browser Connection ended", ErrStale)
	}
	return o.Client, nil
}

// Check reports whether the node behind an Origin is still the one the
// Window State showed. It is stale (ErrStale) when:
//
//   - the Browser Connection or the node's session ended;
//   - the node's frame navigated: its loaderId is not the one recorded, or
//     the frame is gone;
//   - the node is gone: it no longer resolves, or is out of the document.
//
// For a node in a cross-origin iframe the iframe element is checked too.
// Any other failure (a timeout, a CDP error that says nothing about the
// node) is returned as is.
func (o Origin) Check(ctx context.Context) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	return check(ctx, c, o)
}

func check(ctx context.Context, c Caller, o Origin) error {
	if o.Owner != nil {
		owner := Origin{SessionID: o.Owner.SessionID, FrameID: o.Owner.FrameID, LoaderID: o.Owner.LoaderID, BackendNodeID: o.Owner.BackendNodeID}
		if err := check(ctx, c, owner); err != nil {
			return err
		}
	}
	loader, err := frameLoader(ctx, c, o.SessionID, o.FrameID)
	if err != nil {
		return staleOr(err)
	}
	if loader == "" {
		return fmt.Errorf("%w: its frame is gone", ErrStale)
	}
	if o.LoaderID != "" && loader != o.LoaderID {
		return fmt.Errorf("%w: the page navigated", ErrStale)
	}
	var connected bool
	if err := callOn(ctx, c, o.SessionID, o.BackendNodeID, "function(){return this.isConnected}", &connected); err != nil {
		return staleOr(err)
	}
	if !connected {
		return fmt.Errorf("%w: the node left the document", ErrStale)
	}
	return nil
}

// Rect is a Page Element's box in CSS pixels, relative to the Tab's
// viewport (an iframe's offset already added), and the page's
// devicePixelRatio. Ticket 05's Agent Cursor transform is: screen px = the
// UIA document Element's top-left + (Left, Top) x DPR, read fresh for each
// Action (probe-results.md section 3).
type Rect struct {
	Left, Top, Width, Height float64
	DPR                      float64
}

// Rect reads the node's box with getBoundingClientRect, through its own
// session. A node in a cross-origin iframe adds the iframe's content-box
// offset in the Tab's session. It does not scroll anything into view.
func (o Origin) Rect(ctx context.Context) (Rect, error) {
	c, err := o.caller()
	if err != nil {
		return Rect{}, err
	}
	return rect(ctx, c, o)
}

const rectJS = `function(){const r=this.getBoundingClientRect();return [r.left,r.top,r.width,r.height,window.devicePixelRatio]}`

// contentJS is an iframe element's content-box top-left in its parent's
// viewport.
const contentJS = `function(){const r=this.getBoundingClientRect(),s=getComputedStyle(this);return [r.left+this.clientLeft+parseFloat(s.paddingLeft||0),r.top+this.clientTop+parseFloat(s.paddingTop||0),window.devicePixelRatio]}`

func rect(ctx context.Context, c Caller, o Origin) (Rect, error) {
	var box [5]float64
	if err := callOn(ctx, c, o.SessionID, o.BackendNodeID, rectJS, &box); err != nil {
		return Rect{}, staleOr(err)
	}
	r := Rect{Left: box[0], Top: box[1], Width: box[2], Height: box[3], DPR: box[4]}
	if o.Owner != nil {
		var off [3]float64
		if err := callOn(ctx, c, o.Owner.SessionID, o.Owner.BackendNodeID, contentJS, &off); err != nil {
			return Rect{}, staleOr(err)
		}
		r.Left += off[0]
		r.Top += off[1]
		r.DPR = off[2]
	}
	return r, nil
}

// callOn resolves a backendNodeId to a JS object in its session, calls fn
// on it, and decodes the returned value. The object is released.
func callOn(ctx context.Context, c Caller, session string, backendNodeID int64, fn string, out any) error {
	res, err := c.CallSession(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID, "objectGroup": objectGroup})
	if err != nil {
		return err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if json.Unmarshal(res, &rn) != nil || rn.Object.ObjectID == "" {
		return fmt.Errorf("%w: the node no longer resolves", ErrStale)
	}
	defer c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObject", map[string]any{"objectId": rn.Object.ObjectID})
	res, err = c.CallSession(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"objectId": rn.Object.ObjectID, "functionDeclaration": fn, "returnByValue": true, "silent": true,
	})
	if err != nil {
		return err
	}
	var cr struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &cr); err != nil {
		return err
	}
	if cr.ExceptionDetails != nil {
		return fmt.Errorf("reading the node failed: %s", cr.ExceptionDetails.Text)
	}
	return json.Unmarshal(cr.Result.Value, out)
}

// frameLoader is the loaderId of a frame in a session's frame tree, ""
// when the frame is not there.
func frameLoader(ctx context.Context, c Caller, session, frameID string) (string, error) {
	res, err := c.CallSession(ctx, session, "Page.getFrameTree", nil)
	if err != nil {
		return "", err
	}
	var ft struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := json.Unmarshal(res, &ft); err != nil {
		return "", err
	}
	loaders := map[string]string{}
	ft.FrameTree.loaders(loaders, -1)
	return loaders[frameID], nil
}

// frameTree is Page.getFrameTree's answer.
type frameTree struct {
	Frame struct {
		ID       string `json:"id"`
		LoaderID string `json:"loaderId"`
		URL      string `json:"url"`
	} `json:"frame"`
	ChildFrames []frameTree `json:"childFrames"`
}

// loaders collects frame id to loader id, depth levels deep (-1: all).
func (t frameTree) loaders(out map[string]string, depth int) {
	out[t.Frame.ID] = t.Frame.LoaderID
	if depth == 0 {
		return
	}
	for _, c := range t.ChildFrames {
		c.loaders(out, depth-1)
	}
}

// staleOr maps the CDP answers that mean the node or its session is gone
// to ErrStale, and passes anything else through.
func staleOr(err error) error {
	if err == nil || errors.Is(err, ErrStale) {
		return err
	}
	if errors.Is(err, cdp.ErrSessionDetached) || errors.Is(err, cdp.ErrClosed) {
		return fmt.Errorf("%w: %v", ErrStale, err)
	}
	var ce *cdp.Error
	if errors.As(err, &ce) {
		msg := strings.ToLower(ce.Message)
		for _, gone := range []string{"no node", "could not find node", "node with given id", "session with given id", "no session", "detached", "cannot find context"} {
			if strings.Contains(msg, gone) {
				return fmt.Errorf("%w: %v", ErrStale, err)
			}
		}
	}
	return err
}
