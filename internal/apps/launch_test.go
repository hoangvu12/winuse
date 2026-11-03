package apps

// StartApp's offline tests (headless-max ticket 01): the shell's show
// state per mode, and the options only an executable takes. Nothing is
// started: the shell call is only built, and the live guard refuses the
// rest in a test binary.

import (
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/win"
)

func TestShellInfoShowsWithoutActivating(t *testing.T) {
	for _, minimized := range []bool{false, true} {
		info := shellInfo(`C:\Users\me\Desktop\App.lnk`, `C:\Users\me\Desktop`, minimized)
		if want := int32(win.LaunchShowState(minimized)); info.nShow != want {
			t.Errorf("minimized %v: nShow = %d, want %d", minimized, info.nShow, want)
		}
	}
	if info := shellInfo(`C:\x.lnk`, `C:\`, false); info.nShow != 4 {
		t.Errorf("nShow = %d, want SW_SHOWNOACTIVATE (4), never SW_SHOWNORMAL", info.nShow)
	}
}

func TestStartAppRefusesExeOnlyOptionsForShellLaunches(t *testing.T) {
	specs := []Spec{
		{App: "App.lnk", ID: `C:\x\App.lnk`, Kind: IDShortcut},
		{App: "Calculator", ID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
	}
	for _, opts := range []win.LaunchOptions{{Args: []string{"a"}}, {Dir: `C:\`}, {Env: []string{"A=1"}}} {
		if !ExeOnlyOptions(opts) {
			t.Errorf("ExeOnlyOptions(%+v) = false", opts)
		}
		for _, spec := range specs {
			_, err := StartApp(spec, opts)
			if err == nil || !strings.Contains(err.Error(), "need an executable") {
				t.Errorf("StartApp(%s, %+v) = %v, want the executable-only refusal", spec.App, opts, err)
			}
		}
	}
	if ExeOnlyOptions(win.LaunchOptions{Minimized: true}) {
		t.Error("--minimized applies to every kind of App")
	}
}

func TestStartAppRefusesInsideTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	for _, spec := range []Spec{
		{App: "charmap", ID: `C:\Windows\System32\charmap.exe`, Kind: IDExe},
		{App: "App.lnk", ID: `C:\x\App.lnk`, Kind: IDShortcut},
		{App: "Calculator", ID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
	} {
		if _, err := StartApp(spec, win.LaunchOptions{}); !errors.Is(err, win.ErrLiveDesktopInTest) {
			t.Errorf("StartApp(%s) in a test binary = %v, want ErrLiveDesktopInTest", spec.App, err)
		}
	}
}
