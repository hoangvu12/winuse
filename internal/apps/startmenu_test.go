package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkStartMenuRoots(t *testing.T) {
	dir := t.TempDir()
	programDir := filepath.Join(dir, "Programs")
	if err := os.MkdirAll(filepath.Join(programDir, "Sub Folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A shortcut with a parsable target, one without (LinkTarget fails on
	// junk), an uninstaller, and a non-shortcut file.
	if err := os.WriteFile(filepath.Join(programDir, "Edge.lnk"), buildLink(t, true, "", `C:\Program Files\Edge\msedge.exe`, ""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programDir, "Sub Folder", "Deep App.lnk"), buildLink(t, false, `C:\deep\app.exe`, "", ""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programDir, "Junk.lnk"), []byte("not a shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programDir, "Uninstall Thing.lnk"), buildLink(t, false, `C:\x.exe`, "", ""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programDir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := WalkStartMenuRoots([]string{programDir, filepath.Join(dir, "missing")})
	if err != nil {
		t.Fatalf("WalkStartMenuRoots: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3 (Edge, Deep App, Junk):\n%+v", len(got), got)
	}
	byName := map[string]StartMenuEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	edge := byName["Edge"]
	if edge.Path != filepath.Join(programDir, "Edge.lnk") || edge.TargetExe != `C:\Program Files\Edge\msedge.exe` {
		t.Fatalf("Edge entry: %+v", edge)
	}
	deep := byName["Deep App"]
	if deep.TargetExe != `C:\deep\app.exe` {
		t.Fatalf("Deep App entry: %+v", deep)
	}
	junk := byName["Junk"]
	if junk.TargetExe != "" {
		t.Fatalf("unparsable shortcut must keep an empty target: %+v", junk)
	}
	if _, ok := byName["Uninstall Thing"]; ok {
		t.Fatal("uninstallers must be skipped")
	}
	// Sorted by name.
	if got[0].Name != "Deep App" || got[1].Name != "Edge" || got[2].Name != "Junk" {
		t.Fatalf("entries must be sorted by name: %+v", got)
	}
}

func TestWalkStartMenuRootsEmpty(t *testing.T) {
	got, err := WalkStartMenuRoots(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
