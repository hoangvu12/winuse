package win

// The clipboard (desktop-parity ticket 01): clipboard-get, clipboard-set
// and clipboard-clear. OpenClipboard needs no foreground, so all three are
// Headless. The guards are agent-desktop's
// (crates/windows/src/input/clipboard/mod.rs, clipboard_session.rs,
// clipboard_write.rs):
//
//   - OpenClipboard is retried a few times a few milliseconds apart, the
//     way Chromium and agent-desktop open it; another program holding it
//     longer is reported, nothing read or changed.
//   - A clipboard owner that does not answer messages is refused first:
//     a delay-rendered GetClipboardData, and EmptyClipboard's
//     WM_DESTROYCLIPBOARD, wait on the owner without a time limit.
//   - Every session runs on a worker thread of its own that the caller
//     waits for with a time limit. A worker still parked in the owner's
//     render past it is abandoned (it holds the clipboard open until the
//     owner answers), and every later clipboard command is refused until
//     it has returned, instead of contending with it.
//   - A read is taken again when the clipboard changed under it.
//   - Writes open with a message-only owner window (SetClipboardData
//     fails under a NULL owner) and check afterwards that the clipboard
//     is still theirs.
//
// The session logic runs over clipAPI, so the offline tests fake the
// clipboard; the live calls refuse inside a go test binary
// (LiveDesktopGuard), since the clipboard is the user's.

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ClipFormat names what clipboard-get reads: text, image or files.
type ClipFormat string

const (
	ClipText  ClipFormat = "text"
	ClipImage ClipFormat = "image"
	ClipFiles ClipFormat = "files"
)

// ClipFormats are the formats, in the order a listing names them.
var ClipFormats = []ClipFormat{ClipText, ClipImage, ClipFiles}

// ClipContent is one representation on the clipboard: Text for text, PNG
// bytes and their size for an image, Files (absolute paths) for files.
type ClipContent struct {
	Format        ClipFormat
	Text          string
	PNG           []byte
	Width, Height int
	Files         []string
}

// The open retry, agent-desktop's (Chromium's) budget: a handful of
// attempts a few milliseconds apart.
const (
	clipOpenAttempts = 5
	clipOpenInterval = 5 * time.Millisecond

	// clipOwnerProbe bounds the WM_NULL that asks whether the clipboard's
	// owner answers messages.
	clipOwnerProbe = 250 * time.Millisecond

	// clipReadAttempts is how often a read runs while the clipboard keeps
	// changing under it.
	clipReadAttempts = 2
)

// ClipboardBusyError: OpenClipboard kept failing because another program
// has the clipboard open. Nothing was read or changed.
type ClipboardBusyError struct{ Holder uint64 } // the holding window from GetOpenClipboardWindow, or 0

func (e *ClipboardBusyError) Error() string {
	if e.Holder != 0 {
		return fmt.Sprintf("another program has the clipboard open (window %d); nothing was sent", e.Holder)
	}
	return "another program has the clipboard open; nothing was sent"
}

// ClipboardHungError: the clipboard's owner does not answer messages, so
// reading or replacing what it put there could hang. Nothing was sent.
type ClipboardHungError struct{ Owner uint64 }

func (e *ClipboardHungError) Error() string {
	return fmt.Sprintf("the program that owns the clipboard (window %d) is not responding, and the clipboard waits on it; nothing was sent", e.Owner)
}

// ClipboardTimeoutError: the session did not finish in time and its worker
// was abandoned. A read changed nothing; a write may have taken effect.
type ClipboardTimeoutError struct{ Write bool }

func (e *ClipboardTimeoutError) Error() string {
	if e.Write {
		return "the clipboard did not answer in time; the change may have taken effect"
	}
	return "the clipboard's owner did not hand over its content in time; nothing was changed"
}

// ErrClipboardWorker: an abandoned session's worker is still waiting on
// the clipboard's owner and holds the clipboard open.
var ErrClipboardWorker = errors.New("an earlier clipboard command is still waiting for the clipboard's owner and holds the clipboard open; nothing was sent")

