package fixture

// Toasts for the notification e2e tests (desktop-parity 18): the test
// process raises its own toast and hears its button presses, so the tests
// never act on a notification of the user's. PowerShell 5.1 cannot
// subscribe to WinRT events (probes round 5, section 7), so this is raw
// WinRT from Go: combase's activation factories and the toast interfaces
// by vtable slot, no cgo and no new dependency.
//
// The toasts come from Windows PowerShell's App User Model ID
// (ToastAUMID), as the probe's did: ToastNotificationManager takes an
// unpackaged ID only when a Start menu shortcut registers it, and an ID
// registered under HKCU\Software\Classes\AppUserModelId alone fails
// (HRESULT 0x80070490, measured). Every toast carries the group
// ToastGroup, and Close removes that group alone from the Notification
// Center, so the user's own PowerShell notifications stay; it also removes
// the levels of the per-App settings key that Windows created for the
// first toast, when they were not there before. Nothing else of the user's
// is read or changed, except that a toast shows on their screen: the tests
// are opt-in, and they need the user's notifications turned on
// (Toaster.Setting), which only the Settings toggle does.
//
// Interface IIDs and vtable slots are the Windows SDK 10.0.26100.0
// windows.ui.notifications.h and windows.data.xml.dom.h ones. Slots 0-5 of
// every WinRT interface are IInspectable's. The event handlers are
// two-word objects in GlobalAlloc'd memory (vtable, handler id), immortal
// like the UIA provider's objects (uia.go), and they answer IAgileObject:
// the toast's events arrive on a threadpool thread of the MTA.

