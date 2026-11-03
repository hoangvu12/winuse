package helper

// Live re-resolution of name:/id: references (desktop-parity ticket 17,
// uia/liverefs.go): when the Agent's cached Window State cannot answer a
// reference (none after a helper restart, no match, a gone Element), the
// UIA client resolves it in a fresh bounded walk and adds the Element
// after the Window State's indexes. The Action then runs as usual, and its
// output gains
//
//	Note: found name:Save in a fresh read of window 101, as Element Index 245; the Window State you have is unchanged
//
// Element Indexes are never re-resolved: they stay valid only for the
// Window State that printed them.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// LiveResolver is the UIA client's side of the live re-resolution:
// *uia.Client. Services.Actions is type-asserted to it; a fake that only
// implements ElementResolver resolves against the cache alone.
type LiveResolver interface {
	ResolveElementRefLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (uia.LiveResolution, error)
}

var _ LiveResolver = (*uia.Client)(nil)

// liveRefBudget bounds the fresh read: it leaves the Action most of the
// request's time (half of what is left, at most).
const liveRefBudget = 3 * time.Second

// liveBudget is the fresh read's budget under ctx.
func liveBudget(ctx context.Context) time.Duration {
	b := liveRefBudget
	if dl, ok := ctx.Deadline(); ok {
		if half := time.Until(dl) / 2; half < b {
			b = half
		}
	}
	return b
}

// refNotes collects the notes of the references a request re-resolved.
type refNotes struct {
	mu    sync.Mutex
	lines []string
}

type refNotesKey struct{}

// noteFreshRef records that ref was found in a fresh read of w, as index.
func noteFreshRef(ctx context.Context, w win.WindowInfo, ref string, index int) {
	n, ok := ctx.Value(refNotesKey{}).(*refNotes)
	if !ok {
		return
	}
	line := fmt.Sprintf("%sfound %s in a fresh read of window %d, as Element Index %d; the Window State you have is unchanged\n", notePrefix, ref, w.ID, index)
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, l := range n.lines {
		if l == line {
			return // a Batch step resolving the same reference again
		}
	}
	n.lines = append(n.lines, line)
}

// reportRefs adds the re-resolved references' notes after a successful
// Action's output.
func (s Services) reportRefs(cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		n := &refNotes{}
		out, err := cmd(context.WithValue(ctx, refNotesKey{}, n), args)
		if err != nil {
			return out, err
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		return withNotes(out, strings.Join(n.lines, "")), nil
	}
}
