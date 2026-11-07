package uia

// A failed root read (reliability-council 04), offline: a fake source
// whose root fails with a given HRESULT, classified as the COM source does
// (rootReadErr) under a faked IsWindow, and read through retryRoot. A
// cache chunk that fails with E_FAIL is read once more (the fake COM
// objects are txtimeout_test.go's).

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	fakeAccessDenied    = HRESULT(-0x7FF8FFFB) // 0x80070005 E_ACCESSDENIED
	fakeRPCDisconnected = HRESULT(-0x7FFEFEF8) // 0x80010108 RPC_E_DISCONNECTED
)

// flakyRoot is a fake source whose first len(hrs) root reads fail with
// those HRESULTs, on hwnd 1.
type flakyRoot struct {
	*fakeSource
	hrs   []HRESULT
	roots int
}

func (s *flakyRoot) Root() (*fakeNode, NodeData, error) {
	i := s.roots
	s.roots++
	if i < len(s.hrs) {
		return nil, NodeData{}, rootReadErr(1, s.hrs[i])
	}
	return s.fakeSource.Root()
}

// read is the source as a treeRead: one walk per read.
func (s *flakyRoot) read(cfg WalkConfig) (*readResult, error) {
	res, err := Walk[*fakeNode](s, cfg)
	if err != nil {
		return nil, err
	}
	return &readResult{tree: &wstate.Tree{Elements: res.Elements, Notes: res.Notes}}, nil
}

// fakeIsWindow makes every Window exist (alive) or none, for the test.
func fakeIsWindow(t *testing.T, alive bool) {
	t.Helper()
	was := isWindow
	isWindow = func(uintptr) bool { return alive }
	t.Cleanup(func() { isWindow = was })
}

func flakyWindow(hrs ...HRESULT) *flakyRoot {
	root := node(wstate.ControlTypeWindow, "Editor",
		node(wstate.ControlTypeButton, "OK"), node(wstate.ControlTypeButton, "Cancel"),
		node(wstate.ControlTypeEdit, "Name"), node(wstate.ControlTypeEdit, "Path"))
	return &flakyRoot{fakeSource: newFakeSource(root), hrs: hrs}
}

func TestRootReadRetriesATransientFailureOnALiveWindow(t *testing.T) {
	fakeIsWindow(t, true)
	for _, tc := range []struct {
		name string
		hrs  []HRESULT
	}{
		{"E_FAIL once", []HRESULT{fakeEFail}},
		{"E_FAIL twice", []HRESULT{fakeEFail, fakeEFail}},
		{"RPC_E_DISCONNECTED once", []HRESULT{fakeRPCDisconnected}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, s := flakyWindow(tc.hrs...), &fakeSleep{}
			res, err := retryRoot(context.Background(), src.read, s.sleep)(walkConfig(context.Background(), time.Minute))
			if err != nil {
				t.Fatalf("read error = %v, want the tree", err)
			}
			if len(res.tree.Elements) != 5 {
				t.Errorf("read %d Elements, want 5", len(res.tree.Elements))
			}
			if src.roots != len(tc.hrs)+1 {
				t.Errorf("%d root reads, want %d", src.roots, len(tc.hrs)+1)
			}
			for _, d := range s.delays {
				if d != rootRetryDelay {
					t.Errorf("retry delay %s, want %s", d, rootRetryDelay)
				}
			}
			if len(s.delays) != len(tc.hrs) {
				t.Errorf("%d delays, want %d", len(s.delays), len(tc.hrs))
			}
		})
	}
}

// TestRootReadFailingOnALiveWindowIsNotWindowGone: E_FAIL three times on
// a Window that still exists is the tree-read error, not "the Window may
// have closed", after two retries.
func TestRootReadFailingOnALiveWindowIsNotWindowGone(t *testing.T) {
	fakeIsWindow(t, true)
	src, s := flakyWindow(fakeEFail, fakeEFail, fakeEFail), &fakeSleep{}
	_, err := readTree(context.Background(), "editor.exe", time.Minute, DefaultChromiumSettle, thinMark{}, retryRoot(context.Background(), src.read, s.sleep), s.sleep)
	if errors.Is(err, ErrWindowGone) {
		t.Fatalf("read error = %v, want not ErrWindowGone", err)
	}
	var te *TreeReadError
	if !errors.As(err, &te) || te.HR != fakeEFail {
		t.Fatalf("read error = %#v, want a TreeReadError with E_FAIL", err)
	}
	if want := "reading the Window's tree failed (HRESULT 0x80004005); retry"; err.Error() != want {
		t.Errorf("error text = %q, want %q", err, want)
	}
	if src.roots != 1+rootRetries {
		t.Errorf("%d root reads, want %d", src.roots, 1+rootRetries)
	}
}

