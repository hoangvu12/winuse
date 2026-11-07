package uia

// find, region reads and the skeleton (desktop-parity ticket 04,
// agent-desktop's `find`, `--root` and `--skeleton`).
//
// Numbering (the user's option c): find and a region read add the
// Elements they print to the Agent's live store after the ones its latest
// Window State holds (store.add), so the Element Indexes the agent already
// has keep working; a full read still replaces them all. The skeleton is a
// full read of a fixed depth, so it replaces them like any other.

import (
	"context"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// SkeletonDepth is the skeleton read's depth: the Window and three levels
// below it, each node past it with its child count.
const SkeletonDepth = 3

// FindMaxNodes bounds find's walk: well past a printed Window State's
// element cap, so find reaches Elements a full read cut, within the same
// time budget.
const FindMaxNodes = 20000

type skeletonKey struct{}

// WithSkeleton returns ctx asking Capture for a skeleton read: depth
// SkeletonDepth, with "(+N children)" on the nodes it stops at.
func WithSkeleton(ctx context.Context) context.Context {
	return context.WithValue(ctx, skeletonKey{}, true)
}

func skeleton(ctx context.Context) bool {
	v, _ := ctx.Value(skeletonKey{}).(bool)
	return v
}

// Added is the Elements find or a region read added to the Agent's live
// store, with the Element Index each one holds: a new one after the
// Window State's, or the one it already had when an earlier find added the
// same Element (desktop-parity 17).
type Added struct {
	Indexes  []int
	Elements []wstate.Element
	Notes    []string // the walk's partial-tree note, when it was cut
}

// FindResult is find's answer: the matches it added, how many Elements
// matched in all, the roles the Window shows, and the IDs two or more of
// the walk's Elements share (left out of find's lines: speed 13).
type FindResult struct {
	Added
	Count        int
	RolesPresent []string
	SharedIDs    map[string]bool
}

// Find walks hwnd's live tree (up to FindMaxNodes Elements, within the
// read budget), keeps the Elements match accepts (the first limit of them
// when limit is above 0) and adds them to the Agent's live store. Count is
// every match, kept or not. With countOnly nothing is added: only Count is
// set.
func (c *Client) Find(ctx context.Context, hwnd uint64, budget time.Duration, match func(wstate.Element) bool, limit int, countOnly bool) (FindResult, error) {
	var out FindResult
	var jobErr error
	cfg := walkConfig(ctx, budget)
	cfg.MaxNodes = FindMaxNodes
	who := storeAgent(ctx)
	err := c.RunFor(ctx, hwnd, func(w *worker, r *run) {
		if w.thread == nil {
			jobErr = ErrWindowGone // fake workers carry no COM thread
			return
		}
		src := &comSource{th: w.thread, hwnd: uintptr(hwnd)}
		defer src.dispose()
		res, err := Walk[comPtr](src, cfg)
		for _, p := range res.Dropped {
			release(p)
		}
		if err != nil {
			jobErr = err
			return
		}
		out.RolesPresent = wstate.RolesPresent(res.Elements)
		out.SharedIDs = wstate.SharedIDs(res.Elements)
		out.Notes = partialOnly(res.Notes)
		keep := pickMatches(res.Elements, match, limit, countOnly, &out.Count)
		var live []comPtr
		for i := range res.Elements {
			if keep[i] {
				live = append(live, res.Live[i])
				out.Elements = append(out.Elements, res.Elements[i])
				continue
			}
			release(res.Live[i])
		}
		out.Indexes = c.addRead(who, hwnd, live, out.Elements, r)
	})
	if err != nil {
		return FindResult{}, err
	}
	return out, jobErr
}

// pickMatches marks the Elements find keeps and counts every match: it
// keeps every match, or the first limit of them when limit is above 0;
// none with countOnly.
func pickMatches(els []wstate.Element, match func(wstate.Element) bool, limit int, countOnly bool, count *int) []bool {
	keep := make([]bool, len(els))
	for i, e := range els {
		if !match(e) {
			continue
		}
		*count++
		keep[i] = !countOnly && (limit <= 0 || *count <= limit)
	}
	return keep
}

// ReadRegion reads the subtree of the Element behind index of the Agent's
// latest Window State of hwnd, depth levels deep (0: the full read's depth
// cap), and adds its Elements to the Agent's live store, the region's root
// first. With interactive only the region's root and the Elements an agent
// acts on (wstate.Element.ActedOn, disabled ones included) are kept. Depths are relative to the region's root.
func (c *Client) ReadRegion(ctx context.Context, hwnd uint64, index, depth int, interactive bool, budget time.Duration) (Added, error) {
	var out Added
	var jobErr error
	cfg := walkConfig(ctx, budget)
	if depth > 0 {
		cfg.MaxDepth = depth
	}
	who := storeAgent(ctx)
	err := c.RunFor(ctx, hwnd, func(w *worker, r *run) {
		el, lerr := c.lookup(ctx, hwnd, index)
		if lerr != nil {
			jobErr = lerr
			return
		}
		defer c.store.release(el)
		if w.thread == nil {
			jobErr = ErrWindowGone
			return
		}
		src := &comSource{th: w.thread, hwnd: uintptr(hwnd), from: el}
		defer src.dispose()
		res, err := Walk[comPtr](src, cfg)
		for _, p := range res.Dropped {
			release(p)
		}
		if err != nil {
			jobErr = err
			return
		}
		out.Notes = partialOnly(res.Notes)
		var live []comPtr
		for i, e := range res.Elements {
			if i == 0 || !interactive || e.ActedOn() {
				live = append(live, res.Live[i])
				out.Elements = append(out.Elements, e)
				continue
			}
			release(res.Live[i])
		}
		out.Indexes = c.addRead(who, hwnd, live, out.Elements, r)
	})
	if err != nil {
		return Added{}, err
	}
	return out, jobErr
}

// addRead adds a find's, region read's or live resolution's Elements to
// the store, keyed by their RuntimeIds (read here, on the worker), unless
// the run was abandoned (installRead's rule): then they are released.
func (c *Client) addRead(agent string, hwnd uint64, elems []comPtr, elements []wstate.Element, r *run) []int {
	ids := make([]string, len(elems))
	for i, p := range elems {
		ids[i], _ = runtimeID(p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.abandoned {
		for _, p := range elems {
			c.store.release(p)
		}
		return nil
	}
	if len(elems) == 0 {
		return nil
	}
	return c.store.add(agent, hwnd, elems, elements, ids)
}

// partialOnly keeps the walk's partial-tree note: a region or a find is
// not "nearly empty" when it is small.
func partialOnly(notes []string) []string {
	var out []string
	for _, n := range notes {
		if n != nearEmptyNote {
			out = append(out, n)
		}
	}
	return out
}
