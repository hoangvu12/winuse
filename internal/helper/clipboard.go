package helper

// The clipboard commands (desktop-parity ticket 01): clipboard-get,
// clipboard-set and clipboard-clear, done like agent-desktop's. They are
// Desktop commands: aimed at no Window, so not Actions, with no Agent
// Cursor and no Batch step, but serialized on the action gate like an
// Action (CommandsWith), since they change what the user pastes. They are
// Headless: OpenClipboard needs no foreground. The Win32 side and its
// guards (open retry, hung owner, abandoned worker) are win/clipboard.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// ClipboardService is the clipboard the Desktop commands read and write.
// The real one is *win.Clipboard; tests fake it so offline tests never
// touch the user's clipboard.
type ClipboardService interface {
	Get(format win.ClipFormat, timeout time.Duration) (win.ClipContent, error)
	Set(content win.ClipContent, timeout time.Duration) error
	Clear(timeout time.Duration) error
}

// The clipboard command names.
const (
	clipboardGetCommand   = "clipboard-get"
	clipboardSetCommand   = "clipboard-set"
	clipboardClearCommand = "clipboard-clear"
)

// clipboardTimeout bounds one clipboard session, well inside the request
// budget: a clipboard owner slower than this to hand over its content is
// abandoned (win.ClipboardTimeoutError).
const clipboardTimeout = 5 * time.Second

// clipboardBudget is the session budget under ctx: clipboardTimeout,
// shortened to leave a margin before the request's deadline.
func clipboardBudget(ctx context.Context) time.Duration {
	d := clipboardTimeout
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - 500*time.Millisecond; left < d {
			d = left
		}
	}
	if d < 0 {
		d = 0
	}
	return d
}

func clipboardCommands(s Services) map[string]Command {
	return map[string]Command{
		clipboardGetCommand:   clipboardGet(s),
		clipboardSetCommand:   clipboardSet(s),
		clipboardClearCommand: clipboardClear(s),
	}
}

const (
	clipboardGetUsage = "usage: winuse clipboard-get [--format text|image|files] [--out <path>]"
	clipboardSetUsage = "usage: winuse clipboard-set <text> | --image <png> | --file <path>..."
)

// clipboardGet prints the clipboard in one format: the text exactly as it
// is, with nothing added (a secret is printed as is, as agent-desktop
// does); the files one path per line; an image as the path of the PNG it
// wrote and its size.
func clipboardGet(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ClipboardGetArgs
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &a); err != nil {
				return "", Fail("clipboard-get: invalid arguments: "+err.Error(), clipboardGetUsage)
			}
		}
		format := win.ClipText
		if a.Format != "" {
			format = win.ClipFormat(a.Format)
		}
		switch format {
		case win.ClipText, win.ClipImage, win.ClipFiles:
		default:
			return "", Fail(fmt.Sprintf("clipboard-get: unknown format %q", a.Format), clipboardGetUsage)
		}
		if a.Out != "" && format != win.ClipImage {
			return "", Fail("clipboard-get: --out goes with --format image", clipboardGetUsage)
		}
		got, err := s.Clipboard.Get(format, clipboardBudget(ctx))
		if err != nil {
			return "", clipboardError(err)
		}
		switch format {
		case win.ClipFiles:
			return strings.Join(got.Files, "\n") + "\n", nil
		case win.ClipImage:
			path := a.Out
			if path == "" {
				dir := filepath.Join(os.TempDir(), "winuse")
				path = filepath.Join(dir, fmt.Sprintf("winuse-clipboard-%d.png", time.Now().UnixNano()))
			}
			if err := writeFileAtomic(path, got.PNG); err != nil {
				return "", Fail("writing the clipboard's image: "+err.Error(), "pass --out with a path in a folder you can write")
			}
			return fmt.Sprintf("%s (%dx%d px)\n", path, got.Width, got.Height), nil
		}
		return got.Text, nil
	}
}

