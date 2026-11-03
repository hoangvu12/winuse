package helper

// The clipboard's Desktop commands (desktop parity ticket 01) over a fake
// clipboard: argument checks, what each prints, the error hints, and the
// action gate. The user's clipboard is never touched.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// fakeClipboard records the calls and answers from its fields.
type fakeClipboard struct {
	content win.ClipContent
	err     error
	gets    []win.ClipFormat
	set     []win.ClipContent
	clears  int
	timeout time.Duration
}

func (f *fakeClipboard) Get(format win.ClipFormat, timeout time.Duration) (win.ClipContent, error) {
	f.gets = append(f.gets, format)
	f.timeout = timeout
	return f.content, f.err
}

func (f *fakeClipboard) Set(content win.ClipContent, timeout time.Duration) error {
	f.set = append(f.set, content)
	f.timeout = timeout
	return f.err
}

func (f *fakeClipboard) Clear(timeout time.Duration) error {
	f.clears++
	f.timeout = timeout
	return f.err
}

func clipboardCmds(f *fakeClipboard) map[string]Command {
	return CommandsWith(Services{Trees: &fakeTrees{}, Clipboard: f})
}

// TestClipboardGetPrintsTextAsIs: the text comes out exactly, with no
// newline added, and the format defaults to text.
func TestClipboardGetPrintsTextAsIs(t *testing.T) {
	f := &fakeClipboard{content: win.ClipContent{Format: win.ClipText, Text: "secret 🙂\r\nsecond"}}
	out, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-get", nil)
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if out != "secret 🙂\r\nsecond" {
		t.Errorf("out = %q, want the text exactly", out)
	}
	if len(f.gets) != 1 || f.gets[0] != win.ClipText {
		t.Errorf("gets = %v, want one text read", f.gets)
	}
	if f.timeout <= 0 || f.timeout > clipboardTimeout {
		t.Errorf("session timeout %s, want (0, %s]", f.timeout, clipboardTimeout)
	}
}

func TestClipboardGetFilesOnePerLine(t *testing.T) {
	f := &fakeClipboard{content: win.ClipContent{Format: win.ClipFiles, Files: []string{`C:\a.txt`, `C:\b c\d.png`}}}
	out, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-get", protocol.ClipboardGetArgs{Format: "files"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if out != "C:\\a.txt\nC:\\b c\\d.png\n" {
		t.Errorf("out = %q", out)
	}
}

