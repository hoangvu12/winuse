package uia

// Offline tests for Element scroll's decisions (headless-parity spec,
// "Element scroll"): the direction words, the steps bound, the scroll
// amounts per direction, the percent-change verdict and the driver over a
// fake scroller. No COM is touched.

import (
	"errors"
	"math"
	"testing"
)

func TestParseScrollDirection(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ScrollDirection
	}{
		{"up", ScrollUp},
		{"Down", ScrollDown},
		{" LEFT ", ScrollLeft},
		{"right", ScrollRight},
	} {
		got, err := ParseScrollDirection(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseScrollDirection(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "into-view", "sideways", "u", "upward"} {
		if _, err := ParseScrollDirection(bad); !errors.Is(err, ErrScrollDirection) {
			t.Errorf("ParseScrollDirection(%q) error = %v, want ErrScrollDirection", bad, err)
		}
	}
}

func TestScrollDirectionWordsAndAmounts(t *testing.T) {
	for _, tc := range []struct {
		d        ScrollDirection
		word     string
		vertical bool
		h, v     uintptr
	}{
		{ScrollUp, "up", true, scrollNoAmount, scrollSmallDecrement},
		{ScrollDown, "down", true, scrollNoAmount, scrollSmallIncrement},
		{ScrollLeft, "left", false, scrollSmallDecrement, scrollNoAmount},
		{ScrollRight, "right", false, scrollSmallIncrement, scrollNoAmount},
	} {
		if tc.d.String() != tc.word {
			t.Errorf("%v.String() = %q, want %q", tc.d, tc.d.String(), tc.word)
		}
		if tc.d.Vertical() != tc.vertical {
			t.Errorf("%s.Vertical() = %v, want %v", tc.word, tc.d.Vertical(), tc.vertical)
		}
		if h, v := tc.d.amounts(); h != tc.h || v != tc.v {
			t.Errorf("%s amounts = (%d, %d), want (%d, %d)", tc.word, h, v, tc.h, tc.v)
		}
		if back, err := ParseScrollDirection(tc.word); err != nil || back != tc.d {
			t.Errorf("the word %q does not parse back to %v", tc.word, tc.d)
		}
	}
}

func TestCheckScrollSteps(t *testing.T) {
	if DefaultScrollSteps != 3 || MaxScrollSteps != 50 {
		t.Fatalf("default %d, max %d; want 3 and 50", DefaultScrollSteps, MaxScrollSteps)
	}
	for _, n := range []int{1, 3, 50} {
		if err := CheckScrollSteps(n); err != nil {
			t.Errorf("CheckScrollSteps(%d) = %v, want nil", n, err)
		}
	}
	for _, n := range []int{0, -1, 51, 1000} {
		if err := CheckScrollSteps(n); !errors.Is(err, ErrScrollSteps) {
			t.Errorf("CheckScrollSteps(%d) = %v, want ErrScrollSteps", n, err)
		}
	}
}

func TestScrollVerdict(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after float64
		want          scrollVerdict
	}{
		{"moved down", 10, 20, scrollMoved},
		{"moved up", 20, 10, scrollMoved},
		{"a partial move", 99.5, 100, scrollMoved},
		{"at the end", 100, 100, scrollStill},
		{"at the top", 0, 0, scrollStill},
		{"unreadable before", scrollNoScroll, 40, scrollUnreadable},
		{"unreadable after", 40, scrollNoScroll, scrollUnreadable},
		{"NaN", math.NaN(), 40, scrollUnreadable},
		{"out of range", 40, 140, scrollUnreadable},
	} {
		if got := scrollVerdictOf(tc.before, tc.after); got != tc.want {
			t.Errorf("%s: scrollVerdictOf(%v, %v) = %v, want %v", tc.name, tc.before, tc.after, got, tc.want)
		}
	}
}

// fakeScroller is a scroll container: each Scroll call moves its percent
// by step, clamped to 0..100.
type fakeScroller struct {
	percent float64 // scrollNoScroll: unreadable
	step    float64
	failAt  int // the 1-based call that fails; 0: none
	sent    int
}

var errScrollCall = errors.New("scroll call failed")

func (f *fakeScroller) steps() scrollerSteps {
	return scrollerSteps{
		scroll: func(sent int) error {
			if sent != f.sent {
				panic("sent out of step")
			}
			f.sent++
			if f.sent == f.failAt {
				return errScrollCall
			}
			if f.percent != scrollNoScroll {
				f.percent = math.Max(0, math.Min(100, f.percent+f.step))
			}
			return nil
		},
		percent: func() float64 { return f.percent },
		settle:  func(before float64) float64 { return f.percent },
	}
}

func TestScrollBySteps(t *testing.T) {
	cases := []struct {
		name     string
		f        fakeScroller
		steps    int
		wantErr  error // nil: ok; *VerifyError: any VerifyError; otherwise errors.Is
		wantSent int
		wantPct  float64
	}{
		{name: "three steps down", f: fakeScroller{percent: 0, step: 10}, steps: 3, wantSent: 3, wantPct: 30},
		{name: "up", f: fakeScroller{percent: 50, step: -5}, steps: 2, wantSent: 2, wantPct: 40},
		{name: "a partial move is ok", f: fakeScroller{percent: 95, step: 10}, steps: 3, wantSent: 3, wantPct: 100},
		{name: "already at the end", f: fakeScroller{percent: 100, step: 10}, steps: 3, wantErr: &VerifyError{}, wantSent: 3, wantPct: 100},
		{name: "unreadable percent is not judged", f: fakeScroller{percent: scrollNoScroll, step: 10}, steps: 1, wantSent: 1, wantPct: scrollNoScroll},
		{name: "steps out of range send nothing", f: fakeScroller{step: 10}, steps: 51, wantErr: ErrScrollSteps},
		{name: "zero steps send nothing", f: fakeScroller{step: 10}, steps: 0, wantErr: ErrScrollSteps},
		{name: "a failed call stops the run", f: fakeScroller{percent: 0, step: 10, failAt: 2}, steps: 5, wantErr: errScrollCall, wantSent: 2, wantPct: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			err := scrollBySteps(tc.steps, f.steps())
			switch want := tc.wantErr.(type) {
			case nil:
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
			case *VerifyError:
				var verify *VerifyError
				if !errors.As(err, &verify) {
					t.Fatalf("error = %v, want a VerifyError", err)
				}
				if err.Error() != "the Element did not scroll; it may already be at the end" {
					t.Errorf("message = %q", err.Error())
				}
			default:
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			}
			if f.sent != tc.wantSent {
				t.Errorf("scroll calls = %d, want %d", f.sent, tc.wantSent)
			}
			if tc.wantSent > 0 && f.percent != tc.wantPct {
				t.Errorf("percent = %v, want %v", f.percent, tc.wantPct)
			}
		})
	}
}

func TestIntoViewVerdict(t *testing.T) {
	for _, tc := range []struct {
		name              string
		readable, offscrn bool
		want              bool
	}{
		{"on screen", true, false, true},
		{"still offscreen", true, true, false},
		{"unreadable is not judged", false, false, true},
	} {
		if got := intoViewDone(tc.readable, tc.offscrn); got != tc.want {
			t.Errorf("%s: intoViewDone = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVerifyErrorMsgOverrides(t *testing.T) {
	e := &VerifyError{Action: "scroll", Expected: "x", Observed: "y", Msg: "custom wording"}
	if e.Error() != "custom wording" {
		t.Errorf("Error() = %q, want the Msg", e.Error())
	}
}
