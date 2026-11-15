package wstate

import (
	"strings"
	"testing"
)

func offscreenTree() *Tree {
	return &Tree{Title: "Page", App: "brave.exe", FocusedIndex: -1, Elements: []Element{
		{Depth: 0, Role: "document", Name: "Page"},
		{Depth: 1, Role: "link", Name: "Near"},
		{Depth: 1, Role: "link", Name: "Far", Offscreen: true},
		{Depth: 1, Role: "button", Name: "Save"},
	}}
}

func TestVisibleLeavesOutOffscreenElements(t *testing.T) {
	out := RenderView(offscreenTree(), View{Visible: true})
	if strings.Contains(out, "Far") || !strings.Contains(out, "1 link Near") || !strings.Contains(out, "3 button Save") {
		t.Errorf("--visible printed:\n%s", out)
	}
	if !strings.Contains(out, "--visible left out 1 of the 4 Elements") {
		t.Errorf("no note on what --visible left out:\n%s", out)
	}
	if plain := RenderView(offscreenTree(), View{}); !strings.Contains(plain, "2 link Far") || strings.Contains(plain, OffscreenMark) {
		t.Errorf("a plain read changed:\n%s", plain)
	}
}

func TestMarkOffscreenMarksTheLineAfterItsIndex(t *testing.T) {
	out := RenderView(offscreenTree(), View{MarkOffscreen: true})
	if !strings.Contains(out, "\t\t2 "+OffscreenMark+" link Far\n") || !strings.Contains(out, "\t\t1 link Near\n") {
		t.Errorf("marked:\n%s", out)
	}
}
