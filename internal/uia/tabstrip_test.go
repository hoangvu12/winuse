package uia

// The Headless tab switch's offline tests (headless-max 05): selectTab
// over a fake tab strip. A pick's refusal and an empty strip select
// nothing; a Select is read back through IsSelected. The tree walk and
// the Select call themselves run on real Windows only; the opt-in e2e
// suite covers them on a covered launched Chrome.

import (
	"errors"
	"testing"
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
