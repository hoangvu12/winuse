package cli

// The clipboard's Desktop commands, CLI side (desktop-parity ticket 01):
// clipboard-get, clipboard-set and clipboard-clear. They name no Window.
// Argument parsing is local (usage errors exit 2 and never dial the
// helper); paths are resolved against the CLI's working directory, which
// the helper cannot see. clipboard-set's text is literal from its first
// token: a --headed in it is text (headed.go).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const (
	clipboardGetUsage   = "usage: winuse clipboard-get [--format text|image|files] [--out <path>]"
	clipboardSetUsage   = "usage: winuse clipboard-set <text> | --image <png> | --file <path>..."
	clipboardClearUsage = "usage: winuse clipboard-clear"
)

// clipboardGetCmd implements `winuse clipboard-get`: print the clipboard's
// text, its files one per line, or the path of the PNG its image was
// written to.
func clipboardGetCmd(args []string) int {
	a, err := parseClipboardGetArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, clipboardGetUsage)
		return 2
	}
	return runClipboard("clipboard-get", a)
}

// clipboardSetCmd implements `winuse clipboard-set`: replace the
// clipboard's content with text, a PNG image or a list of files.
func clipboardSetCmd(args []string) int {
	a, err := parseClipboardSetArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, clipboardSetUsage)
		return 2
	}
	return runClipboard("clipboard-set", a)
}

// clipboardClearCmd implements `winuse clipboard-clear`.
func clipboardClearCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "winuse: clipboard-clear takes no arguments\n%s\n", clipboardClearUsage)
		return 2
	}
	return runClipboard("clipboard-clear", nil)
}

func runClipboard(cmd string, args any) int {
	if err := runWithArgs(cmd, args); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseClipboardGetArgs parses --format and --out (each also as
// --flag=value). --out goes with --format image only.
func parseClipboardGetArgs(args []string) (protocol.ClipboardGetArgs, error) {
	var a protocol.ClipboardGetArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var flag string
		switch {
		case arg == "--format" || strings.HasPrefix(arg, "--format="):
			flag = "--format"
		case arg == "--out" || strings.HasPrefix(arg, "--out="):
			flag = "--out"
		default:
			return protocol.ClipboardGetArgs{}, fmt.Errorf("clipboard-get: unexpected argument %q", arg)
		}
		value, ok := strings.CutPrefix(arg, flag+"=")
		if !ok {
			if i+1 >= len(args) {
				return protocol.ClipboardGetArgs{}, fmt.Errorf("%s takes a value", flag)
			}
			i++
			value = args[i]
		}
		if flag == "--format" {
			if a.Format != "" {
				return protocol.ClipboardGetArgs{}, errors.New("--format is given twice")
			}
			switch value {
			case "text", "image", "files":
			default:
				return protocol.ClipboardGetArgs{}, fmt.Errorf("--format takes text, image or files, got %q", value)
			}
			a.Format = value
			continue
		}
		if a.Out != "" {
			return protocol.ClipboardGetArgs{}, errors.New("--out is given twice")
		}
		if strings.TrimSpace(value) == "" {
			return protocol.ClipboardGetArgs{}, errors.New("--out takes a file path")
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return protocol.ClipboardGetArgs{}, fmt.Errorf("resolving %s: %v", value, err)
		}
		a.Out = abs
	}
	if a.Out != "" && a.Format != "image" {
		return protocol.ClipboardGetArgs{}, errors.New("--out goes with --format image")
	}
	return a, nil
}

// parseClipboardSetArgs parses clipboard-set's one representation:
// --image <png>, --file <path>... (each path may repeat the flag, as in
// agent-desktop's --file-url), or else the text, its arguments joined by
// spaces as type-text joins them.
func parseClipboardSetArgs(args []string) (protocol.ClipboardSetArgs, error) {
	if len(args) == 0 {
		return protocol.ClipboardSetArgs{}, errors.New("clipboard-set takes a text, --image <png> or --file <path>; clipboard-clear empties the clipboard")
	}
	switch args[0] {
	case "--image":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return protocol.ClipboardSetArgs{}, errors.New("--image takes one PNG file")
		}
		abs, err := filepath.Abs(args[1])
		if err != nil {
			return protocol.ClipboardSetArgs{}, fmt.Errorf("resolving %s: %v", args[1], err)
		}
		return protocol.ClipboardSetArgs{Image: abs}, nil
	case "--file":
		var files []string
		for _, p := range args[1:] {
			if p == "--file" {
				continue
			}
			if strings.TrimSpace(p) == "" {
				return protocol.ClipboardSetArgs{}, errors.New("--file takes file paths")
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				return protocol.ClipboardSetArgs{}, fmt.Errorf("resolving %s: %v", p, err)
			}
			files = append(files, abs)
		}
		if len(files) == 0 {
			return protocol.ClipboardSetArgs{}, errors.New("--file takes file paths")
		}
		return protocol.ClipboardSetArgs{Files: files}, nil
	}
	text := strings.Join(args, " ")
	return protocol.ClipboardSetArgs{Text: &text}, nil
}
