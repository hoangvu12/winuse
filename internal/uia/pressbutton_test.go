package uia

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

func TestPickButton(t *testing.T) {
	cases := []struct {
		name   string
		els    []wstate.Element
		want   int
		wantOK bool
	}{
		{"the one Allow button", []wstate.Element{
			{Role: "window", Name: "Allow remote debugging?"},
			{Role: "text", Name: "Allow"},
			{Role: "button", Name: "Allow"},
			{Role: "button", Name: "Cancel"},
		}, 2, true},
		{"a disabled one is skipped", []wstate.Element{
			{Role: "button", Name: "Allow", Disabled: true},
		}, 0, false},
		{"matching is exact", []wstate.Element{
			{Role: "button", Name: "allow"},
			{Role: "button", Name: "Allow all"},
		}, 0, false},
		{"several never guess", []wstate.Element{
			{Role: "button", Name: "Allow"},
			{Role: "button", Name: "Allow"},
		}, 0, false},
		{"none", nil, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pickButton(c.els, "Allow")
			if ok != c.wantOK || (ok && got != c.want) {
				t.Errorf("pickButton = %d, %v; want %d, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}
