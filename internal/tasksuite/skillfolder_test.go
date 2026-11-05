package tasksuite

import (
	"os"
	"path/filepath"
	"testing"
)

// An arm's skill is the folder that holds its SKILL.md: the core and the
// topics in references\ beside it all reach the scratch folder, and a
// changed topic changes the arm's hash.
func TestArmSkillIsTheFolder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "lean")
	if err := os.MkdirAll(filepath.Join(src, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, rel), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", "core")
	write(filepath.Join("references", "browser.md"), "browser")
	write("notes.txt", "not part of the skill")

	p, err := resolveSkillPath(src)
	if err != nil {
		t.Fatalf("resolveSkillPath: %v", err)
	}
	files, err := readSkillFolder(p)
	if err != nil {
		t.Fatalf("readSkillFolder: %v", err)
	}
	if len(files) != 2 || string(files["SKILL.md"]) != "core" || string(files[filepath.Join("references", "browser.md")]) != "browser" {
		t.Fatalf("read %d files: %v", len(files), files)
	}

	scratch := t.TempDir()
	if err := installSkill(files, scratch); err != nil {
		t.Fatalf("installSkill: %v", err)
	}
	for _, h := range []string{".claude", ".agents"} {
		for rel, want := range map[string]string{"SKILL.md": "core", filepath.Join("references", "browser.md"): "browser"} {
			got, err := os.ReadFile(filepath.Join(scratch, h, "skills", "winuse", rel))
			if err != nil || string(got) != want {
				t.Errorf("%s\\skills\\winuse\\%s = %q, %v; want %q", h, rel, got, err, want)
			}
		}
	}

	before := skillHash(files)
	write(filepath.Join("references", "browser.md"), "browser, edited")
	again, err := readSkillFolder(p)
	if err != nil {
		t.Fatal(err)
	}
	if skillHash(again) == before {
		t.Errorf("editing a topic left the skill's hash unchanged")
	}
	// The file itself names the same folder as its directory does.
	direct, err := readSkillFolder(filepath.Join(src, "SKILL.md"))
	if err != nil || skillHash(direct) != skillHash(again) {
		t.Errorf("SKILL.md and its folder hash differently: %v", err)
	}
}