// ErrClipboardChanging: the clipboard changed during every read attempt.
var ErrClipboardChanging = errors.New("the clipboard kept changing while winuse read it; nothing was changed")

// ClipboardMissingError: the clipboard holds nothing in the asked format;
// Has lists the formats it does hold.
type ClipboardMissingError struct {
	Want ClipFormat
	Has  []ClipFormat
}

func (e *ClipboardMissingError) Error() string {
	if len(e.Has) == 0 {
		return "the clipboard is empty"
	}
	return fmt.Sprintf("the clipboard holds no %s", e.Want)
}

// ClipboardWriteError: a write failed after EmptyClipboard, so the
// clipboard was emptied and may hold part of the content.
type ClipboardWriteError struct{ Err error }

func (e *ClipboardWriteError) Error() string {
	return fmt.Sprintf("writing the clipboard failed after it was emptied, so the change may have taken effect: %v", e.Err)
}

func (e *ClipboardWriteError) Unwrap() error { return e.Err }

// clipAPI is the Win32 clipboard the session logic drives. liveClipAPI
// makes the calls; tests fake it.
type clipAPI interface {
	open(owner uintptr) bool                  // OpenClipboard
	close()                                   // CloseClipboard
	empty() bool                              // EmptyClipboard
	available(format uint32) bool             // IsClipboardFormatAvailable
	data(format uint32) ([]byte, bool, error) // GetClipboardData, copied out; false when the format is absent
	set(format uint32, data []byte) error     // GlobalAlloc, SetClipboardData
	owner() uintptr                           // GetClipboardOwner
	openWindow() uintptr                      // GetOpenClipboardWindow
	sequence() uint32                         // GetClipboardSequenceNumber
	responsive(hwnd uintptr) bool             // WM_NULL through SendMessageTimeout, SMTO_ABORTIFHUNG
	pngFormat() uint32                        // the registered "PNG" format; 0 if it could not be registered
	writeOwner() (uintptr, error)             // the message-only window writes open the clipboard with
	sleep(d time.Duration)
}

// Clipboard runs clipboard sessions. One per helper: its count of
// abandoned workers refuses the commands that follow one.
type Clipboard struct {
	api   clipAPI
	guard func() error

	outstanding atomic.Int32   // sessions still running, abandoned ones included
	ownWindow   atomic.Uintptr // the write owner window, once a write created it
}

// NewClipboard returns the live clipboard.
func NewClipboard() *Clipboard {
	return &Clipboard{api: &liveClipAPI{}, guard: LiveDesktopGuard}
}

// Get reads the clipboard in one format, waiting at most timeout. A
// clipboard without that format fails with *ClipboardMissingError.
func (c *Clipboard) Get(format ClipFormat, timeout time.Duration) (ClipContent, error) {
	if err := c.guard(); err != nil {
		return ClipContent{}, err
	}
	deadline := time.Now().Add(timeout)
	for attempt := 0; attempt < clipReadAttempts; attempt++ {
		if err := c.ownerAnswers(); err != nil {
			return ClipContent{}, err
		}
		before := c.api.sequence()
		var got ClipContent
		err := c.session(0, deadline, false, func() error {
			var err error
			got, err = c.read(format)
			return err
		})
		if err != nil {
			return ClipContent{}, err
		}
		if c.api.sequence() == before {
			return got, nil
		}
	}
	return ClipContent{}, ErrClipboardChanging
}

// Set replaces the clipboard's content with one representation, waiting
// at most timeout. Text goes on as CF_UNICODETEXT, an image (PNG bytes) as
// the "PNG" format and a CF_DIB, files as CF_HDROP.
func (c *Clipboard) Set(content ClipContent, timeout time.Duration) error {
	if err := c.guard(); err != nil {
		return err
	}
	formats, err := c.prepare(content)
	if err != nil {
		return fmt.Errorf("%v; nothing was sent", err)
	}
	return c.write(formats, timeout)
}

// Clear empties the clipboard, waiting at most timeout.
func (c *Clipboard) Clear(timeout time.Duration) error {
	if err := c.guard(); err != nil {
		return err
	}
	return c.write(nil, timeout)
}

