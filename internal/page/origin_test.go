package page

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// fakeNodes is a fake endpoint for Origin checks: frame F1 of session S
// at loader *loader, nodes resolving unless gone, node 98 out of the
// document, and fixed boxes.
type fakeNodes struct {
	srv    *cdptest.Server
	mu     sync.Mutex
	loader string
	gone   map[int64]bool
}

func newFakeNodes(t *testing.T) (*fakeNodes, *cdp.Client) {
	t.Helper()
	f := &fakeNodes{srv: cdptest.NewServer(), loader: "L1", gone: map[int64]bool{}}
	t.Cleanup(f.srv.Close)
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if m.SessionID == "CHILD1" {
			return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F2", "loaderId": "L2"}}}, nil
		}
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": f.loader}}}, nil
	})
	f.srv.Handle("DOM.resolveNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			BackendNodeID int64 `json:"backendNodeId"`
		}
		json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		gone := f.gone[p.BackendNodeID]
		f.mu.Unlock()
		if gone {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "No node with given id found"}
		}
		id, _ := json.Marshal(p.BackendNodeID)
		return map[string]any{"object": map[string]any{"type": "object", "objectId": m.SessionID + ":" + string(id)}}, nil
	})
	f.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ObjectID string `json:"objectId"`
			Fn       string `json:"functionDeclaration"`
		}
		json.Unmarshal(m.Params, &p)
		var v any
		switch {
		case strings.Contains(p.Fn, "isConnected"):
			v = !strings.HasSuffix(p.ObjectID, ":98")
		case strings.Contains(p.Fn, "paddingLeft"):
			v = []float64{10, 20, 1.5}
		default:
			v = []float64{5, 6, 70, 30, 1.25}
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
	})
	ws, err := cdp.DialWS(context.Background(), f.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return f, c
}

func TestOriginCheck(t *testing.T) {
	f, c := newFakeNodes(t)
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	ctx := context.Background()
	if err := o.Check(ctx); err != nil {
		t.Fatalf("a live node: %v", err)
	}

	gone := o
	gone.BackendNodeID = 99
	f.gone[99] = true
	if err := gone.Check(ctx); !errors.Is(err, ErrStale) {
		t.Errorf("a node that no longer resolves: %v", err)
	}
	out := o
	out.BackendNodeID = 98
	if err := out.Check(ctx); !errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "left the document") {
		t.Errorf("a node out of the document: %v", err)
	}
	other := o
	other.FrameID = "F9"
	if err := other.Check(ctx); !errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "frame is gone") {
		t.Errorf("a frame that is gone: %v", err)
	}

	// Navigation: a new document in the frame.
	f.mu.Lock()
	f.loader = "L2"
	f.mu.Unlock()
	if err := o.Check(ctx); !errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "navigated") {
		t.Errorf("after navigation: %v", err)
	}

	// A node of a cross-origin iframe is stale when its iframe element's
	// page navigated, even if its own frame did not.
	child := Origin{Client: c, SessionID: "CHILD1", FrameID: "F2", LoaderID: "L2", BackendNodeID: 40,
		Owner: &Owner{SessionID: "S", BackendNodeID: 300, FrameID: "F1", LoaderID: "L1"}}
	if err := child.Check(ctx); !errors.Is(err, ErrStale) {
		t.Errorf("iframe node after its parent navigated: %v", err)
	}
	child.Owner.LoaderID = "L2"
	if err := child.Check(ctx); err != nil {
		t.Errorf("iframe node: %v", err)
	}

	// The connection ended.
	c.Close()
	if err := o.Check(ctx); !errors.Is(err, ErrStale) {
		t.Errorf("after the connection closed: %v", err)
	}
	if err := (Origin{}).Check(ctx); !errors.Is(err, ErrStale) {
		t.Errorf("no connection: %v", err)
	}
}

func TestOriginRect(t *testing.T) {
	_, c := newFakeNodes(t)
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	r, err := o.Rect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r != (Rect{Left: 5, Top: 6, Width: 70, Height: 30, DPR: 1.25}) {
		t.Errorf("rect = %+v", r)
	}
	// A cross-origin iframe's node adds the iframe's content box offset.
	o = Origin{Client: c, SessionID: "CHILD1", FrameID: "F2", BackendNodeID: 40, Owner: &Owner{SessionID: "S", BackendNodeID: 300}}
	r, err = o.Rect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r != (Rect{Left: 15, Top: 26, Width: 70, Height: 30, DPR: 1.5}) {
		t.Errorf("iframe rect = %+v", r)
	}
}
