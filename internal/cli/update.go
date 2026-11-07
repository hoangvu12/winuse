package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/version"
)

const updateUsage = "usage: winuse update [--to <version>] [--no-skill]"

// Where releases come from, and the seams the tests replace.
var (
	releaseLatestURL   = "https://api.github.com/repos/hoangvu12/winuse/releases/latest"
	releaseDownloadURL = "https://github.com/hoangvu12/winuse/releases/download"
	updateHTTP         = &http.Client{Timeout: 2 * time.Minute}
	// updateExecutable is the winuse.exe to replace.
	updateExecutable = os.Executable
	// probeVersion runs a downloaded winuse.exe's `version`.
	probeVersion = func(exe string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, exe, "version").Output()
		return strings.TrimSpace(string(out)), err
	}
	// installSkillWith runs exe's `skill install <dir>`, so the skill
	// written is the one the new binary carries.
	installSkillWith = func(exe, dir string) error {
		out, err := exec.Command(exe, "skill", "install", dir).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// updateSkillDirs are the skills folders the installer writes.
	updateSkillDirs = defaultSkillDirs
)

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// updateArgs is update's parsed command line.
type updateArgs struct {
	to      string // the version asked for, without its v; "" for the latest
	noSkill bool
}

func parseUpdateArgs(args []string) (updateArgs, error) {
	var a updateArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-skill":
			a.noSkill = true
		case arg == "--to" || strings.HasPrefix(arg, "--to="):
			v, ok := strings.CutPrefix(arg, "--to=")
			if !ok {
				if i+1 >= len(args) {
					return a, errors.New("--to takes a version, such as 0.4.0")
				}
				i++
				v = args[i]
			}
			v = strings.TrimPrefix(strings.TrimSpace(v), "v")
			if !versionPattern.MatchString(v) {
				return a, fmt.Errorf("--to %q is not a version such as 0.4.0", v)
			}
			a.to = v
		default:
			return a, fmt.Errorf("update: unexpected argument %q", arg)
		}
	}
	return a, nil
}

// updateCmd implements `winuse update [--to <version>] [--no-skill]`: it
// replaces this winuse.exe with a release (the latest, or the one --to
// names; older ones too) and rewrites the skill copies the installer wrote.
// It never stops the helper: a running exe cannot be overwritten on
// Windows but it can be renamed, so the old file steps aside and the new
// one takes its name. A helper of the old build keeps running from the
// renamed file, finishes the requests in flight, and exits at the next
// command, which starts the new one (server.go's stale exit). Every
// failure before the swap leaves the installed exe as it was.
func updateCmd(args []string) int {
	a, err := parseUpdateArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, updateUsage)
		return 2
	}
	if err := runUpdate(os.Stdout, a); err != nil {
		fmt.Fprintf(os.Stderr, "winuse: update: %v\n", err)
		return 1
	}
	return 0
}

func runUpdate(w io.Writer, a updateArgs) error {
	target := a.to
	if target == "" {
		latest, err := latestRelease()
		if err != nil {
			return fmt.Errorf("finding the latest release: %w", err)
		}
		target = latest
	}
	if target == version.Version {
		fmt.Fprintf(w, "winuse %s is already installed\n", target)
		return nil
	}
	exe, err := updateExecutable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	removeOldExes(exe)

	tmp, err := downloadRelease(w, exe, target)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // a no-op once it has become exe
	if got, err := probeVersion(tmp); err != nil {
		return fmt.Errorf("the downloaded winuse.exe does not run (%v); %s is unchanged", err, exe)
	} else if got != target {
		return fmt.Errorf("the downloaded winuse.exe says it is %q, not %s; %s is unchanged", got, target, exe)
	}
	if err := swapExe(exe, tmp); err != nil {
		return err
	}
	fmt.Fprintf(w, "winuse %s -> %s (%s)\n", version.Version, target, exe)
	if !a.noSkill {
		updateSkills(w, exe)
	}
	fmt.Fprintln(w, "a running helper finishes its requests and restarts at the next command")
	return nil
}

