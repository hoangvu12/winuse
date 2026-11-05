package skill

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse"
	"github.com/hoangvu12/winuse/internal/cli"
)

// readSkill returns the shipped SKILL.md, the skill's core, skipping the
// frontmatter.
func readSkill(t *testing.T) (frontmatter, body string) {
	t.Helper()
	raw, err := os.ReadFile("../../skills/winuse/SKILL.md")
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	block := regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n(.*)\z`)
	m := block.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("SKILL.md must start with a `---` frontmatter block")
	}
	return m[1], m[2]
}

// readTopics returns the shipped topics, references/<topic>.md, by name.
func readTopics(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../../skills/winuse/references/*.md")
	if err != nil || len(paths) == 0 {
		t.Fatalf("listing skills/winuse/references: %v (%d topics)", err, len(paths))
	}
	topics := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		topics[strings.TrimSuffix(filepath.Base(p), ".md")] = strings.ReplaceAll(string(raw), "\r\n", "\n")
	}
	return topics
}

// wholeSkill is the core's body and every topic, joined.
func wholeSkill(t *testing.T) string {
	t.Helper()
	_, body := readSkill(t)
	topics := readTopics(t)
	parts := []string{body}
	for _, name := range slices.Sorted(maps.Keys(topics)) {
		parts = append(parts, topics[name])
	}
	return strings.Join(parts, "\n")
}

func TestSkillFrontmatterIsValid(t *testing.T) {
	front, _ := readSkill(t)
	name := ""
	desc := ""
	for _, line := range strings.Split(front, "\n") {
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	if name != "winuse" {
		t.Errorf("frontmatter name = %q, want %q (the skills CLI uses it as the install directory name)", name, "winuse")
	}
	if desc == "" {
		t.Errorf("frontmatter description is empty — `npx skills add` skips skills without a description")
	}
	if strings.ContainsAny(desc, "\n") {
		t.Errorf("frontmatter description must be one line")
	}
	// A plain (unquoted) YAML scalar cannot contain ": ": the frontmatter
	// is parsed as YAML by the skills CLI and by every harness, and a
	// colon-space makes the whole block a parse error, silently skipping
	// the skill.
	for _, line := range strings.Split(front, "\n") {
		if !strings.HasPrefix(line, "name:") && !strings.HasPrefix(line, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			continue // quoted scalar, colons allowed
		}
		if strings.Contains(value, ": ") {
			t.Errorf("frontmatter line %q contains \": \", which breaks YAML plain scalars — rephrase or quote the value", line)
		}
	}
}

// commandNames extracts the command names from the CLI's usage text: the
// first token of every line indented exactly two spaces under "commands:".
func commandNames(t *testing.T) []string {
	t.Helper()
	var names []string
	inCommands := false
	for _, line := range strings.Split(cli.Usage, "\n") {
		switch {
		case line == "commands:":
			inCommands = true
		case inCommands && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(name) {
				continue
			}
			names = append(names, name)
		}
	}
	if len(names) < 10 {
		t.Fatalf("extracted only %d command names from cli.Usage — the parser drifted: %v", len(names), names)
	}
	return names
}

// TestSkillCoversFullCommandSurface: every command is mentioned in the
// skill, in its core or in a topic.
func TestSkillCoversFullCommandSurface(t *testing.T) {
	all := wholeSkill(t)
	for _, name := range commandNames(t) {
		if !strings.Contains(all, name) {
			t.Errorf("neither SKILL.md nor a topic mentions the command %q — the skill must cover the full command surface", name)
		}
	}
}

func TestSkillNeverMentionsUseBrowser(t *testing.T) {
	if strings.Contains(strings.ToLower(wholeSkill(t)), "use-browser") {
		t.Errorf("the skill mentions use-browser — the skill must be self-contained and never reference it")
	}
}

func TestInstallerAssetMatchesReleaseWorkflow(t *testing.T) {
	ps, err := os.ReadFile("../../install.ps1")
	if err != nil {
		t.Fatalf("reading install.ps1: %v", err)
	}
	yml, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("reading .github/workflows/release.yml: %v", err)
	}
	repo := "hoangvu12/winuse"
	if !strings.Contains(string(ps), repo) {
		t.Errorf("install.ps1 does not reference the %s repo", repo)
	}
	if !strings.Contains(string(yml), repo) && !strings.Contains(string(yml), "./cmd/winuse") {
		t.Errorf("release.yml does not build the winuse binary")
	}
	// The installer derives assets as winuse_windows_$arch.exe; the workflow
	// must build exactly those names or every install 404s.
	assetPattern := regexp.MustCompile(`"dist/(winuse_windows_\$\{?[A-Za-z]*\}?[^"]*)"`)
	wfAssets := assetPattern.FindAllStringSubmatch(string(yml), -1)
	if len(wfAssets) == 0 {
		t.Fatalf("release.yml builds no dist/ assets — cannot verify the installer's asset names")
	}
	for _, m := range wfAssets {
		if !strings.HasPrefix(m[1], "winuse_windows_") || !strings.HasSuffix(m[1], ".exe") {
			t.Errorf("release.yml asset %q is not a winuse_windows_<arch>.exe name", m[1])
		}
	}
	if !strings.Contains(string(ps), "winuse_windows_") {
		t.Errorf("install.ps1 does not download a winuse_windows_<arch> asset")
	}
	if !strings.Contains(string(ps), `"latest/download/"`) &&
		!strings.Contains(string(ps), "releases/latest/download/") {
		t.Errorf("install.ps1 does not download from the latest GitHub Release")
	}
	// Both architectures must be installable.
	for _, arch := range []string{"amd64", "arm64"} {
		if !strings.Contains(string(yml), arch) {
			t.Errorf("release.yml does not build the %s asset", arch)
		}
	}
	if !strings.Contains(string(ps), `"arm64"`) {
		t.Errorf("install.ps1 does not map ARM64 machines to the arm64 asset")
	}
}

