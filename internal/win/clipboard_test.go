package win

// The clipboard's session logic (desktop-parity ticket 01) over a fake
// clipboard: format selection, the hung-owner refusal, the abandoned
// worker, the open retry and the write's ownership check. The live
// clipboard is the user's and is never touched here.

import (
	"errors"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const fakePNGFormat = 0xC123

// fakeClip is an in-memory clipboard. Every field is guarded by mu: the
// session runs on a worker goroutine.
type fakeClip struct {
	mu        sync.Mutex
	formats   map[uint32][]byte
	ownerWnd  uintptr
	seq       uint32
	hung      map[uintptr]bool
	openFails int           // OpenClipboard fails this many times first
	opens     int           // OpenClipboard calls
	isOpen    bool          // between open and close
	block     chan struct{} // data() waits on it, like a delay render
	blocked   chan struct{} // closed when data() starts waiting
	changeOn  int           // the sequence moves on this many reads
	writeWnd  uintptr
	stealOn   uint32 // set() of this format hands the clipboard to another owner
	probes    []uintptr
	calls     []string
}

func newFakeClip() *fakeClip {
	return &fakeClip{formats: map[uint32][]byte{}, hung: map[uintptr]bool{}, writeWnd: 900}
}

func (f *fakeClip) record(s string) { f.calls = append(f.calls, s) }

func (f *fakeClip) open(owner uintptr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openFails > 0 {
		f.openFails--
		return false
	}
	f.isOpen = true
	f.record("open")
	return true
}

func (f *fakeClip) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isOpen = false
	f.record("close")
}

func (f *fakeClip) empty() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formats = map[uint32][]byte{}
	f.ownerWnd = f.writeWnd
	f.seq++
	f.record("empty")
	return true
}

func (f *fakeClip) available(format uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.formats[format]
	return ok
}

func (f *fakeClip) data(format uint32) ([]byte, bool, error) {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil {
		close(f.blocked)
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("data")
	if f.changeOn > 0 {
		f.changeOn--
		f.seq++
	}
	b, ok := f.formats[format]
	return b, ok, nil
}

func (f *fakeClip) set(format uint32, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formats[format] = data
	f.seq++
	if format == f.stealOn {
		f.ownerWnd = 777
	}
	f.record("set")
	return nil
}

func (f *fakeClip) owner() uintptr {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ownerWnd
}

func (f *fakeClip) openWindow() uintptr { return 555 }

func (f *fakeClip) sequence() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seq
}

func (f *fakeClip) responsive(hwnd uintptr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes = append(f.probes, hwnd)
	return !f.hung[hwnd]
}

func (f *fakeClip) pngFormat() uint32            { return fakePNGFormat }
func (f *fakeClip) writeOwner() (uintptr, error) { return f.writeWnd, nil }
func (f *fakeClip) sleep(time.Duration)          {}

func (f *fakeClip) put(format uint32, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formats[format] = b
}

