package browser

// The clone route (ub/clone.go): copy the user's real profile into a
// non-default user-data-dir, then launch the browser on the copy with
// flag-based debugging. The browser carries the user's real logins and
// cookies, with no inspect toggle and no approval prompt.
//
// It works because Chromium 136 ignores --remote-debugging-port only for a
// browser's default profile directory; a copy lives elsewhere. The real
// profile is only ever read, never launched with debugging, so the user's
// everyday browser keeps its own encryption key. The copy is kept and
// synced on every clone: only changed files move, and files the real
// profile deleted are pruned.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CloneOptions are `winuse browser clone`'s arguments.
type CloneOptions struct {
	Browser     string // short name; "" is the pin, else the first installed
	Profile     string // profile folder to copy; "" is Default
	Fresh       bool   // re-copy from scratch instead of syncing
	CloseSource bool   // close the running browser first so its cookies copy, and reopen its tabs in the clone
}

// How long --close-source waits for the browser to let go of its cookie
// database, and for a running clone (same executable) to exit.
const (
	closeSourceWait = 30 * time.Second
	cloneExitWait   = 15 * time.Second
)

type cloneMetadata struct {
	Browser       string `json:"browser"`
	SourceProfile string `json:"sourceProfile"`
}

func cloneMetadataPath(dir string) string {
	return filepath.Join(dir, "winuse-clone.json")
}

func readCloneMetadata(dir string) (cloneMetadata, bool) {
	var meta cloneMetadata
	b, err := os.ReadFile(cloneMetadataPath(dir))
	if err != nil || json.Unmarshal(b, &meta) != nil {
		return cloneMetadata{}, false
	}
	return meta, true
}

func writeCloneMetadata(dir, browserName, profileDir string) error {
	b, err := json.Marshal(cloneMetadata{Browser: browserName, SourceProfile: profileDir})
	if err != nil {
		return err
	}
	return os.WriteFile(cloneMetadataPath(dir), b, 0o644)
}

// listProfiles returns the profile folders of a user-data-dir (the ones
// with a Preferences file), e.g. Default, "Profile 1".
func listProfiles(userDataDir string) []string {
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(userDataDir, e.Name(), "Preferences")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// cacheDirs only hold regenerable caches. Skipping them keeps the clone
// small; none of them hold logins.
var cacheDirs = map[string]bool{
	"Cache": true, "Code Cache": true, "GPUCache": true, "DawnCache": true,
	"DawnGraphiteCache": true, "DawnWebGPUCache": true, "GrShaderCache": true,
	"ShaderCache": true, "CacheStorage": true, "ScriptCache": true,
	"Service Worker": true, "component_crx_cache": true, "extensions_crx_cache": true,
}

// copyTree copies src into dst, skipping cache directories. A file that
// cannot be read (a lock held by a running browser) is counted and
// skipped rather than aborting the copy.
func copyTree(src, dst string) (skipped int) {
	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			skipped++
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if cacheDirs[d.Name()] {
				return filepath.SkipDir
			}
			os.MkdirAll(filepath.Join(dst, rel), 0o755)
			return nil
		}
		if err := copyFile(path, filepath.Join(dst, rel)); err != nil {
			skipped++
		}
		return nil
	})
	return skipped
}

// copyFile copies one file and carries its modification time across;
// without it every later sync would see every file as changed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(src); err == nil {
		os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
	return nil
}

// dbSuffixes are the sidecars of Chromium's SQLite databases.
var dbSuffixes = []string{"-wal", "-shm", "-journal"}

// groupKey names the SQLite group a file belongs to: "Cookies",
// "Cookies-wal" and "Cookies-shm" all answer "Cookies". A file with no
// sidecar suffix is a group of one.
func groupKey(name string) string {
	for _, suf := range dbSuffixes {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

type syncStat struct {
	Copied  int
	Skipped int
	Bytes   int64
	Removed int
	Freed   int64
}

// syncTree copies only what differs between src and dst, by size and
// modification time. A SQLite database and its sidecars move as one group:
// a fresh Cookies beside a stale Cookies-wal describes a transaction its
// journal no longer explains, and the browser reads it as corrupt.
func syncTree(src, dst string) syncStat {
	var st syncStat
	changed := map[string]map[string]bool{} // changed[dir][group]
	type entry struct{ rel, dir, name string }
	var files []entry

	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			st.Skipped++
			return nil
		}
		if d.IsDir() {
			if cacheDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return nil
		}
		dir := filepath.Dir(rel)
		files = append(files, entry{rel, dir, d.Name()})
		si, err := d.Info()
		if err != nil {
			st.Skipped++
			return nil
		}
		di, err := os.Stat(filepath.Join(dst, rel))
		if err == nil && di.Size() == si.Size() && di.ModTime().Equal(si.ModTime()) {
			return nil
		}
		if changed[dir] == nil {
			changed[dir] = map[string]bool{}
		}
		changed[dir][groupKey(d.Name())] = true
		return nil
	})

	present := map[string]bool{}
	for _, f := range files {
		present[f.rel] = true
		if !changed[f.dir][groupKey(f.name)] {
			continue
		}
		if err := copyFile(filepath.Join(src, f.rel), filepath.Join(dst, f.rel)); err != nil {
			st.Skipped++
			continue
		}
		st.Copied++
		if fi, err := os.Stat(filepath.Join(dst, f.rel)); err == nil {
			st.Bytes += fi.Size()
		}
	}
	// A sidecar the source no longer has must not survive in the copy, or
	// the browser replays a journal against a database that moved past it.
	for _, f := range files {
		if !changed[f.dir][groupKey(f.name)] {
			continue
		}
		for _, suf := range dbSuffixes {
			side := filepath.Join(f.dir, groupKey(f.name)+suf)
			if !present[side] {
				os.Remove(filepath.Join(dst, side))
			}
		}
	}
	st.Removed, st.Freed = pruneTree(dst, present)
	return st
}