// TestSkillShipsAlone pins the one-command install: `npx skills add`
// copies the whole folder that holds SKILL.md into every agent's skills
// directory, so the skill lives in its own folder (a SKILL.md at the repo
// root would ship the source tree, the tickets and the repo's own
// CLAUDE.md/AGENTS.md with it) holding only SKILL.md and its topics,
// references/*.md, there is no root SKILL.md for the CLI to stop at, and
// install.ps1 installs the skill after the binary.
func TestSkillShipsAlone(t *testing.T) {
	entries, err := os.ReadDir("../../skills/winuse")
	if err != nil {
		t.Fatalf("reading skills/winuse: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "SKILL.md" && !(e.Name() == "references" && e.IsDir()) {
			t.Errorf("skills/winuse/%s ships to every agent with the skill; keep the folder to SKILL.md and references/", e.Name())
		}
	}
	refs, err := os.ReadDir("../../skills/winuse/references")
	if err != nil {
		t.Fatalf("reading skills/winuse/references: %v", err)
	}
	for _, e := range refs {
		if e.IsDir() || !regexp.MustCompile(`^[a-z][a-z-]*\.md$`).MatchString(e.Name()) {
			t.Errorf("skills/winuse/references/%s: keep the folder to <topic>.md files, one level deep, named in lowercase", e.Name())
		}
	}
	if _, err := os.Stat("../../SKILL.md"); err == nil {
		t.Errorf("a root SKILL.md makes `npx skills add` copy the whole repository; keep the skill in skills/winuse/")
	}
	ps, err := os.ReadFile("../../install.ps1")
	if err != nil {
		t.Fatalf("reading install.ps1: %v", err)
	}
	if !strings.Contains(string(ps), "skills add $repo") {
		t.Errorf("install.ps1 does not install the agent skill (npx skills add)")
	}
}

