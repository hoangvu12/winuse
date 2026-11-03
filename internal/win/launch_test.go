package win

// The launcher's offline tests (headless-max ticket 01): the show state
// per mode, the command line's quoting, the working directory and the
// environment block. buildLaunch touches nothing; StartProcess refuses in
// a test binary.

import (
	"errors"
	"slices"
	"testing"
	"unicode/utf16"
)

func TestLaunchShowStateNeverActivates(t *testing.T) {
	if got := LaunchShowState(false); got != 4 { // SW_SHOWNOACTIVATE
		t.Errorf("LaunchShowState(false) = %d, want SW_SHOWNOACTIVATE (4)", got)
	}
	if got := LaunchShowState(true); got != 7 { // SW_SHOWMINNOACTIVE
		t.Errorf("LaunchShowState(true) = %d, want SW_SHOWMINNOACTIVE (7)", got)
	}
	for _, minimized := range []bool{false, true} {
		c, err := buildLaunch(`C:\Apps\app.exe`, LaunchOptions{Minimized: minimized}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.show != LaunchShowState(minimized) {
			t.Errorf("minimized %v: show state %d, want %d", minimized, c.show, LaunchShowState(minimized))
		}
	}
}

func TestBuildLaunchQuotesEachArgument(t *testing.T) {
	cases := []struct {
		name string
		path string
		args []string
		want string
	}{
		{"no arguments", `C:\Apps\app.exe`, nil, `C:\Apps\app.exe`},
		{"a path with spaces", `C:\Program Files\App\app.exe`, []string{"-x"}, `"C:\Program Files\App\app.exe" -x`},
		{"an argument with spaces", `C:\a.exe`, []string{`C:\My Files\doc.txt`}, `C:\a.exe "C:\My Files\doc.txt"`},
		{"an empty argument", `C:\a.exe`, []string{""}, `C:\a.exe ""`},
		{"embedded quotes", `C:\a.exe`, []string{`say "hi"`}, `C:\a.exe "say \"hi\""`},
		{"a trailing backslash in quotes", `C:\a.exe`, []string{`C:\dir with space\`}, `C:\a.exe "C:\dir with space\\"`},
		{"a flag and its value stay apart", `C:\a.exe`, []string{"--name", "two words"}, `C:\a.exe --name "two words"`},
	}
	for _, tc := range cases {
		c, err := buildLaunch(tc.path, LaunchOptions{Args: tc.args}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if c.cmdLine != tc.want {
			t.Errorf("%s: command line %s, want %s", tc.name, c.cmdLine, tc.want)
		}
		if c.app != tc.path {
			t.Errorf("%s: application name %q, want %q", tc.name, c.app, tc.path)
		}
	}
	if _, err := buildLaunch(`C:\a.exe`, LaunchOptions{Args: []string{"a\x00b"}}, nil); err == nil {
		t.Error("an argument with a NUL must be refused")
	}
}

func TestBuildLaunchWorkingDirectory(t *testing.T) {
	c, _ := buildLaunch(`C:\Apps\Tool\tool.exe`, LaunchOptions{}, nil)
	if c.dir != `C:\Apps\Tool` {
		t.Errorf("default dir = %q, want the executable's folder", c.dir)
	}
	c, _ = buildLaunch(`C:\Apps\Tool\tool.exe`, LaunchOptions{Dir: `D:\work`}, nil)
	if c.dir != `D:\work` {
		t.Errorf("dir = %q, want D:\\work", c.dir)
	}
}

func TestBuildLaunchEnvironment(t *testing.T) {
	base := []string{"=C:=C:\\x", "Path=C:\\Windows", "HOME=C:\\Users\\me"}
	c, _ := buildLaunch(`C:\a.exe`, LaunchOptions{}, base)
	if c.env != nil {
		t.Errorf("no --env: env = %q, want nil (the helper's environment is inherited)", c.env)
	}
	c, err := buildLaunch(`C:\a.exe`, LaunchOptions{Env: []string{"PATH=D:\\bin", "NEW=a=b", "EMPTY="}}, base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"=C:=C:\\x", "PATH=D:\\bin", "HOME=C:\\Users\\me", "NEW=a=b", "EMPTY="}
	if !slices.Equal(c.env, want) {
		t.Errorf("env = %q, want %q (PATH replaces Path in place; new names append)", c.env, want)
	}
	if !slices.Equal(base, []string{"=C:=C:\\x", "Path=C:\\Windows", "HOME=C:\\Users\\me"}) {
		t.Errorf("the base environment was modified: %q", base)
	}
	for _, bad := range []string{"NOVALUE", "=x", ""} {
		if _, err := buildLaunch(`C:\a.exe`, LaunchOptions{Env: []string{bad}}, base); err == nil {
			t.Errorf("--env %q must be refused", bad)
		}
	}
}

func TestEnvBlockIsDoubleNULTerminated(t *testing.T) {
	got := envBlock([]string{"A=1", "B=é"})
	want := append(utf16.Encode([]rune("A=1\x00B=é\x00")), 0)
	if !slices.Equal(got, want) {
		t.Errorf("envBlock = %v, want %v", got, want)
	}
	if got := envBlock(nil); !slices.Equal(got, []uint16{0, 0}) {
		t.Errorf("empty envBlock = %v, want two NULs", got)
	}
}

func TestStartProcessRefusesInsideTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	if _, err := StartProcess(`C:\Windows\System32\charmap.exe`, LaunchOptions{}); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Fatalf("StartProcess in a test binary = %v, want ErrLiveDesktopInTest", err)
	}
}
