package apps

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StartMenuEntry is one launchable Start-menu shortcut.
type StartMenuEntry struct {
	Name      string // display name: the file name without .lnk
	Path      string // full shortcut path
	TargetExe string // the shortcut's local target, "" when unknown
}

// StartMenuRoots returns the Start-menu Programs folders: the common one
// (all users) and the per-user one. Real environment access.
func StartMenuRoots() []string {
	var roots []string
	if pd := os.Getenv("ProgramData"); pd != "" {
		roots = append(roots, filepath.Join(pd, `Microsoft\Windows\Start Menu\Programs`))
	}
	if ad := os.Getenv("AppData"); ad != "" {
		roots = append(roots, filepath.Join(ad, `Microsoft\Windows\Start Menu\Programs`))
	}
	return roots
}

// WalkStartMenu walks the Start-menu Programs folders for shortcuts
// (launch-app name resolution — list-apps does not print installed Apps,
// only running and recently used ones).
func WalkStartMenu() ([]StartMenuEntry, error) {
	return WalkStartMenuRoots(StartMenuRoots())
}

// WalkStartMenuRoots walks roots recursively for .lnk shortcuts, reading
// each one's local target (LinkTarget; unresolved targets stay ""). Names
// containing "uninstall" are skipped, and entries come back sorted by
// name. Missing roots yield nothing. Real filesystem access, read-only.
func WalkStartMenuRoots(roots []string) ([]StartMenuEntry, error) {
	var out []StartMenuEntry
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable root or subtree: skip, best-effort
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			name := trimExt(filepath.Base(path))
			if strings.Contains(strings.ToLower(name), "uninstall") {
				return nil
			}
			out = append(out, StartMenuEntry{Name: name, Path: path, TargetExe: LnkTargetFile(path)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
