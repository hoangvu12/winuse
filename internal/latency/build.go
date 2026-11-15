package latency

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Kinds of arm source.
const (
	kindRef  = "ref"
	kindTree = "tree"
	kindExe  = "exe"
)

// sourceKind says what an arm's source names: a winuse.exe, a source tree
// (a directory holding go.mod, such as "." for the working tree with its
// uncommitted changes), or else a git ref of the repository.
func sourceKind(src string) string {
	if strings.EqualFold(filepath.Ext(src), ".exe") {
		if st, err := os.Stat(src); err == nil && !st.IsDir() {
			return kindExe
		}
	}
	if st, err := os.Stat(filepath.Join(src, "go.mod")); err == nil && !st.IsDir() {
		return kindTree
	}
	return kindRef
}

// git runs git in repo and returns its trimmed stdout.
func git(repo string, args ...string) (string, error) {
	cmd := win.NoConsoleWindow(exec.Command("git", append([]string{"-C", repo}, args...)...))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBase is the merge-base of HEAD with main: the default arm A.
func MergeBase(repo string) (string, error) {
	sha, err := git(repo, "merge-base", "HEAD", "main")
	if err != nil {
		return "", fmt.Errorf("%w (name arm A with --a <ref> when there is no main)", err)
	}
	return sha, nil
}

// PrepareArm puts arm's winuse.exe into dir: a git ref is exported with
// `git archive` (the repository and its worktrees are never touched) and
// built, a tree is built where it is, an exe is copied (so cleaning up the
// run's own helper by its path never touches an installed winuse).
func PrepareArm(repo, dir string, arm *Arm) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(dir, "winuse.exe")
	switch sourceKind(arm.Source) {
	case kindExe:
		abs, _ := filepath.Abs(arm.Source)
		arm.Desc = abs
		if err := copyFile(abs, exe); err != nil {
			return fmt.Errorf("arm %s: copying %s: %w", arm.Label, abs, err)
		}
	case kindTree:
		abs, _ := filepath.Abs(arm.Source)
		arm.Desc = abs + " (the tree as it is)"
		if err := buildWinuse(abs, exe); err != nil {
			return fmt.Errorf("arm %s: %w", arm.Label, err)
		}
	default:
		sha, err := git(repo, "rev-parse", "--verify", arm.Source+"^{commit}")
		if err != nil {
			return fmt.Errorf("arm %s: %q is not a winuse.exe, a source tree or a commit: %w", arm.Label, arm.Source, err)
		}
		arm.Desc = fmt.Sprintf("%s (%.10s)", arm.Source, sha)
		zipPath := filepath.Join(dir, "src.zip")
		if _, err := git(repo, "archive", "--format=zip", "-o", zipPath, sha); err != nil {
			return fmt.Errorf("arm %s: %w", arm.Label, err)
		}
		src := filepath.Join(dir, "src")
		if err := unzip(zipPath, src); err != nil {
			return fmt.Errorf("arm %s: unpacking %s: %w", arm.Label, sha, err)
		}
		if err := buildWinuse(src, exe); err != nil {
			return fmt.Errorf("arm %s: %w", arm.Label, err)
		}
	}
	arm.Exe = exe
	arm.Build = version.ExeBuild(exe)
	return nil
}

// buildWinuse builds cmd/winuse of the tree at src into exe.
func buildWinuse(src, exe string) error {
	return goBuild(src, exe, "./cmd/winuse")
}

// BuildFixture builds the fixture app of the tree at src (a GUI binary, as
// the e2e runner builds it) into dir.
func BuildFixture(src, dir string) (string, error) {
	exe := filepath.Join(dir, "winusefixture.exe")
	return exe, goBuild(src, exe, "-ldflags", "-H windowsgui", "./cmd/winusefixture")
}

func goBuild(src, exe string, args ...string) error {
	cmd := win.NoConsoleWindow(exec.Command("go", append([]string{"build", "-o", exe}, args...)...))
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build in %s: %v: %s", src, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// unzip extracts a git archive into dir. An entry that would land outside
// dir is refused.
func unzip(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("entry %q leaves the folder", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue // a symlink: winuse's tree has none it builds from
		}
		if err := extract(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
