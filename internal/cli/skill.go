package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hoangvu12/winuse"
)

const skillUsage = "usage: winuse skill | winuse skill install <skills dir>"

// skillCmd implements `winuse skill`, which prints the SKILL.md embedded
// in the binary, and `winuse skill install <dir>`, which writes it to
// <dir>\winuse\SKILL.md (the layout every agent's skills folder uses) and
// prints that path. Neither talks to the helper, so neither needs the
// desktop.
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
	default:
		fmt.Fprintln(os.Stderr, skillUsage)
		return 2
	}
}

// installSkill writes the embedded SKILL.md into dir\winuse, creating the
// folders and replacing an older copy, and returns the file's path.
func installSkill(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("the skills dir is empty")
	}
	skillDir := filepath.Join(dir, "winuse")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(winuse.Skill), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
