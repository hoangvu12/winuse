package cli

// Offline tests for `winuse skill` and `winuse skill install`: both work
// on the embedded SKILL.md and the filesystem only, never the helper, so
// no desktop is touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse"
)

// runMain runs Main with stdout and stderr captured in one file.
func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create output file: %v", err)
	}
	oldStderr, oldStdout := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = f, f
	code := Main(args)
	os.Stderr, os.Stdout = oldStderr, oldStdout
	f.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return code, string(got)
}

func TestSkillPrintsTheEmbeddedSkill(t *testing.T) {
	code, out := runMain(t, "skill")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; output %q", code, out)
	}
	if out != winuse.Skill {
		t.Errorf("winuse skill printed %d bytes, want the %d embedded bytes verbatim", len(out), len(winuse.Skill))
	}
	if !strings.HasPrefix(out, "---\nname: winuse\n") {
		t.Errorf("winuse skill does not start with the skill's frontmatter: %q", out[:min(len(out), 40)])
	}
}

func TestSkillInstallWritesTheSkillFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	want := filepath.Join(dir, "winuse", "SKILL.md")
	for _, run := range []string{"fresh", "over an older copy"} {
		code, out := runMain(t, "skill", "install", dir)
		if code != 0 {
			t.Fatalf("%s: exit = %d, want 0; output %q", run, code, out)
		}
		if strings.TrimSpace(out) != want {
			t.Errorf("%s: printed %q, want the path %q", run, out, want)
		}
		got, err := os.ReadFile(want)
		if err != nil {
			t.Fatalf("%s: reading the installed skill: %v", run, err)
		}
		if string(got) != winuse.Skill {
			t.Errorf("%s: the installed SKILL.md differs from the embedded one", run)
		}
		for _, topic := range skillTopics() {
			got, err := os.ReadFile(filepath.Join(dir, "winuse", "references", topic+".md"))
			text, _ := skillTopic(topic)
			if err != nil || string(got) != text {
				t.Errorf("%s: the installed topic %s differs from the embedded one (%v)", run, topic, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "winuse", "references", "gone.md")); err == nil {
			t.Errorf("%s: a topic the older copy had and this one does not was left behind", run)
		}
		// The older copy for the next run: an old core and a topic since
		// removed.
		if err := os.WriteFile(want, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "winuse", "references", "gone.md"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillTopicsPrint(t *testing.T) {
	topics := skillTopics()
	if len(topics) < 5 {
		t.Fatalf("the binary embeds %d topics: %v", len(topics), topics)
	}
	for _, topic := range topics {
		code, out := runMain(t, "skill", topic)
		want, _ := skillTopic(topic)
		if code != 0 || out != want || !strings.HasPrefix(out, "# ") {
			t.Errorf("skill %s: exit %d, %d bytes; want exit 0 and the topic's %d", topic, code, len(out), len(want))
		}
	}
	code, out := runMain(t, "skill", "list")
	if code != 0 {
		t.Errorf("skill list: exit %d", code)
	}
	for _, topic := range topics {
		if !strings.Contains(out, "\n"+topic+" ") && !strings.HasPrefix(out, topic+" ") {
			t.Errorf("skill list does not list %s:\n%s", topic, out)
		}
	}
	if strings.Contains(out, "  \n") || strings.Count(out, "Read when") > 0 {
		t.Errorf("skill list prints each topic's Read when text, not the label:\n%s", out)
	}
	code, out = runMain(t, "skill", "all")
	if code != 0 || !strings.HasPrefix(out, winuse.Skill) {
		t.Errorf("skill all: exit %d; want the core first", code)
	}
	for _, topic := range topics {
		if text, _ := skillTopic(topic); !strings.Contains(out, text) {
			t.Errorf("skill all lacks topic %s", topic)
		}
	}
}

func TestSkillUnknownTopicSuggests(t *testing.T) {
	code, out := runMain(t, "skill", "browsr")
	if code != 2 || !strings.Contains(out, `no topic "browsr"; did you mean browser?`) || !strings.Contains(out, "topics: ") {
		t.Errorf("skill browsr: exit %d, output %q", code, out)
	}
	for _, name := range []string{"../SKILL", "references/browser", ""} {
		if _, ok := skillTopic(name); ok {
			t.Errorf("skillTopic(%q) found a topic; only bare topic names do", name)
		}
	}
}

func TestSkillInstallFailureExits1(t *testing.T) {
	// A file where the skills folder should be: the folder cannot be made.
	// An empty dir is refused the same way.
	file := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{file, ""} {
		code, out := runMain(t, "skill", "install", dir)
		if code != 1 {
			t.Fatalf("%q: exit = %d, want 1; output %q", dir, code, out)
		}
		if !strings.HasPrefix(out, "winuse: installing the skill: ") {
			t.Errorf("%q: output = %q, want a one-line winuse: failure", dir, out)
		}
	}
}

func TestSkillUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{
		{"skill", "install"},
		{"skill", "show"},
		{"skill", "install", "a", "b"},
	} {
		code, out := runMain(t, args...)
		if code != 2 {
			t.Errorf("%q: exit = %d, want 2", args, code)
		}
		if !strings.Contains(out, skillUsage) {
			t.Errorf("%q: output = %q, want the skill usage", args, out)
		}
	}
}

func TestUsageMentionsSkill(t *testing.T) {
	for _, want := range []string{"skill ", "skill install <dir>"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("Usage does not mention %q", want)
		}
	}
}
