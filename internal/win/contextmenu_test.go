package win

import "testing"

func TestChromiumMenuPopup(t *testing.T) {
	menu := WindowInfo{Class: ChromiumClass, Style: 0x96000000, ExStyle: 0x08200088, Owner: 5179818} // measured, Chrome 155
	cases := []struct {
		name string
		w    WindowInfo
		want bool
	}{
		{"Chrome's menu popup", menu, true},
		{"the browser Window", WindowInfo{Class: ChromiumClass, Style: 0x16cf0000, ExStyle: 0x200100}, false},
		{"no owner", WindowInfo{Class: menu.Class, Style: menu.Style, ExStyle: menu.ExStyle}, false},
		{"takes activation", WindowInfo{Class: menu.Class, Style: menu.Style, ExStyle: wsExToolWindow, Owner: 1}, false},
		{"a frameless Electron Window", WindowInfo{Class: ChromiumClass, Style: 0x96000000, ExStyle: 0x100, Owner: 0}, false},
		{"another class", WindowInfo{Class: MenuClass, Style: menu.Style, ExStyle: menu.ExStyle, Owner: 1}, false},
	}
	for _, c := range cases {
		if got := ChromiumMenuPopup(c.w); got != c.want {
			t.Errorf("%s: ChromiumMenuPopup = %v, want %v", c.name, got, c.want)
		}
	}
}
