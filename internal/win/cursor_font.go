package win

// The name tag's font (cursor-look issue 11): Figtree Bold, embedded in
// winuse.exe (third_party/figtree, SIL Open Font License 1.1) and added
// to this process alone with AddFontMemResourceEx, so nothing is
// installed and no other program sees it. Should that fail, the tag falls
// back to Segoe UI Bold (cursorLabelFaces).

import (
	"sync"
	"unsafe"

	"github.com/hoangvu12/winuse/third_party/figtree"
)

var procAddFontMemResourceEx = gdi32.NewProc("AddFontMemResourceEx")

// cursorFontOnce loads the embedded font the first time a tag's font is
// chosen; the fonts stay loaded for the process's life.
var cursorFontOnce sync.Once

// loadCursorFont adds Figtree Bold to the process's fonts, best effort.
func loadCursorFont() {
	cursorFontOnce.Do(func() {
		if procAddFontMemResourceEx.Find() != nil || len(figtree.Bold) == 0 {
			return
		}
		var n uint32
		procAddFontMemResourceEx.Call(uintptr(unsafe.Pointer(&figtree.Bold[0])), uintptr(len(figtree.Bold)), 0,
			uintptr(unsafe.Pointer(&n)))
	})
}