// clipPayload is one format to write and its bytes.
type clipPayload struct {
	format uint32
	data   []byte
}

// prepare marshals the content before the clipboard is opened, so a bad
// argument changes nothing.
func (c *Clipboard) prepare(content ClipContent) ([]clipPayload, error) {
	switch content.Format {
	case ClipText:
		b, err := encodeClipText(content.Text)
		if err != nil {
			return nil, err
		}
		return []clipPayload{{cfUnicodeText, b}}, nil
	case ClipImage:
		img, _, err := decodeCheckedPNG(content.PNG)
		if err != nil {
			return nil, err
		}
		var out []clipPayload
		if f := c.api.pngFormat(); f != 0 {
			out = append(out, clipPayload{f, content.PNG})
		}
		return append(out, clipPayload{cfDIB, encodeDIB(img)}), nil
	case ClipFiles:
		b, err := encodeHDrop(content.Files)
		if err != nil {
			return nil, err
		}
		return []clipPayload{{cfHDrop, b}}, nil
	}
	return nil, fmt.Errorf("unknown clipboard format %q", content.Format)
}

// write empties the clipboard and puts formats on it (none: a clear).
func (c *Clipboard) write(formats []clipPayload, timeout time.Duration) error {
	if err := c.ownerAnswers(); err != nil {
		return err
	}
	owner, err := c.api.writeOwner()
	if err != nil {
		return fmt.Errorf("the clipboard's owner window could not be created; nothing was sent: %v", err)
	}
	c.ownWindow.Store(owner)
	return c.session(owner, time.Now().Add(timeout), true, func() error {
		if !c.api.empty() {
			return errors.New("EmptyClipboard failed; nothing was changed")
		}
		after := c.api.sequence()
		for _, p := range formats {
			if err := c.api.set(p.format, p.data); err != nil {
				return &ClipboardWriteError{Err: err}
			}
		}
		// agent-desktop's ownership re-check: another program writing in
		// between would own the clipboard now.
		if len(formats) > 0 && (c.api.owner() != owner || c.api.sequence() < after) {
			return &ClipboardWriteError{Err: errors.New("another program took the clipboard over during the write")}
		}
		return nil
	})
}

// ownerAnswers refuses a clipboard whose owner does not answer messages.
// No owner, or winuse's own window, needs no probe.
func (c *Clipboard) ownerAnswers() error {
	if c.outstanding.Load() > 0 {
		return ErrClipboardWorker
	}
	owner := c.api.owner()
	if owner == 0 || owner == c.ownWindow.Load() || c.api.responsive(owner) {
		return nil
	}
	return &ClipboardHungError{Owner: uint64(owner)}
}

// session opens the clipboard (with owner; 0 for a read) on a fresh
// OS-locked thread, runs fn, closes it, and waits until deadline. The
// clipboard is opened and closed by one thread, and the thread is never
// unlocked, so it exits with the goroutine. A session past its deadline
// is abandoned: it keeps running, counted in outstanding, and Get, Set
// and Clear refuse until it returns.
func (c *Clipboard) session(owner uintptr, deadline time.Time, write bool, fn func() error) error {
	if c.outstanding.Load() > 0 {
		return ErrClipboardWorker
	}
	c.outstanding.Add(1)
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		var err error
		if err = c.open(owner, deadline); err == nil {
			err = fn()
			c.api.close()
		}
		// Counted down before the answer, so the next command never sees
		// this finished session as abandoned.
		c.outstanding.Add(-1)
		done <- err
	}()
	wait := time.Until(deadline)
	if wait < 0 {
		wait = 0
	}
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		return &ClipboardTimeoutError{Write: write}
	}
}

// open is OpenClipboard with the retry; it reports the holder when every
// attempt failed.
func (c *Clipboard) open(owner uintptr, deadline time.Time) error {
	for attempt := 0; attempt < clipOpenAttempts; attempt++ {
		if c.api.open(owner) {
			return nil
		}
		if attempt+1 < clipOpenAttempts && time.Until(deadline) > clipOpenInterval {
			c.api.sleep(clipOpenInterval)
		}
	}
	return &ClipboardBusyError{Holder: uint64(c.api.openWindow())}
}

