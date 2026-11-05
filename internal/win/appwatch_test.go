package win

// The App watch's offline tests (speed ticket 01): which WinEvents count,
// what a watch does with them, when two samples say an App is quiet, and
// the process snapshot's parse. The live sample reads only this test
// process: one of its own threads blocked in GetMessage. No hook is
// installed and no window is created.

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestClassifyWindowEvent(t *testing.T) {
	const target, popup, child = 10, 20, 30
	topLevel := func(h uint64) bool { return h != child }
	cases := []struct {
		name     string
		event    uint32
		hwnd     uint64
		idObject int32
		idChild  int32
		want     windowEventKind
	}{
		{"a top-level window created", eventObjectCreate, popup, objidWindow, childidSelf, windowCreated},
		{"a child control created", eventObjectCreate, child, objidWindow, childidSelf, windowIgnored},
		{"a top-level window shown", eventObjectShow, popup, objidWindow, childidSelf, windowShown},
		{"a child control shown", eventObjectShow, child, objidWindow, childidSelf, windowIgnored},
		{"an accessible object shown inside a window", eventObjectShow, popup, -4, childidSelf, windowIgnored},
		{"a list item shown", eventObjectShow, popup, objidWindow, 3, windowIgnored},
		{"any window destroyed", eventObjectDestroy, child, objidWindow, childidSelf, windowDestroyed},
		{"the target hidden", eventObjectHide, target, objidWindow, childidSelf, targetHidden},
		{"another window hidden", eventObjectHide, popup, objidWindow, childidSelf, windowIgnored},
		{"no window", eventObjectShow, 0, objidWindow, childidSelf, windowIgnored},
	}
	for _, c := range cases {
		if got := classifyWindowEvent(c.event, c.hwnd, c.idObject, c.idChild, target, topLevel); got != c.want {
			t.Errorf("%s: kind %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAppWatchPendingAndSignals(t *testing.T) {
	w := &AppWatch{target: 10, pending: map[uint64]bool{}, events: make(chan struct{}, 1)}
	if w.apply(windowCreated, 20) || !w.Pending() {
		t.Fatal("a created window is pending and no signal")
	}
	if !w.apply(windowShown, 20) || w.Pending() {
		t.Fatal("a shown window is a signal and no longer pending")
	}
	w.apply(windowCreated, 21)
	if w.apply(windowDestroyed, 21) || w.Pending() {
		t.Fatal("a pending window destroyed is no longer pending, and no signal")
	}
	if !w.apply(windowDestroyed, 10) {
		t.Fatal("the target destroyed is a signal")
	}
	if !w.apply(targetHidden, 10) {
		t.Fatal("the target hidden is a signal")
	}
}

func TestAppWatchWaitReturnsOnASignal(t *testing.T) {
	w := &AppWatch{target: 10, pending: map[uint64]bool{}, events: make(chan struct{}, 1)}
	if w.Wait(0) {
		t.Fatal("no signal yet")
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		w.mu.Lock()
		w.apply(windowCreated, 20)
		w.mu.Unlock()
		w.events <- struct{}{}
	}()
	start := time.Now()
	if !w.Wait(5 * time.Second) {
		t.Fatal("Wait timed out, want it to return on the signal")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Wait took %s, want it to return as the signal arrives", d)
	}
	if w.Wait(time.Millisecond) {
		t.Error("the signal was taken once")
	}
}

func TestAppQuiet(t *testing.T) {
	idle := AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 40, 2: 7}}
	same := AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 40, 2: 7}}
	cases := []struct {
		name string
		a, b AppSample
		want bool
	}{
		{"nothing ran", idle, same, true},
		{"the UI thread woke", idle, AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 41, 2: 7}}, false},
		{"a worker woke", idle, AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 40, 2: 8}}, false},
		{"a thread started", idle, AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 40, 2: 7, 3: 1}}, false},
		{"a thread ended and another started", idle, AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 40, 3: 7}}, false},
		{"a thread is running", idle, AppSample{OK: true, UIWaiting: true, Busy: true, Switches: same.Switches}, false},
		{"the UI thread is not waiting for messages", idle, AppSample{OK: true, Switches: same.Switches}, false},
		{"the process is gone", idle, AppSample{}, false},
		{"no first sample", AppSample{}, same, false},
	}
	for _, c := range cases {
		if got := AppQuiet(c.a, c.b); got != c.want {
			t.Errorf("%s: quiet %v, want %v", c.name, got, c.want)
		}
	}
}