// clipboardSet replaces the clipboard's content with the text, the PNG
// or the file list. The files are read and checked first, so a bad
// argument leaves the clipboard as it was.
func clipboardSet(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ClipboardSetArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("clipboard-set: "+err.Error(), clipboardSetUsage)
		}
		given := 0
		for _, set := range []bool{a.Text != nil, a.Image != "", len(a.Files) > 0} {
			if set {
				given++
			}
		}
		if given != 1 {
			return "", Fail("clipboard-set takes one of a text, --image or --file", clipboardSetUsage)
		}
		var content win.ClipContent
		switch {
		case a.Text != nil:
			content = win.ClipContent{Format: win.ClipText, Text: *a.Text}
		case a.Image != "":
			b, rerr := readClipboardPNG(a.Image)
			if rerr != nil {
				return "", rerr
			}
			content = win.ClipContent{Format: win.ClipImage, PNG: b}
		default:
			for _, f := range a.Files {
				if _, err := os.Stat(f); err != nil {
					return "", Fail(fmt.Sprintf("--file %s does not exist; nothing was sent", f), "pass paths of existing files or folders")
				}
			}
			content = win.ClipContent{Format: win.ClipFiles, Files: a.Files}
		}
		if err := s.Clipboard.Set(content, clipboardBudget(ctx)); err != nil {
			return "", clipboardError(err)
		}
		return okOutput, nil
	}
}

// readClipboardPNG reads an --image file: a regular file of at most
// win.MaxClipboardPNG bytes. The clipboard checks that it is a PNG.
func readClipboardPNG(path string) ([]byte, *Error) {
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, Fail(fmt.Sprintf("--image %s cannot be read: %v; nothing was sent", path, err), "pass the path of a PNG file")
	case !fi.Mode().IsRegular():
		return nil, Fail(fmt.Sprintf("--image %s is not a file; nothing was sent", path), "pass the path of a PNG file")
	case fi.Size() > win.MaxClipboardPNG:
		return nil, Fail(fmt.Sprintf("--image %s is larger than %d MiB; nothing was sent", path, win.MaxClipboardPNG>>20), "pass a smaller PNG")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, Fail(fmt.Sprintf("--image %s cannot be read: %v; nothing was sent", path, err), "pass the path of a PNG file")
	}
	return b, nil
}

// clipboardClear empties the clipboard.
func clipboardClear(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
			return "", Fail("clipboard-clear takes no arguments", "usage: winuse clipboard-clear")
		}
		if err := s.Clipboard.Clear(clipboardBudget(ctx)); err != nil {
			return "", clipboardError(err)
		}
		return okOutput, nil
	}
}

// clipboardError maps a clipboard failure to its line and recovery hint.
func clipboardError(err error) *Error {
	var (
		missing *win.ClipboardMissingError
		busy    *win.ClipboardBusyError
		hung    *win.ClipboardHungError
		timeout *win.ClipboardTimeoutError
		write   *win.ClipboardWriteError
	)
	switch {
	case errors.As(err, &missing):
		if len(missing.Has) == 0 {
			return Fail(err.Error(), "copy something first, or put it there with winuse clipboard-set")
		}
		var has []string
		for _, f := range missing.Has {
			has = append(has, string(f))
		}
		return Fail(err.Error(), fmt.Sprintf("it holds %s; read it with winuse clipboard-get --format %s",
			strings.Join(has, " and "), missing.Has[0]))
	case errors.As(err, &busy), errors.Is(err, win.ErrClipboardChanging):
		return Fail(err.Error(), "retry in a moment")
	case errors.As(err, &hung), errors.Is(err, win.ErrClipboardWorker):
		return Fail(err.Error(), "wait until that program responds, then retry")
	case errors.As(err, &timeout) && !timeout.Write:
		return Fail(err.Error(), "the program that copied it may be busy; retry once")
	case errors.As(err, &timeout), errors.As(err, &write):
		return Fail(err.Error(), "run winuse clipboard-get to see what the clipboard holds before you repeat it")
	}
	return Fail(err.Error(), "")
}
