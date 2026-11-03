package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildOfThisTestBinary(t *testing.T) {
	// The test binary is a real Go binary: Build is the last part of its
	// Go build ID, as `go tool buildid` prints it.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := Build()
	if b == "" || strings.Contains(b, "/") {
		t.Fatalf("Build() = %q, want the Go build ID's last part", b)
	}
	if got := ExeBuild(exe); got != b {
		t.Errorf("ExeBuild(own exe) = %q, Build() = %q", got, b)
	}
	out, err := exec.Command("go", "tool", "buildid", exe).Output()
	if err != nil {
		t.Skipf("go tool buildid: %v", err)
	}
	id := strings.TrimSpace(string(out))
	if want := id[strings.LastIndexByte(id, '/')+1:]; b != want {
		t.Errorf("Build() = %q, want %q (from %q)", b, want, id)
	}
}

func TestExeBuildWithoutStampIsSizeAndModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.exe")
	if err := os.WriteFile(path, []byte("MZ not a Go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	first := ExeBuild(path)
	if !strings.HasPrefix(first, "18-") {
		t.Fatalf("ExeBuild = %q, want the size (18) and the modification time", first)
	}
	later := when.Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if second := ExeBuild(path); second == first {
		t.Errorf("a rewritten file keeps build %q", second)
	}
	if got := ExeBuild(filepath.Join(t.TempDir(), "missing.exe")); got != "" {
		t.Errorf("a missing file: %q, want \"\"", got)
	}
}

func TestGoBuildID(t *testing.T) {
	data := append([]byte("MZ\x90\x00 padding "), []byte("\xff Go build ID: \"aa/bb/cc/dd\"\n \xff rest")...)
	if got := goBuildID(data); got != "aa/bb/cc/dd" {
		t.Errorf("goBuildID = %q", got)
	}
	if got := goBuildID([]byte("\xff Go build ID: \"unterminated")); got != "" {
		t.Errorf("an unterminated ID: %q", got)
	}
}
