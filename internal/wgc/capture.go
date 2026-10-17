package wgc

// The capture flow (wgc.md §2): support gate, borderless consent,
// window-shape pre-checks, then one capture — item from the HWND, the
// free-threaded pool at the item's size, session with the optional
// cursor/border/secondary-window options, StartCapture, poll
// TryGetNextFrame within the budget, copy the frame texture to a staging
// texture and Map it, crop, cross-check against the Window's extended
// frame bounds, retry once if the frame is near-black — and on any doubt,
// return an error so the capture chain falls back to the GDI methods.
//
// Everything below the Method runs on the dedicated worker thread; the
// Method is the shot.Method face the chain calls from any goroutine.

import (
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows"
)

// Budgets and limits (wgc.md §5: about 2 s for WGC total, well inside the
// 10 s request budget; the watchdog adds slack for setup and teardown so
// the GDI fallbacks still fit).
const (
	// DefaultBudget bounds one whole WGC capture: setup, polling and
	// mapping. Overridable with WINUSE_WGC_BUDGET (a duration like
	// "1500ms" or a plain number of seconds).
	DefaultBudget = 2 * time.Second

	// blankRetryBudget bounds the extra wait for a second frame after a
	// near-black first one (the classic "nothing rendered yet" frame).
	blankRetryBudget = 150 * time.Millisecond

	// consentBudget bounds the one-shot borderless-consent request.
	consentBudget = 1500 * time.Millisecond

	// pollInterval is the sleep between TryGetNextFrame polls.
	pollInterval = 8 * time.Millisecond

	// frameSizeTolerance is how far the captured frame's size may be from
	// the Window's extended frame bounds (DWM rounding) before the frame
	// is distrusted and the capture falls back.
	frameSizeTolerance = 4

	// poolBuffers is the frame pool's buffer count: double-buffered, so
	// the pool can compose the next frame while the current one is being
	// read.
	poolBuffers = 2
)

// watchdogOverhead is what the caller-side watchdog adds to the budget:
// the one-shot borderless consent, the one blank-frame retry, and slack
// for setup, mapping and teardown — so a healthy first capture is never
// abandoned, while a wedged one still falls back well inside the 10 s
// request budget (the GDI methods fit behind it).
const watchdogOverhead = consentBudget + blankRetryBudget + 1500*time.Millisecond

// ErrDisabled reports that Windows.Graphics.Capture was switched off
// (WINUSE_WGC=off): the capture chain starts at the GDI methods instead.
var ErrDisabled = errors.New("Windows.Graphics.Capture is disabled (WINUSE_WGC=off)")

// ErrUnsupported reports that this Windows build cannot do
// Windows.Graphics.Capture at all (before Windows 10 1903, or the
// GraphicsCaptureSession statics say no): the GDI methods take over.
var ErrUnsupported = errors.New("Windows.Graphics.Capture is not supported on this system")

// ErrWorkerFailed reports that the capture worker thread could not start
// (WinRT or Direct3D unavailable in this helper process).
var ErrWorkerFailed = errors.New("Windows.Graphics.Capture could not start in the helper")

// win32 pre-checks. GetWindowDisplayAffinity is duplicated here (win
// keeps a private copy for the region method) so this package needs no
// windowing exports of its own.
var procGetWindowDisplayAffinity = windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowDisplayAffinity")

// wdaNone is GetWindowDisplayAffinity's "no restriction" value; any other
// affinity means the Window excludes itself from capture.
const wdaNone = 0

// displayAffinity returns the Window's SetWindowDisplayAffinity value, or
// 0 when it cannot be read.
func displayAffinity(hwnd uint64) uint32 {
	var aff uint32
	if r, _, _ := procGetWindowDisplayAffinity.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&aff))); r == 0 {
		return 0
	}
	return aff
}

// Method is the capture chain's Windows.Graphics.Capture step: it
// implements shot.Method and is prepended in front of the GDI methods
// (internal/helper wires the chain). Construct with New.
//
// All WinRT and Direct3D work runs on one dedicated OS-locked,
// RoInitialize(MTA) worker thread, reached through a job queue — separate
// from the UIA thread, so a WGC stall can never block a tree read. A
// worker wedged past its watchdog is abandoned (its thread is leaked,
// never killed) and the next capture starts a replacement.
type Method struct {
	disabled bool
	budget   time.Duration

	// Log is an optional diagnostic sink: the helper wires log.Printf so
	// which method captured each frame lands in the helper log. Nil means
	// quiet.
	Log func(format string, args ...any)

	mu     sync.Mutex
	worker *worker
}