func TestRootReadOfAGoneWindowIsWindowGone(t *testing.T) {
	fakeIsWindow(t, false)
	src, s := flakyWindow(fakeEFail), &fakeSleep{}
	_, err := retryRoot(context.Background(), src.read, s.sleep)(walkConfig(context.Background(), time.Minute))
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("read error = %v, want ErrWindowGone", err)
	}
	if src.roots != 1 || len(s.delays) != 0 {
		t.Errorf("%d root reads and %d retries, want 1 and none", src.roots, len(s.delays))
	}
}

func TestRootReadDoesNotRetryAccessDeniedOrATimeout(t *testing.T) {
	fakeIsWindow(t, true)
	for _, tc := range []struct {
		name    string
		hr      HRESULT
		refused bool
	}{
		{"E_ACCESSDENIED", fakeAccessDenied, true},
		{"UIA_E_TIMEOUT", uiaErrTimeout, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, s := flakyWindow(tc.hr), &fakeSleep{}
			_, err := retryRoot(context.Background(), src.read, s.sleep)(walkConfig(context.Background(), time.Minute))
			var te *TreeReadError
			if !errors.As(err, &te) || te.HR != tc.hr || te.Refused() != tc.refused {
				t.Fatalf("read error = %#v, want a TreeReadError with %s (refused: %v)", err, tc.hr, tc.refused)
			}
			if src.roots != 1 || len(s.delays) != 0 {
				t.Errorf("%d root reads and %d retries, want 1 and none", src.roots, len(s.delays))
			}
		})
	}
}

// TestRootRetryStopsAtTheDeadline: a retry the request has no time for is
// not made; the failure stands.
func TestRootRetryStopsAtTheDeadline(t *testing.T) {
	fakeIsWindow(t, true)
	src, s := flakyWindow(fakeEFail), &fakeSleep{err: context.DeadlineExceeded}
	_, err := retryRoot(context.Background(), src.read, s.sleep)(walkConfig(context.Background(), time.Minute))
	var te *TreeReadError
	if !errors.As(err, &te) {
		t.Fatalf("read error = %v, want the TreeReadError", err)
	}
	if src.roots != 1 {
		t.Errorf("%d root reads, want 1", src.roots)
	}
}

func TestHRTransient(t *testing.T) {
	if eFail.String() != "0x80004005" || eAccessDenied.String() != "0x80070005" {
		t.Fatalf("eFail = %s, eAccessDenied = %s", eFail, eAccessDenied)
	}
	for _, tc := range []struct {
		hr   HRESULT
		want bool
	}{
		{eFail, true},
		{fakeRPCDisconnected, true},
		{HRESULT(-0x7FFBFE03), true}, // 0x800401FD CO_E_OBJNOTCONNECTED
		{HRESULT(-0x7FFEFEFB), true}, // 0x80010105 RPC_E_SERVERFAULT
		{HRESULT(-0x7FF8F946), true}, // 0x800706BA RPC_S_SERVER_UNAVAILABLE
		{eAccessDenied, false},
		{uiaErrTimeout, false},
		{uiaErrElementGone, false},
	} {
		if got := hrTransient(tc.hr); got != tc.want {
			t.Errorf("hrTransient(%s) = %v, want %v", tc.hr, got, tc.want)
		}
	}
}

// flakyNode's cache build fails with E_FAIL its first fails times, then
// succeeds with no children.
var flakyNode = struct {
	fails, calls int
	obj          *fakeCOM
}{}

func init() {
	f := newFakeCOM(map[uintptr]uintptr{elGetCachedChildren: fakeLeafChildren})
	f.vtbl[elBuildUpdatedCache] = syscall.NewCallback(func(this, req uintptr, out *comPtr) uintptr {
		flakyNode.calls++
		if flakyNode.calls <= flakyNode.fails {
			hr := fakeEFail
			return uintptr(uint32(hr))
		}
		*out = f.ptr()
		return 0
	})
	flakyNode.obj = f
}

func TestChunkRetriesEFailOnce(t *testing.T) {
	auto := newFakeCOM(map[uintptr]uintptr{uia2PutTransactionTimeout: fakeOK})
	src := &comSource{th: &thread{auto: auto.ptr()}}
	for _, tc := range []struct {
		name      string
		fails     int
		remaining time.Duration
		calls     int
		wantErr   bool
	}{
		{"E_FAIL then success", 1, time.Minute, 2, false},
		{"E_FAIL twice", 2, time.Minute, 2, true},
		{"E_FAIL with no time for a retry", 1, chunkRetryDelay, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flakyNode.fails, flakyNode.calls = tc.fails, 0
			_, _, err := src.Children(flakyNode.obj.ptr(), tc.remaining)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want an error: %v", err, tc.wantErr)
			}
			if flakyNode.calls != tc.calls {
				t.Errorf("%d cache builds, want %d", flakyNode.calls, tc.calls)
			}
		})
	}
}
