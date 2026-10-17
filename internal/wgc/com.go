// Package wgc captures Window screenshots through Windows.Graphics.Capture
// — Windows's own capture stack, the spec's first capture method (Codex's
// behaviour) — from Go with no cgo: hand-written WinRT and Direct3D
// interop over syscall.SyscallN vtable calls. The recipe, including every
// IID and vtable slot below (verified against the Windows SDK 10.0.26100.0
// headers and IDL), is the project's research notes for this ticket,
// wgc.md; the pitfalls it lists are the design here:
//
//   - no FrameArrived handler, no async on the frame path, no
//     SoftwareBitmap, no DispatcherQueue — the free-threaded frame pool is
//     simply polled with a deadline (Codex's Win10 deadlock class, its
//     issue #35446);
//   - the cursor/border/secondary-window options are feature-detected by
//     QueryInterface and skipped, never assumed (Codex's Win10
//     IsBorderRequired crash, its issue #25178);
//   - everything runs on one dedicated OS-locked, RoInitialize(MTA)
//     worker thread, separate from the UIA thread, so a WGC stall can
//     never block a tree read; a worker wedged past the watchdog is
//     abandoned and replaced, never killed;
//   - the frame is cropped to min(ContentSize, texture size),
//     cross-checked against the Window's extended frame bounds read fresh
//     at frame time, and a near-black first frame is retried once — any
//     doubt falls back to the GDI methods (the chain, internal/shot).
//
// The only by-value struct argument (SizeInt32, 8 bytes) packs into a
// single register under the Windows x64 and ARM64 calling conventions; a
// compile-time guard below refuses 32-bit builds, where that packing
// would be wrong.
package wgc

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hresult is a Win32 HRESULT; a negative value is a failure.
type hresult int32

// Failed reports whether the HRESULT marks a failure.
func (h hresult) Failed() bool { return h < 0 }

// ok reports success (any non-negative HRESULT).
func (h hresult) ok() bool { return h >= 0 }

// String renders the HRESULT as 0x%08x, the style used in winuse errors.
func (h hresult) String() string { return fmt.Sprintf("0x%08x", uint32(h)) }

// comPtr is a raw COM interface pointer: it points at the object's vtable
// pointer, in non-Go memory the GC ignores. COM pointers are stored as
// comPtr (never as uintptr) so go vet stays quiet; they are only ever
// dereferenced from the worker thread that owns the apartment.
type comPtr = unsafe.Pointer

// vtable returns the function pointer at slot i of o's vtable. IUnknown
// occupies slots 0 (QueryInterface), 1 (AddRef), 2 (Release); IInspectable
// adds 3 (GetIids), 4 (GetRuntimeClassName), 5 (GetTrustLevel) — so the
// first method of a WinRT interface is slot 6, and of a plain COM
// interface slot 3.
func vtable(o comPtr, i uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(unsafe.Pointer(o))
	return *(*uintptr)(unsafe.Add(vtbl, i*unsafe.Sizeof(uintptr(0))))
}

// The flat exports, combase.dll (WinRT) and d3d11.dll (the Direct3D
// bridge). LazyProc.Call is //go:uintptrescapes, so passing converted
// pointers in its argument list keeps them alive and un-moved for the
// call.
var (
	combase = windows.NewLazySystemDLL("combase.dll")
	d3d11   = windows.NewLazySystemDLL("d3d11.dll")

	procRoInitialize        = combase.NewProc("RoInitialize")
	procRoUninitialize      = combase.NewProc("RoUninitialize")
	procRoGetFactory        = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString = combase.NewProc("WindowsDeleteString")

	procD3D11CreateDevice = d3d11.NewProc("D3D11CreateDevice")
	procDeviceFromDXGI    = d3d11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
)

// COM and WinRT constants (wgc.md §4; SDK headers).
const (
	roInitMultithreaded = 1                  // RO_INIT_MULTITHREADED
	rpcEChangedMode     = uint32(0x80010106) // thread already initialized otherwise: carry on
)