// New creates the capture method, reading its environment knobs: WINUSE_WGC
// ("off" disables it — the chain then starts at the screen-region capture)
// and WINUSE_WGC_BUDGET (a duration like "1500ms" or a plain number of
// seconds; the default is DefaultBudget). Invalid values keep the
// defaults. The knobs are helper-side: they are read once per helper
// process at first use.
func New() *Method {
	m := &Method{budget: DefaultBudget}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WINUSE_WGC"))) {
	case "off", "0", "false", "no", "disabled":
		m.disabled = true
	}
	if v := strings.TrimSpace(os.Getenv("WINUSE_WGC_BUDGET")); v != "" {
		if d, err := durationOrSeconds(v); err == nil && d > 0 {
			m.budget = d
		}
	}
	return m
}

// durationOrSeconds parses a duration ("250ms", "2s") or a plain number
// of seconds ("3").
func durationOrSeconds(v string) (time.Duration, error) {
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	var secs int
	if _, err := fmt.Sscanf(v, "%d", &secs); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("%q is neither a duration nor a number of seconds", v)
}

// Name implements shot.Method.
func (m *Method) Name() string { return "Windows.Graphics.Capture" }

// CaptureWindow implements shot.Method. A failure — any failure — is
// this method saying "fall back to the next one"; it never retries a
// whole capture by itself, and it never blocks longer than its budget
// plus the watchdog slack.
func (m *Method) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	if m.disabled {
		err := ErrDisabled
		m.logf("wgc: window %d fell back: %v", hwnd, err)
		return shot.Frame{}, err
	}
	w, err := m.ensureWorker()
	if err != nil {
		return shot.Frame{}, err
	}
	type captureResult struct {
		frame shot.Frame
		err   error
	}
	done := make(chan captureResult, 1)
	job := func() {
		defer func() {
			if r := recover(); r != nil {
				done <- captureResult{err: fmt.Errorf("the Windows.Graphics.Capture worker crashed: %v", r)}
				m.abandon(w) // its COM state is suspect; replace it
			}
		}()
		f, err := w.th.captureWindow(hwnd, m.budget)
		done <- captureResult{frame: f, err: err}
	}
	if !w.send(job) {
		return shot.Frame{}, errors.New("the Windows.Graphics.Capture worker is gone; capture again (the helper replaces it)")
	}
	select {
	case r := <-done:
		if r.err == nil {
			m.logf("wgc: captured window %d (%dx%d px)", hwnd, r.frame.Rect.Dx(), r.frame.Rect.Dy())
		} else {
			m.logf("wgc: window %d fell back: %v", hwnd, r.err)
		}
		return r.frame, r.err
	case <-time.After(m.budget + watchdogOverhead):
		m.abandon(w)
		err := fmt.Errorf("no answer within %s (the capture worker was abandoned; retrying falls back)",
			m.budget+watchdogOverhead)
		m.logf("wgc: window %d fell back: %v", hwnd, err)
		return shot.Frame{}, err
	case <-w.exited:
		err := errors.New("the Windows.Graphics.Capture worker exited mid-capture; capture again")
		m.logf("wgc: window %d fell back: %v", hwnd, err)
		return shot.Frame{}, err
	}
}

// logf writes to the optional diagnostic sink (a no-op when unwired).
func (m *Method) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(format, args...)
	}
}

// ensureWorker returns the current worker, starting one if needed.
func (m *Method) ensureWorker() (*worker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker != nil {
		return m.worker, nil
	}
	w, err := startWorker()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkerFailed, err)
	}
	m.worker = w
	return w, nil
}

// abandon gives up on w: it is detached so the next capture starts a
// replacement, and its job channel is closed so the thread winds down
// after its current job (or is leaked if that job never returns).
func (m *Method) abandon(w *worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == w {
		m.worker = nil
		w.close()
	}
}

// worker is one OS-locked, RoInitialize(MTA) thread serving capture jobs.
// The thread owns no window and pumps no messages: the free-threaded
// frame pool needs neither.
type worker struct {
	jobs   chan func()
	exited chan struct{}
	th     *thread // set before the worker answers ready

	mu     sync.Mutex // guards jobs' closure against concurrent sends
	closed bool
}

// send delivers one job to the worker thread, reporting false when the
// worker is already winding down.
func (w *worker) send(job func()) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	select {
	case w.jobs <- job:
		return true
	case <-w.exited:
		return false
	}
}

