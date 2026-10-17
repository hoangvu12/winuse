package fixture

import (
	"os"
	"runtime"
	"strconv"
	"unicode/utf16"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows"
)

// app is the fixture application. Exactly one instance exists per process,
// running on a single locked OS thread — the thread that owns both windows,
// pumps messages and serves the UIA provider callbacks (see uia.go).
// Everything below runs on that thread, so no locking is needed.
type app struct {
	state     State
	statePath string

	instance   uintptr
	mainHwnd   uintptr
	dialogHwnd uintptr
	font       uintptr

	uia *uiaProviders

	focusMain   Elem // logical focus inside the main window
	focusDialog Elem // logical focus inside the dialog

	pressedElem Elem // element under the last button-down
	dragging    bool
	dragFrom    Point

	ctrlDown bool       // a Control key is held (chord state, e.g. Ctrl+A)
	chars    charStream // recombines UTF-16 halves arriving as separate WM_CHAR

	// provider tree cache, rebuilt when the state generation changes
	treeGen    uint64
	mainTree   *TreeMap
	dialogTree *TreeMap

	status string // current status text (the status element's suffix)
}

var theApp *app

// Main runs the fixture app: creates the windows, pumps messages until the
// app closes, and keeps the state file current. It must be called once from
// the process entry point on a locked thread.
func Main(statePath string) int {
	runtime.LockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		// Not fatal: coordinates stay physical whenever the OS allows it.
		os.Stderr.WriteString("winusefixture: dpi awareness: " + err.Error() + "\n")
	}

	a := &app{state: initial(), statePath: statePath, focusMain: ElemTextField, status: "ready"}
	theApp = a
	if err := a.createWindows(); err != nil {
		os.Stderr.WriteString("winusefixture: " + err.Error() + "\n")
		return 1
	}
	defer a.writeState()
	a.loop()
	return 0
}

// createWindows registers the two window classes and creates both windows.
// The dialog starts hidden: it becomes a visible owned top-level Window
// only when opened.
func (a *app) createWindows() error {
	hmod, _, err := procGetModuleHandleW.Call(0)
	if hmod == 0 {
		return err
	}
	a.instance = hmod
	a.font = createFont()

	mainProc := newCallback(a.wndMain)
	dialogProc := newCallback(a.wndDialog)
	if err := registerClass(MainWindowClass, mainProc, hmod); err != nil {
		return err
	}
	if err := registerClass(DialogWindowClass, dialogProc, hmod); err != nil {
		return err
	}

	// A fixed-size window: caption, system menu, minimize box, no resize.
	const wsFixed = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox
	const cwUseDefault = 0x80000000
	mw, mh := windowSizeFor(wsFixed, ClientWidth, ClientHeight)
	title, class := utf16Ptr(WindowTitle), utf16Ptr(MainWindowClass)
	h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		uintptr(wsFixed), uintptr(cwUseDefault), uintptr(cwUseDefault), mw, mh,
		0, 0, hmod, 0)
	if h == 0 {
		return err
	}
	a.mainHwnd = h
	a.state.WindowID = uint64(h)

	// The "Save As" dialog: an owned top-level popup. Owned windows with a
	// title are their own Window in winuse listings (ticket 01's contract).
	dTitle, dClass := utf16Ptr(DialogTitle), utf16Ptr(DialogWindowClass)
	const wsDialog = wsPopup | wsCaption | wsSysMenu
	dw, dht := windowSizeFor(wsDialog, DialogClientWidth, DialogClientHeight)
	dh, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(dClass)), uintptr(unsafe.Pointer(dTitle)),
		uintptr(wsDialog), 60, 60, dw, dht,
		h, 0, hmod, 0)
	if dh == 0 {
		return err
	}
	a.dialogHwnd = dh
	a.state.DialogWindowID = uint64(dh)

	a.uia = newProviders(a)

	procShowWindow.Call(h, swShow)
	procUpdateWindow.Call(h)
	procSetTimer.Call(h, 1, 500, 0)
	a.updateCanvasRect()
	a.changed()
	return nil
}

// windowSizeFor converts a wanted client size into the window size
// CreateWindowExW needs (it takes the full window size, caption included).
func windowSizeFor(style uint32, clientW, clientH int32) (uintptr, uintptr) {
	r := win.Rect{Left: 0, Top: 0, Right: clientW, Bottom: clientH}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, 0)
	return uintptr(r.Right - r.Left), uintptr(r.Bottom - r.Top)
}

