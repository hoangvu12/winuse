package cli

// `winuse update` against a fake release server: the exe it replaces, the
// skills folders and the version probe are temp files and fakes.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/version"
)

// fakeReleases serves releases: version -> the exe's bytes; sums says
// whether each publishes SHA256SUMS. It counts the requests.
type fakeReleases struct {
	latest   string
	exes     map[string]string
	sums     map[string]string // version -> SHA256SUMS text; missing: 404
	requests int
}

func (f *fakeReleases) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests++
	if r.URL.Path == "/latest" {
		fmt.Fprintf(w, `{"tag_name":"v%s"}`, f.latest)
		return
	}
	asset := fmt.Sprintf("winuse_windows_%s.exe", runtime.GOARCH)
	for v, body := range f.exes {
		switch r.URL.Path {
		case "/download/v" + v + "/" + asset:
			fmt.Fprint(w, body)
			return
		case "/download/v" + v + "/SHA256SUMS":
			if sums, ok := f.sums[v]; ok {
				fmt.Fprint(w, sums)
				return
			}
		}
	}
	http.NotFound(w, r)
}

func sumsFor(body string) string {
	h := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%s  winuse_windows_%s.exe\n", hex.EncodeToString(h[:]), runtime.GOARCH)
}

// updateEnv points update at f, a temp winuse.exe and temp skills folders,
// and returns the exe's path and the skills folders.
func updateEnv(t *testing.T, f *fakeReleases) (exe string, agents, claude string) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	root := t.TempDir()
	exe = filepath.Join(root, "bin", "winuse.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	agents = filepath.Join(root, ".agents", "skills")
	claude = filepath.Join(root, ".claude", "skills")
	saved := []any{releaseLatestURL, releaseDownloadURL, updateExecutable, probeVersion, installSkillWith, updateSkillDirs}
	t.Cleanup(func() {
		releaseLatestURL = saved[0].(string)
		releaseDownloadURL = saved[1].(string)
		updateExecutable = saved[2].(func() (string, error))
		probeVersion = saved[3].(func(string) (string, error))
		installSkillWith = saved[4].(func(string, string) error)
		updateSkillDirs = saved[5].(func() []string)
	})
	releaseLatestURL = srv.URL + "/latest"
	releaseDownloadURL = srv.URL + "/download"
	updateExecutable = func() (string, error) { return exe, nil }
	// The fake exes hold their version as their text.
	probeVersion = func(path string) (string, error) {
		raw, err := os.ReadFile(path)
		return strings.TrimPrefix(string(raw), "exe "), err
	}
	installSkillWith = func(_, dir string) error { _, err := installSkill(dir); return err }
	updateSkillDirs = func() []string { return []string{agents, claude} }
	return exe, agents, claude
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUpdateToTheLatestRelease(t *testing.T) {
	f := &fakeReleases{latest: "9.1.0", exes: map[string]string{"9.1.0": "exe 9.1.0"}}
	f.sums = map[string]string{"9.1.0": sumsFor("exe 9.1.0")}
	exe, agents, _ := updateEnv(t, f)
	if _, err := installSkill(agents); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(agents, "winuse", "SKILL.md"), []byte("old skill"), 0o644)
	code, out := runMain(t, "update")
	if code != 0 {
		t.Fatalf("exit %d: %q", code, out)
	}
	if got := readFile(t, exe); got != "exe 9.1.0" {
		t.Errorf("winuse.exe holds %q, want the 9.1.0 release", got)
	}
	for _, want := range []string{"winuse " + version.Version + " -> 9.1.0", "skill updated in " + filepath.Join(agents, "winuse")} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	checkInstalled(t, filepath.Join(agents, "winuse"))
	if olds, _ := filepath.Glob(exe + ".old-*"); len(olds) != 0 {
		t.Errorf("the old exe was left behind: %v", olds)
	}
	if strings.Contains(out, "note:") {
		t.Errorf("a checked download printed a note:\n%s", out)
	}
}