// pruneKeep live in the copy on purpose with no source: winuse writes the
// first two, the browser the rest while it runs the clone.
var pruneKeep = map[string]bool{
	portFileName: true, "winuse-clone.json": true, "DevToolsActivePort": true, "lockfile": true,
	"SingletonLock": true, "SingletonCookie": true, "SingletonSocket": true,
}

// pruneTree deletes files the source no longer has, so the clone does not
// grow forever. Cache directories are left alone: the source never had
// them and the clone builds its own.
func pruneTree(dst string, present map[string]bool) (removed int, freed int64) {
	var dirs []string
	filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if cacheDirs[d.Name()] {
				return filepath.SkipDir
			}
			if path != dst {
				dirs = append(dirs, path)
			}
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil || present[rel] || pruneKeep[d.Name()] {
			return nil
		}
		var size int64
		if fi, err := d.Info(); err == nil {
			size = fi.Size()
		}
		if os.Remove(path) == nil {
			removed++
			freed += size
		}
		return nil
	})
	// Deepest first, so a directory the prune emptied goes too; Remove
	// fails on anything still occupied, which is the guard we want.
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Remove(dirs[i])
	}
	return removed, freed
}

// cookiesLocked reports whether the cookie database cannot be opened. A
// running Chromium holds it without share-read on Windows. A missing file
// is not locked: there is nothing to copy.
func cookiesLocked(path string) bool {
	f, err := os.Open(path)
	if err == nil {
		f.Close()
		return false
	}
	return !os.IsNotExist(err)
}

// cloneArgs are launchArgs plus the profile folder, and the session
// restore after --close-source closed the user's browser: the clone
// reopens their tabs rather than replacing them.
func cloneArgs(port int, userDataDir, profile string, restoreSession bool) []string {
	args := append(launchArgs(port, userDataDir), "--profile-directory="+profile)
	if restoreSession {
		args = append(args, "--restore-last-session")
	}
	return args
}

