package browser

// Ported from use-browser v0.9.0's clone_test.go: the incremental sync,
// pruning, SQLite groups and clone metadata.

import (
	"os"
	"path/filepath"
	"testing"
)

// A sync that only ever adds leaves the clone growing forever, so syncTree
// must delete what the source dropped, without touching the caches the
// clone built for itself or the files winuse writes.
func TestSyncTreePrunes(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()

	writeFile(t, filepath.Join(src, "Preferences"), "kept")
	writeFile(t, filepath.Join(src, "sub", "Bookmarks"), "kept too")

	writeFile(t, filepath.Join(dst, "Preferences"), "stale")
	writeFile(t, filepath.Join(dst, "sub", "Bookmarks"), "stale")
	writeFile(t, filepath.Join(dst, "gone.txt"), "source deleted this")
	writeFile(t, filepath.Join(dst, "old", "deep", "gone.txt"), "and this")
	writeFile(t, filepath.Join(dst, "Cache", "block"), "regenerable, and ours")
	writeFile(t, filepath.Join(dst, "winuse-port"), "9222")

	st := syncTree(src, dst)

	for _, p := range []string{"Preferences", "sub/Bookmarks"} {
		if !exists(filepath.Join(dst, p)) {
			t.Errorf("%s: missing after sync", p)
		}
	}
	for _, p := range []string{"gone.txt", "old/deep/gone.txt", "old"} {
		if exists(filepath.Join(dst, p)) {
			t.Errorf("%s: still present, prune missed it", p)
		}
	}
	for _, p := range []string{"Cache/block", "winuse-port"} {
		if !exists(filepath.Join(dst, p)) {
			t.Errorf("%s: pruned, but nothing in the source ever owned it", p)
		}
	}
	if st.Removed != 2 {
		t.Errorf("Removed = %d, want 2", st.Removed)
	}
	if st.Copied != 2 {
		t.Errorf("Copied = %d, want 2", st.Copied)
	}
}

// Cookies and Cookies-wal describe one transaction between them: a fresh
// database beside a stale journal reads as corrupt.
func TestSyncTreeMovesSQLiteGroupTogether(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()

	writeFile(t, filepath.Join(src, "Cookies"), "new database")
	writeFile(t, filepath.Join(src, "Cookies-wal"), "matching journal")
	// A different size from the source's, so the database differs even
	// when both files land in the same modification-time tick (use-browser's
	// "old database" had the same size and made this test flaky).
	writeFile(t, filepath.Join(dst, "Cookies"), "an older database")
	// Same size and mtime as the source, so on its own it looks up to date.
	writeFile(t, filepath.Join(dst, "Cookies-wal"), "matching journal")
	si, err := os.Stat(filepath.Join(src, "Cookies-wal"))
	if err != nil {
		t.Fatal(err)
	}
	os.Chtimes(filepath.Join(dst, "Cookies-wal"), si.ModTime(), si.ModTime())

	if st := syncTree(src, dst); st.Copied != 2 {
		t.Errorf("Copied = %d, want 2: the journal must travel with its database", st.Copied)
	}
}

// A journal the source has dropped must not survive, or the browser
// replays it against a database that has moved past it.
func TestSyncTreeDropsOrphanSidecar(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()

	writeFile(t, filepath.Join(src, "Cookies"), "checkpointed database")
	writeFile(t, filepath.Join(dst, "Cookies"), "older database")
	writeFile(t, filepath.Join(dst, "Cookies-wal"), "journal the source no longer has")

	syncTree(src, dst)

	if exists(filepath.Join(dst, "Cookies-wal")) {
		t.Error("Cookies-wal: orphan journal survived the sync")
	}
}

func TestCloneMetadataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := writeCloneMetadata(dir, "brave", "Profile 1"); err != nil {
		t.Fatal(err)
	}
	meta, ok := readCloneMetadata(dir)
	if !ok {
		t.Fatal("clone metadata was not readable")
	}
	if meta.Browser != "brave" || meta.SourceProfile != "Profile 1" {
		t.Fatalf("metadata = %#v", meta)
	}
}

// A second sync with nothing changed copies nothing: copyFile carries the
// modification time across.
func TestSyncTreeIsIncremental(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "Preferences"), "p")
	writeFile(t, filepath.Join(src, "Network", "Cookies"), "c")
	if st := syncTree(src, dst); st.Copied != 2 {
		t.Fatalf("first sync Copied = %d, want 2", st.Copied)
	}
	if st := syncTree(src, dst); st.Copied != 0 || st.Removed != 0 {
		t.Errorf("second sync = %+v, want nothing to do", st)
	}
}

func TestCopyTreeSkipsCaches(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "Preferences"), "p")
	writeFile(t, filepath.Join(src, "Code Cache", "js", "x"), "cache")
	writeFile(t, filepath.Join(src, "Service Worker", "y"), "cache")
	if skipped := copyTree(src, dst); skipped != 0 {
		t.Errorf("skipped = %d", skipped)
	}
	if !exists(filepath.Join(dst, "Preferences")) || exists(filepath.Join(dst, "Code Cache")) || exists(filepath.Join(dst, "Service Worker")) {
		t.Error("copyTree copied a cache or missed a file")
	}
}