func TestUpdateToAnOlderReleaseWithoutChecksums(t *testing.T) {
	f := &fakeReleases{exes: map[string]string{"0.1.0": "exe 0.1.0"}}
	exe, agents, _ := updateEnv(t, f)
	code, out := runMain(t, "update", "--to", "v0.1.0")
	if code != 0 {
		t.Fatalf("exit %d: %q", code, out)
	}
	if got := readFile(t, exe); got != "exe 0.1.0" {
		t.Errorf("winuse.exe holds %q, want 0.1.0", got)
	}
	if !strings.Contains(out, "note: v0.1.0 publishes no SHA256SUMS") {
		t.Errorf("no note about the missing SHA256SUMS:\n%s", out)
	}
	if !strings.Contains(out, "no installed skill copy found") {
		t.Errorf("no word on the missing skill copies:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(agents, "winuse")); err == nil {
		t.Errorf("update created a skill copy that was not there")
	}
}

func TestUpdateToTheInstalledVersionDownloadsNothing(t *testing.T) {
	f := &fakeReleases{}
	exe, _, _ := updateEnv(t, f)
	code, out := runMain(t, "update", "--to", version.Version)
	if code != 0 || !strings.Contains(out, "is already installed") {
		t.Fatalf("exit %d: %q", code, out)
	}
	if f.requests != 0 {
		t.Errorf("%d requests, want none", f.requests)
	}
	if got := readFile(t, exe); got != "old exe" {
		t.Errorf("winuse.exe changed: %q", got)
	}
}

func TestUpdateFailuresLeaveTheExe(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeReleases
		args []string
		want string
	}{
		{"unknown version", &fakeReleases{}, []string{"--to", "9.9.9"}, "no release v9.9.9"},
		{"checksum mismatch", &fakeReleases{exes: map[string]string{"9.2.0": "exe 9.2.0"}, sums: map[string]string{"9.2.0": sumsFor("something else")}},
			[]string{"--to", "9.2.0"}, "does not match v9.2.0's SHA256SUMS"},
		{"checksum missing its line", &fakeReleases{exes: map[string]string{"9.2.0": "exe 9.2.0"}, sums: map[string]string{"9.2.0": "abc  other.exe\n"}},
			[]string{"--to", "9.2.0"}, "SHA256SUMS lists no"},
		{"wrong version inside", &fakeReleases{exes: map[string]string{"9.3.0": "exe 9.2.9"}},
			[]string{"--to", "9.3.0"}, `says it is "9.2.9"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe, _, _ := updateEnv(t, tc.f)
			code, out := runMain(t, append([]string{"update"}, tc.args...)...)
			if code != 1 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit %d, want 1 with %q: %q", code, tc.want, out)
			}
			if got := readFile(t, exe); got != "old exe" {
				t.Errorf("winuse.exe changed: %q", got)
			}
			if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), "winuse-update-*")); len(left) != 0 {
				t.Errorf("the download was left behind: %v", left)
			}
		})
	}
}

func TestUpdateUsage(t *testing.T) {
	for _, args := range [][]string{{"update", "--to"}, {"update", "--to", "latest"}, {"update", "now"}} {
		if code, out := runMain(t, args...); code != 2 || !strings.Contains(out, updateUsage) {
			t.Errorf("%v: exit %d, %q; want 2 with the usage", args, code, out)
		}
	}
}

// The skill copies: a plain folder is rewritten, a link to it (npx skills)
// rides along, and a link elsewhere (Fleet's, into its repo) is left alone.
func TestUpdateSkipsSkillCopiesLinkedElsewhere(t *testing.T) {
	f := &fakeReleases{exes: map[string]string{"9.4.0": "exe 9.4.0"}}

	t.Run("npx skills layout", func(t *testing.T) {
		_, agents, claude := updateEnv(t, f)
		os.MkdirAll(claude, 0o755)
		if _, err := installSkill(agents); err != nil {
			t.Fatal(err)
		}
		makeJunction(t, filepath.Join(claude, "winuse"), filepath.Join(agents, "winuse"))
		code, out := runMain(t, "update", "--to", "9.4.0")
		if code != 0 {
			t.Fatalf("exit %d: %q", code, out)
		}
		if strings.Contains(out, "left alone") {
			t.Errorf("the link to the updated copy was reported as left alone:\n%s", out)
		}
	})

	t.Run("fleet layout", func(t *testing.T) {
		_, agents, claude := updateEnv(t, f)
		fleet := filepath.Join(t.TempDir(), "fleet", "skills", "universal", "winuse")
		os.MkdirAll(fleet, 0o755)
		os.WriteFile(filepath.Join(fleet, "SKILL.md"), []byte("fleet's copy"), 0o644)
		os.MkdirAll(agents, 0o755)
		os.MkdirAll(claude, 0o755)
		makeJunction(t, filepath.Join(agents, "winuse"), fleet)
		makeJunction(t, filepath.Join(claude, "winuse"), filepath.Join(agents, "winuse"))
		code, out := runMain(t, "update", "--to", "9.4.0")
		if code != 0 {
			t.Fatalf("exit %d: %q", code, out)
		}
		if got := readFile(t, filepath.Join(fleet, "SKILL.md")); got != "fleet's copy" {
			t.Errorf("Fleet's copy was overwritten")
		}
		if n := strings.Count(out, "skill left alone in"); n != 2 {
			t.Errorf("%d left-alone lines, want 2:\n%s", n, out)
		}
	})

	t.Run("--no-skill", func(t *testing.T) {
		_, agents, _ := updateEnv(t, f)
		if _, err := installSkill(agents); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(agents, "winuse", "SKILL.md"), []byte("old skill"), 0o644)
		if code, out := runMain(t, "update", "--to", "9.4.0", "--no-skill"); code != 0 || strings.Contains(out, "skill updated") {
			t.Fatalf("exit %d: %q", code, out)
		}
		if got := readFile(t, filepath.Join(agents, "winuse", "SKILL.md")); got != "old skill" {
			t.Errorf("--no-skill rewrote the skill")
		}
	})
}

// A rename-swap whose second step fails names the old exe back.
func TestSwapExeRollsBack(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "winuse.exe")
	os.WriteFile(exe, []byte("old exe"), 0o755)
	err := swapExe(exe, filepath.Join(dir, "missing.exe"))
	if err == nil || !strings.Contains(err.Error(), "is unchanged") {
		t.Fatalf("swapExe = %v, want an unchanged failure", err)
	}
	if got := readFile(t, exe); got != "old exe" {
		t.Errorf("winuse.exe holds %q after the rollback", got)
	}
}