// latestRelease asks GitHub for the latest release's version.
func latestRelease() (string, error) {
	resp, err := updateHTTP.Get(releaseLatestURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", releaseLatestURL, resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(r.Tag, "v")
	if !versionPattern.MatchString(v) {
		return "", fmt.Errorf("the latest release's tag %q is no version", r.Tag)
	}
	return v, nil
}

// downloadRelease downloads release v's winuse.exe next to exe and checks
// it against the release's SHA256SUMS, and returns the file's path.
// Releases before SHA256SUMS existed are taken with a note.
func downloadRelease(w io.Writer, exe, v string) (string, error) {
	asset := fmt.Sprintf("winuse_windows_%s.exe", runtime.GOARCH)
	base := fmt.Sprintf("%s/v%s/", releaseDownloadURL, v)
	resp, err := updateHTTP.Get(base + asset)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", base+asset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("winuse has no release v%s with %s (the releases: https://github.com/hoangvu12/winuse/releases)", v, asset)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", base+asset, resp.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(exe), "winuse-update-*.exe")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, hash), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("downloading %s: %w", base+asset, err)
	}
	want, err := releaseChecksum(base, asset)
	switch {
	case errors.Is(err, errNoChecksums):
		fmt.Fprintf(w, "note: v%s publishes no SHA256SUMS, so the download is not checked against one\n", v)
	case err != nil:
		os.Remove(f.Name())
		return "", err
	case want != hex.EncodeToString(hash.Sum(nil)):
		os.Remove(f.Name())
		return "", fmt.Errorf("%s does not match v%s's SHA256SUMS; %s is unchanged", asset, v, exe)
	}
	return f.Name(), nil
}

var errNoChecksums = errors.New("the release has no SHA256SUMS")

// releaseChecksum reads asset's SHA-256 from the release's SHA256SUMS.
func releaseChecksum(base, asset string) (string, error) {
	resp, err := updateHTTP.Get(base + "SHA256SUMS")
	if err != nil {
		return "", fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errNoChecksums
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading SHA256SUMS: %s", resp.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		sum, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if ok && strings.TrimLeft(strings.TrimSpace(name), "*") == asset {
			return strings.ToLower(sum), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS lists no %s", asset)
}

// swapExe puts the file at tmp in exe's place: exe is renamed aside first
// (a running exe can be renamed, not overwritten), and named back if the
// new file cannot take its place. The renamed file is removed when nothing
// runs from it any more; removeOldExes catches the ones left behind.
func swapExe(exe, tmp string) error {
	var suffix [4]byte
	rand.Read(suffix[:])
	old := exe + ".old-" + hex.EncodeToString(suffix[:])
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("moving %s aside: %w; it is unchanged", exe, err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("putting the new winuse.exe in place: %w; and naming the old one back failed too (%v): rename %s to %s", err, rerr, old, exe)
		}
		return fmt.Errorf("putting the new winuse.exe in place: %w; %s is unchanged", err, exe)
	}
	os.Remove(old) // fails while a helper still runs from it
	return nil
}

// removeOldExes removes the old winuse.exe files earlier updates renamed
// aside, once no helper runs from them.
func removeOldExes(exe string) {
	olds, _ := filepath.Glob(exe + ".old-*")
	for _, old := range olds {
		os.Remove(old)
	}
}

// defaultSkillDirs are the skills folders install.ps1 writes the skill to:
// ~\.agents\skills, and Claude Code's (CLAUDE_CONFIG_DIR\skills when set).
func defaultSkillDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	claude := os.Getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	return []string{filepath.Join(home, ".agents", "skills"), filepath.Join(claude, "skills")}
}

// updateSkills rewrites, with the new exe, each skill copy that is already
// there. A winuse entry that is a link to somewhere winuse did not write
// (Fleet links ~\.agents\skills\winuse into its repo) belongs to whatever
// made the link, and is left alone; a link to a folder written here (npx
// skills links ~\.claude\skills\winuse to ~\.agents\skills\winuse) was
// updated with it.
func updateSkills(w io.Writer, exe string) {
	type entry struct{ dir, named, target string }
	var real, links []entry
	for _, dir := range updateSkillDirs() {
		named := filepath.Join(dir, "winuse")
		fi, err := os.Lstat(named)
		if err != nil {
			continue // no skill here to update
		}
		target, err := followLinks(named)
		if err != nil {
			fmt.Fprintf(w, "skill not updated in %s: %v\n", named, err)
			continue
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 && !strings.EqualFold(target, filepath.Clean(named)) {
			links = append(links, entry{dir, named, target})
		} else {
			real = append(real, entry{dir, named, target})
		}
	}
	written := map[string]bool{}
	for _, e := range real {
		if err := installSkillWith(exe, e.dir); err != nil {
			fmt.Fprintf(w, "skill not updated in %s: %v; run: winuse skill install \"%s\"\n", e.named, err, e.dir)
			continue
		}
		written[strings.ToLower(e.target)] = true
		fmt.Fprintf(w, "skill updated in %s\n", e.named)
	}
	for _, e := range links {
		if written[strings.ToLower(e.target)] {
			continue
		}
		fmt.Fprintf(w, "skill left alone in %s: it links to %s, which winuse did not install (Fleet or another tool manages it)\n", e.named, e.target)
	}
	if len(real) == 0 && len(links) == 0 {
		fmt.Fprintln(w, "no installed skill copy found to update; winuse skill prints the new one")
	}
}
