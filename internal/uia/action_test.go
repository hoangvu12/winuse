package uia

// The element-side Action tests (ticket 05): the Element Index → point
// resolution's store lookups (no state vs. out-of-range, both stale
// failures) and the stale HRESULT classification. The COM path itself
// (clickable point, bounds centre) runs on real Windows only; the e2e
// suite covers it against the fixture app.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestElementPointNoCachedState(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	_, err := c.ElementPoint(context.Background(), 101, 0)
	if !errors.Is(err, ErrNoState) {
		t.Fatalf("error = %v, want ErrNoState", err)
	}
}

func TestElementPointIndexNotInCachedState(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	// Fake element pointers: distinct real addresses; seeded through the
	// real beginRead/install path.
	a, b := 1, 2
	c.store.install(101, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b)})

	for _, index := range []int{2, 100, -1} {
		_, err := c.ElementPoint(context.Background(), 101, index)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("index %d: error = %v, want ErrStale", index, err)
		}
	}
	// A different Window's state does not resolve either.
	_, err := c.ElementPoint(context.Background(), 202, 0)
	if !errors.Is(err, ErrNoState) {
		t.Fatalf("window 202: error = %v, want ErrNoState", err)
	}
	// In-range indexes of a stored Window resolve without error (fake
	// workers carry no COM thread, so no point is computed).
	if _, err := c.ElementPoint(context.Background(), 101, 1); err != nil {
		t.Fatalf("index 1: error = %v, want nil (the fake worker skips COM)", err)
	}
}

func TestElementPointSurfacesContextCancellation(t *testing.T) {
	c := testClient(func() (*worker, error) { return hangWorker(), nil })
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.ElementPoint(ctx, 101, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context error", err)
	}
}

// TestStaleHRESULTs pins the "stale" HRESULT classification: a one-digit
// slip would silently misclassify a dead Element as an unknown failure.
func TestStaleHRESULTs(t *testing.T) {
	for _, tc := range []struct {
		hr    HRESULT
		want  bool
		label string
	}{
		{HRESULT(-0x7FFBFDFF), true, "UIA_E_ELEMENTNOTAVAILABLE"},
		{HRESULT(-0x7FFEFEF8), true, "RPC_E_DISCONNECTED"},
		{HRESULT(-0x7FFBFE03), true, "CO_E_OBJNOTCONNECTED"},
		{HRESULT(-0x7FFEFEFB), true, "RPC_E_SERVERFAULT"},
		{uiaErrTimeout, false, "UIA_E_TIMEOUT"},
		{uiaErrNoClickablePoint, false, "UIA_E_NOCLICKABLEPOINT"},
		{HRESULT(0), false, "S_OK"},
	} {
		if got := hrStale(tc.hr); got != tc.want {
			t.Errorf("hrStale(%s 0x%08x) = %v, want %v", tc.label, uint32(tc.hr), got, tc.want)
		}
	}
	var noClickable = uiaErrNoClickablePoint
	if uint32(noClickable) != 0x80040202 {
		t.Errorf("uiaErrNoClickablePoint = 0x%08x, want 0x80040202", uint32(noClickable))
	}
}

// TestElementPointWrappedSentinelErrors verifies the sentinel wrapping the
// helper's error mapping relies on (errors.Is through fmt.Errorf).
func TestElementPointWrappedSentinelErrors(t *testing.T) {
	if !errors.Is(fmt.Errorf("%w: element 3 is not in the cached Window State of window 9", ErrStale), ErrStale) {
		t.Error("wrapped ErrStale must stay recognizable")
	}
	if !errors.Is(fmt.Errorf("%w: window 9", ErrNoState), ErrNoState) {
		t.Error("wrapped ErrNoState must stay recognizable")
	}
}
