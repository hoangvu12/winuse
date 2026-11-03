package cli

// launch-app's command line (headless-max ticket 01): --minimized, the
// repeatable --arg and --env, --cwd and --timeout around the App, and
// --headed never eaten from an option's value.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseLaunchArgs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	a, timeout, err := parseLaunchArgs([]string{
		"--minimized", `C:\Apps\tool.exe`, "--arg", "--verbose", "--arg=two words", "--arg", "",
		"--env", "A=1", "--env=B=x=y", "--cwd", "work", "--timeout", "30",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs: %v", err)
	}
	if a.App != `C:\Apps\tool.exe` || !a.Minimized || timeout != 30*time.Second {
		t.Errorf("app %q, minimized %v, timeout %s", a.App, a.Minimized, timeout)
	}
	if want := []string{"--verbose", "two words", ""}; !slices.Equal(a.Args, want) {
		t.Errorf("args = %q, want %q (in order, a value that looks like a flag kept)", a.Args, want)
	}
	if want := []string{"A=1", "B=x=y"}; !slices.Equal(a.Env, want) {
		t.Errorf("env = %q, want %q", a.Env, want)
	}
	if want := filepath.Join(cwd, "work"); a.Cwd != want {
		t.Errorf("cwd = %q, want it resolved against the CLI's folder: %q", a.Cwd, want)
	}

	// A name stays a name; a relative path becomes absolute.
	if a, _, _ := parseLaunchArgs([]string{"Excel"}); a.App != "Excel" || a.Args != nil || a.Minimized {
		t.Errorf("plain name: %+v", a)
	}
	if a, _, _ := parseLaunchArgs([]string{`.\bin\tool.exe`}); a.App != filepath.Join(cwd, "bin", "tool.exe") {
		t.Errorf("relative path: App = %q", a.App)
	}
}

func TestParseLaunchArgsRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "exactly one App"},
		{[]string{"a", "b"}, "exactly one App"},
		{[]string{"a", "--arg"}, "--arg needs a value"},
		{[]string{"a", "--cwd"}, "--cwd needs a value"},
		{[]string{"a", "--cwd", "x", "--cwd", "y"}, "--cwd given twice"},
		{[]string{"a", "--cwd", " "}, "--cwd needs a folder"},
		{[]string{"a", "--env", "NOVALUE"}, "want KEY=VALUE"},
		{[]string{"a", "--env", "=x"}, "want KEY=VALUE"},
		{[]string{"a", "--detach"}, "unknown flag --detach"},
		{[]string{"a", "--timeout", "121"}, "at most 120"},
	}
	for _, tc := range cases {
		_, _, err := parseLaunchArgs(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseLaunchArgs(%q) = %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
}

func TestHeadedIsNotTakenFromALaunchOption(t *testing.T) {
	rest, headed := extractHeaded([]string{"launch-app", "tool.exe", "--arg", "--headed", "--headed", "--env", "X=1"})
	if !headed {
		t.Error("the free --headed must still be seen")
	}
	if want := []string{"launch-app", "tool.exe", "--arg", "--headed", "--env", "X=1"}; !slices.Equal(rest, want) {
		t.Errorf("rest = %q, want %q", rest, want)
	}
	rest, headed = extractHeaded([]string{"launch-app", "tool.exe", "--arg", "--headed"})
	if headed || !slices.Equal(rest, []string{"launch-app", "tool.exe", "--arg", "--headed"}) {
		t.Errorf("rest = %q, headed %v: --arg's value is the App's", rest, headed)
	}
}

// --cdp (headless-max 15) is a plain flag anywhere around the App, and the
// token after --arg stays the App's own even when it is --cdp.
func TestParseLaunchArgsCDP(t *testing.T) {
	a, _, err := parseLaunchArgs([]string{`C:\Apps\demo.exe`, "--cdp", "--arg", "."})
	if err != nil || !a.CDP || !slices.Equal(a.Args, []string{"."}) {
		t.Errorf("--cdp: %+v, %v", a, err)
	}
	a, _, err = parseLaunchArgs([]string{"--arg", "--cdp", `C:\Apps\demo.exe`})
	if err != nil || a.CDP || !slices.Equal(a.Args, []string{"--cdp"}) {
		t.Errorf("--arg --cdp: %+v, %v", a, err)
	}
	if a, _, _ := parseLaunchArgs([]string{"Excel"}); a.CDP {
		t.Error("--cdp is off by default")
	}
}
