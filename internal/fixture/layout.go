package fixture

import "github.com/hoangvu12/winuse/internal/win"

// The fixture's layout: fixed client coordinates in physical pixels (the
// process is per-monitor-v2 DPI aware, so these are physical at any
// scaling). The window is not resizable, so these never change; the e2e
// tests only ever need the canvas region (CanvasRect in the state file),
// but every rect is here so painting, hit-testing and the provider's
// BoundingRectangles agree — one table, three consumers.
const (
	// Main window client size.
	ClientWidth  = 800
	ClientHeight = 470

	itemHeight = 24
)

// Rects returns the client rectangle of an element of the main window, and
// whether it has one (always present except for the window root, whose
// rectangle comes from the window itself).
func Rect(e Elem, listFirstVisible int) (win.Rect, bool) {
	if r, ok := staticRect(e); ok {
		return r, true
	}
	// List items move with the scroll position.
	for i, item := range ListItems {
		if item == e {
			top := listTop + int32(i-listFirstVisible)*itemHeight
			return win.Rect{Left: listLeft + 2, Top: top, Right: listRight - scrollbarWidth, Bottom: top + itemHeight}, true
		}
	}
	return win.Rect{}, false
}

// Dialogue element rects (client coordinates of the dialog window).
func dialogRect(e Elem) (win.Rect, bool) {
	switch e {
	case ElemDialogLabelFileName:
		return win.Rect{Left: 12, Top: 18, Right: 108, Bottom: 32}, true
	case ElemDialogEditFileName:
		return win.Rect{Left: 112, Top: 12, Right: 308, Bottom: 36}, true
	case ElemDialogButtonSave:
		return win.Rect{Left: 112, Top: 60, Right: 208, Bottom: 88}, true
	case ElemDialogButtonCancel:
		return win.Rect{Left: 212, Top: 60, Right: 308, Bottom: 88}, true
	}
	return win.Rect{}, false
}

// Dialog client size.
const (
	DialogClientWidth  = 320
	DialogClientHeight = 110
)

// Static rects of the main window's elements.
const (
	menuBarTop, menuBarBottom = 0, 28

	toolbarTop, toolbarBottom = 28, 56

	textLabelTop = 62

	listTop, listBottom = 212, 332
	listLeft, listRight = 8, 472
	scrollbarWidth      = 8

	canvasLeft, canvasTop, canvasRight, canvasBottom = 480, 28, 792, 436

	statusTop, statusBottom = 444, 460
)

func staticRect(e Elem) (win.Rect, bool) {
	switch e {
	case ElemWindow:
		return win.Rect{Left: 0, Top: 0, Right: ClientWidth, Bottom: ClientHeight}, false
	case ElemMenuBar:
		return win.Rect{Left: 0, Top: menuBarTop, Right: ClientWidth, Bottom: menuBarBottom}, true
	case ElemMenuFile:
		return win.Rect{Left: 8, Top: 4, Right: 56, Bottom: 24}, true
	case ElemMenuEdit:
		return win.Rect{Left: 60, Top: 4, Right: 108, Bottom: 24}, true
	case ElemMenuHelp:
		return win.Rect{Left: 112, Top: 4, Right: 160, Bottom: 24}, true

	// Dropdown menus hang below their menu-bar item.
	case ElemMenuFileNew:
		return win.Rect{Left: 8, Top: 28, Right: 208, Bottom: 52}, true
	case ElemMenuFileOpen:
		return win.Rect{Left: 8, Top: 52, Right: 208, Bottom: 76}, true
	case ElemMenuFileExit:
		return win.Rect{Left: 8, Top: 76, Right: 208, Bottom: 100}, true
	case ElemMenuEditSelectAll:
		return win.Rect{Left: 60, Top: 28, Right: 260, Bottom: 52}, true
	case ElemMenuEditClear:
		return win.Rect{Left: 60, Top: 52, Right: 260, Bottom: 76}, true
	case ElemMenuHelpAbout:
		return win.Rect{Left: 112, Top: 28, Right: 312, Bottom: 52}, true

	case ElemHoverToolbar:
		return win.Rect{Left: 0, Top: toolbarTop, Right: ClientWidth, Bottom: toolbarBottom}, true
	case ElemToolbarBold:
		return win.Rect{Left: 8, Top: 30, Right: 88, Bottom: 54}, true
	case ElemToolbarItalic:
		return win.Rect{Left: 92, Top: 30, Right: 172, Bottom: 54}, true
	case ElemToolbarUnderline:
		return win.Rect{Left: 176, Top: 30, Right: 268, Bottom: 54}, true

	case ElemTextField:
		return win.Rect{Left: 8, Top: 76, Right: 472, Bottom: 104}, true

	case ElemButtonClick:
		return win.Rect{Left: 8, Top: 112, Right: 108, Bottom: 140}, true
	case ElemButtonSaveAs:
		return win.Rect{Left: 112, Top: 112, Right: 212, Bottom: 140}, true
	case ElemButtonDisabled:
		return win.Rect{Left: 216, Top: 112, Right: 336, Bottom: 140}, true

	case ElemTwinA:
		return win.Rect{Left: 8, Top: 144, Right: 108, Bottom: 172}, true
	case ElemTwinB:
		return win.Rect{Left: 112, Top: 144, Right: 212, Bottom: 172}, true
	case ElemSoloOne:
		return win.Rect{Left: 216, Top: 144, Right: 316, Bottom: 172}, true
	case ElemSoloTwo:
		return win.Rect{Left: 320, Top: 144, Right: 420, Bottom: 172}, true

	case ElemCheckboxNotify:
		return win.Rect{Left: 8, Top: 180, Right: 160, Bottom: 204}, true

	case ElemItemList:
		return win.Rect{Left: listLeft, Top: listTop, Right: listRight, Bottom: listBottom}, true

	case ElemDetailsExpander:
		return win.Rect{Left: 8, Top: 340, Right: 200, Bottom: 364}, true
	case ElemDetailsText:
		return win.Rect{Left: 20, Top: 372, Right: 472, Bottom: 392}, true

	case ElemCanvas:
		return win.Rect{Left: canvasLeft, Top: canvasTop, Right: canvasRight, Bottom: canvasBottom}, true
	case ElemStatus:
		return win.Rect{Left: 8, Top: statusTop, Right: ClientWidth, Bottom: statusBottom}, true
	}
	return win.Rect{}, false
}

// canvasClientPoint converts canvas-local coordinates to the window's
// client coordinates.
func canvasClientPoint(p Point) point {
	return point{X: canvasLeft + p.X, Y: canvasTop + p.Y}
}

// clientToCanvas converts window client coordinates to canvas-local ones.
func clientToCanvas(x, y int32) Point {
	return Point{X: x - canvasLeft, Y: y - canvasTop}
}

// inRect reports whether the point is inside the rect.
func inRect(x, y int32, r win.Rect) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