// TestInstallerWritesEverySkillsFolder pins where install.ps1 writes the
// skill: Claude Code reads only ~\.claude\skills (or CLAUDE_CONFIG_DIR's
// skills folder), the other harnesses ~\.agents\skills (speed issue 04).
func TestInstallerWritesEverySkillsFolder(t *testing.T) {
	ps, err := os.ReadFile("../../install.ps1")
	if err != nil {
		t.Fatalf("reading install.ps1: %v", err)
	}
	for _, want := range []string{`".agents\skills"`, `".claude"`, "CLAUDE_CONFIG_DIR", "skill install $dir"} {
		if !strings.Contains(string(ps), want) {
			t.Errorf("install.ps1 does not contain %q", want)
		}
	}
}

// TestSkillCoreKeepsTheRulesFirst: the core holds what an agent must never
// lose, first. Claude Code keeps only the first 5,000 tokens of a skill
// after compaction; the core stays well under that, and under the 500
// lines the Agent Skills guidance sets.
func TestSkillCoreKeepsTheRulesFirst(t *testing.T) {
	_, body := readSkill(t)
	loop := strings.Index(body, "\n## The loop\n")
	if loop < 0 {
		t.Fatalf("SKILL.md has no \"## The loop\" section")
	}
	for _, h := range []string{"## Safety", "## When the user presses Esc", "## Recover"} {
		i := strings.Index(body, "\n"+h+"\n")
		if i < 0 || i > loop {
			t.Errorf("SKILL.md must have %q before \"## The loop\" (found at %d, the loop at %d)", h, i, loop)
		}
	}
	for _, needle := range []string{"130", "Esc", "untrusted", "name:", "id:", "@x,y", "install.ps1", "winuse batch", "winuse skill <topic>"} {
		if !strings.Contains(body, needle) {
			t.Errorf("SKILL.md does not mention %q, which the core requires", needle)
		}
	}
	raw, err := os.ReadFile("../../skills/winuse/SKILL.md")
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	// 16 KiB is about 4,000 tokens of this English and Markdown: the whole
	// core survives compaction with room to spare.
	if n := len(raw); n > 16<<10 {
		t.Errorf("SKILL.md is %d bytes; keep the core under 16 KiB (move detail into a topic)", n)
	}
	if n := strings.Count(string(raw), "\n"); n > 300 {
		t.Errorf("SKILL.md is %d lines; keep the core under 300 (move detail into a topic)", n)
	}
}

// topicMention is how the skill points at a topic: `winuse skill <name>`,
// topic `<name>`, topics `<a>` and `<b>`, or a "| `<name>` |" row of the
// core's topic table.
var topicMention = regexp.MustCompile("(?:winuse skill ([a-z-]+)`|[Tt]opics? `([a-z-]+)`(?: and `([a-z-]+)`)?|^\\| `([a-z-]+)` \\|)")

// TestSkillTopicsAreListedAndResolve: every topic is a row of the core's
// topic table and starts with a title and a "Read when:" line, and every
// mention of a topic anywhere in the skill names one that exists.
func TestSkillTopicsAreListedAndResolve(t *testing.T) {
	_, body := readSkill(t)
	topics := readTopics(t)
	for name, text := range topics {
		if !strings.Contains(body, "| `"+name+"` |") {
			t.Errorf("topic %q is not a row of SKILL.md's topic table", name)
		}
		lines := strings.Split(text, "\n")
		if !strings.HasPrefix(lines[0], "# ") || len(lines) < 3 || !strings.HasPrefix(lines[2], "Read when: ") {
			t.Errorf("topic %q must start with a \"# \" title, then a \"Read when: \" line", name)
		}
	}
	notTopics := map[string]bool{"install": true, "list": true, "all": true}
	files := map[string]string{"SKILL.md": body}
	for name, text := range topics {
		files["references/"+name+".md"] = text
	}
	seen := 0
	for file, text := range files {
		for _, line := range strings.Split(text, "\n") {
			for _, m := range topicMention.FindAllStringSubmatch(line, -1) {
				for _, name := range m[1:] {
					if name == "" || notTopics[name] {
						continue
					}
					seen++
					if _, ok := topics[name]; !ok {
						t.Errorf("%s points at topic %q, which does not exist: %q", file, name, line)
					}
				}
			}
		}
	}
	if seen < len(topics) {
		t.Fatalf("found only %d topic mentions for %d topics; the pattern drifted", seen, len(topics))
	}
}