// GUIDs of every interface used, as struct literals (parsed-from-string
// would fail at runtime; these fail at compile time). Sources: the SDK
// 10.0.26100.0 IDL/headers listed in wgc.md's sources.
var (
	iidItemInterop    = windows.GUID{Data1: 0x3628e81b, Data2: 0x3cac, Data3: 0x4c60, Data4: [8]byte{0xb7, 0xf4, 0x23, 0xce, 0x0e, 0x0c, 0x33, 0x56}} // IGraphicsCaptureItemInterop
	iidItem           = windows.GUID{Data1: 0x79c3f95b, Data2: 0x31f7, Data3: 0x4ec2, Data4: [8]byte{0xa4, 0x64, 0x63, 0x2e, 0xf5, 0xd3, 0x07, 0x60}} // IGraphicsCaptureItem
	iidPoolStatics2   = windows.GUID{Data1: 0x589b103f, Data2: 0x6bbc, Data3: 0x5df5, Data4: [8]byte{0xa9, 0x91, 0x02, 0xe2, 0x8b, 0x3b, 0x66, 0xd5}} // IDirect3D11CaptureFramePoolStatics2
	iidSessionStatics = windows.GUID{Data1: 0x2224a540, Data2: 0x5974, Data3: 0x49aa, Data4: [8]byte{0xb2, 0x32, 0x08, 0x82, 0x53, 0x6f, 0x4c, 0xb5}} // IGraphicsCaptureSessionStatics
	iidSession2       = windows.GUID{Data1: 0x2c39ae40, Data2: 0x7d2e, Data3: 0x5044, Data4: [8]byte{0x80, 0x4e, 0x8b, 0x67, 0x99, 0xd4, 0xcf, 0x9e}} // IGraphicsCaptureSession2 (cursor)
	iidSession3       = windows.GUID{Data1: 0xf2cdd966, Data2: 0x22ae, Data3: 0x5ea1, Data4: [8]byte{0x95, 0x96, 0x3a, 0x28, 0x93, 0x44, 0xc3, 0xbe}} // IGraphicsCaptureSession3 (border)
	iidSession6       = windows.GUID{Data1: 0xd7419236, Data2: 0xbe20, Data3: 0x5e9f, Data4: [8]byte{0xbc, 0xd6, 0xc4, 0xe9, 0x8f, 0xd6, 0xaf, 0xdc}} // IGraphicsCaptureSession6 (secondary windows)
	iidAccessStatics  = windows.GUID{Data1: 0x743ed370, Data2: 0x06ec, Data3: 0x5040, Data4: [8]byte{0xa5, 0x8a, 0x90, 0x1f, 0x0f, 0x75, 0x70, 0x95}} // IGraphicsCaptureAccessStatics
	iidClosable       = windows.GUID{Data1: 0x30d5a829, Data2: 0x7fa4, Data3: 0x4026, Data4: [8]byte{0x83, 0xbb, 0xd7, 0x5b, 0xae, 0x4e, 0xa9, 0x9e}} // IClosable
	iidAsyncInfo      = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}} // IAsyncInfo
	iidDirect3DDevice = windows.GUID{Data1: 0xa37624ab, Data2: 0x8d5f, Data3: 0x4650, Data4: [8]byte{0x9d, 0x3e, 0x9e, 0xae, 0x3d, 0x9b, 0xc6, 0x70}} // IDirect3DDevice (WinRT)
	iidDXGIDevice     = windows.GUID{Data1: 0x54ec77fa, Data2: 0x1377, Data3: 0x44e6, Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}} // IDXGIDevice
	iidDXGIAccess     = windows.GUID{Data1: 0xa9b3d012, Data2: 0x3df2, Data3: 0x4ee3, Data4: [8]byte{0xb8, 0xd1, 0x86, 0x95, 0xf4, 0x57, 0xd3, 0xc1}} // IDirect3DDxgiInterfaceAccess
	iidTexture2D      = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}} // ID3D11Texture2D
)

// Activatable class names (HSTRINGs).
const (
	classCaptureItem    = "Windows.Graphics.Capture.GraphicsCaptureItem"
	classFramePool      = "Windows.Graphics.Capture.Direct3D11CaptureFramePool"
	classCaptureSession = "Windows.Graphics.Capture.GraphicsCaptureSession"
	classCaptureAccess  = "Windows.Graphics.Capture.GraphicsCaptureAccess"
)

