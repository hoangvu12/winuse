package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hoangvu12/winuse"
)

const skillUsage = "usage: winuse skill [<topic> | list | all] | winuse skill install <skills dir>"

// topicDir is where the skill's topics sit in winuse.SkillFiles, one
// <topic>.md each; the core is winuse.Skill.
const topicDir = "skills/winuse/references"

// skillCmd implements `winuse skill`, which prints the skill's core (the
// SKILL.md embedded in the binary); `winuse skill <topic>`, one topic;
// `winuse skill list`, the topics with when to read each; `winuse skill
// all`, the core and every topic; and `winuse skill install <dir>`, which
// writes the skill's folder to <dir>\winuse (the layout every agent's
// skills folder uses) and prints its SKILL.md's path. None talks to the
// helper, so none needs the desktop.
func skillCmd(args []string) int {
	switch {
	case len(args) == 0:
		io.WriteString(os.Stdout, winuse.Skill)
		return 0
	case len(args) == 2 && args[0] == "install":
		path, err := installSkill(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "winuse: installing the skill: %v\n", err)
			return 1
		}
		fmt.Println(path)
		return 0
	case len(args) == 1 && args[0] == "list":
		io.WriteString(os.Stdout, topicList())
		return 0
	case len(args) == 1 && args[0] == "all":
		io.WriteString(os.Stdout, winuse.Skill)
		for _, t := range skillTopics() {
			text, _ := skillTopic(t)
			io.WriteString(os.Stdout, "\n"+text)
		}
		return 0
	case len(args) == 1 && args[0] != "install":
		if text, ok := skillTopic(args[0]); ok {
			io.WriteString(os.Stdout, text)
			return 0
		}
		fmt.Fprintf(os.Stderr, "winuse: the skill has no topic %q%s\n%s\ntopics: %s\n",
			args[0], didYouMean(args[0], skillTopics()), skillUsage, strings.Join(skillTopics(), ", "))
		return 2
	default:
		fmt.Fprintln(os.Stderr, skillUsage)
		return 2
	}
}

// skillTopics lists the skill's topics, sorted: the names of its
// references/*.md files.
func skillTopics() []string {
	entries, err := fs.ReadDir(winuse.SkillFiles, topicDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".md"); ok && !e.IsDir() {
			names = append(names, name)
		}
	}
	return names
}

// skillTopic returns one topic's text.
func skillTopic(name string) (string, bool) {
	if !fs.ValidPath(name) || strings.Contains(name, "/") {
		return "", false
	}
	raw, err := fs.ReadFile(winuse.SkillFiles, path.Join(topicDir, name+".md"))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// topicList is `winuse skill list`: each topic and its "Read when:" line.
func topicList() string {
	var b strings.Builder
	for _, t := range skillTopics() {
		text, _ := skillTopic(t)
		when := ""
		for _, line := range strings.Split(text, "\n") {
			if w, ok := strings.CutPrefix(line, "Read when: "); ok {
				when = w
				break
			}
		}
		fmt.Fprintf(&b, "%-16s %s\n", t, when)
	}
	b.WriteString("\nwinuse skill <topic> prints one; winuse skill prints the core.\n")
	return b.String()
}

// didYouMean is "; did you mean <name>?" for the candidate nearest a
// mistyped name, or "" when none is near.
func didYouMean(name string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := editDistance(strings.ToLower(name), c); d < bestD {
			best, bestD = c, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("; did you mean %s?", best)
}

// installSkill writes the embedded skill folder into dir\winuse: SKILL.md
// and references\<topic>.md, creating the folders, replacing an older
// copy and removing the topics an older copy had that this one does not.
// It returns SKILL.md's path.
func installSkill(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("the skills dir is empty")
	}
	skillDir := filepath.Join(dir, "winuse")
	refDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		return "", err
	}
	topics := skillTopics()
	for _, t := range topics {
		text, _ := skillTopic(t)
		if err := os.WriteFile(filepath.Join(refDir, t+".md"), []byte(text), 0o644); err != nil {
			return "", err
		}
	}
	if entries, err := os.ReadDir(refDir); err == nil {
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".md")
			if ok && !e.IsDir() && !slices.Contains(topics, name) {
				os.Remove(filepath.Join(refDir, e.Name()))
			}
		}
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(winuse.Skill), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
