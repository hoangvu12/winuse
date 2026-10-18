package win

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Found live: a lazy proc bound to the wrong DLL (GetModuleHandleW from
// user32) or a misspelled export (CreatePenW) panics only on first call,
// which no offline test reached — it killed the helper at startup. This
// test scans every package in the module for NewProc lookups and checks
// each one resolves in its DLL.
var (
	dllVarRe     = regexp.MustCompile(`(\w+)\s*=\s*windows\.NewLazySystemDLL\("([^"]+)"\)`)
	procRe       = regexp.MustCompile(`(\w+)\.NewProc\("(\w+)"\)`)
	inlineProcRe = regexp.MustCompile(`windows\.NewLazySystemDLL\("([^"]+)"\)\.NewProc\("(\w+)"\)`)
)

func TestAllLazyProcsResolve(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Source per package directory: DLL variables are package-scoped.
	pkgs := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkgs[filepath.Dir(path)] = append(pkgs[filepath.Dir(path)], string(src))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for dir, files := range pkgs {
		all := strings.Join(files, "\n")
		dlls := map[string]string{}
		for _, m := range dllVarRe.FindAllStringSubmatch(all, -1) {
			dlls[m[1]] = m[2]
		}
		check := func(dll, proc string) {
			checked++
			if err := windows.NewLazySystemDLL(dll).NewProc(proc).Find(); err != nil {
				rel, _ := filepath.Rel(root, dir)
				t.Errorf("%s: %s!%s does not resolve: %v", rel, dll, proc, err)
			}
		}
		for _, m := range inlineProcRe.FindAllStringSubmatch(all, -1) {
			check(m[1], m[2])
		}
		for _, m := range procRe.FindAllStringSubmatch(all, -1) {
			if dll, ok := dlls[m[1]]; ok {
				check(dll, m[2])
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d procs found; the scan is broken", checked)
	}
}