// Direct3D constants (d3d11.h, dxgiformat.h, winerror.h).
const (
	dxgiFormatB8G8R8A8UNorm = 87   // = DirectXPixelFormat.B8G8R8A8UIntNormalized
	d3dDriverHardware       = 1    // D3D_DRIVER_TYPE_HARDWARE
	d3dDriverWarp           = 5    // D3D_DRIVER_TYPE_WARP (VM, RDP, no GPU)
	d3d11CreateBGRA         = 0x20 // D3D11_CREATE_DEVICE_BGRA_SUPPORT
	d3d11SDKVersion         = 7
	d3d11UsageStaging       = 3 // D3D11_USAGE_STAGING
	d3d11CPUAccessRead      = 0x20000
	d3d11MapRead            = 1 // D3D11_MAP_READ
	dxgiErrDeviceRemoved    = 0x887a0005
	dxgiErrDeviceReset      = 0x887a0007
	dxgiErrAccessLost       = 0x887a0026
)

// sizeInt32 is Windows.Graphics.SizeInt32: an 8-byte struct passed to
// CreateFreeThreaded/Recreate by value. Under the Windows x64 and ARM64
// ABIs an 8-byte struct of two int32s travels in one register, so reg()
// packs it; on a 32-bit build uintptr is 4 bytes, the high half would be
// lost silently, and the guard below turns that into a compile error.
type sizeInt32 struct{ W, H int32 }

// reg returns the by-value register form of s for SyscallN argument lists.
func (s sizeInt32) reg() uintptr {
	return uintptr(uint32(s.W)) | uintptr(uint32(s.H))<<32
}

// The 32-bit refusal: [0]struct{} on 64-bit (uintptr 8 → 1-1 = 0), a type
// mismatch on 32-bit (uintptr 4 → 1-0 = 1).
var _ = [1 - (unsafe.Sizeof(uintptr(0))-4)/4]struct{}{}