// Clone answers `winuse browser clone [browser] [--profile <folder>]
// [--fresh] [--close-source]`: copy or sync the user's real profile into
// winuse's clone, launch the browser on it and connect. Notes (files that
// could not be copied, logins that did not sync) follow the ok line. It
// pins the browser.
func (m *Manager) Clone(ctx context.Context, opts CloneOptions) (string, error) {
	b, err := m.pick(opts.Browser, false)
	if err != nil {
		return "", err
	}
	if err := m.sys.SetPin(b.Name); err != nil {
		return "", err
	}
	profile := opts.Profile
	if profile == "" {
		profile = "Default"
	}
	clone := m.sys.CloneProfileDir(b.Name)
	// As in launch, only an instance winuse started counts. --close-source
	// asks for fresh logins, so a running clone does not end it here.
	if eps := m.ownEndpoints(ctx, b.Name); len(eps) > 0 && !opts.CloseSource {
		if _, err := m.dial(ctx, eps[0]); err != nil {
			return "", err
		}
		return fmt.Sprintf("ok %s already running from winuse's %s profile (port %d)\n", b.Name, eps[0].Mode, eps[0].Port), nil
	}
	if !opts.CloseSource && m.profileInUse(clone) {
		return "", &Problem{Kind: ProfileBusy, Browser: b.Name, Mode: ModeClone, Dir: clone}
	}

	src := m.sys.Env.UserDataDirs()[b.Name]
	if _, err := os.Stat(src); err != nil {
		return "", &Problem{Kind: NoUserData, Browser: b.Name, Dir: src}
	}
	if _, err := os.Stat(filepath.Join(src, profile)); err != nil {
		avail := listProfiles(src)
		if len(avail) == 0 {
			return "", &Problem{Kind: NoUserData, Browser: b.Name, Dir: src}
		}
		return "", &Problem{Kind: NoSuchProfile, Browser: b.Name, Profile: profile, Detail: strings.Join(avail, ", ")}
	}

	var notes []string
	fresh := opts.Fresh
	if meta, ok := readCloneMetadata(clone); ok && (meta.Browser != b.Name || meta.SourceProfile != profile) {
		notes = append(notes, fmt.Sprintf("the source profile changed from %s to %s, so the clone was copied again", meta.SourceProfile, profile))
		fresh = true
	}

	cookies := filepath.Join(src, profile, "Network", "Cookies")
	restoreSession := false
	if opts.CloseSource && m.sys.running(b) {
		// No /F: Chromium shuts down cleanly on the close request, flushing
		// SQLite and its session file; a kill would tear both.
		if err := m.sys.CloseApp(b.Exe()); err != nil {
			return "", fmt.Errorf("asking %s to close: %w", b.Display(), err)
		}
		if !m.sys.poll(ctx, closeSourceWait, 300*time.Millisecond, func() bool { return !cookiesLocked(cookies) }) {
			return "", &Problem{Kind: CloseFailed, Browser: b.Name}
		}
		// The clone runs the same executable, so the close took it down too.
		if !m.sys.poll(ctx, cloneExitWait, 300*time.Millisecond, func() bool { return !m.profileInUse(clone) }) {
			return "", &Problem{Kind: ProfileBusy, Browser: b.Name, Mode: ModeClone, Dir: clone}
		}
		m.drop(b.Name, nil)
		restoreSession = true
	}

	needCopy := fresh
	if _, err := os.Stat(filepath.Join(clone, profile)); err != nil {
		needCopy = true
	}
	if needCopy {
		if m.sys.running(b) {
			notes = append(notes, fmt.Sprintf("%s was running, so open login files may have been skipped; for a complete copy run winuse browser clone %s --fresh --close-source", b.Display(), b.Name))
		}
		if fresh {
			os.RemoveAll(clone)
		}
		if err := os.MkdirAll(clone, 0o755); err != nil {
			return "", err
		}
		// Local State holds the profile's encryption key; without it the
		// copied cookies and saved passwords cannot be decrypted.
		copyFile(filepath.Join(src, "Local State"), filepath.Join(clone, "Local State"))
		if skipped := copyTree(filepath.Join(src, profile), filepath.Join(clone, profile)); skipped > 0 {
			notes = append(notes, fmt.Sprintf("%d file(s) were locked or unreadable and were skipped", skipped))
		}
	} else {
		// Ask the file, not the process list: a browser asked to close still
		// shows in the process table for a moment, and this one file is
		// exactly what cannot be copied while it runs.
		if cookiesLocked(cookies) {
			notes = append(notes, fmt.Sprintf("%s holds its cookie database open, so logins did not sync (everything else did); run winuse browser clone %s --close-source to close it and reopen its tabs in the clone", b.Display(), b.Name))
		}
		copyFile(filepath.Join(src, "Local State"), filepath.Join(clone, "Local State"))
		st := syncTree(filepath.Join(src, profile), filepath.Join(clone, profile))
		if st.Copied > 0 || st.Removed > 0 {
			note := fmt.Sprintf("synced %d file(s), %.1f MB", st.Copied, float64(st.Bytes)/(1024*1024))
			if st.Skipped > 0 {
				note += fmt.Sprintf(", %d skipped (locked or unreadable)", st.Skipped)
			}
			if st.Removed > 0 {
				note += fmt.Sprintf(", pruned %d file(s), %.1f MB freed", st.Removed, float64(st.Freed)/(1024*1024))
			}
			notes = append(notes, note)
		}
	}
	if err := writeCloneMetadata(clone, b.Name, profile); err != nil {
		return "", fmt.Errorf("recording the clone's source: %w", err)
	}

	port := m.sys.FreePort()
	if err := writePortFile(clone, port); err != nil {
		return "", err
	}
	pid, err := m.sys.Start(b.Path, cloneArgs(port, clone, profile, restoreSession))
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", b.Path, err)
	}
	ep, err := m.awaitOwn(ctx, b.Name, ModeClone, clone, cloneWait, pid, port)
	if err != nil {
		return "", err
	}
	if _, err := m.dial(ctx, ep); err != nil {
		return "", err
	}
	out := fmt.Sprintf("ok %s clone started: pid %d, port %d, profile %s (your logins)\n", b.Name, pid, port, strconv.Quote(profile))
	for _, n := range notes {
		out += "note: " + n + "\n"
	}
	return out, nil
}