func fakeClipboard(f *fakeClip) *Clipboard {
	return &Clipboard{api: f, guard: func() error { return nil }}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	b, err := encodePNG(image.NewNRGBA(image.Rect(0, 0, w, h)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClipboardGetText(t *testing.T) {
	f := newFakeClip()
	b, _ := encodeClipText("hello 🙂")
	f.put(cfUnicodeText, b)
	got, err := fakeClipboard(f).Get(ClipText, time.Second)
	if err != nil || got.Text != "hello 🙂" || got.Format != ClipText {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if f.isOpen {
		t.Error("the clipboard was left open")
	}
}

// TestClipboardGetImagePrefersPNG: the registered PNG format is returned
// as is, before any bitmap.
func TestClipboardGetImagePrefersPNG(t *testing.T) {
	f := newFakeClip()
	png := testPNG(t, 5, 4)
	f.put(fakePNGFormat, png)
	f.put(cfDIB, encodeDIB(image.NewNRGBA(image.Rect(0, 0, 1, 1))))
	got, err := fakeClipboard(f).Get(ClipImage, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.PNG, png) || got.Width != 5 || got.Height != 4 {
		t.Errorf("Get = %dx%d (%d bytes), want the PNG format as is (5x4)", got.Width, got.Height, len(got.PNG))
	}
}

// TestClipboardGetImageFromDIB: without PNG, a CF_DIBV5 wins over a
// CF_DIB, and comes back as PNG.
func TestClipboardGetImageFromDIB(t *testing.T) {
	f := newFakeClip()
	v5 := encodeDIB(image.NewNRGBA(image.Rect(0, 0, 3, 2)))
	f.put(cfDIBV5, v5)
	f.put(cfDIB, encodeDIB(image.NewNRGBA(image.Rect(0, 0, 1, 1))))
	got, err := fakeClipboard(f).Get(ClipImage, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 3 || got.Height != 2 {
		t.Errorf("Get = %dx%d, want the CF_DIBV5's 3x2", got.Width, got.Height)
	}
	if _, err := checkPNG(got.PNG); err != nil {
		t.Errorf("the bitmap did not come back as PNG: %v", err)
	}
}

func TestClipboardGetFiles(t *testing.T) {
	f := newFakeClip()
	paths := []string{`C:\a.txt`, `C:\b c.txt`}
	b, _ := encodeHDrop(paths)
	f.put(cfHDrop, b)
	got, err := fakeClipboard(f).Get(ClipFiles, time.Second)
	if err != nil || !reflect.DeepEqual(got.Files, paths) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

// TestClipboardGetMissingListsWhatItHolds: asking for text from a
// clipboard holding an image and files names both; an empty clipboard
// says it is empty.
func TestClipboardGetMissingListsWhatItHolds(t *testing.T) {
	f := newFakeClip()
	f.put(cfDIB, encodeDIB(image.NewNRGBA(image.Rect(0, 0, 1, 1))))
	b, _ := encodeHDrop([]string{`C:\a`})
	f.put(cfHDrop, b)
	_, err := fakeClipboard(f).Get(ClipText, time.Second)
	var missing *ClipboardMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *ClipboardMissingError", err)
	}
	if !reflect.DeepEqual(missing.Has, []ClipFormat{ClipImage, ClipFiles}) || missing.Error() != "the clipboard holds no text" {
		t.Errorf("missing = %+v (%q)", missing, missing.Error())
	}

	_, err = fakeClipboard(newFakeClip()).Get(ClipImage, time.Second)
	if !errors.As(err, &missing) || missing.Error() != "the clipboard is empty" {
		t.Errorf("empty clipboard: err = %v", err)
	}
}

// TestClipboardRefusesAHungOwner: an owner that does not answer WM_NULL is
// refused before the clipboard is opened, for reads and writes alike.
func TestClipboardRefusesAHungOwner(t *testing.T) {
	f := newFakeClip()
	f.ownerWnd = 4242
	f.hung[4242] = true
	b, _ := encodeClipText("x")
	f.put(cfUnicodeText, b)
	c := fakeClipboard(f)
	var hung *ClipboardHungError
	if _, err := c.Get(ClipText, time.Second); !errors.As(err, &hung) || hung.Owner != 4242 {
		t.Fatalf("Get: err = %v, want the hung-owner refusal", err)
	}
	if err := c.Set(ClipContent{Format: ClipText, Text: "y"}, time.Second); !errors.As(err, &hung) {
		t.Fatalf("Set: err = %v, want the hung-owner refusal", err)
	}
	if err := c.Clear(time.Second); !errors.As(err, &hung) {
		t.Fatalf("Clear: err = %v, want the hung-owner refusal", err)
	}
	if f.opens != 0 {
		t.Errorf("the clipboard was opened %d times; a hung owner must be refused first", f.opens)
	}
	if !strings.Contains(hung.Error(), "nothing was sent") {
		t.Errorf("message %q must say nothing was sent", hung.Error())
	}
}

// TestClipboardSkipsTheProbeForItsOwnWindow: after a write the owner is
// winuse's own window, which is never probed.
func TestClipboardSkipsTheProbeForItsOwnWindow(t *testing.T) {
	f := newFakeClip()
	c := fakeClipboard(f)
	if err := c.Set(ClipContent{Format: ClipText, Text: "mine"}, time.Second); err != nil {
		t.Fatal(err)
	}
	f.probes = nil
	if got, err := c.Get(ClipText, time.Second); err != nil || got.Text != "mine" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if len(f.probes) != 0 {
		t.Errorf("probed %v; winuse's own window needs no probe", f.probes)
	}
}

// TestClipboardAbandonsASlowOwner pins the owner timeout: a read whose
// owner does not hand its content over in time fails, its worker is
// abandoned, and every clipboard command refuses until it returns.
func TestClipboardAbandonsASlowOwner(t *testing.T) {
	f := newFakeClip()
	b, _ := encodeClipText("late")
	f.put(cfUnicodeText, b)
	release := make(chan struct{})
	f.block = release
	f.blocked = make(chan struct{})
	c := fakeClipboard(f)

	start := time.Now()
	_, err := c.Get(ClipText, 100*time.Millisecond)
	var timeout *ClipboardTimeoutError
	if !errors.As(err, &timeout) || timeout.Write {
		t.Fatalf("err = %v, want a read timeout", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the read waited %s; the timeout must bound it", d)
	}
	<-f.blocked
	if !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("message %q must say nothing was changed", err.Error())
	}

	for name, run := range map[string]func() error{
		"get":   func() error { _, err := c.Get(ClipText, time.Second); return err },
		"set":   func() error { return c.Set(ClipContent{Format: ClipText, Text: "x"}, time.Second) },
		"clear": func() error { return c.Clear(time.Second) },
	} {
		if err := run(); !errors.Is(err, ErrClipboardWorker) {
			t.Errorf("%s while the worker waits: err = %v, want ErrClipboardWorker", name, err)
		}
	}

	f.mu.Lock()
	f.block = nil // later reads answer at once
	f.mu.Unlock()
	close(release) // the owner answers the parked read
	deadline := time.Now().Add(5 * time.Second)
	for c.outstanding.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the abandoned worker never finished after the owner answered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got, err := c.Get(ClipText, time.Second); err != nil || got.Text != "late" {
		t.Errorf("Get after the worker returned = %+v, %v", got, err)
	}
	if f.isOpen {
		t.Error("the abandoned worker left the clipboard open")
	}
}

// TestClipboardOpenRetries: OpenClipboard is retried a few times, then
// the holder is named and nothing is read.
func TestClipboardOpenRetries(t *testing.T) {
	f := newFakeClip()
	b, _ := encodeClipText("x")
	f.put(cfUnicodeText, b)
	f.openFails = clipOpenAttempts - 1
	if got, err := fakeClipboard(f).Get(ClipText, time.Second); err != nil || got.Text != "x" {
		t.Fatalf("Get after %d failed opens = %+v, %v", clipOpenAttempts-1, got, err)
	}

	f = newFakeClip()
	f.openFails = 100
	_, err := fakeClipboard(f).Get(ClipText, time.Second)
	var busy *ClipboardBusyError
	if !errors.As(err, &busy) || busy.Holder != 555 {
		t.Fatalf("err = %v, want the busy refusal naming window 555", err)
	}
	if f.opens != clipOpenAttempts {
		t.Errorf("OpenClipboard ran %d times, want %d", f.opens, clipOpenAttempts)
	}
}

// TestClipboardRereadsWhenItChanges: a read the clipboard changed under is
// taken again; one that changes every time fails.
func TestClipboardRereadsWhenItChanges(t *testing.T) {
	f := newFakeClip()
	b, _ := encodeClipText("x")
	f.put(cfUnicodeText, b)
	f.changeOn = 1
	if got, err := fakeClipboard(f).Get(ClipText, time.Second); err != nil || got.Text != "x" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	f.changeOn = clipReadAttempts
	if _, err := fakeClipboard(f).Get(ClipText, time.Second); !errors.Is(err, ErrClipboardChanging) {
		t.Errorf("err = %v, want ErrClipboardChanging", err)
	}
}

// TestClipboardSetFormats: text, image and files each write their formats
// under winuse's owner window, after EmptyClipboard.
func TestClipboardSetFormats(t *testing.T) {
	png := testPNG(t, 2, 2)
	cases := []struct {
		content ClipContent
		want    []uint32
	}{
		{ClipContent{Format: ClipText, Text: "hi"}, []uint32{cfUnicodeText}},
		{ClipContent{Format: ClipImage, PNG: png}, []uint32{fakePNGFormat, cfDIB}},
		{ClipContent{Format: ClipFiles, Files: []string{`C:\a`}}, []uint32{cfHDrop}},
	}
	for _, tc := range cases {
		f := newFakeClip()
		f.put(cfUnicodeText, []byte{'o', 0, 0, 0}) // the old content goes
		if err := fakeClipboard(f).Set(tc.content, time.Second); err != nil {
			t.Fatalf("%s: %v", tc.content.Format, err)
		}
		var got []uint32
		for k := range f.formats {
			got = append(got, k)
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: formats %v, want %v", tc.content.Format, got, tc.want)
		}
		for _, w := range tc.want {
			if _, ok := f.formats[w]; !ok {
				t.Errorf("%s: format %d missing", tc.content.Format, w)
			}
		}
		if f.ownerWnd != f.writeWnd {
			t.Errorf("%s: owner %d, want the write owner window", tc.content.Format, f.ownerWnd)
		}
		if !reflect.DeepEqual(f.calls[:2], []string{"open", "empty"}) || f.calls[len(f.calls)-1] != "close" {
			t.Errorf("%s: calls %v, want open, empty, ..., close", tc.content.Format, f.calls)
		}
	}
}

// TestClipboardSetRefusesBadContentFirst: a bad argument fails before the
// clipboard is opened, so nothing changes.
func TestClipboardSetRefusesBadContentFirst(t *testing.T) {
	for _, content := range []ClipContent{
		{Format: ClipText, Text: "a\x00b"},
		{Format: ClipImage, PNG: []byte("not a png")},
		{Format: ClipFiles},
	} {
		f := newFakeClip()
		if err := fakeClipboard(f).Set(content, time.Second); err == nil {
			t.Errorf("%s: Set accepted bad content", content.Format)
		}
		if f.opens != 0 {
			t.Errorf("%s: the clipboard was opened for bad content", content.Format)
		}
	}
}

// TestClipboardSetChecksOwnership: another owner after the write is a
// write that may have taken effect.
func TestClipboardSetChecksOwnership(t *testing.T) {
	f := newFakeClip()
	f.stealOn = cfUnicodeText
	err := fakeClipboard(f).Set(ClipContent{Format: ClipText, Text: "x"}, time.Second)
	var werr *ClipboardWriteError
	if !errors.As(err, &werr) || !strings.Contains(err.Error(), "may have taken effect") {
		t.Errorf("err = %v, want the may-have-taken-effect write failure", err)
	}
}

func TestClipboardClear(t *testing.T) {
	f := newFakeClip()
	b, _ := encodeClipText("x")
	f.put(cfUnicodeText, b)
	c := fakeClipboard(f)
	if err := c.Clear(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(f.formats) != 0 {
		t.Errorf("formats after Clear: %v", f.formats)
	}
	if _, err := c.Get(ClipText, time.Second); err == nil || err.Error() != "the clipboard is empty" {
		t.Errorf("Get after Clear: err = %v", err)
	}
}

// TestLiveClipboardRefusedInTests: the real clipboard is the user's.
func TestLiveClipboardRefusedInTests(t *testing.T) {
	if testing.Testing() && LiveDesktopGuard() == nil {
		t.Skip("WINUSE_E2E=1: the live guard is off")
	}
	c := NewClipboard()
	if _, err := c.Get(ClipText, time.Second); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("Get: err = %v, want ErrLiveDesktopInTest", err)
	}
	if err := c.Clear(time.Second); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("Clear: err = %v, want ErrLiveDesktopInTest", err)
	}
}
