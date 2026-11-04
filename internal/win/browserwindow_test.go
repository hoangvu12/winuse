package win

import "testing"

// TestPickContentRect (browser-ux 04): the Page's screen origin is the one
// visible Chrome_RenderWidgetHostHWND inside the Window; hidden Tabs'
// views, other classes and a second visible view (DevTools docked) leave
// the choice to UI Automation. The numbers are the user's maximized Brave
// at 150%: the Window overhangs the screen by 11 px, the content starts
// below the 121 px tall frame.
func TestPickContentRect(t *testing.T) {
	outer := Rect{Left: -11, Top: -11, Right: 2571, Bottom: 1380}
	content := Rect{Left: 0, Top: 121, Right: 2560, Bottom: 1368}
	d3d := contentChild{class: "Intermediate D3D Window", visible: true, rect: Rect{Right: 2560, Bottom: 1368}}
	view := contentChild{class: chromiumContentClass, visible: true, rect: content}
	hidden := contentChild{class: chromiumContentClass, visible: false, rect: content}
	devtools := contentChild{class: chromiumContentClass, visible: true, rect: Rect{Left: 1800, Top: 121, Right: 2560, Bottom: 1368}}
	cases := []struct {
		name string
		kids []contentChild
		want bool
	}{
		{"the one view", []contentChild{d3d, view}, true},
		{"hidden Tabs ignored", []contentChild{hidden, view, hidden}, true},
		{"none", []contentChild{d3d}, false},
		{"only hidden", []contentChild{hidden}, false},
		{"DevTools docked", []contentChild{view, devtools}, false},
		{"empty", []contentChild{{class: chromiumContentClass, visible: true}}, false},
		{"outside the Window", []contentChild{{class: chromiumContentClass, visible: true, rect: Rect{Left: 3000, Top: 0, Right: 3100, Bottom: 100}}}, false},
	}
	for _, tc := range cases {
		r, ok := pickContentRect(outer, tc.kids)
		if ok != tc.want || (ok && r != content) {
			t.Errorf("%s: %+v, %v", tc.name, r, ok)
		}
	}
}