// texture2DDesc is D3D11_TEXTURE2D_DESC (44 bytes, d3d11.h).
type texture2DDesc struct {
	Width, Height, MipLevels, ArraySize         uint32
	Format                                      uint32 // DXGI_FORMAT
	SampleCount, SampleQuality                  uint32 // DXGI_SAMPLE_DESC inline
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

var _ [44]byte = [unsafe.Sizeof(texture2DDesc{})]byte{}

// mappedSubresource is D3D11_MAPPED_SUBRESOURCE (16 bytes, d3d11.h).
type mappedSubresource struct {
	pData      unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

var _ [16]byte = [unsafe.Sizeof(mappedSubresource{})]byte{}

// hrError carries the HRESULT of a failed vtable call: the message names
// the operation, and callers can match the code (device lost) to rebuild
// state.
type hrError struct {
	op string
	hr hresult
}

func (e *hrError) Error() string { return e.op + " failed (HRESULT " + e.hr.String() + ")" }

// hr wraps an HRESULT into an hrError (nil for success).
func hr(op string, h hresult) error {
	if h.ok() {
		return nil
	}
	return &hrError{op: op, hr: h}
}

// deviceLost reports whether an HRESULT means the Direct3D device died
// (removed, reset, or its access was lost): the cached device must be
// rebuilt before the next capture.
func deviceLost(h hresult) bool {
	switch uint32(h) {
	case dxgiErrDeviceRemoved, dxgiErrDeviceReset, dxgiErrAccessLost:
		return true
	}
	return false
}

// Every vtable call below is a tiny named function that passes converted
// pointers directly in syscall.SyscallN's argument list — SyscallN is
// //go:uintptrkeepalive, so the compiler pins the pointed-to values for
// the duration of the call (the one safe pattern; a shared generic
// wrapper would lose it, wgc.md §5).

// queryInterface is IUnknown::QueryInterface(o, iid, &out) — slot 0.
func queryInterface(o comPtr, iid *windows.GUID) (comPtr, hresult) {
	var out comPtr
	r, _, _ := syscall.SyscallN(vtable(o, 0), uintptr(o),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, hresult(int32(r))
}

// release drops one reference (IUnknown::Release — slot 2).
func release(o comPtr) {
	if o != nil {
		syscall.SyscallN(vtable(o, 2), uintptr(o))
	}
}

// closeRelease is the WinRT teardown for frames, sessions and pools:
// IClosable::Close (slot 6, via QueryInterface) then Release. Close stops
// a capture (and its border) immediately; failures are ignored — the
// Release still happens.
func closeRelease(o comPtr) {
	if o == nil {
		return
	}
	if c, h := queryInterface(o, &iidClosable); h.ok() && c != nil {
		syscall.SyscallN(vtable(c, 6), uintptr(c))
		release(c)
	}
	release(o)
}

// eFail is E_FAIL (0x80004005) as a negative HRESULT, for the single
// error path where Windows handed back no HRESULT at all.
const eFail = hresult(-2147467259)

// activationFactory returns the class's factory interface for iid
// (RoGetActivationFactory): the interop, statics and access interfaces
// all come from here. The HSTRING is deleted as soon as the factory
// exists. A failure means the class or interface is absent on this
// build of Windows — callers fall back.
func activationFactory(class string, iid *windows.GUID) (comPtr, hresult) {
	u, err := windows.UTF16FromString(class)
	if err != nil {
		return nil, eFail
	}
	var h uintptr // HSTRING
	r, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if hresult(int32(r)).Failed() {
		return nil, hresult(int32(r))
	}
	defer procWindowsDeleteString.Call(h)
	var f comPtr
	r, _, _ = procRoGetFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	return f, hresult(int32(r))
}

// interopCreateForWindow is IGraphicsCaptureItemInterop::CreateForWindow
// (slot 3): the HWND → GraphicsCaptureItem bridge. It fails with
// E_INVALIDARG for child/invalid windows (Codex issue #49383) and on
// Windows 10 before 1903.
func interopCreateForWindow(interop comPtr, hwnd uintptr) (comPtr, hresult) {
	var item comPtr
	r, _, _ := syscall.SyscallN(vtable(interop, 3), uintptr(interop), hwnd,
		uintptr(unsafe.Pointer(&iidItem)), uintptr(unsafe.Pointer(&item)))
	return item, hresult(int32(r))
}

// itemSize is IGraphicsCaptureItem::get_Size (slot 7).
func itemSize(item comPtr) (sizeInt32, hresult) {
	var s sizeInt32
	r, _, _ := syscall.SyscallN(vtable(item, 7), uintptr(item), uintptr(unsafe.Pointer(&s)))
	return s, hresult(int32(r))
}

// sessionIsSupported is IGraphicsCaptureSessionStatics::IsSupported
// (slot 6). The WinRT boolean is 1 byte.
func sessionIsSupported(statics comPtr) (bool, hresult) {
	var b byte
	r, _, _ := syscall.SyscallN(vtable(statics, 6), uintptr(statics), uintptr(unsafe.Pointer(&b)))
	return b != 0, hresult(int32(r))
}

// createFreeThreaded is IDirect3D11CaptureFramePoolStatics2::
// CreateFreeThreaded (slot 6): a pool that delivers frames on any
// thread — no DispatcherQueue, no FrameArrived needed.
func createFreeThreaded(statics, device comPtr, format uint32, buffers int32, size sizeInt32) (comPtr, hresult) {
	var pool comPtr
	r, _, _ := syscall.SyscallN(vtable(statics, 6), uintptr(statics), uintptr(device),
		uintptr(format), uintptr(uint32(buffers)), size.reg(), uintptr(unsafe.Pointer(&pool)))
	return pool, hresult(int32(r))
}

// poolRecreate is IDirect3D11CaptureFramePool::Recreate (slot 6):
// reconfigure the pool when the Window outgrew the size it was created
// with (the frames would otherwise be clipped).
func poolRecreate(pool, device comPtr, format uint32, buffers int32, size sizeInt32) hresult {
	r, _, _ := syscall.SyscallN(vtable(pool, 6), uintptr(pool), uintptr(device),
		uintptr(format), uintptr(uint32(buffers)), size.reg())
	return hresult(int32(r))
}

// tryGetNextFrame is IDirect3D11CaptureFramePool::TryGetNextFrame
// (slot 7). S_OK with a nil frame means "no frame ready yet".
func tryGetNextFrame(pool comPtr) (comPtr, hresult) {
	var frame comPtr
	r, _, _ := syscall.SyscallN(vtable(pool, 7), uintptr(pool), uintptr(unsafe.Pointer(&frame)))
	return frame, hresult(int32(r))
}

// createCaptureSession is IDirect3D11CaptureFramePool::
// CreateCaptureSession (slot 10).
func createCaptureSession(pool, item comPtr) (comPtr, hresult) {
	var session comPtr
	r, _, _ := syscall.SyscallN(vtable(pool, 10), uintptr(pool), uintptr(item), uintptr(unsafe.Pointer(&session)))
	return session, hresult(int32(r))
}

// startCapture is IGraphicsCaptureSession::StartCapture (slot 6).
func startCapture(session comPtr) hresult {
	r, _, _ := syscall.SyscallN(vtable(session, 6), uintptr(session))
	return hresult(int32(r))
}

// putSessionBoolean sets a boolean session option feature-detected by
// QueryInterface: IGraphicsCaptureSession2::put_IsCursorCaptureEnabled,
// Session3::put_IsBorderRequired and Session6::put_IncludeSecondaryWindows
// are all slot 7 of their interface. E_NOINTERFACE (the build predates
// the option) comes back as the error; callers treat it as "skip".
func putSessionBoolean(session comPtr, iid *windows.GUID, value bool) hresult {
	s, h := queryInterface(session, iid)
	if h.Failed() {
		return h
	}
	defer release(s)
	v := uintptr(0)
	if value {
		v = 1
	}
	r, _, _ := syscall.SyscallN(vtable(s, 7), uintptr(s), v)
	return hresult(int32(r))
}

// frameContentSize is IDirect3D11CaptureFrame::get_ContentSize
// (slot 8): how much of the pool-sized texture holds the Window right
// now.
func frameContentSize(frame comPtr) (sizeInt32, hresult) {
	var s sizeInt32
	r, _, _ := syscall.SyscallN(vtable(frame, 8), uintptr(frame), uintptr(unsafe.Pointer(&s)))
	return s, hresult(int32(r))
}

// frameSurface is IDirect3D11CaptureFrame::get_Surface (slot 6).
func frameSurface(frame comPtr) (comPtr, hresult) {
	var surface comPtr
	r, _, _ := syscall.SyscallN(vtable(frame, 6), uintptr(frame), uintptr(unsafe.Pointer(&surface)))
	return surface, hresult(int32(r))
}

// getDXGITexture is IDirect3DDxgiInterfaceAccess::GetInterface (slot 3):
// the WinRT surface's underlying ID3D11Texture2D.
func getDXGITexture(access comPtr, iid *windows.GUID) (comPtr, hresult) {
	var tex comPtr
	r, _, _ := syscall.SyscallN(vtable(access, 3), uintptr(access),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&tex)))
	return tex, hresult(int32(r))
}

// requestBorderlessConsent asks Windows for permission to capture without
// the yellow border: GraphicsCaptureAccess::RequestAccessAsync(Borderless)
// (statics slot 6), polled through IAsyncInfo::get_Status (slot 7) until
// it leaves Started, then Closed (slot 10). Everything about it is
// optional: a missing class (pre-Win11), a slow or denied request, any
// failure — the border may then show briefly and nothing else changes.
// Unpackaged apps are allowed without a prompt (wgc.md §5, from OBS
// behaviour), which is why winuse can call it unattended.
func requestBorderlessConsent(timeout time.Duration) {
	statics, h := activationFactory(classCaptureAccess, &iidAccessStatics)
	if h.Failed() || statics == nil {
		return
	}
	defer release(statics)
	var op comPtr
	r, _, _ := syscall.SyscallN(vtable(statics, 6), uintptr(statics),
		0 /*GraphicsCaptureAccessKind::Borderless*/, uintptr(unsafe.Pointer(&op)))
	if hresult(int32(r)).Failed() || op == nil {
		return
	}
	defer release(op)
	info, h := queryInterface(op, &iidAsyncInfo)
	if h.Failed() || info == nil {
		return
	}
	defer release(info)
	deadline := time.Now().Add(timeout)
	for {
		var status uint32 // AsyncStatus: 0 Started, 1 Completed, 2 Canceled, 3 Error
		r, _, _ := syscall.SyscallN(vtable(info, 7), uintptr(info), uintptr(unsafe.Pointer(&status)))
		if hresult(int32(r)).Failed() || status != 0 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	syscall.SyscallN(vtable(info, 10), uintptr(info)) // Close
}

// createD3D11Device is the flat D3D11CreateDevice with the BGRA flag WGC
// requires, hardware first and WARP (software) as the retry for machines
// without a usable GPU (VMs, RDP).
func createD3D11Device(driverType uint32) (dev, ctx comPtr, h hresult) {
	r, _, _ := procD3D11CreateDevice.Call(0, uintptr(driverType), 0, d3d11CreateBGRA, 0, 0, d3d11SDKVersion,
		uintptr(unsafe.Pointer(&dev)), 0, uintptr(unsafe.Pointer(&ctx)))
	return dev, ctx, hresult(int32(r))
}

// d3dDeviceFromD3D11 bridges the D3D11 device into WinRT: QueryInterface
// for IDXGIDevice, CreateDirect3D11DeviceFromDXGIDevice (d3d11.dll flat
// export), then QueryInterface the returned IInspectable for
// IDirect3DDevice — the interface CreateFreeThreaded takes. UniteHUD
// passes the raw IInspectable without the last step; wgc.md flags that as
// one of its bugs.
func d3dDeviceFromD3D11(dev comPtr) (comPtr, error) {
	dxgi, h := queryInterface(dev, &iidDXGIDevice)
	if h.Failed() {
		return nil, hr("QueryInterface(IDXGIDevice)", h)
	}
	defer release(dxgi)
	var insp comPtr
	r, _, _ := procDeviceFromDXGI.Call(uintptr(dxgi), uintptr(unsafe.Pointer(&insp)))
	if hresult(int32(r)).Failed() {
		return nil, hr("CreateDirect3D11DeviceFromDXGIDevice", hresult(int32(r)))
	}
	defer release(insp)
	rtDev, h := queryInterface(insp, &iidDirect3DDevice)
	if h.Failed() {
		return nil, hr("QueryInterface(IDirect3DDevice)", h)
	}
	return rtDev, nil
}

// textureDesc is ID3D11Texture2D::GetDesc (slot 10, void).
func textureDesc(tex comPtr) texture2DDesc {
	var d texture2DDesc
	syscall.SyscallN(vtable(tex, 10), uintptr(tex), uintptr(unsafe.Pointer(&d)))
	return d
}

// createStagingTexture is ID3D11Device::CreateTexture2D (slot 5) with the
// frame texture's desc rewritten for CPU readback: staging usage, read
// access, no bindings, one mip, one sample.
func createStagingTexture(dev comPtr, d texture2DDesc) (comPtr, hresult) {
	var staging comPtr
	r, _, _ := syscall.SyscallN(vtable(dev, 5), uintptr(dev),
		uintptr(unsafe.Pointer(&d)), 0, uintptr(unsafe.Pointer(&staging)))
	return staging, hresult(int32(r))
}

// copyResource is ID3D11DeviceContext::CopyResource (slot 47, void): the
// GPU-side copy from the capture texture to the staging texture. An
// ID3D11Texture2D* is usable as the ID3D11Resource* the method takes
// (single inheritance, same pointer).
func copyResource(ctx, dst, src comPtr) {
	syscall.SyscallN(vtable(ctx, 47), uintptr(ctx), uintptr(dst), uintptr(src))
}

// mapStaging is ID3D11DeviceContext::Map (slot 14) with D3D11_MAP_READ.
func mapStaging(ctx comPtr, staging comPtr) (mappedSubresource, hresult) {
	var m mappedSubresource
	r, _, _ := syscall.SyscallN(vtable(ctx, 14), uintptr(ctx), uintptr(staging),
		0, d3d11MapRead, 0, uintptr(unsafe.Pointer(&m)))
	return m, hresult(int32(r))
}

// unmapStaging is ID3D11DeviceContext::Unmap (slot 15, void).
func unmapStaging(ctx comPtr, staging comPtr) {
	syscall.SyscallN(vtable(ctx, 15), uintptr(ctx), uintptr(staging), 0)
}
