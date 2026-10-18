package win

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ResolveError is a Window-resolution failure with its recovery hint; the
// helper prints the message as the failure line and the hint after it.
type ResolveError struct {
	Msg  string
	Hint string
}

func (e *ResolveError) Error() string { return e.Msg }

// ResolveWindow finds one Window from the visible set by ref: a Window ID
// when ref is numeric, otherwise an App name (the App display name or the
// executable's base name, case-insensitively, exact match).
//
// An App name resolves only when exactly one Window matches; several
// matches fail with the candidates listed in the hint, so an agent never
// acts on the wrong Window. A Window can never be named by its title.
func ResolveWindow(windows []WindowInfo, ref string) (WindowInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return WindowInfo{}, &ResolveError{
			Msg:  "a window reference is required: a Window ID, or an App name when that App has exactly one Window",
			Hint: "run winuse list-windows to see the Windows",
		}
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		for _, w := range windows {
			if w.ID == id {
				return w, nil
			}
		}
		return WindowInfo{}, &ResolveError{
			Msg:  fmt.Sprintf("no open Window with ID %d", id),
			Hint: "the Window may have closed; run winuse list-windows",
		}
	}
	var matches []WindowInfo
	for _, w := range windows {
		if appMatches(w, ref) {
			matches = append(matches, w)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return WindowInfo{}, &ResolveError{
			Msg:  fmt.Sprintf("app %q has no open Windows", ref),
			Hint: "a Window is named by its Window ID, or by its App name when that App has exactly one Window (see winuse list-windows)",
		}
	default:
		return WindowInfo{}, &ResolveError{
			Msg:  fmt.Sprintf("app %q has %d open Windows; name the Window by its Window ID", ref, len(matches)),
			Hint: "candidates:\n" + FormatWindows(matches),
		}
	}
}

// appMatches reports whether WindowInfo w's App is ref (case-insensitive,
// exact). Both the App display name (what list-windows prints) and the
// executable's base name (with and without extension) match, so
// "Notepad", "notepad.exe" and "NOTEPAD" all find Notepad.
func appMatches(w WindowInfo, ref string) bool {
	r := strings.ToLower(ref)
	if w.App != "" && strings.ToLower(w.App) == r {
		return true
	}
	if w.ExePath != "" {
		base := filepath.Base(w.ExePath)
		if strings.ToLower(base) == r || strings.ToLower(trimExt(base)) == r {
			return true
		}
	}
	return false
}

// AppExeName returns the App name for a Window State's header line:
// the executable's file name (Codex's header format — the bare exe name,
// not the display name), falling back to the App display name.
func AppExeName(w WindowInfo) string {
	if w.ExePath != "" {
		if base := filepath.Base(w.ExePath); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	if w.App != "" {
		return w.App
	}
	return "unknown"
}