func registerClass(name string, proc, hmod uintptr) error {
	cn := utf16Ptr(name)
	wc := wndclassex{
		Size:      uint32(unsafe.Sizeof(wndclassex{})),
		Style:     csDblClks,
		WndProc:   proc,
		Instance:  hmod,
		ClassName: cn,
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	return nil
}

// loop is the message pump. Provider callbacks and all input arrive here.
func (a *app) loop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// ---- message handling ----

func (a *app) wndMain(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	if r, handled := a.handleWMGetObject(hwnd, msg, wp, lp); handled {
		return r
	}
	switch msg {
	case wmPaint:
		a.paintMain(hwnd)
		return 0
	case wmDestroy:
		procKillTimer.Call(hwnd, 1)
		procPostQuitMessage.Call(0)
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmWindowPosChanged:
		a.updateCanvasRect()
		a.writeState() // CanvasRect follows the window
		procInvalidateRect.Call(hwnd, 0, 1)
	case wmTimer:
		a.writeState()
		return 0
	case wmMouseLeave:
		if a.state.ToolbarVisible {
			a.state.ToolbarVisible = false
			if a.focusMain == ElemToolbarBold || a.focusMain == ElemToolbarItalic || a.focusMain == ElemToolbarUnderline {
				a.focusMain = ""
			}
			a.changed()
		}
		return 0
	case wmMousemove:
		a.onMouseMove(lParamPoint(lp))
		return 0
	case wmLButtonDown, wmRButtonDown:
		a.onMouseDown(lParamPoint(lp), msg == wmRButtonDown)
		return 0
	case wmLButtonUp, wmRButtonUp:
		a.onMouseUp(lParamPoint(lp), msg == wmRButtonUp)
		return 0
	case wmLButtonDblClk:
		a.onDoubleClick(lParamPoint(lp))
		return 0
	case wmMouseWheel:
		// lParam is screen coordinates for the wheel messages: convert to
		// client before the hit-test uses the client-space rects.
		a.onWheel(screenToClient(hwnd, lParamPoint(lp)), 0, wheelDelta(wp))
		return 0
	case wmMouseHWheel:
		a.onWheel(screenToClient(hwnd, lParamPoint(lp)), wheelDelta(wp), 0)
		return 0
	case wmKeydown:
		a.onKeyDown(uint32(wp))
		return 0
	case wmKeyup:
		a.onKeyUp(uint32(wp))
		return 0
	case wmSyskeydown:
		a.onKeyDown(uint32(wp))
		// Fall through to DefWindowProc: Alt+F4 and friends must keep working.
	case wmSyskeyup:
		a.onKeyUp(uint32(wp))
		// Fall through too: modifier bookkeeping must not eat the key-up.
	case wmChar:
		a.onChar(rune(uint32(wp)))
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (a *app) wndDialog(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	if r, handled := a.handleWMGetObject(hwnd, msg, wp, lp); handled {
		return r
	}
	switch msg {
	case wmPaint:
		a.paintDialog(hwnd)
		return 0
	case wmClose:
		// Closing via the X button cancels, like a real Save As dialog.
		a.closeDialog("cancel")
		return 0
	case wmLButtonDown:
		a.pressedElem = a.hitTestDialog(lParamPoint(lp))
		return 0
	case wmLButtonUp:
		elem := a.hitTestDialog(lParamPoint(lp))
		if elem != a.pressedElem {
			a.pressedElem = ""
			return 0
		}
		a.pressedElem = ""
		if elem != "" {
			a.click(elem, Point{})
		}
		return 0
	case wmKeydown:
		a.onKeyDown(uint32(wp))
		return 0
	case wmKeyup:
		a.onKeyUp(uint32(wp))
		return 0
	case wmSyskeydown:
		a.onKeyDown(uint32(wp))
		// Fall through to DefWindowProc: Alt+F4 and friends must keep working.
	case wmSyskeyup:
		a.onKeyUp(uint32(wp))
		// Fall through too: modifier bookkeeping must not eat the key-up.
	case wmChar:
		a.onChar(rune(uint32(wp)))
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// handleWMGetObject answers the UIA root-provider request for a window.
// A WM_GETOBJECT can arrive while the windows are still being created
// (accessibility tools poll), before the providers exist; those fall back
// to DefWindowProc, which answers "no UIA provider yet".
func (a *app) handleWMGetObject(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	if msg != wmGetobject || int32(lp) != uiaRootObjectId || a.uia == nil {
		return 0, false
	}
	dialog := hwnd == a.dialogHwnd
	provider := a.uia.simple(dialog, providerRootElem(dialog))
	r, _, _ := procUiaReturnRawElementProvider.Call(hwnd, wp, lp, provider)
	return r, true
}

// providerRootElem returns the root element of a window's provider tree.
func providerRootElem(dialog bool) Elem {
	if dialog {
		return DialogRootElem
	}
	return ElemWindow
}

// ---- input ----

// onMouseMove keeps the hover toolbar revealed while the pointer is in the
// hover band, and asks for leave notifications so it hides when the cursor
// exits the window.
func (a *app) onMouseMove(p point) {
	inBand := p.Y >= toolbarTop && p.Y < toolbarBottom && p.X < canvasLeft && !a.state.DialogOpen
	if inBand != a.state.ToolbarVisible {
		a.state.ToolbarVisible = inBand
		if !inBand && (a.focusMain == ElemToolbarBold || a.focusMain == ElemToolbarItalic || a.focusMain == ElemToolbarUnderline) {
			a.focusMain = ""
		}
		a.changed()
	}
	var tme trackMouseEvent
	tme.Size = uint32(unsafe.Sizeof(tme))
	tme.Flags = tmeLeave
	tme.HwndTrack = a.mainHwnd
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

func (a *app) onMouseDown(p point, right bool) {
	a.pressedElem = a.hitTest(p)
	if a.pressedElem == ElemCanvas && !right {
		a.dragging = true
		a.dragFrom = clientToCanvas(p.X, p.Y)
		procSetCapture.Call(a.mainHwnd)
	}
}

func (a *app) onMouseUp(p point, right bool) {
	if a.dragging {
		procReleaseCapture.Call()
		to := clientToCanvas(p.X, p.Y)
		a.dragging = false
		if abs32(to.X-a.dragFrom.X) > 4 || abs32(to.Y-a.dragFrom.Y) > 4 {
			a.state.CanvasDrags = append(a.state.CanvasDrags, Drag{From: a.dragFrom, To: to})
			a.changed()
			return
		}
	}
	elem := a.hitTest(p)
	if elem != a.pressedElem {
		// The press moved off the element: no click (standard behaviour).
		a.pressedElem = ""
		return
	}
	a.pressedElem = ""
	if elem == "" {
		return
	}
	if right {
		if elem == ElemCanvas {
			a.state.CanvasRightClicks = append(a.state.CanvasRightClicks, clientToCanvas(p.X, p.Y))
			a.changed()
		}
		return
	}
	a.click(elem, clientToCanvas(p.X, p.Y))
}

func (a *app) onDoubleClick(p point) {
	if a.hitTest(p) != ElemCanvas {
		return
	}
	// A double click is a click too; the counter tells them apart. The
	// second button-up follows the dbl-click message: clearing the press
	// target keeps it from recording a third click.
	a.pressedElem = ""
	a.state.CanvasClicks = append(a.state.CanvasClicks, clientToCanvas(p.X, p.Y))
	a.state.CanvasDoubleClicks++
	a.changed()
}

func (a *app) onWheel(p point, dx, dy int32) {
	if inRect(p.X, p.Y, win.Rect{Left: canvasLeft, Top: canvasTop, Right: canvasRight, Bottom: canvasBottom}) {
		a.state.CanvasScrolls = append(a.state.CanvasScrolls, Point{X: dx, Y: dy})
		a.changed()
		return
	}
	if inRect(p.X, p.Y, win.Rect{Left: listLeft, Top: listTop, Right: listRight, Bottom: listBottom}) && dy != 0 {
		// Positive delta scrolls up (away from the user).
		a.scrollList(int(-dy) / 120)
	}
}

// hitTest maps a client point of the main window to the element under it,
// in input priority order: an open dropdown menu swallows clicks inside it
// (and closes on clicks anywhere else — menu dismissal eats the click), the
// revealed toolbar comes next, then the static elements.
func (a *app) hitTest(p point) Elem {
	if a.state.OpenMenu != "" {
		for _, item := range menuItems(Elem(a.state.OpenMenu)) {
			if r, ok := Rect(item, a.state.ListFirstVisible); ok && inRect(p.X, p.Y, r) {
				return item
			}
		}
		a.state.OpenMenu = ""
		a.changed()
		return ""
	}
	if a.state.DialogOpen {
		return "" // the main window is disabled while the modal dialog is up
	}
	if a.state.ToolbarVisible {
		for _, b := range []Elem{ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline} {
			if r, ok := Rect(b, 0); ok && inRect(p.X, p.Y, r) {
				return b
			}
		}
	}
	if inRect(p.X, p.Y, win.Rect{Left: canvasLeft, Top: canvasTop, Right: canvasRight, Bottom: canvasBottom}) {
		return ElemCanvas
	}
	for _, e := range []Elem{
		ElemMenuFile, ElemMenuEdit, ElemMenuHelp,
		ElemTextField, ElemButtonClick, ElemButtonSaveAs, ElemButtonDisabled,
		ElemTwinA, ElemTwinB, ElemSoloOne, ElemSoloTwo,
		ElemCheckboxNotify, ElemDetailsExpander,
	} {
		if r, ok := Rect(e, a.state.ListFirstVisible); ok && inRect(p.X, p.Y, r) {
			return e
		}
	}
	for _, item := range ListItems {
		if r, ok := Rect(item, a.state.ListFirstVisible); ok && inRect(p.X, p.Y, r) {
			return item
		}
	}
	if inRect(p.X, p.Y, win.Rect{Left: listLeft, Top: listTop, Right: listRight, Bottom: listBottom}) {
		return ElemItemList
	}
	return ""
}

func (a *app) hitTestDialog(p point) Elem {
	for _, e := range []Elem{ElemDialogEditFileName, ElemDialogButtonSave, ElemDialogButtonCancel} {
		if r, ok := dialogRect(e); ok && inRect(p.X, p.Y, r) {
			return e
		}
	}
	return ""
}

// click performs an element's primary action: exactly what a person's click
// does, and exactly what the provider's Invoke/Select/Toggle/Expand do.
func (a *app) click(elem Elem, canvas Point) {
	switch {
	case elem == ElemCanvas:
		a.state.CanvasClicks = append(a.state.CanvasClicks, canvas)
		a.changed()
	case elem == ElemTextField || elem == ElemDialogEditFileName:
		a.setFocus(elem)
	case elem == ElemCheckboxNotify:
		a.state.Checkbox = !a.state.Checkbox
		a.changed()
	case elem == ElemDetailsExpander:
		a.state.DetailsExpanded = !a.state.DetailsExpanded
		a.changed()
	case elem == ElemMenuFile, elem == ElemMenuEdit, elem == ElemMenuHelp:
		if a.state.OpenMenu == string(elem) {
			a.state.OpenMenu = ""
		} else {
			a.state.OpenMenu = string(elem)
		}
		a.changed()
	case elem == ElemButtonSaveAs:
		a.openDialog()
	case elem == ElemButtonDisabled:
		// Disabled elements ignore input (IsEnabled is false).
	case isListItem(elem):
		a.selectItem(elem)
	case isMenuItem(elem):
		a.invokeMenu(elem)
	case elem == ElemDialogButtonSave:
		a.closeDialog("save")
	case elem == ElemDialogButtonCancel:
		a.closeDialog("cancel")
	default:
		// Click Me, the twins, the solos, the toolbar buttons.
		a.invokeButton(elem)
	}
}

func isMenuItem(e Elem) bool {
	switch e {
	case ElemMenuFileNew, ElemMenuFileOpen, ElemMenuFileExit,
		ElemMenuEditSelectAll, ElemMenuEditClear, ElemMenuHelpAbout:
		return true
	}
	return false
}

// invokeMenu performs a dropdown item's action. The menu closes first —
// a menu item disappears when invoked, like a real menu.
func (a *app) invokeMenu(elem Elem) {
	a.state.LastMenu = string(elem)
	a.state.OpenMenu = ""
	switch elem {
	case ElemMenuFileNew:
		a.state.Text = ""
		a.state.Typed = ""
		a.state.TextSelected = false
		a.setStatus("new")
	case ElemMenuFileOpen:
		a.setStatus("open")
		a.changed()
		a.openDialog()
		return
	case ElemMenuFileExit:
		a.changed()
		procPostMessageW.Call(a.mainHwnd, wmClose, 0, 0)
		return
	case ElemMenuEditSelectAll:
		a.selectAll()
		a.setStatus("selected all text")
	case ElemMenuEditClear:
		a.state.Text = ""
		a.state.TextSelected = false
		a.setStatus("cleared")
	case ElemMenuHelpAbout:
		a.setStatus("about winuse fixture")
	}
	a.changed()
}

// invokeButton records a button or toolbar invocation.
func (a *app) invokeButton(elem Elem) {
	s, ok := SpecFor(elem)
	if !ok {
		return
	}
	switch elem {
	case ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline:
		a.state.LastToolbar = s.Name
	default:
		a.state.LastButton = s.Name
	}
	a.setStatus("invoked " + s.Name)
	a.changed()
}

func (a *app) selectItem(elem Elem) {
	spec, ok := SpecFor(elem)
	if !ok {
		return
	}
	a.state.SelectedItem = spec.Name
	a.changed()
}

func (a *app) openDialog() {
	if a.state.DialogOpen {
		return
	}
	procShowWindow.Call(a.dialogHwnd, swShow)
	procEnableWindow.Call(a.mainHwnd, 0) // modal: the main window is disabled
	procSetFocus.Call(a.dialogHwnd)
	a.state.DialogOpen = true
	a.state.DialogResult = ""
	a.state.DialogFileName = ""
	a.state.TextSelected = false
	a.state.ToolbarVisible = false
	a.focusDialog = ElemDialogEditFileName
	a.setStatus("dialog opened")
	a.changed()
}

func (a *app) closeDialog(result string) {
	if !a.state.DialogOpen {
		return
	}
	procShowWindow.Call(a.dialogHwnd, swHide)
	procEnableWindow.Call(a.mainHwnd, 1)
	procSetFocus.Call(a.mainHwnd)
	a.state.DialogOpen = false
	a.state.DialogResult = result
	a.state.TextSelected = false
	a.focusDialog = ""
	a.setStatus("dialog closed: " + result)
	a.changed()
}

func (a *app) scrollList(delta int) {
	max := len(ListItems) - ListVisibleItems
	next := a.state.ListFirstVisible + delta
	if next < 0 {
		next = 0
	}
	if next > max {
		next = max
	}
	if next != a.state.ListFirstVisible {
		a.state.ListFirstVisible = next
		a.changed()
	}
}

// ---- keyboard ----

// Virtual keys the keyboard handling tracks. Control arrives as VK_CONTROL
// from real keyboards and as VK_LCONTROL/VK_RCONTROL from injected chords.
const (
	vkControlKey  = 0x11
	vkLcontrolKey = 0xA2
	vkRcontrolKey = 0xA3
	vkAKey        = 0x41
)

// charStream recombines the UTF-16 halves of a surrogate pair that arrive
// as separate WM_CHAR messages: Unicode key injection delivers one code
// unit per event, so an emoji reaches the app as two WM_CHAR messages, and
// a real app recombines them (Notepad does; so does the fixture).
type charStream struct {
	pending rune // the pending high surrogate; 0 when none
}

// char feeds one WM_CHAR rune and returns the complete runes it produced:
// nothing while a high half waits, the combined rune on the low half, and
// the rune itself when it is not part of a pair (a stray pending half is
// dropped).
func (cs *charStream) char(r rune) []rune {
	switch {
	case r >= 0xD800 && r <= 0xDBFF: // a high surrogate starts a pair
		cs.pending = r
		return nil
	case r >= 0xDC00 && r <= 0xDFFF && cs.pending != 0: // the low half completes it
		full := utf16.DecodeRune(cs.pending, r)
		cs.pending = 0
		return []rune{full}
	}
	cs.pending = 0
	return []rune{r}
}

func (a *app) onKeyDown(vk uint32) {
	if vk == vkControlKey || vk == vkLcontrolKey || vk == vkRcontrolKey {
		a.ctrlDown = true
	}
	a.state.LastKey = keyName(vk)
	// Control+A selects the focused edit's whole content (every real edit
	// control does); typing then replaces it.
	if a.ctrlDown && vk == vkAKey {
		a.selectAll()
	}
	// Space/Return activate the focused control, like real Windows.
	focus := a.focusElem()
	switch {
	case focus == ElemCheckboxNotify && vk == 0x20:
		a.state.Checkbox = !a.state.Checkbox
	case focus != "" && HasPattern(focus, PatternInvoke) && (vk == 0x0D || vk == 0x20):
		if a.enabled(focus) {
			a.click(focus, Point{})
		}
	}
	a.changed()
}

// onKeyUp tracks modifier releases: a Control key-up clears the chord
// state (press-key chords release in reverse order, so the modifier
// always leaves last).
func (a *app) onKeyUp(vk uint32) {
	if vk == vkControlKey || vk == vkLcontrolKey || vk == vkRcontrolKey {
		a.ctrlDown = false
	}
}

func (a *app) onChar(r rune) {
	if r == 0x08 {
		a.backspace()
		return
	}
	if r == 0x0D || r == 0x09 || r == 0x1B {
		// Return/Tab/Escape are recorded as key names, not typed text.
		return
	}
	if r >= 0x20 {
		for _, full := range a.chars.char(r) {
			a.typeRune(full)
		}
	}
}

// selectAll marks the focused text edit's whole content as selected
// (Control+A, or the Edit menu's Select All): typing or Backspace then
// replaces it.
func (a *app) selectAll() {
	switch a.focusElem() {
	case ElemTextField, ElemDialogEditFileName:
		a.state.TextSelected = true
	}
}

func (a *app) typeRune(r rune) {
	switch a.focusElem() {
	case ElemTextField:
		if a.state.TextSelected {
			a.state.Text = ""
			a.state.TextSelected = false
		}
		a.state.Text += string(r)
	case ElemDialogEditFileName:
		if a.state.TextSelected {
			a.state.DialogFileName = ""
			a.state.TextSelected = false
		}
		a.state.DialogFileName += string(r)
	default:
		return
	}
	a.state.Typed = capTyped(a.state.Typed + string(r))
	a.changed()
}

// backspace deletes one character behind the caret — or the whole
// selection, when the focused edit's content is selected.
func (a *app) backspace() {
	switch a.focusElem() {
	case ElemTextField:
		if a.state.TextSelected {
			a.state.TextSelected = false
			a.state.Text = ""
		} else if a.state.Text != "" {
			a.state.Text = dropLastRune(a.state.Text)
		}
	case ElemDialogEditFileName:
		if a.state.TextSelected {
			a.state.TextSelected = false
			a.state.DialogFileName = ""
		} else if a.state.DialogFileName != "" {
			a.state.DialogFileName = dropLastRune(a.state.DialogFileName)
		}
	default:
		return
	}
	a.changed()
}

func (a *app) focusElem() Elem {
	if a.state.DialogOpen {
		return a.focusDialog
	}
	return a.focusMain
}

func (a *app) setFocus(e Elem) {
	if InDialog(e) {
		if !a.state.DialogOpen {
			return
		}
		a.focusDialog = e
	} else {
		if a.state.DialogOpen {
			return
		}
		a.focusMain = e
	}
	a.state.TextSelected = false // moving focus drops the selection
	a.changed()
}

// ---- state ----

func (a *app) setStatus(s string) { a.status = s }

func (a *app) changed() {
	a.state.Generation++
	a.writeState()
	procInvalidateRect.Call(a.mainHwnd, 0, 1)
	if a.dialogHwnd != 0 {
		procInvalidateRect.Call(a.dialogHwnd, 0, 1)
	}
}

// writeState atomically rewrites the state file (write + rename).
func (a *app) writeState() {
	data, err := jsonMarshal(&a.state)
	if err != nil {
		return
	}
	tmp := a.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, a.statePath)
}

// updateCanvasRect recomputes the canvas's screen rectangle.
func (a *app) updateCanvasRect() {
	if a.mainHwnd == 0 {
		return
	}
	origin := point{X: canvasLeft, Y: canvasTop}
	far := point{X: canvasRight, Y: canvasBottom}
	procClientToScreen.Call(a.mainHwnd, uintptr(unsafe.Pointer(&origin)))
	procClientToScreen.Call(a.mainHwnd, uintptr(unsafe.Pointer(&far)))
	a.state.CanvasRect = [4]int32{origin.X, origin.Y, far.X, far.Y}
}

// enabled mirrors what a person can interact with: the disabled button
// never; main-window elements only while no modal dialog is open.
func (a *app) enabled(e Elem) bool {
	if e == ElemButtonDisabled {
		return false
	}
	if a.state.DialogOpen && !InDialog(e) {
		return false
	}
	return true
}

// offscreen reports IsOffscreen: list items outside the viewport.
func (a *app) offscreen(e Elem) bool {
	for i, item := range ListItems {
		if item == e {
			return i < a.state.ListFirstVisible || i >= a.state.ListFirstVisible+ListVisibleItems
		}
	}
	return false
}

// focusable reports IsKeyboardFocusable.
func (a *app) focusable(e Elem) bool {
	switch e {
	case ElemTextField, ElemCheckboxNotify, ElemItemList, ElemDialogEditFileName:
		return true
	}
	if isListItem(e) {
		return true
	}
	return HasPattern(e, PatternInvoke) && a.enabled(e)
}

// ---- small helpers ----

func capTyped(s string) string {
	r := []rune(s)
	if len(r) > 200 {
		return string(r[len(r)-200:])
	}
	return s
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// keyName maps a virtual key to a Codex/X keysym-style name, the naming
// later tickets' press-key tests will use. Letters are recorded lowercase
// (shift state is not tracked).
func keyName(vk uint32) string {
	switch vk {
	case 0x08:
		return "BackSpace"
	case 0x09:
		return "Tab"
	case 0x0D:
		return "Return"
	case 0x13:
		return "Pause"
	case 0x14:
		return "Caps_Lock"
	case 0x1B:
		return "Escape"
	case 0x20:
		return "space"
	case 0x21:
		return "Prior"
	case 0x22:
		return "Next"
	case 0x23:
		return "End"
	case 0x24:
		return "Home"
	case 0x25:
		return "Left"
	case 0x26:
		return "Up"
	case 0x27:
		return "Right"
	case 0x28:
		return "Down"
	case 0x2D:
		return "Insert"
	case 0x2E:
		return "Delete"
	}
	switch {
	case vk >= 0x30 && vk <= 0x39:
		return string(rune('0' + vk - 0x30))
	case vk >= 0x41 && vk <= 0x5A:
		return string(rune('a' + vk - 0x41))
	case vk >= 0x70 && vk <= 0x7B:
		return "F" + strconv.Itoa(int(vk-0x6F))
	}
	const hex = "0123456789abcdef"
	s := "vk-0x"
	for shift := 24; shift >= 0; shift -= 4 {
		s += string(hex[(vk>>uint(shift))&0xf])
	}
	return s
}

// ---- painting ----

// Colors (COLORREF: 0x00BBGGRR).
const (
	colWindowBg = 0x00F0F0F0 // window background
	colFace     = 0x00E0E0E0 // control face
	colWhite    = 0x00FFFFFF
	colBlack    = 0x00000000
	colGray     = 0x00999999
	colLight    = 0x00C8C8C8
	colAccent   = 0x00B34700 // focus frames (BGR: strong orange)
	colSelect   = 0x00CC9A00 // selected list item
	colText     = 0x00222222
	colMark     = 0x002200CC // canvas click marks (BGR: red-ish)
)

func createFont() uintptr {
	const defaultCharset = 1
	const clearType = 5
	name, _ := windows.UTF16PtrFromString("Segoe UI")
	f, _, _ := procCreateFontW.Call(0xFFFFFFE8 /* -18: char height */, 0, 0, 0,
		400 /* FW_NORMAL */, 0, 0, 0, defaultCharset, 0, 0, clearType, 0,
		uintptr(unsafe.Pointer(name)))
	return f
}

// paintMain draws the main window. Flat and simple on purpose — the
// fixture's job is deterministic structure, not looks — but everything the
// UIA tree exposes is drawn, so screenshots and tree agree. The open
// dropdown menu is drawn last so it overlays the content beneath it.
func (a *app) paintMain(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	oldFont, _, _ := procSelectObject.Call(hdc, a.font)
	defer procSelectObject.Call(hdc, oldFont)
	procSetBkMode.Call(hdc, transparentBk)

	var cr win.Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	fillRect(hdc, cr, colWindowBg)

	a.paintMenuBar(hdc)
	if a.state.ToolbarVisible {
		a.paintToolbar(hdc)
	}
	a.paintTextField(hdc)
	for _, e := range []Elem{ElemButtonClick, ElemButtonSaveAs, ElemButtonDisabled, ElemTwinA, ElemTwinB, ElemSoloOne, ElemSoloTwo} {
		a.paintButton(hdc, e)
	}
	a.paintCheckbox(hdc)
	a.paintList(hdc)
	a.paintExpander(hdc)
	a.paintCanvas(hdc)
	a.paintStatus(hdc)
	a.paintOpenMenu(hdc)
}

func (a *app) paintDialog(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	oldFont, _, _ := procSelectObject.Call(hdc, a.font)
	defer procSelectObject.Call(hdc, oldFont)
	procSetBkMode.Call(hdc, transparentBk)

	var cr win.Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	fillRect(hdc, cr, colWindowBg)

	r, _ := dialogRect(ElemDialogLabelFileName)
	drawText(hdc, "File name:", r, dtLeft|dtVCenter|dtSingleLine, colText)

	r, _ = dialogRect(ElemDialogEditFileName)
	fillRect(hdc, r, colWhite)
	frameRect(hdc, r, boolColor(a.focusDialog == ElemDialogEditFileName, colAccent, colBlack))
	drawText(hdc, a.state.DialogFileName, inset(r, 6), dtLeft|dtVCenter|dtSingleLine, colText)

	for _, e := range []Elem{ElemDialogButtonSave, ElemDialogButtonCancel} {
		r, _ = dialogRect(e)
		s, _ := SpecFor(e)
		fillRect(hdc, r, colFace)
		frameRect(hdc, r, boolColor(a.focusDialog == e, colAccent, colBlack))
		drawText(hdc, s.Name, r, dtCenter|dtVCenter|dtSingleLine, colText)
	}
}

func boolColor(b bool, t, f uint32) uint32 {
	if b {
		return t
	}
	return f
}

func inset(r win.Rect, d int32) win.Rect {
	return win.Rect{Left: r.Left + d, Top: r.Top, Right: r.Right - d, Bottom: r.Bottom}
}

func (a *app) paintMenuBar(hdc uintptr) {
	bar, _ := Rect(ElemMenuBar, 0)
	fillRect(hdc, bar, colLight)
	for _, e := range []Elem{ElemMenuFile, ElemMenuEdit, ElemMenuHelp} {
		r, _ := Rect(e, 0)
		s, _ := SpecFor(e)
		if a.state.OpenMenu == string(e) {
			fillRect(hdc, r, colAccent)
			drawText(hdc, s.Name, r, dtCenter|dtVCenter|dtSingleLine, colWhite)
		} else {
			drawText(hdc, s.Name, r, dtCenter|dtVCenter|dtSingleLine, colText)
		}
	}
}

// paintOpenMenu draws the dropdown of the open menu, over everything.
func (a *app) paintOpenMenu(hdc uintptr) {
	if a.state.OpenMenu == "" {
		return
	}
	for _, item := range menuItems(Elem(a.state.OpenMenu)) {
		r, _ := Rect(item, 0)
		s, _ := SpecFor(item)
		fillRect(hdc, r, colWhite)
		frameRect(hdc, r, colBlack)
		drawText(hdc, s.Name, inset(r, 8), dtLeft|dtVCenter|dtSingleLine, colText)
	}
}

func (a *app) paintToolbar(hdc uintptr) {
	for _, e := range []Elem{ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline} {
		r, _ := Rect(e, 0)
		s, _ := SpecFor(e)
		fillRect(hdc, r, colFace)
		frameRect(hdc, r, boolColor(a.focusMain == e, colAccent, colBlack))
		drawText(hdc, s.Name, r, dtCenter|dtVCenter|dtSingleLine, colText)
	}
}

func (a *app) paintTextField(hdc uintptr) {
	r, _ := Rect(ElemTextField, 0)
	drawText(hdc, "Text Field:", win.Rect{Left: r.Left, Top: textLabelTop - 12, Right: r.Right, Bottom: r.Top}, dtLeft|dtSingleLine, colGray)
	fillRect(hdc, r, colWhite)
	frameRect(hdc, r, boolColor(a.focusElem() == ElemTextField, colAccent, colBlack))
	drawText(hdc, a.state.Text, inset(r, 6), dtLeft|dtVCenter|dtSingleLine, colText)
}

func (a *app) paintButton(hdc uintptr, e Elem) {
	r, _ := Rect(e, 0)
	s, _ := SpecFor(e)
	fillRect(hdc, r, colFace)
	frameRect(hdc, r, boolColor(a.focusMain == e, colAccent, colBlack))
	drawText(hdc, s.Name, r, dtCenter|dtVCenter|dtSingleLine, boolColor(a.enabled(e), colText, colGray))
}

func (a *app) paintCheckbox(hdc uintptr) {
	r, _ := Rect(ElemCheckboxNotify, 0)
	box := win.Rect{Left: r.Left, Top: r.Top + 3, Right: r.Left + 18, Bottom: r.Bottom - 3}
	fillRect(hdc, box, colWhite)
	frameRect(hdc, box, boolColor(a.focusElem() == ElemCheckboxNotify, colAccent, colBlack))
	if a.state.Checkbox {
		pen := solidPen(colBlack)
		oldPen, _, _ := procSelectObject.Call(hdc, pen)
		line(hdc, box.Left+4, box.Top+8, box.Left+7, box.Bottom-6)
		line(hdc, box.Left+7, box.Bottom-6, box.Left+14, box.Top+4)
		procSelectObject.Call(hdc, oldPen)
		procDeleteObject.Call(pen)
	}
	drawText(hdc, "Notify me", win.Rect{Left: r.Left + 24, Top: r.Top, Right: r.Right, Bottom: r.Bottom}, dtLeft|dtVCenter|dtSingleLine, colText)
}

func (a *app) paintList(hdc uintptr) {
	r, _ := Rect(ElemItemList, 0)
	fillRect(hdc, r, colWhite)
	frameRect(hdc, r, colBlack)
	for i, item := range ListItems {
		ir, _ := Rect(item, a.state.ListFirstVisible)
		if ir.Top >= r.Bottom-2 || ir.Bottom <= r.Top+2 || i < a.state.ListFirstVisible || i >= a.state.ListFirstVisible+ListVisibleItems {
			continue
		}
		s, _ := SpecFor(item)
		if a.state.SelectedItem == s.Name {
			fillRect(hdc, ir, colSelect)
			drawText(hdc, s.Name, inset(ir, 6), dtLeft|dtVCenter|dtSingleLine, colWhite)
		} else {
			drawText(hdc, s.Name, inset(ir, 6), dtLeft|dtVCenter|dtSingleLine, colText)
		}
	}
	// A scroll indicator (the list is custom-drawn; scrolling is via wheel
	// and the Scroll pattern).
	if max := len(ListItems) - ListVisibleItems; max > 0 {
		track := win.Rect{Left: r.Right - scrollbarWidth, Top: r.Top, Right: r.Right, Bottom: r.Bottom}
		fillRect(hdc, track, colLight)
		thumbH := track.Height() * int32(ListVisibleItems) / int32(len(ListItems))
		thumbY := track.Top + track.Height()*int32(a.state.ListFirstVisible)/int32(len(ListItems))
		fillRect(hdc, win.Rect{Left: track.Left + 1, Top: thumbY, Right: track.Right - 1, Bottom: thumbY + thumbH}, colGray)
	}
}

func (a *app) paintExpander(hdc uintptr) {
	r, _ := Rect(ElemDetailsExpander, 0)
	arrow := ">"
	if a.state.DetailsExpanded {
		arrow = "v"
	}
	drawText(hdc, arrow, win.Rect{Left: r.Left, Top: r.Top, Right: r.Left + 20, Bottom: r.Bottom}, dtCenter|dtVCenter|dtSingleLine, colText)
	drawText(hdc, "Details", win.Rect{Left: r.Left + 20, Top: r.Top, Right: r.Right, Bottom: r.Bottom}, dtLeft|dtVCenter|dtSingleLine, colText)
	if a.state.DetailsExpanded {
		dr, _ := Rect(ElemDetailsText, 0)
		drawText(hdc, "Some details text.", inset(dr, 6), dtLeft|dtVCenter|dtSingleLine, colGray)
	}
}

func (a *app) paintCanvas(hdc uintptr) {
	r, _ := Rect(ElemCanvas, 0)
	fillRect(hdc, r, colWhite)
	frameRect(hdc, r, colBlack)

	// A grid so screenshots show structure.
	pen := solidPen(colLight)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	for x := r.Left + 20; x < r.Right; x += 20 {
		line(hdc, x, r.Top+1, x, r.Bottom-1)
	}
	for y := r.Top + 20; y < r.Bottom; y += 20 {
		line(hdc, r.Left+1, y, r.Right-1, y)
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)

	// Recorded input: clicks as marks, the last drag as a line.
	markPen := solidPen(colMark)
	oldPen, _, _ = procSelectObject.Call(hdc, markPen)
	for _, c := range a.state.CanvasClicks {
		p := canvasClientPoint(c)
		line(hdc, p.X-4, p.Y-4, p.X+4, p.Y+4)
		line(hdc, p.X-4, p.Y+4, p.X+4, p.Y-4)
	}
	if n := len(a.state.CanvasDrags); n > 0 {
		d := a.state.CanvasDrags[n-1]
		from, to := canvasClientPoint(d.From), canvasClientPoint(d.To)
		procMoveToEx.Call(hdc, uintptr(from.X), uintptr(from.Y), 0)
		procLineTo.Call(hdc, uintptr(to.X), uintptr(to.Y))
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(markPen)

	drawText(hdc, strconv.Itoa(len(a.state.CanvasClicks))+" clicks",
		win.Rect{Left: r.Left + 6, Top: r.Top + 4, Right: r.Right - 6, Bottom: r.Top + 24}, dtLeft|dtSingleLine, colGray)
}

func (a *app) paintStatus(hdc uintptr) {
	r, _ := Rect(ElemStatus, 0)
	drawText(hdc, StatusNamePrefix+a.status, r, dtLeft|dtVCenter|dtSingleLine, colText)
}

// ---- GDI helpers ----

func fillRect(hdc uintptr, r win.Rect, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	defer procDeleteObject.Call(brush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
}

func frameRect(hdc uintptr, r win.Rect, color uint32) {
	pen := solidPen(color)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, nullBrush())
	procRectangle.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func solidPen(color uint32) uintptr {
	pen, _, _ := procCreatePen.Call(0 /*PS_SOLID*/, 1, uintptr(color))
	return pen
}

func nullBrush() uintptr {
	const nullBrushStock = 5
	b, _, _ := procGetStockObject.Call(nullBrushStock)
	return b
}

func line(hdc uintptr, x1, y1, x2, y2 int32) {
	procMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(hdc, uintptr(x2), uintptr(y2))
}

func drawText(hdc uintptr, s string, r win.Rect, flags uint32, color uint32) {
	procSetTextColor.Call(hdc, uintptr(color))
	t, _ := windows.UTF16PtrFromString(s)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(t)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags))
}