// TestClipboardGetImageWritesThePNG: --out gets the PNG bytes; without it
// a new file in %TEMP%\winuse. The output is the path and the size.
func TestClipboardGetImageWritesThePNG(t *testing.T) {
	png := []byte("\x89PNG fake bytes")
	f := &fakeClipboard{content: win.ClipContent{Format: win.ClipImage, PNG: png, Width: 640, Height: 480}}
	out := filepath.Join(t.TempDir(), "sub", "clip.png")
	got, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-get", protocol.ClipboardGetArgs{Format: "image", Out: out})
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if got != out+" (640x480 px)\n" {
		t.Errorf("out = %q", got)
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != string(png) {
		t.Errorf("--out file = %q, %v", b, err)
	}

	t.Setenv("TMP", t.TempDir())
	t.Setenv("TEMP", os.Getenv("TMP"))
	got, cmdErr = runCommand(t, clipboardCmds(f), "clipboard-get", protocol.ClipboardGetArgs{Format: "image"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	path := strings.TrimSuffix(got, " (640x480 px)\n")
	if filepath.Dir(path) != filepath.Join(os.TempDir(), "winuse") || !strings.HasPrefix(filepath.Base(path), "winuse-clipboard-") {
		t.Errorf("default path %q, want %%TEMP%%\\winuse\\winuse-clipboard-*.png", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != string(png) {
		t.Errorf("default file = %q, %v", b, err)
	}
}

func TestClipboardGetRefusesBadArguments(t *testing.T) {
	f := &fakeClipboard{}
	for _, a := range []protocol.ClipboardGetArgs{
		{Format: "html"},
		{Format: "text", Out: `C:\x.png`},
		{Out: `C:\x.png`},
	} {
		if _, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-get", a); cmdErr == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
	if len(f.gets) != 0 {
		t.Errorf("the clipboard was read for bad arguments: %v", f.gets)
	}
}

// TestClipboardErrorsHaveHints: each failure keeps its line and gets the
// recovery hint.
func TestClipboardErrorsHaveHints(t *testing.T) {
	cases := []struct {
		err      error
		msg      string
		hintPart string
	}{
		{&win.ClipboardMissingError{Want: win.ClipText, Has: []win.ClipFormat{win.ClipImage, win.ClipFiles}},
			"the clipboard holds no text", "it holds image and files; read it with winuse clipboard-get --format image"},
		{&win.ClipboardMissingError{Want: win.ClipText}, "the clipboard is empty", "winuse clipboard-set"},
		{&win.ClipboardBusyError{Holder: 7}, "another program has the clipboard open (window 7); nothing was sent", "retry in a moment"},
		{&win.ClipboardHungError{Owner: 9}, "not responding", "wait until that program responds"},
		{win.ErrClipboardWorker, "still waiting", "wait until that program responds"},
		{&win.ClipboardTimeoutError{}, "nothing was changed", "retry once"},
		{&win.ClipboardTimeoutError{Write: true}, "may have taken effect", "winuse clipboard-get"},
		{&win.ClipboardWriteError{Err: errors.New("x")}, "may have taken effect", "winuse clipboard-get"},
	}
	for _, tc := range cases {
		f := &fakeClipboard{err: tc.err}
		_, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-get", nil)
		if cmdErr == nil {
			t.Fatalf("%T: no error", tc.err)
		}
		if !strings.Contains(cmdErr.Msg, tc.msg) || !strings.Contains(cmdErr.Hint, tc.hintPart) {
			t.Errorf("%T: %q / hint %q; want %q / %q", tc.err, cmdErr.Msg, cmdErr.Hint, tc.msg, tc.hintPart)
		}
	}
}

func TestClipboardSetText(t *testing.T) {
	for _, text := range []string{"hello world", ""} {
		f := &fakeClipboard{}
		out, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", protocol.ClipboardSetArgs{Text: strPtr(text)})
		if cmdErr != nil {
			t.Fatal(cmdErr.Msg)
		}
		if out != okOutput {
			t.Errorf("out = %q, want ok", out)
		}
		if len(f.set) != 1 || f.set[0].Format != win.ClipText || f.set[0].Text != text {
			t.Errorf("set = %+v, want the text %q", f.set, text)
		}
	}
}

func TestClipboardSetImageReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("png bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeClipboard{}
	if _, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", protocol.ClipboardSetArgs{Image: path}); cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if len(f.set) != 1 || f.set[0].Format != win.ClipImage || string(f.set[0].PNG) != "png bytes" {
		t.Errorf("set = %+v", f.set)
	}

	for _, bad := range []string{filepath.Join(dir, "missing.png"), dir} {
		f := &fakeClipboard{}
		_, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", protocol.ClipboardSetArgs{Image: bad})
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "nothing was sent") {
			t.Errorf("--image %s: %v, want a refusal saying nothing was sent", bad, cmdErr)
		}
		if len(f.set) != 0 {
			t.Errorf("--image %s reached the clipboard", bad)
		}
	}
}

func TestClipboardSetFilesMustExist(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeClipboard{}
	if _, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", protocol.ClipboardSetArgs{Files: []string{a, dir}}); cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if len(f.set) != 1 || f.set[0].Format != win.ClipFiles || len(f.set[0].Files) != 2 {
		t.Errorf("set = %+v", f.set)
	}

	f = &fakeClipboard{}
	missing := filepath.Join(dir, "gone.txt")
	_, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", protocol.ClipboardSetArgs{Files: []string{a, missing}})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, missing) || !strings.Contains(cmdErr.Msg, "nothing was sent") {
		t.Errorf("a missing file: %v", cmdErr)
	}
	if len(f.set) != 0 {
		t.Error("a missing file reached the clipboard")
	}
}

func TestClipboardSetTakesOneRepresentation(t *testing.T) {
	for _, a := range []protocol.ClipboardSetArgs{
		{},
		{Text: strPtr("x"), Image: `C:\a.png`},
		{Image: `C:\a.png`, Files: []string{`C:\a`}},
	} {
		f := &fakeClipboard{}
		if _, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-set", a); cmdErr == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
}

func TestClipboardClear(t *testing.T) {
	f := &fakeClipboard{}
	out, cmdErr := runCommand(t, clipboardCmds(f), "clipboard-clear", nil)
	if cmdErr != nil || out != okOutput || f.clears != 1 {
		t.Errorf("clear = %q, %v, %d clears", out, cmdErr, f.clears)
	}
	if _, cmdErr := clipboardCmds(f)["clipboard-clear"](context.Background(), json.RawMessage(`{"x":1}`)); cmdErr == nil {
		t.Error("clipboard-clear accepted arguments")
	}
}

// TestClipboardCommandsWaitForTheActionGate: the Desktop commands are
// serialized with the Actions; one queued behind an Action past its budget
// fails without touching the clipboard.
func TestClipboardCommandsWaitForTheActionGate(t *testing.T) {
	for _, name := range []string{"clipboard-get", "clipboard-set", "clipboard-clear"} {
		if observationOnlyCommand(name) {
			t.Errorf("%s must be serialized on the action gate", name)
		}
	}
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	svc := spy.services()
	clip := &fakeClipboard{}
	svc.Clipboard = clip
	inClick := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		once.Do(func() { close(inClick) })
		<-release
		return nil
	}
	cmds := CommandsWith(svc)
	index := 1
	// A Headed click (headless-max ticket 21) holds the desktop through the
	// v1 fakes' injected click.
	raw, _ := json.Marshal(protocol.ClickArgs{Window: "101", ElementIndex: &index, Headed: true})
	go cmds["click"](context.Background(), raw)
	<-inClick

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cmdErr := cmds["clipboard-clear"](ctx, nil)
	if cmdErr == nil || cmdErr.Msg != queuedOutError().Msg {
		t.Errorf("queued clipboard-clear = %v, want the queued-past-budget failure", cmdErr)
	}
	if clip.clears != 0 {
		t.Error("the queued clipboard-clear reached the clipboard")
	}
	close(release)
}