// read reads one format with the clipboard open.
func (c *Clipboard) read(format ClipFormat) (ClipContent, error) {
	got, ok, err := c.readFormat(format)
	if err != nil {
		return ClipContent{}, err
	}
	if !ok {
		return ClipContent{}, &ClipboardMissingError{Want: format, Has: c.held()}
	}
	return got, nil
}

func (c *Clipboard) readFormat(format ClipFormat) (ClipContent, bool, error) {
	switch format {
	case ClipText:
		b, ok, err := c.payload(cfUnicodeText)
		if !ok || err != nil {
			return ClipContent{}, false, err
		}
		text, err := decodeClipText(b)
		return ClipContent{Format: ClipText, Text: text}, err == nil, err
	case ClipFiles:
		b, ok, err := c.payload(cfHDrop)
		if !ok || err != nil {
			return ClipContent{}, false, err
		}
		files, err := decodeHDrop(b)
		return ClipContent{Format: ClipFiles, Files: files}, err == nil && len(files) > 0, err
	case ClipImage:
		return c.readImage()
	}
	return ClipContent{}, false, fmt.Errorf("unknown clipboard format %q", format)
}

// readImage prefers the "PNG" format, as is, then a CF_DIBV5, then a
// CF_DIB (which Windows also synthesizes from a CF_BITMAP), as PNG.
func (c *Clipboard) readImage() (ClipContent, bool, error) {
	if f := c.api.pngFormat(); f != 0 {
		b, ok, err := c.payload(f)
		if err != nil {
			return ClipContent{}, false, err
		}
		if ok {
			cfg, err := checkPNG(b)
			if err != nil {
				return ClipContent{}, false, fmt.Errorf("the clipboard's PNG: %v", err)
			}
			return ClipContent{Format: ClipImage, PNG: b, Width: cfg.Width, Height: cfg.Height}, true, nil
		}
	}
	for _, f := range []uint32{cfDIBV5, cfDIB} {
		b, ok, err := c.payload(f)
		if err != nil {
			return ClipContent{}, false, err
		}
		if !ok {
			continue
		}
		img, err := decodeDIB(b)
		if err != nil {
			return ClipContent{}, false, err
		}
		return pngContent(img)
	}
	return ClipContent{}, false, nil
}

func pngContent(img image.Image) (ClipContent, bool, error) {
	b, err := encodePNG(img)
	if err != nil {
		return ClipContent{}, false, err
	}
	r := img.Bounds()
	return ClipContent{Format: ClipImage, PNG: b, Width: r.Dx(), Height: r.Dy()}, true, nil
}

// payload reads one format's bytes; false when the clipboard lacks it.
func (c *Clipboard) payload(format uint32) ([]byte, bool, error) {
	if !c.api.available(format) {
		return nil, false, nil
	}
	return c.api.data(format)
}

// held lists the formats the clipboard holds, without reading them.
func (c *Clipboard) held() []ClipFormat {
	var has []ClipFormat
	if c.api.available(cfUnicodeText) {
		has = append(has, ClipText)
	}
	if f := c.api.pngFormat(); (f != 0 && c.api.available(f)) || c.api.available(cfDIBV5) || c.api.available(cfDIB) {
		has = append(has, ClipImage)
	}
	if c.api.available(cfHDrop) {
		has = append(has, ClipFiles)
	}
	return has
}

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardOwner          = user32.NewProc("GetClipboardOwner")
	procGetOpenClipboardWindow     = user32.NewProc("GetOpenClipboardWindow")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procIsHungAppWindow            = user32.NewProc("IsHungAppWindow")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
	procRtlMoveMemory              = kernel32.NewProc("RtlMoveMemory")
	clipOwnerWindow                atomic.Uintptr // the message-only owner window, once created
	clipOwnerOnce                  sync.Once
	clipOwnerErr                   error
	clipPNGOnce                    sync.Once
	clipPNG                        uint32
)

