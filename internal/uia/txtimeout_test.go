package uia

// The worker's transaction timeout (reliability-council 01), offline: a
// cache chunk near its time limit puts a tiny timeout on the worker's
// IUIAutomation2, and the next call on that worker (an Invoke here) must
// run under the fixed timeout again, not the chunk's. The COM objects are
// fakes: Go vtables of syscall.NewCallback shims that record the calls.

import (
	"slices"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// fakeCOM is a COM object: its first word points at its vtable.
type fakeCOM struct{ vtbl *[64]uintptr }

func (f *fakeCOM) ptr() comPtr { return unsafe.Pointer(f) }

// txRecorder is the fake IUIAutomation2's state: every put_TransactionTimeout
// in order, and the timeout each fake Invoke ran under.
var txRecorder struct {
	puts    []uint32
	invoked []uint32
}

func currentTx() uint32 {
	if n := len(txRecorder.puts); n > 0 {
		return txRecorder.puts[n-1]
	}
	return 20000 // UIA's default
}

// The shims are package-level: NewCallback slots are never freed.
var (
	fakeNotImpl = syscall.NewCallback(func(this uintptr) uintptr { return 0x80004001 }) // E_NOTIMPL
	fakeOK      = syscall.NewCallback(func(this uintptr) uintptr { return 0 })
	fakePutTx   = syscall.NewCallback(func(this, ms uintptr) uintptr {
		txRecorder.puts = append(txRecorder.puts, uint32(ms))
		return 0
	})
	fakeInvoke = syscall.NewCallback(func(this uintptr) uintptr {
		txRecorder.invoked = append(txRecorder.invoked, currentTx())
		return 0
	})
	fakeTimedOutCache = syscall.NewCallback(func(this, req, out uintptr) uintptr {
		hr := uiaErrTimeout
		return uintptr(uint32(hr))
	})
	fakeLeafChildren = syscall.NewCallback(func(this uintptr, out *comPtr) uintptr {
		*out = nil // no cached children
		return 0
	})
)

func newFakeCOM(slots map[uintptr]uintptr) *fakeCOM {
	var vtbl [64]uintptr
	for i := range vtbl {
		vtbl[i] = fakeNotImpl
	}
	vtbl[1], vtbl[2] = fakeOK, fakeOK // AddRef, Release
	for i, fn := range slots {
		vtbl[i] = fn
	}
	return &fakeCOM{vtbl: &vtbl}
}

// leafNode is a node whose chunk succeeds with no children: its
// BuildUpdatedCache returns itself.
var leafNode = func() *fakeCOM {
	f := newFakeCOM(map[uintptr]uintptr{elGetCachedChildren: fakeLeafChildren})
	f.vtbl[elBuildUpdatedCache] = syscall.NewCallback(func(this, req uintptr, out *comPtr) uintptr {
		*out = f.ptr()
		return 0
	})
	return f
}()

func TestAChunkRestoresTheWorkerTransactionTimeout(t *testing.T) {
	auto := newFakeCOM(map[uintptr]uintptr{uia2PutTransactionTimeout: fakePutTx})
	timedOut := newFakeCOM(map[uintptr]uintptr{elBuildUpdatedCache: fakeTimedOutCache})
	pattern := newFakeCOM(map[uintptr]uintptr{invokeInvoke: fakeInvoke})
	src := &comSource{th: &thread{auto: auto.ptr()}}
	fixed := uint32(workerTransactionTimeout.Milliseconds())

	cases := []struct {
		name      string
		node      *fakeCOM
		remaining time.Duration
		tx        uint32 // the chunk's own timeout
	}{
		{"a chunk that ran into its time limit", timedOut, 0, 1},
		{"a chunk with 3 ms left", leafNode, 3 * time.Millisecond, 3},
		{"a chunk with time to spare", leafNode, time.Minute, uint32(maxTransactionTimeout.Milliseconds())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txRecorder.puts, txRecorder.invoked = nil, nil
			src.Children(tc.node.ptr(), tc.remaining)
			if hr := call(pattern.ptr(), invokeInvoke); hr.Failed() {
				t.Fatalf("Invoke: %s", hr)
			}
			if want := []uint32{tc.tx, fixed}; !slices.Equal(txRecorder.puts, want) {
				t.Errorf("put_TransactionTimeout calls = %v, want %v", txRecorder.puts, want)
			}
			if want := []uint32{fixed}; !slices.Equal(txRecorder.invoked, want) {
				t.Errorf("the Invoke after the chunk ran under %v ms, want %v", txRecorder.invoked, want)
			}
		})
	}
}
