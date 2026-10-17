package skill

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cli"
)

// readSkill returns the shipped SKILL.md, skipping the frontmatter.
func readSkill(t *testing.T) (frontmatter, body string) {
	t.Helper()
	raw, err := os.ReadFile("../../SKILL.md")
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

func TestSkillCoversFullCommandSurface(t *testing.T) {
	_, body := readSkill(t)
	for _, name := range commandNames(t) {
		if !strings.Contains(body, name) {
			t.Errorf("SKILL.md does not mention the command %q — the skill must cover the full command surface", name)
		}
	}
}

func TestSkillNeverMentionsUseBrowser(t *testing.T) {
	raw, err := os.ReadFile("../../SKILL.md")
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "use-browser") {
		t.Errorf("SKILL.md mentions use-browser — the skill must be self-contained and never reference it")
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

func TestSkillMentionsEscExitCode(t *testing.T) {
	_, body := readSkill(t)
	for _, needle := range []string{"130", "Esc", "name:", "id:", "@x,y", "install.ps1"} {
		if !strings.Contains(body, needle) {
			t.Errorf("SKILL.md does not mention %q, which the guidance requires", needle)
		}
	}
	// sanity: the frontmatter mentions description once, and the whole file
	// is small enough to load cheaply when a harness pulls it in.
	info, err := os.Stat("../../SKILL.md")
	if err != nil {
		t.Fatalf("stat SKILL.md: %v", err)
	}
	if info.Size() > 64<<10 {
		t.Errorf("SKILL.md is %d bytes — keep it lean; the whole file is loaded when used", info.Size())
	}
}