// close winds the worker down: its job channel is closed so the thread
// releases its COM state after the current job and exits.
func (w *worker) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		close(w.jobs)
	}
}

// startWorker starts the capture thread. It returns only after the thread
// has entered the MTA (or failed).
func startWorker() (*worker, error) {
	w := &worker{jobs: make(chan func()), exited: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, _, _ := procRoInitialize.Call(roInitMultithreaded)
		hr := hresult(int32(r))
		var uninit bool
		switch {
		case hr == 0 || hr == 1: // S_OK / S_FALSE: one matching RoUninitialize
			uninit = true
		case uint32(hr) == rpcEChangedMode:
			// The thread is already initialized another way; carry on
			// (wgc.md §2) without uninitializing it.
		default:
			ready <- fmt.Errorf("RoInitialize failed (HRESULT %s)", hr)
			close(w.exited)
			return
		}
		w.th = &thread{}
		ready <- nil
		for job := range w.jobs {
			job()
		}
		// Wind-down: drop the cached factories and device while the
		// apartment is still alive, then leave it.
		w.th.dispose()
		if uninit {
			procRoUninitialize.Call()
		}
		close(w.exited)
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return w, nil
}

// thread is the worker thread's COM state: the cached activation
// factories and Direct3D device (created on first use, rebuilt after a
// device loss), the one-time support and consent results, and the
// IncludeSecondaryWindows capability learned from the first session.
type thread struct {
	itemFactory comPtr // IGraphicsCaptureItemInterop
	poolFactory comPtr // IDirect3D11CaptureFramePoolStatics2
	dev, ctx    comPtr // ID3D11Device, ID3D11DeviceContext
	rtDev       comPtr // WinRT IDirect3DDevice for CreateFreeThreaded

	support          int8 // 0 unknown, 1 supported, -1 unsupported
	consentRequested bool
	includeSecondary bool // this build's sessions carry Session6
}

// dispose releases the thread's cached COM objects (wind-down, or after a
// device loss).
func (th *thread) dispose() {
	release(th.itemFactory)
	release(th.poolFactory)
	th.itemFactory, th.poolFactory = nil, nil
	th.dropDevice()
}

// dropDevice forgets the cached Direct3D device (after a device-lost
// HRESULT): the next capture rebuilds it.
func (th *thread) dropDevice() {
	release(th.rtDev)
	release(th.ctx)
	release(th.dev)
	th.rtDev, th.ctx, th.dev = nil, nil, nil
}

// supported answers whether Windows.Graphics.Capture works here, checking
// once (IGraphicsCaptureSessionStatics::IsSupported) and caching the
// answer for the thread's lifetime. Below Windows 10 1903 the factory
// call itself fails; both land on the same fallback.
func (th *thread) supported() error {
	if th.support != 0 {
		if th.support < 0 {
			return ErrUnsupported
		}
		return nil
	}
	statics, h := activationFactory(classCaptureSession, &iidSessionStatics)
	if h.Failed() {
		th.support = -1
		return ErrUnsupported
	}
	defer release(statics)
	ok, h := sessionIsSupported(statics)
	if h.Failed() || !ok {
		th.support = -1
		return ErrUnsupported
	}
	th.support = 1
	return nil
}

// ensureDevice lazily creates the Direct3D device — hardware first, WARP
// (software) for machines without a usable GPU — and the WinRT device
// view the frame pool takes. The device is cached for the helper's
// lifetime and rebuilt after a device loss (dropDevice).
func (th *thread) ensureDevice() error {
	if th.rtDev != nil {
		return nil
	}
	dev, ctx, h := createD3D11Device(d3dDriverHardware)
	if h.Failed() {
		dev, ctx, h = createD3D11Device(d3dDriverWarp)
		if h.Failed() {
			return hr("D3D11CreateDevice", h)
		}
	}
	th.dev, th.ctx = dev, ctx
	rtDev, err := d3dDeviceFromD3D11(dev)
	if err != nil {
		release(ctx)
		release(dev)
		th.dev, th.ctx = nil, nil
		return err
	}
	th.rtDev = rtDev
	return nil
}

// ensureFactories lazily caches the activation factories the capture
// needs (item interop, frame-pool statics).
func (th *thread) ensureFactories() error {
	if th.itemFactory == nil {
		f, h := activationFactory(classCaptureItem, &iidItemInterop)
		if h.Failed() {
			return hr("RoGetActivationFactory "+classCaptureItem, h)
		}
		th.itemFactory = f
	}
	if th.poolFactory == nil {
		f, h := activationFactory(classFramePool, &iidPoolStatics2)
		if h.Failed() {
			return hr("RoGetActivationFactory "+classFramePool, h)
		}
		th.poolFactory = f
	}
	return nil
}

// captureWindow runs one whole capture on the worker thread. Every error
// return means "fall back to the next method"; the frame is returned only
// when it is trustworthy. deviceLost failures drop the cached device so
// the next capture rebuilds it.
func (th *thread) captureWindow(hwnd uint64, budget time.Duration) (_ shot.Frame, err error) {
	// One budget bounds the whole capture: the one-shot consent, the
	// window pre-checks, the Direct3D setup and the frame poll all draw
	// from it (wgc.md §5: about 2 s for WGC total).
	deadline := time.Now().Add(budget)
	defer func() {
		if err != nil {
			var hrErr *hrError
			if errors.As(err, &hrErr) && deviceLost(hrErr.hr) {
				th.dropDevice()
			}
		}
	}()
	if err := th.supported(); err != nil {
		return shot.Frame{}, err
	}
	if !th.consentRequested {
		requestBorderlessConsent(consentBudget)
		th.consentRequested = true
	}

	// Window-shape pre-checks, from the same enumeration the region
	// capture uses: gone, minimized, self-excluded, or carrying popups
	// this capture cannot include.
	all, err := win.Collect()
	if err != nil {
		return shot.Frame{}, err
	}
	var target win.WindowInfo
	found := false
	for _, w := range all {
		if w.ID == hwnd {
			target = w
			found = true
			break
		}
	}
	if !found {
		return shot.Frame{}, fmt.Errorf("%w (window %d): the Window is no longer enumerable", win.ErrGone, hwnd)
	}
	if target.Minimized {
		return shot.Frame{}, errors.New("the Window is minimized")
	}
	if aff := displayAffinity(hwnd); aff != wdaNone {
		return shot.Frame{}, fmt.Errorf("the Window excludes itself from capture (display affinity 0x%x)", aff)
	}
	// Windows.Graphics.Capture captures only the Window's own window
	// tree: popups (menus, tooltips, dropdowns) are separate top-level
	// windows and would be missing from the screenshot (spec, user story
	// 19). With them open, defer to the screen-region capture, which
	// unions them in — except on builds whose sessions carry
	// IncludeSecondaryWindows (24H2+), where popups within the Window
	// bounds are drawn into the capture.
	popups := win.PopupPresent(all, target)
	popupsOutside := win.PopupUnion(all, target) != target.Bounds
	if popups && (popupsOutside || !th.includeSecondary) {
		return shot.Frame{}, errors.New("the Window has popups open over it (the screen-region capture includes them)")
	}

	// The Direct3D and factory setup, cached across captures.
	if err := th.ensureFactories(); err != nil {
		return shot.Frame{}, err
	}
	if err := th.ensureDevice(); err != nil {
		return shot.Frame{}, err
	}

	// The capture item for the Window (its size is what the pool is
	// created at).
	item, h := interopCreateForWindow(th.itemFactory, uintptr(hwnd))
	if h.Failed() || item == nil {
		return shot.Frame{}, hr("CreateForWindow", h)
	}
	defer release(item)
	sz, h := itemSize(item)
	if h.Failed() || sz.W <= 0 || sz.H <= 0 {
		return shot.Frame{}, hr("GraphicsCaptureItem.Size", h)
	}

	// Pool and session.
	pool, h := createFreeThreaded(th.poolFactory, th.rtDev, dxgiFormatB8G8R8A8UNorm, poolBuffers, sz)
	if h.Failed() || pool == nil {
		return shot.Frame{}, hr("CreateFreeThreaded", h)
	}
	defer closeRelease(pool)
	session, h := createCaptureSession(pool, item)
	if h.Failed() || session == nil {
		return shot.Frame{}, hr("CreateCaptureSession", h)
	}
	defer closeRelease(session)

	// Session options, each feature-detected: a missing interface (an
	// older build) — or a refused option — is skipped, never fatal (wgc.md
	// §2; the missing-interface crash is Codex's issue #25178). Cursor
	// off — the GDI methods never include it either; border off where
	// Windows allows it (Win11); secondary windows on where they exist
	// (24H2), the capability this method's popup pre-check keys off.
	putSessionBoolean(session, &iidSession2, false)          // put_IsCursorCaptureEnabled
	putSessionBoolean(session, &iidSession3, false)          // put_IsBorderRequired
	if putSessionBoolean(session, &iidSession6, true).ok() { // put_IncludeSecondaryWindows
		th.includeSecondary = true
	}
	if h := startCapture(session); h.Failed() {
		return shot.Frame{}, hr("StartCapture", h)
	}

	// Poll for the first frame within the budget.
	frame, err := th.pollFrame(pool, deadline)
	if err != nil {
		return shot.Frame{}, err
	}
	defer func() { closeRelease(frame) }()

	// A Window that grew between item.Size and the frame would be clipped
	// by the pool: recreate the pool at the content size and take the
	// next frame instead (once — a Window still growing falls back to the
	// EFB cross-check below).
	content, h := frameContentSize(frame)
	if h.Failed() {
		return shot.Frame{}, hr("get_ContentSize", h)
	}
	if (content.W > sz.W || content.H > sz.H) && content.W > 0 && content.H > 0 {
		if h := poolRecreate(pool, th.rtDev, dxgiFormatB8G8R8A8UNorm, poolBuffers, content); h.Failed() {
			return shot.Frame{}, hr("Recreate", h)
		}
		sz = content
		closeRelease(frame)
		frame, err = th.pollFrame(pool, deadline)
		if err != nil {
			return shot.Frame{}, err
		}
		content, h = frameContentSize(frame)
		if h.Failed() {
			return shot.Frame{}, hr("get_ContentSize", h)
		}
	}

	img, crop, err := th.readFramePixels(frame, content)
	if err != nil {
		return shot.Frame{}, err
	}

	// A near-black frame is the classic "nothing was rendered yet"
	// failure (WDA_MONITOR windows, some GL apps): wait briefly for one
	// more frame and use it if it carries content. A frame that stays
	// black is returned as-is — the capture chain decides whether the
	// GDI methods do any better.
	if shot.IsBlank(img) {
		if f2, err := th.pollFrame(pool, time.Now().Add(blankRetryBudget)); err == nil && f2 != nil {
			c2, h := frameContentSize(f2)
			if h.ok() {
				if img2, crop2, err := th.readFramePixels(f2, c2); err == nil && !shot.IsBlank(img2) {
					closeRelease(frame)
					frame, img, crop = f2, img2, crop2
				} else {
					closeRelease(f2)
				}
			} else {
				closeRelease(f2)
			}
		}
	}

	// Cross-check against the Window's extended frame bounds, read fresh
	// at frame time: the WGC texture's (0,0) is the extended frame bounds
	// top-left, so a frame within tolerance of the bounds anchors exactly
	// like the GDI methods and ticket 08's coordinate mapping carries
	// over unchanged.
	efb, err := win.FrameBounds(hwnd)
	if err != nil {
		return shot.Frame{}, err
	}
	w, hh, ok := frameSize(int(crop.W), int(crop.H), int(efb.Width()), int(efb.Height()))
	if !ok {
		return shot.Frame{}, fmt.Errorf("the captured frame is %dx%d px but the Window is %dx%d px",
			crop.W, crop.H, efb.Width(), efb.Height())
	}
	return shot.Frame{
		Image:  subImage(img, w, hh),
		Rect:   image.Rect(int(efb.Left), int(efb.Top), int(efb.Left)+w, int(efb.Top)+hh),
		Anchor: image.Point{X: int(efb.Left), Y: int(efb.Top)},
	}, nil
}

// pollFrame waits for the pool's next frame until the deadline.
// TryGetNextFrame answering S_OK with no frame means "not ready yet" (the
// free-threaded pool composes on its own schedule); a deadline passing
// without a frame is the timeout class Codex hit on apps that never
// deliver — winuse keeps the budget short and falls back.
func (th *thread) pollFrame(pool comPtr, deadline time.Time) (comPtr, error) {
	for {
		frame, h := tryGetNextFrame(pool)
		if h.Failed() {
			return nil, hr("TryGetNextFrame", h)
		}
		if frame != nil {
			return frame, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("no frame arrived in time")
		}
		time.Sleep(pollInterval)
	}
}

// readFramePixels maps one frame's texture to an RGBA image: the WinRT
// surface, IDirect3DDxgiInterfaceAccess, then the ID3D11Texture2D behind
// it; a GPU-side copy to a staging texture; Map for CPU read; crop to the
// content size (min of ContentSize and the texture); convert from BGRA.
// The staging texture, the frame's texture, the access and the surface
// are released before returning; the frame itself is the caller's.
func (th *thread) readFramePixels(frame comPtr, content sizeInt32) (*image.RGBA, sizeInt32, error) {
	surface, h := frameSurface(frame)
	if h.Failed() || surface == nil {
		return nil, sizeInt32{}, hr("get_Surface", h)
	}
	defer release(surface)
	acc, h := queryInterface(surface, &iidDXGIAccess)
	if h.Failed() || acc == nil {
		return nil, sizeInt32{}, hr("QueryInterface(IDirect3DDxgiInterfaceAccess)", h)
	}
	defer release(acc)
	tex, h := getDXGITexture(acc, &iidTexture2D)
	if h.Failed() || tex == nil {
		return nil, sizeInt32{}, hr("GetInterface(ID3D11Texture2D)", h)
	}
	defer release(tex)

	desc := textureDesc(tex)
	cropW, cropH := int(content.W), int(content.H)
	if cropW <= 0 || cropW > int(desc.Width) {
		cropW = int(desc.Width)
	}
	if cropH <= 0 || cropH > int(desc.Height) {
		cropH = int(desc.Height)
	}
	if cropW <= 0 || cropH <= 0 {
		return nil, sizeInt32{}, errors.New("the capture texture is empty")
	}

	stagingDesc := desc
	stagingDesc.MipLevels, stagingDesc.ArraySize = 1, 1
	stagingDesc.SampleCount, stagingDesc.SampleQuality = 1, 0
	stagingDesc.Usage, stagingDesc.BindFlags = d3d11UsageStaging, 0
	stagingDesc.CPUAccessFlags, stagingDesc.MiscFlags = d3d11CPUAccessRead, 0
	staging, h := createStagingTexture(th.dev, stagingDesc)
	if h.Failed() || staging == nil {
		return nil, sizeInt32{}, hr("CreateTexture2D", h)
	}
	copyResource(th.ctx, staging, tex)
	m, h := mapStaging(th.ctx, staging)
	if h.Failed() {
		release(staging)
		return nil, sizeInt32{}, hr("Map", h)
	}
	src := unsafe.Slice((*byte)(m.pData), int(m.RowPitch)*cropH)
	img := rgbaFromBGRA(src, int(m.RowPitch), cropW, cropH)
	unmapStaging(th.ctx, staging)
	release(staging)
	if img == nil {
		return nil, sizeInt32{}, errors.New("the mapped frame is empty")
	}
	return img, sizeInt32{W: int32(cropW), H: int32(cropH)}, nil
}

// frameSize decides the frame's pixel size from the captured crop against
// the Window's extended frame bounds: the capture is trusted only when it
// is within frameSizeTolerance of the Window (otherwise the Window, or
// the pool, changed under the capture — fall back), and it is clipped to
// the Window's extent so the frame describes exactly what it shows. Pure
// function: offline-tested.
func frameSize(cropW, cropH, efbW, efbH int) (w, h int, ok bool) {
	if cropW <= 0 || cropH <= 0 || efbW <= 0 || efbH <= 0 {
		return 0, 0, false
	}
	if abs(cropW-efbW) > frameSizeTolerance || abs(cropH-efbH) > frameSizeTolerance {
		return 0, 0, false
	}
	return min(cropW, efbW), min(cropH, efbH), true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// rgbaFromBGRA converts one mapped BGRA texture into a tight RGBA image,
// cropping to w×h and honouring the row pitch (GPU textures are padded).
// Alpha is forced opaque: WGC leaves it undefined for some Windows, and
// screenshots are opaque by contract. Pure function: offline-tested.
func rgbaFromBGRA(src []byte, rowPitch, w, h int) *image.RGBA {
	if w <= 0 || h <= 0 || rowPitch < w*4 || len(src) < rowPitch*h {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := src[y*rowPitch:]
		dst := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			dst[4*x] = row[4*x+2] // R ← B
			dst[4*x+1] = row[4*x+1]
			dst[4*x+2] = row[4*x] // B ← R
			dst[4*x+3] = 255
		}
	}
	return img
}

// subImage returns the top-left w×h of img as a tight copy: the frame
// contract wants Rect and Image to agree exactly, and a shared SubImage
// would pin the whole mapped texture's rows alive.
func subImage(img *image.RGBA, w, h int) *image.RGBA {
	if img.Rect.Dx() == w && img.Rect.Dy() == h {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+w*4]
		copy(out.Pix[y*out.Stride:y*out.Stride+w*4], src)
	}
	return out
}