// snapshotBuffer builds a SystemProcessInformation buffer: one entry per
// process, its threads after it.
func snapshotBuffer(procs map[uint32][]systemThreadInformation, order []uint32) []byte {
	procSize := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	threadSize := int(unsafe.Sizeof(systemThreadInformation{}))
	var buf []byte
	for i, pid := range order {
		threads := procs[pid]
		entry := make([]byte, procSize+len(threads)*threadSize)
		p := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&entry[0]))
		p.UniqueProcessID = uintptr(pid)
		p.NumberOfThreads = uint32(len(threads))
		if i < len(order)-1 {
			p.NextEntryOffset = uint32(len(entry))
		}
		for j, th := range threads {
			*(*systemThreadInformation)(unsafe.Pointer(&entry[procSize+j*threadSize])) = th
		}
		buf = append(buf, entry...)
	}
	return buf
}

func TestAppSampleFromSnapshot(t *testing.T) {
	waitingUI := systemThreadInformation{UniqueThread: 11, ThreadState: threadWaiting, WaitReason: waitWrUserRequest, ContextSwitches: 300}
	worker := systemThreadInformation{UniqueThread: 12, ThreadState: threadWaiting, WaitReason: 15, ContextSwitches: 9}
	running := systemThreadInformation{UniqueThread: 12, ThreadState: threadRunning, ContextSwitches: 9}
	buf := snapshotBuffer(map[uint32][]systemThreadInformation{
		4: {{UniqueThread: 5, ThreadState: threadRunning}},
		7: {waitingUI, worker},
		8: {waitingUI, running},
	}, []uint32{4, 7, 8})

	s := appSampleFrom(buf, 7, 11)
	if !s.OK || !s.UIWaiting || s.Busy || s.Switches[11] != 300 || s.Switches[12] != 9 || len(s.Switches) != 2 {
		t.Errorf("idle App sampled as %+v", s)
	}
	if s := appSampleFrom(buf, 8, 11); !s.OK || !s.Busy {
		t.Errorf("an App with a running thread sampled as %+v, want busy", s)
	}
	if s := appSampleFrom(buf, 7, 12); s.UIWaiting {
		t.Error("a worker waiting on its queue is not a UI thread waiting for messages")
	}
	if s := appSampleFrom(buf, 99, 11); s.OK {
		t.Error("a process not in the snapshot is not found")
	}
	if s := appSampleFrom(buf[:len(buf)-8], 8, 11); s.OK {
		t.Error("a cut snapshot is not read")
	}
}

// TestSampleProcessSeesAThreadWaitingForMessages samples this test
// process: a thread blocked in GetMessage is a UI thread waiting for
// messages, and while it stays blocked two samples find it quiet.
func TestSampleProcessSeesAThreadWaitingForMessages(t *testing.T) {
	tid := make(chan uint32)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		var m msg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoremove)
		tid <- windows.GetCurrentThreadId()
		procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
	}()
	ui := <-tid
	defer func() {
		procPostThreadMessageW.Call(uintptr(ui), wmQuit, 0, 0)
		<-done
	}()

	pid := windows.GetCurrentProcessId()
	deadline := time.Now().Add(2 * time.Second)
	var s AppSample
	for {
		s = sampleProcess(pid, ui)
		if s.UIWaiting || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !s.OK || !s.UIWaiting {
		t.Fatalf("the thread blocked in GetMessage sampled as OK %v, waiting for messages %v", s.OK, s.UIWaiting)
	}
	if _, ok := s.Switches[ui]; !ok {
		t.Error("the sample has no context switch count for the thread")
	}
	if s := sampleProcess(pid, windows.GetCurrentThreadId()); s.UIWaiting {
		t.Error("the running test thread sampled as waiting for messages")
	}
}

// BenchmarkSampleProcess is one App sample: a system process snapshot
// and the walk to this process's threads.
func BenchmarkSampleProcess(b *testing.B) {
	pid, tid := windows.GetCurrentProcessId(), windows.GetCurrentThreadId()
	for b.Loop() {
		if !sampleProcess(pid, tid).OK {
			b.Fatal("this process was not found")
		}
	}
}
