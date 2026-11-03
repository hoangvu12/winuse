package uia

// The Headless tab switch's offline tests (headless-max 05): selectTab
// over a fake tab strip. A pick's refusal and an empty strip select
// nothing; a Select is read back through IsSelected. The tree walk and
// the Select call themselves run on real Windows only; the opt-in e2e
// suite covers them on a covered launched Chrome.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeStrip is a tab strip: its items' names, which one is selected, and
// what the live calls return.
type fakeStrip struct {
	names     []string
	selected  int
	namesErr  error
	selectErr error
	ignores   bool // the Select is delivered but the selection does not move
	selects   []int
}

func (f *fakeStrip) steps() tabSteps {
	return tabSteps{
		names: func() ([]string, error) { return f.names, f.namesErr },
		selectAt: func(i int) error {
			f.selects = append(f.selects, i)
			if f.selectErr != nil {
				return f.selectErr
			}
			if !f.ignores {
				f.selected = i
			}
			return nil
		},
		selected: func(i int) bool { return f.selected == i },
	}
}

func pickName(want string) func([]string) (int, error) {
	return func(names []string) (int, error) {
		for i, n := range names {
			if n == want {
				return i, nil
			}
		}
		return -1, errors.New("no such tab")
	}
}

func TestSelectTabSelectsAndReadsBack(t *testing.T) {
	f := &fakeStrip{names: []string{"TAB-A - Memory usage - 17.3 MB", "TAB-B"}}
	if err := selectTab(f.steps(), pickName("TAB-B")); err != nil {
		t.Fatalf("selectTab: %v", err)
	}
	if len(f.selects) != 1 || f.selects[0] != 1 || f.selected != 1 {
		t.Errorf("selects %v, selected %d; want item 1 selected once", f.selects, f.selected)
	}
}

func TestSelectTabRefusalsSelectNothing(t *testing.T) {
	f := &fakeStrip{}
	if err := selectTab(f.steps(), pickName("TAB-B")); !errors.Is(err, ErrNoTabStrip) {
		t.Errorf("empty strip: %v, want ErrNoTabStrip", err)
	}
	f = &fakeStrip{names: []string{"TAB-A"}}
	if err := selectTab(f.steps(), pickName("TAB-B")); err == nil || err.Error() != "no such tab" {
		t.Errorf("no match: %v, want the pick's error", err)
	}
	f = &fakeStrip{namesErr: errors.New("tree gone")}
	if err := selectTab(f.steps(), pickName("TAB-B")); err == nil || err.Error() != "tree gone" {
		t.Errorf("listing failed: %v", err)
	}
	if len(f.selects) != 0 {
		t.Errorf("a refusal selected %v", f.selects)
	}
}

func TestSelectTabFailures(t *testing.T) {
	write := &WriteError{What: "selecting the tab", Err: errors.New("E_FAIL")}
	f := &fakeStrip{names: []string{"TAB-A", "TAB-B"}, selectErr: write}
	if err := selectTab(f.steps(), pickName("TAB-B")); !errors.As(err, new(*WriteError)) {
		t.Errorf("failed Select: %v, want the WriteError", err)
	}
	f = &fakeStrip{names: []string{"TAB-A", "TAB-B"}, ignores: true}
	err := selectTab(f.steps(), pickName("TAB-B"))
	var verify *VerifyError
	if !errors.As(err, &verify) || verify.Expected != "selected" {
		t.Errorf("ignored Select: %v, want a VerifyError", err)
	}
}

// A strip that shows no items yet is walked again until it does (a lazy
// Chromium tree); one that never does ends in ErrNoTabStrip within the
// budget; any other answer, success or a pick's refusal, is final at once.
func TestSettleTabStripWaitsForALazyStrip(t *testing.T) {
	noSleep := func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()

	walks := 0
	err := settleTabStrip(ctx, time.Hour, time.Millisecond, noSleep, func() error {
		walks++
		if walks < 3 {
			return ErrNoTabStrip
		}
		return nil
	})
	if err != nil || walks != 3 {
		t.Errorf("a strip built on the third walk: %v after %d walks, want success after 3", err, walks)
	}

	walks = 0
	start := time.Now()
	err = settleTabStrip(ctx, 50*time.Millisecond, 10*time.Millisecond, func(ctx context.Context, d time.Duration) error {
		time.Sleep(d)
		return nil
	}, func() error { walks++; return ErrNoTabStrip })
	if !errors.Is(err, ErrNoTabStrip) || walks < 2 || time.Since(start) > time.Second {
		t.Errorf("a strip that never shows: %v after %d walks in %s, want ErrNoTabStrip after a few walks within the budget", err, walks, time.Since(start))
	}

	refused := errors.New("no such tab")
	walks = 0
	if err := settleTabStrip(ctx, time.Hour, time.Millisecond, noSleep, func() error { walks++; return refused }); err != refused || walks != 1 {
		t.Errorf("a pick's refusal: %v after %d walks, want it at once", err, walks)
	}

	// A request whose deadline is nearer than the next walk stops at once.
	short, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	walks = 0
	if err := settleTabStrip(short, time.Hour, 100*time.Millisecond, noSleep, func() error { walks++; return ErrNoTabStrip }); !errors.Is(err, ErrNoTabStrip) || walks != 1 {
		t.Errorf("a near deadline: %v after %d walks, want ErrNoTabStrip after 1", err, walks)
	}
}