// oldSections is where each section of the single-file SKILL.md (as of
// dbce051) went when the skill split into a core and topics: nothing was
// dropped, only moved (speed issue 04). The core keeps a short form of
// "Name an Element" and "Read the result"; their full text is in the topic
// named here. The sentence-level check of the move is
// .scratch/speed/preserve_check.py.
var oldSections = []struct{ heading, home string }{
	{"If winuse is missing", "setup"},
	{"The loop", "SKILL.md"},
	{"Several agents", "multi-agent"},
	{"Find the Window", "windows"},
	{"The taskbar and the shell", "shell"},
	{"Read the Window State", "reading"},
	{"Dense Windows", "reading"},
	{"Displays", "windows"},
	{"Name an Element", "reading"},
	{"Choose Headless or Headed", "headed"},
	{"Keep the user's foreground", "headed"},
	{"What winuse cannot do headless", "headed"},
	{"Fill in fields, keys and text", "input"},
	{"Type into a named field", "input"},
	{"Select text", "input"},
	{"Posted Keys", "input"},
	{"Run a menu command", "input"},
	{"Scroll", "input"},
	{"Manage Windows", "windows"},
	{"Clipboard", "input"},
	{"Browser Windows", "browser"},
	{"Get a Browser Connection", "browser"},
	{"Act on a Page", "browser"},
	{"Tabs", "tabs"},
	{"Batches", "batch-and-wait"},
	{"Read the result", "troubleshooting"},
	{"Wait for a change", "batch-and-wait"},
	{"When the user presses Esc", "SKILL.md"},
	{"Refusals and recovery", "troubleshooting"},
	{"Safety", "SKILL.md"},
	{"Command reference", "commands"},
}

func TestEveryOldSectionHasAHome(t *testing.T) {
	_, body := readSkill(t)
	topics := readTopics(t)
	heading := regexp.MustCompile(`(?m)^#{1,4} (.+)$`)
	for _, s := range oldSections {
		text := body
		if s.home != "SKILL.md" {
			var ok bool
			if text, ok = topics[s.home]; !ok {
				t.Errorf("section %q's home, topic %q, does not exist", s.heading, s.home)
				continue
			}
		}
		found := false
		for _, m := range heading.FindAllStringSubmatch(text, -1) {
			found = found || m[1] == s.heading
		}
		if !found {
			t.Errorf("section %q is not a heading of its home, %s", s.heading, s.home)
		}
	}
}

// TestEmbeddedSkillMatchesTheShippedFiles pins `winuse skill`: the copy
// embedded in the binary is byte for byte skills/winuse/, SKILL.md and
// every topic, so an agent that re-reads it after an upgrade gets the
// text `npx skills add` ships for the same commit.
func TestEmbeddedSkillMatchesTheShippedFiles(t *testing.T) {
	raw, err := os.ReadFile("../../skills/winuse/SKILL.md")
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	if winuse.Skill != string(raw) {
		t.Errorf("the embedded skill (%d bytes) differs from skills/winuse/SKILL.md (%d bytes); rebuild, or fix the go:embed in the root skill.go", len(winuse.Skill), len(raw))
	}
	embedded := map[string]bool{}
	err = fs.WalkDir(winuse.SkillFiles, "skills/winuse", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embedded[path] = true
		got, _ := fs.ReadFile(winuse.SkillFiles, path)
		want, err := os.ReadFile("../../" + path)
		if err != nil || string(got) != string(want) {
			t.Errorf("the embedded %s differs from the shipped file (%v)", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded skill: %v", err)
	}
	for name := range readTopics(t) {
		if !embedded["skills/winuse/references/"+name+".md"] {
			t.Errorf("topic %q is not embedded in the binary", name)
		}
	}
}
