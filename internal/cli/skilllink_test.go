package cli

// `winuse skill install` through a junction or a directory symlink, the
// layout `npx skills` makes (~\.claude\skills\winuse links to
// ~\.agents\skills\winuse). Only temp folders are touched.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse"
)

// makeJunction links link to the directory target with mklink /J, which
// needs no privilege.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J is not available: %v: %s", err, out)
	}
}

// checkInstalled checks the skill folder at dir holds the embedded skill.
func checkInstalled(t *testing.T, dir string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != winuse.Skill {
		t.Fatalf("%s\\SKILL.md: %v (or it differs from the embedded core)", dir, err)
	}
	for _, topic := range skillTopics() {
		got, err := os.ReadFile(filepath.Join(dir, "references", topic+".md"))
		text, _ := skillTopic(topic)
		if err != nil || string(got) != text {
			t.Errorf("%s\\references\\%s.md: %v (or it differs)", dir, topic, err)
		}
	}
}

func TestSkillInstallThroughALinkedSkillFolder(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, ".agents", "skills")
	claude := filepath.Join(root, ".claude", "skills")
	for _, d := range []string{agents, claude} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The installer's order: ~\.agents\skills first, then ~\.claude\skills,
	// whose winuse is a junction to the first one's.
	if code, out := runMain(t, "skill", "install", agents); code != 0 {
		t.Fatalf("install into %s: exit %d, %q", agents, code, out)
	}
	makeJunction(t, filepath.Join(claude, "winuse"), filepath.Join(agents, "winuse"))
	if err := os.WriteFile(filepath.Join(agents, "winuse", "references", "gone.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runMain(t, "skill", "install", claude)
	if code != 0 {
		t.Fatalf("install through the junction: exit %d, %q", code, out)
	}
	if want := filepath.Join(claude, "winuse", "SKILL.md"); strings.TrimSpace(out) != want {
		t.Errorf("printed %q, want the path as named, %q", out, want)
	}
	checkInstalled(t, filepath.Join(agents, "winuse"))
	if _, err := os.Stat(filepath.Join(agents, "winuse", "references", "gone.md")); err == nil {
		t.Errorf("the stale topic in the junction's target was left behind")
	}
	fi, err := os.Lstat(filepath.Join(claude, "winuse"))
	if err != nil || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Errorf("the junction was replaced: %v, mode %v", err, fi.Mode())
	}
	// Installing again, in either order, writes the same folder again.
	for _, d := range []string{claude, agents, claude} {
		if code, out := runMain(t, "skill", "install", d); code != 0 {
			t.Fatalf("reinstall into %s: exit %d, %q", d, code, out)
		}
	}
	checkInstalled(t, filepath.Join(agents, "winuse"))
}

func TestSkillInstallIntoALinkedSkillsDir(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real-skills")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "skills")
	makeJunction(t, link, real)
	if code, out := runMain(t, "skill", "install", link); code != 0 {
		t.Fatalf("install into a junctioned skills dir: exit %d, %q", code, out)
	}
	checkInstalled(t, filepath.Join(real, "winuse"))
}

func TestSkillInstallThroughADirectorySymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "agents", "winuse")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(root, "claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	// A relative symlink, resolved against the link's own folder.
	if err := os.Symlink(filepath.Join("..", "agents", "winuse"), filepath.Join(claude, "winuse")); err != nil {
		t.Skipf("directory symlinks need Developer Mode or the privilege here: %v", err)
	}
	if code, out := runMain(t, "skill", "install", claude); code != 0 {
		t.Fatalf("install through a directory symlink: exit %d, %q", code, out)
	}
	checkInstalled(t, target)
}

func TestFollowLinksKeepsAPlainPath(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "not", "there", "yet")
	got, err := followLinks(want)
	if err != nil || got != want {
		t.Errorf("followLinks(%q) = %q, %v; want it unchanged", want, got, err)
	}
}