import (
	"encoding/xml"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ToastAUMID is the App User Model ID the e2e toasts come from,
// ToastAppName the App name a toast banner shows for it, and ToastGroup
// the Notification Center group every e2e toast is in.
const (
	ToastAUMID   = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`
	ToastAppName = "Windows PowerShell"
	ToastGroup   = "winuse-e2e"
)

// The NotificationSetting values (ToastNotifier.Setting).
const (
	ToastEnabled                = 0
	ToastDisabledForApplication = 1
	ToastDisabledForUser        = 2
	ToastDisabledByGroupPolicy  = 3
	ToastDisabledByManifest     = 4
)

// The ToastDismissalReason values (Toast.Dismissed).
const (
	ToastUserCanceled      = 0
	ToastApplicationHidden = 1
	ToastTimedOut          = 2
)

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRaw    = combase.NewProc("WindowsGetStringRawBuffer")
)

var (
	iidToastManagerStatics  = windows.GUID{Data1: 0x50ac103f, Data2: 0xd235, Data3: 0x4598, Data4: [8]byte{0xbb, 0xef, 0x98, 0xfe, 0x4d, 0x1a, 0x3a, 0xd4}}
	iidToastManagerStatics2 = windows.GUID{Data1: 0x7ab93c52, Data2: 0x0e48, Data3: 0x4750, Data4: [8]byte{0xba, 0x9d, 0x1a, 0x41, 0x13, 0x98, 0x18, 0x47}}
	iidToastHistory         = windows.GUID{Data1: 0x5caddc63, Data2: 0x01d3, Data3: 0x4c97, Data4: [8]byte{0x98, 0x6f, 0x05, 0x33, 0x48, 0x3f, 0xee, 0x14}}
	iidToastFactory         = windows.GUID{Data1: 0x04124b20, Data2: 0x82c6, Data3: 0x4229, Data4: [8]byte{0xb1, 0x09, 0xfd, 0x9e, 0xd4, 0x66, 0x2b, 0x53}}
	iidToastNotification    = windows.GUID{Data1: 0x997e2675, Data2: 0x059e, Data3: 0x4e60, Data4: [8]byte{0x8b, 0x06, 0x17, 0x60, 0x91, 0x7c, 0x8b, 0x80}}
	iidToastNotification2   = windows.GUID{Data1: 0x9dfb9fd1, Data2: 0x143a, Data3: 0x490e, Data4: [8]byte{0x90, 0xbf, 0xb9, 0xfb, 0xa7, 0x13, 0x2d, 0xe7}}
	iidToastActivatedArgs   = windows.GUID{Data1: 0xe3bf92f3, Data2: 0xc197, Data3: 0x436f, Data4: [8]byte{0x82, 0x65, 0x06, 0x25, 0x82, 0x4f, 0x8d, 0xac}}
	iidToastDismissedArgs   = windows.GUID{Data1: 0x3f89d935, Data2: 0xd9cb, Data3: 0x4538, Data4: [8]byte{0xa0, 0xf0, 0xff, 0xe7, 0x65, 0x99, 0x38, 0xf8}}
	iidXmlDocumentIO        = windows.GUID{Data1: 0x6cd0e74e, Data2: 0xee65, Data3: 0x4489, Data4: [8]byte{0x9e, 0xbf, 0xca, 0x43, 0xe8, 0x7b, 0xa6, 0x37}}
	iidXmlDocument          = windows.GUID{Data1: 0xf7f3a506, Data2: 0x1e87, Data3: 0x42d6, Data4: [8]byte{0xbc, 0xfb, 0xb8, 0xc8, 0x09, 0xfa, 0x54, 0x94}}
	iidAgileObject          = windows.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	// TypedEventHandler<ToastNotification, Object> (Activated) and
	// TypedEventHandler<ToastNotification, ToastDismissedEventArgs>
	// (Dismissed): the parameterized IIDs.
	iidActivatedHandler = windows.GUID{Data1: 0xab54de2d, Data2: 0x97d9, Data3: 0x5528, Data4: [8]byte{0xb6, 0xad, 0x10, 0x5a, 0xfe, 0x15, 0x65, 0x30}}
	iidDismissedHandler = windows.GUID{Data1: 0x61c2402f, Data2: 0x0ed0, Data3: 0x5a18, Data4: [8]byte{0xab, 0x69, 0x59, 0xf4, 0xaa, 0x99, 0xa3, 0x68}}
)

// The vtable slots used, after IInspectable's six.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotCreateNotifierWithID = 7 // IToastNotificationManagerStatics
	slotGetHistory           = 6 // IToastNotificationManagerStatics2
	slotHistoryRemoveGroupID = 7 // IToastNotificationHistory.RemoveGroupWithId (after RemoveGroup)
	slotCreateNotification   = 6 // IToastNotificationFactory
	slotNotifierShow         = 6 // IToastNotifier
	slotNotifierHide         = 7
	slotNotifierSetting      = 8
	slotAddDismissed         = 9 // IToastNotification
	slotAddActivated         = 11
	slotPutTag               = 6 // IToastNotification2
	slotPutGroup             = 8
	slotLoadXML              = 6 // IXmlDocumentIO
	slotActivatedArguments   = 6 // IToastActivatedEventArgs
	slotDismissedReason      = 6 // IToastDismissedEventArgs
)

// winrtCall calls method slot of the COM object obj and returns its HRESULT
// as an error.
func winrtCall(obj uintptr, slot int, args ...uintptr) error {
	vtbl := *(*uintptr)(cptr(obj))
	fn := *(*uintptr)(cptr(vtbl + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT %#08x", uint32(r))
	}
	return nil
}

// winrtRelease releases a COM reference (0 is a no-op).
func winrtRelease(obj uintptr) {
	if obj != 0 {
		winrtCall(obj, slotRelease)
	}
}

// winrtQuery asks obj for iid.
func winrtQuery(obj uintptr, iid *windows.GUID) (uintptr, error) {
	var out uintptr
	if err := winrtCall(obj, slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); err != nil {
		return 0, err
	}
	return out, nil
}

// hstring is a WinRT string; free it with deleteHString.
func hstring(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if int32(r) < 0 {
		return 0, fmt.Errorf("WindowsCreateString: HRESULT %#08x", uint32(r))
	}
	return h, nil
}

func deleteHString(h uintptr) {
	if h != 0 {
		procWindowsDeleteString.Call(h)
	}
}

// goString reads a WinRT string (it stays the caller's to free).
func goString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := procWindowsGetStringRaw.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	return windows.UTF16ToString(unsafe.Slice((*uint16)(cptr(p)), n))
}

// withWinRT runs f on a thread in the multithreaded apartment.
func withWinRT(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procRoInitialize.Call(1)          // RO_INIT_MULTITHREADED
	if int32(r) < 0 && uint32(r) != 0x80010106 { // RPC_E_CHANGED_MODE: an STA thread still reaches the MTA's objects
		return fmt.Errorf("RoInitialize: HRESULT %#08x", uint32(r))
	}
	return f()
}

// activationFactory returns class's factory for iid.
func activationFactory(class string, iid *windows.GUID) (uintptr, error) {
	h, err := hstring(class)
	if err != nil {
		return 0, err
	}
	defer deleteHString(h)
	var f uintptr
	r, _, _ := procRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	if int32(r) < 0 {
		return 0, fmt.Errorf("RoGetActivationFactory(%s): HRESULT %#08x", class, uint32(r))
	}
	return f, nil
}

// notifierFor creates the ToastNotifier of an App User Model ID.
func notifierFor(aumid string) (uintptr, error) {
	statics, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidToastManagerStatics)
	if err != nil {
		return 0, err
	}
	defer winrtRelease(statics)
	h, err := hstring(aumid)
	if err != nil {
		return 0, err
	}
	defer deleteHString(h)
	var notifier uintptr
	if err := winrtCall(statics, slotCreateNotifierWithID, h, uintptr(unsafe.Pointer(&notifier))); err != nil {
		return 0, fmt.Errorf("CreateToastNotifierWithId: %w", err)
	}
	return notifier, nil
}

// Setting reads the notifier's ToastNotifier.Setting: ToastEnabled, or
// ToastDisabledForUser while the user's notifications are off. It needs
// the registration NewToaster writes: with notifications on, an
// unregistered ID's Setting fails (HRESULT 0x80070490).
func (t *Toaster) Setting() (int, error) {
	setting := -1
	err := withWinRT(func() error {
		var v int32
		if err := winrtCall(t.notifier, slotNotifierSetting, uintptr(unsafe.Pointer(&v))); err != nil {
			return fmt.Errorf("ToastNotifier.Setting: %w", err)
		}
		setting = int(v)
		return nil
	})
	return setting, err
}

// toastSettingsRoot is where Windows keeps each App's notification
// settings; the first toast of an App adds its key there, one level per
// part of the ID.
const toastSettingsRoot = `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings`

// Toaster raises toasts from ToastAUMID. Close it: it removes ToastGroup
// from the Notification Center and the settings key levels Windows added.
type Toaster struct {
	notifier uintptr
	mu       sync.Mutex
	toasts   []*Toast
	absent   []string // the settings key levels that did not exist at NewToaster, deepest first
}

// Toast is one raised toast. Activated receives the arguments of the
// button pressed (or the toast's launch arguments for a press on its
// body), Dismissed the ToastDismissalReason; each at most once.
type Toast struct {
	Tag       string
	Activated chan string
	Dismissed chan int
	obj       uintptr // IToastNotification
}

// NewToaster opens ToastAUMID's notifier, noting which levels of its
// settings key are absent (Close removes them again).
func NewToaster() (*Toaster, error) {
	t := &Toaster{absent: absentSettingLevels()}
	err := withWinRT(func() error {
		var err error
		t.notifier, err = notifierFor(ToastAUMID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// absentSettingLevels lists the levels of ToastAUMID's settings key that do
// not exist, deepest first.
func absentSettingLevels() []string {
	var absent []string
	key := toastSettingsRoot
	for _, part := range strings.Split(ToastAUMID, `\`) {
		key += `\` + part
		if k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE); err == nil {
			k.Close()
			continue
		}
		absent = append([]string{key}, absent...)
	}
	return absent
}

// toastXML is a toast's document: the title and text, then one
// foreground-activated button per name, whose arguments are its name in
// lower case.
func toastXML(tag, title, text string, buttons []string) string {
	esc := func(s string) string {
		var b strings.Builder
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`<toast launch="` + esc(tag) + `"><visual><binding template="ToastGeneric">`)
	b.WriteString(`<text>` + esc(title) + `</text><text>` + esc(text) + `</text></binding></visual>`)
	if len(buttons) > 0 {
		b.WriteString(`<actions>`)
		for _, name := range buttons {
			b.WriteString(`<action content="` + esc(name) + `" arguments="` + esc(strings.ToLower(name)) + `" activationType="foreground"/>`)
		}
		b.WriteString(`</actions>`)
	}
	b.WriteString(`</toast>`)
	return b.String()
}

// Show raises a toast with a title, a text and buttons. tag names it in
// the Notification Center.
func (t *Toaster) Show(tag, title, text string, buttons ...string) (*Toast, error) {
	toast, err := newToast(tag, title, text, buttons)
	if err == nil {
		err = withWinRT(func() error {
			if err := winrtCall(t.notifier, slotNotifierShow, toast.obj); err != nil {
				return fmt.Errorf("ToastNotifier.Show: %w", err)
			}
			return nil
		})
	}
	if err != nil {
		if toast != nil {
			winrtRelease(toast.obj)
		}
		return nil, err
	}
	t.mu.Lock()
	t.toasts = append(t.toasts, toast)
	t.mu.Unlock()
	return toast, nil
}

// newToast builds a toast and subscribes to its events, without showing
// it.
func newToast(tag, title, text string, buttons []string) (*Toast, error) {
	toast := &Toast{Tag: tag, Activated: make(chan string, 1), Dismissed: make(chan int, 1)}
	err := withWinRT(func() error {
		doc, err := loadXML(toastXML(tag, title, text, buttons))
		if err != nil {
			return err
		}
		defer winrtRelease(doc)
		factory, err := activationFactory("Windows.UI.Notifications.ToastNotification", &iidToastFactory)
		if err != nil {
			return err
		}
		defer winrtRelease(factory)
		if err := winrtCall(factory, slotCreateNotification, doc, uintptr(unsafe.Pointer(&toast.obj))); err != nil {
			return fmt.Errorf("CreateToastNotification: %w", err)
		}
		if n2, err := winrtQuery(toast.obj, &iidToastNotification2); err == nil {
			if h, err := hstring(tag); err == nil {
				winrtCall(n2, slotPutTag, h)
				deleteHString(h)
			}
			if h, err := hstring(ToastGroup); err == nil {
				winrtCall(n2, slotPutGroup, h)
				deleteHString(h)
			}
			winrtRelease(n2)
		}
		var cookie int64
		activated := newHandler(&iidActivatedHandler, func(args uintptr) {
			arguments := ""
			if a, err := winrtQuery(args, &iidToastActivatedArgs); err == nil {
				var h uintptr
				if winrtCall(a, slotActivatedArguments, uintptr(unsafe.Pointer(&h))) == nil {
					arguments = goString(h)
					deleteHString(h)
				}
				winrtRelease(a)
			}
			select {
			case toast.Activated <- arguments:
			default:
			}
		})
		if err := winrtCall(toast.obj, slotAddActivated, activated, uintptr(unsafe.Pointer(&cookie))); err != nil {
			return fmt.Errorf("add_Activated: %w", err)
		}
		dismissed := newHandler(&iidDismissedHandler, func(args uintptr) {
			reason := int32(-1)
			if a, err := winrtQuery(args, &iidToastDismissedArgs); err == nil {
				winrtCall(a, slotDismissedReason, uintptr(unsafe.Pointer(&reason)))
				winrtRelease(a)
			}
			select {
			case toast.Dismissed <- int(reason):
			default:
			}
		})
		if err := winrtCall(toast.obj, slotAddDismissed, dismissed, uintptr(unsafe.Pointer(&cookie))); err != nil {
			return fmt.Errorf("add_Dismissed: %w", err)
		}
		return nil
	})
	if err != nil {
		winrtRelease(toast.obj)
		return nil, err
	}
	return toast, nil
}

// Hide takes a toast off the screen (Dismissed: ToastApplicationHidden).
func (t *Toaster) Hide(toast *Toast) error {
	return withWinRT(func() error { return winrtCall(t.notifier, slotNotifierHide, toast.obj) })
}

// Close removes ToastGroup's notifications from the Notification Center,
// releases the toasts and removes the settings key levels Windows added.
func (t *Toaster) Close() error {
	var errs []error
	errs = append(errs, withWinRT(func() error {
		statics, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidToastManagerStatics2)
		if err != nil {
			return err
		}
		defer winrtRelease(statics)
		var history uintptr
		if err := winrtCall(statics, slotGetHistory, uintptr(unsafe.Pointer(&history))); err != nil {
			return fmt.Errorf("ToastNotificationManager.History: %w", err)
		}
		defer winrtRelease(history)
		group, err := hstring(ToastGroup)
		if err != nil {
			return err
		}
		defer deleteHString(group)
		id, err := hstring(ToastAUMID)
		if err != nil {
			return err
		}
		defer deleteHString(id)
		if err := winrtCall(history, slotHistoryRemoveGroupID, group, id); err != nil {
			return fmt.Errorf("History.RemoveGroup(%s): %w", ToastGroup, err)
		}
		return nil
	}))
	t.mu.Lock()
	for _, toast := range t.toasts {
		winrtRelease(toast.obj)
	}
	t.toasts = nil
	t.mu.Unlock()
	winrtRelease(t.notifier)
	t.notifier = 0
	for _, key := range t.absent {
		if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil && !errors.Is(err, registry.ErrNotExist) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			errs = append(errs, fmt.Errorf("removing HKCU\\%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// loadXML builds a Windows.Data.Xml.Dom.XmlDocument from xml.
func loadXML(xml string) (uintptr, error) {
	class, err := hstring("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return 0, err
	}
	defer deleteHString(class)
	var inspectable uintptr
	if r, _, _ := procRoActivateInstance.Call(class, uintptr(unsafe.Pointer(&inspectable))); int32(r) < 0 {
		return 0, fmt.Errorf("RoActivateInstance(XmlDocument): HRESULT %#08x", uint32(r))
	}
	defer winrtRelease(inspectable)
	io, err := winrtQuery(inspectable, &iidXmlDocumentIO)
	if err != nil {
		return 0, fmt.Errorf("IXmlDocumentIO: %w", err)
	}
	defer winrtRelease(io)
	h, err := hstring(xml)
	if err != nil {
		return 0, err
	}
	defer deleteHString(h)
	if err := winrtCall(io, slotLoadXML, h); err != nil {
		return 0, fmt.Errorf("LoadXml: %w", err)
	}
	return winrtQuery(inspectable, &iidXmlDocument)
}

// The event handler objects: vtable, then the handler's id in handlers.
var (
	handlerOnce   sync.Once
	handlerVtable unsafe.Pointer
	handlersMu    sync.Mutex
	handlers      = map[uintptr]toastHandler{}
)

// toastHandler is one handler object's IID and what it does with the
// event's arguments.
type toastHandler struct {
	iid windows.GUID
	run func(args uintptr)
}

// newHandler makes an immortal TypedEventHandler object for iid.
func newHandler(iid *windows.GUID, run func(args uintptr)) uintptr {
	handlerOnce.Do(func() {
		vt := (*[4]uintptr)(malloc16(4 * unsafe.Sizeof(uintptr(0))))
		vt[0] = newCallback(handlerQueryInterface)
		vt[1] = newCallback(comAddRef) // immortal, as the provider's objects (uia.go)
		vt[2] = newCallback(comRelease)
		vt[3] = newCallback(handlerInvoke)
		handlerVtable = unsafe.Pointer(vt)
	})
	obj := (*[2]uintptr)(malloc16(2 * unsafe.Sizeof(uintptr(0))))
	obj[0] = uintptr(handlerVtable)
	ptr := uintptr(unsafe.Pointer(obj))
	handlersMu.Lock()
	handlers[ptr] = toastHandler{iid: *iid, run: run}
	handlersMu.Unlock()
	return ptr
}

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	const eNoInterface = 0x80004002
	want := *(*windows.GUID)(cptr(riid))
	handlersMu.Lock()
	h, ok := handlers[this]
	handlersMu.Unlock()
	if !ok || (want != iidIUnknown && want != iidAgileObject && want != h.iid) {
		*(*uintptr)(cptr(ppv)) = 0
		return eNoInterface
	}
	*(*uintptr)(cptr(ppv)) = this
	return 0
}

func handlerInvoke(this, sender, args uintptr) uintptr {
	handlersMu.Lock()
	h, ok := handlers[this]
	handlersMu.Unlock()
	if ok {
		h.run(args)
	}
	return 0
}
