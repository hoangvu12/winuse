package notes

import (
	"slices"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseArgsReadsEachSubcommand(t *testing.T) {
	cases := []struct {
		args []string
		wire string
		want protocol.NoteArgs
		text []string
	}{
		{[]string{"add", "301", "the", "editor", "is", "a", "placeholder"}, "note-add",
			protocol.NoteArgs{Target: "301"}, []string{"the", "editor", "is", "a", "placeholder"}},
		{[]string{"add", "301", "--app", "--headed is literal here"}, "note-add",
			protocol.NoteArgs{Target: "301", App: true}, []string{"--headed is literal here"}},
		{[]string{"add", "--app", "301", "text"}, "note-add",
			protocol.NoteArgs{Target: "301", App: true}, []string{"text"}},
		{[]string{"add", "github.com", "--stdin"}, "note-add",
			protocol.NoteArgs{Target: "github.com"}, []string{"--stdin"}},
		{[]string{"add", "github.com", "--file", "lesson.txt"}, "note-add",
			protocol.NoteArgs{Target: "github.com"}, []string{"--file", "lesson.txt"}},
		{[]string{"list"}, "note-list", protocol.NoteArgs{}, nil},
		{[]string{"list", "excel.exe"}, "note-list", protocol.NoteArgs{Target: "excel.exe"}, nil},
		{[]string{"list", "301", "--app"}, "note-list", protocol.NoteArgs{Target: "301", App: true}, nil},
		{[]string{"remove", "github.com", "2"}, "note-remove", protocol.NoteArgs{Target: "github.com", N: 2}, nil},
		{[]string{"remove", "--app", "301", "1"}, "note-remove", protocol.NoteArgs{Target: "301", App: true, N: 1}, nil},
		{[]string{"clear", "localhost:3000"}, "note-clear", protocol.NoteArgs{Target: "localhost:3000"}, nil},
	}
	for _, c := range cases {
		got, err := ParseArgs(c.args)
		if err != nil {
			t.Errorf("ParseArgs(%q): %v", c.args, err)
			continue
		}
		if got.Wire != c.wire || got.Args != c.want || !slices.Equal(got.Text, c.text) {
			t.Errorf("ParseArgs(%q) = %+v, want %s %+v text %q", c.args, got, c.wire, c.want, c.text)
		}
	}
}

func TestParseArgsRefusesBadCommandLines(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"edit", "github.com"},
		{"add"},
		{"add", "github.com"},
		{"add", "github.com", "--app"},
		{"list", "a.com", "b.com"},
		{"list", "--verbose"},
		{"remove", "github.com"},
		{"remove", "github.com", "two"},
		{"remove", "github.com", "0"},
		{"remove", "github.com", "1", "2"},
		{"clear"},
		{"clear", "github.com", "extra"},
	} {
		if got, err := ParseArgs(args); err == nil {
			t.Errorf("ParseArgs(%q) = %+v, want a usage error", args, got)
		}
	}
}

func TestTextStartsAfterTargetAndApp(t *testing.T) {
	cases := []struct {
		args []string
		n    int
	}{
		{[]string{"add", "301", "text"}, 2},
		{[]string{"add", "301", "--app", "text"}, 3},
		{[]string{"add", "--app", "301", "text"}, 3},
		{[]string{"list", "301"}, -1},
		{nil, -1},
	}
	for _, c := range cases {
		if got := TextStart(c.args); got != c.n {
			t.Errorf("TextStart(%q) = %d, want %d", c.args, got, c.n)
		}
	}
}