const (
	gmemMoveable = 0x0002
	wmNull       = 0x0000
	hwndMessage  = ^uintptr(2) // HWND_MESSAGE, (HWND)-3
)

// liveClipAPI makes the Win32 calls. Locked memory is copied with
// RtlMoveMemory, so no address returned by Windows becomes a Go pointer.
type liveClipAPI struct{}

func (liveClipAPI) open(owner uintptr) bool {
	r, _, _ := procOpenClipboard.Call(owner)
	return r != 0
}

func (liveClipAPI) close() { procCloseClipboard.Call() }

func (liveClipAPI) empty() bool {
	r, _, _ := procEmptyClipboard.Call()
	return r != 0
}

func (liveClipAPI) available(format uint32) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(uintptr(format))
	return r != 0
}

func (liveClipAPI) data(format uint32) ([]byte, bool, error) {
	h, _, _ := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil, false, nil
	}
	size, _, _ := procGlobalSize.Call(h)
	switch {
	case size == 0:
		return nil, false, errors.New("the clipboard's content has no size")
	case size > maxClipPayload:
		return nil, false, fmt.Errorf("the clipboard's content is larger than %d MiB", maxClipPayload>>20)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return nil, false, fmt.Errorf("GlobalLock failed on the clipboard's content: %v", err)
	}
	out := make([]byte, size)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), p, size)
	procGlobalUnlock.Call(h)
	return out, true, nil
}

func (liveClipAPI) set(format uint32, data []byte) error {
	if len(data) == 0 {
		return errors.New("an empty payload")
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc failed: %v", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed: %v", err)
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procGlobalUnlock.Call(h)
	// A successful SetClipboardData hands the memory to the system; a
	// failed one leaves it ours to free.
	if r, _, err := procSetClipboardData.Call(uintptr(format), h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData(%d) failed: %v", format, err)
	}
	return nil
}

func (liveClipAPI) owner() uintptr {
	r, _, _ := procGetClipboardOwner.Call()
	return r
}

func (liveClipAPI) openWindow() uintptr {
	r, _, _ := procGetOpenClipboardWindow.Call()
	return r
}

func (liveClipAPI) sequence() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

// responsive asks the owner for a WM_NULL. UIPI refuses the message to a
// window of a higher integrity level (an administrator App copied
// something); IsHungAppWindow, which UIPI does not filter, answers then.
func (liveClipAPI) responsive(hwnd uintptr) bool {
	var result uintptr
	r, _, err := procSendMessageTimeoutW.Call(hwnd, wmNull, 0, 0,
		smtoAbortIfHung, uintptr(clipOwnerProbe.Milliseconds()), uintptr(unsafe.Pointer(&result)))
	if r != 0 {
		return true
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		hung, _, _ := procIsHungAppWindow.Call(hwnd)
		return hung == 0
	}
	return false
}

func (liveClipAPI) pngFormat() uint32 {
	clipPNGOnce.Do(func() {
		name, _ := windows.UTF16PtrFromString("PNG")
		r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		clipPNG = uint32(r)
	})
	return clipPNG
}

// writeOwner creates, once per process, a message-only STATIC window on a
// pumping thread of its own (the clipboard sends its owner
// WM_DESTROYCLIPBOARD when another program empties it). It never shows
// and is never listed: EnumWindows skips message-only windows.
func (liveClipAPI) writeOwner() (uintptr, error) {
	clipOwnerOnce.Do(func() {
		_, clipOwnerErr = startUIThread(func() error {
			class, _ := windows.UTF16PtrFromString("STATIC")
			title, _ := windows.UTF16PtrFromString("winuse clipboard owner")
			inst, _, _ := procGetModuleHandleW.Call(0)
			h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
				0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
			if h == 0 {
				return fmt.Errorf("CreateWindowEx: %v", err)
			}
			clipOwnerWindow.Store(h)
			return nil
		})
	})
	if clipOwnerErr != nil {
		return 0, clipOwnerErr
	}
	return clipOwnerWindow.Load(), nil
}

func (liveClipAPI) sleep(d time.Duration) { time.Sleep(d) }
