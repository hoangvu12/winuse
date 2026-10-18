package apps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

func winInfo(id uint64, app, title, exe string, pid uint32) win.WindowInfo {
	return win.WindowInfo{ID: id, App: app, Title: title, ExePath: exe, PID: pid}
}

func TestRunningFromWindowsGroupsByExe(t *testing.T) {
	ws := []win.WindowInfo{
		winInfo(10, "Notepad", "Untitled - Notepad", `C:\Windows\System32\notepad.exe`, 7),
		winInfo(11, "Notepad", "Notes", `C:\WINDOWS\System32\Notepad.exe`, 8), // same exe, different case
		winInfo(12, "Edge", "Docs", `C:\Program Files\Edge\msedge.exe`, 9),
		winInfo(13, "Mystery", "no exe path", "", 10), // dropped: no launchable identity
	}
	apps := RunningFromWindows(ws)
	if len(apps) != 2 {
		t.Fatalf("got %d Apps, want 2:\n%+v", len(apps), apps)
	}
	if apps[0].Name != "Notepad" || apps[0].ID != `C:\Windows\System32\notepad.exe` || !apps[0].Running {
		t.Fatalf("first App: %+v", apps[0])
	}
	if len(apps[0].Windows) != 2 || apps[0].Windows[0].ID != 10 || apps[0].Windows[1].ID != 11 {
		t.Fatalf("Notepad Windows must stay in z-order: %+v", apps[0].Windows)
	}
	if apps[1].Name != "Edge" || len(apps[1].Windows) != 1 {
		t.Fatalf("second App: %+v", apps[1])
	}
}

func TestCatalogRunningRecentAndStore(t *testing.T) {
	now := time.Now()
	older := now.Add(-2 * time.Hour)
	sources := Sources{
		Running: []App{
			{Name: "Notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe, Running: true,
				Windows: []win.WindowInfo{winInfo(10, "Notepad", "Untitled", `C:\Windows\System32\notepad.exe`, 7)}},
			{Name: "Calculator", ID: `C:\Program Files\WindowsApps\Microsoft.WindowsCalculator_8wekyb3d8bbwe\CalculatorApp.exe`,
				Kind: IDExe, Running: true,
				Windows: []win.WindowInfo{winInfo(11, "Calculator", "Calculator", `C:\Program Files\WindowsApps\Microsoft.WindowsCalculator_8wekyb3d8bbwe\CalculatorApp.exe`, 8)}},
		},
		Recent: []App{
			// Same App as the running Notepad: merges its usage in.
			{Name: "notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe, LastUsed: older, UseCount: 4},
			// A shortcut recent whose target is Edge, not running: kept.
			{Name: "Microsoft Edge", ID: `C:\Users\u\AppData\...\Start Menu\Programs\Microsoft Edge.lnk`,
				Kind: IDShortcut, TargetExe: `C:\Program Files\Edge\msedge.exe`, LastUsed: now, UseCount: 2},
			// A nameless AUMID recent the store will name.
			{ID: "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", Kind: IDAppUserModelID, LastUsed: now, UseCount: 1},
		},
		Store: []StoreApp{
			// The running packaged Calculator adopts the AUMID.
			{Name: "Calculator", AppID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App"},
			// Names the nameless WindowsTerminal recent entry.
			{Name: "Windows Terminal", AppID: "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App"},
		},
	}

	got := Catalog(sources)
	if len(got) != 4 {
		t.Fatalf("got %d Apps, want 4 (running Notepad+Calculator, Edge recent, Terminal recent):\n%+v", len(got), got)
	}
	// Running first, in input order.
	if got[0].Name != "Notepad" {
		t.Fatalf("App 0 = %+v, want the running Notepad", got[0])
	}
	if got[0].LastUsed != older || got[0].UseCount != 4 {
		t.Fatalf("recent usage must merge into the running App: %+v", got[0])
	}
	if got[1].Name != "Calculator" || got[1].ID != "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App" || got[1].Kind != IDAppUserModelID {
		t.Fatalf("running packaged App must adopt the AUMID: %+v", got[1])
	}
	if got[1].TargetExe == "" || !strings.HasSuffix(got[1].TargetExe, "CalculatorApp.exe") {
		t.Fatalf("adopted AUMID must keep the running exe as TargetExe: %+v", got[1])
	}
	if !got[1].Running || len(got[1].Windows) != 1 {
		t.Fatalf("adopted App must keep running state and Windows: %+v", got[1])
	}
	// Recents, most recent first. Edge and Terminal share "now"; stable order.
	if got[2].Name != "Microsoft Edge" || got[2].Running || got[2].ID == "" {
		t.Fatalf("App 2 = %+v, want the not-running Edge recent", got[2])
	}
	if got[3].Name != "Windows Terminal" || got[3].Kind != IDAppUserModelID {
		t.Fatalf("App 3 = %+v, want the store-named Terminal recent", got[3])
	}
}

func TestCatalogCapsAndOrdersRecent(t *testing.T) {
	base := time.Now()
	var recents []App
	for i := 0; i < MaxRecentApps+5; i++ {
		recents = append(recents, App{
			Name:     "app" + string(rune('a'+i%26)) + itoa(i),
			ID:       `C:\apps\app` + itoa(i) + ".exe",
			Kind:     IDExe,
			LastUsed: base.Add(-time.Duration(i) * time.Minute), // older as i grows
			UseCount: 1,
		})
	}
	got := Catalog(Sources{Recent: recents})
	if len(got) != MaxRecentApps {
		t.Fatalf("got %d recent Apps, want the cap %d", len(got), MaxRecentApps)
	}
	// Most recent first.
	for i := 1; i < len(got); i++ {
		if got[i].LastUsed.After(got[i-1].LastUsed) {
			t.Fatalf("recents must be most-recent-first: %v after %v", got[i].LastUsed, got[i-1].LastUsed)
		}
	}
	if got[0].ID != `C:\apps\app0.exe` {
		t.Fatalf("most recent app first, got %+v", got[0])
	}
}

func TestCatalogDropsNamelessAUMIDRecents(t *testing.T) {
	got := Catalog(Sources{Recent: []App{
		{ID: "Some.App_8wekyb3d8bbwe!App", Kind: IDAppUserModelID, LastUsed: time.Now()},
	}})
	if len(got) != 0 {
		t.Fatalf("nameless AUMID recent must be dropped, got %+v", got)
	}
}

func TestFormatApps(t *testing.T) {
	lastUsed := time.Date(2026, 2, 3, 14, 5, 0, 0, time.FixedZone("+07:00", 7*3600))
	apps := []App{
		{
			Name: "Notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe, Running: true,
			LastUsed: lastUsed, UseCount: 12,
			Windows: []win.WindowInfo{
				winInfo(131072, "Notepad", "Untitled - Notepad", `C:\Windows\System32\notepad.exe`, 7),
				winInfo(131100, "Notepad", "Save As", `C:\Windows\System32\notepad.exe`, 7),
			},
		},
		{Name: "Visual Studio Code", ID: `C:\Users\u\AppData\...\Start Menu\Programs\Visual Studio Code.lnk`, Kind: IDShortcut},
	}
	want := "Notepad\trunning\t2026-02-03T14:05:00+07:00\t12\tC:\\Windows\\System32\\notepad.exe\n" +
		"\t131072\tUntitled - Notepad\n" +
		"\t131100\tSave As\n" +
		"Visual Studio Code\tnot running\t-\t0\tC:\\Users\\u\\AppData\\...\\Start Menu\\Programs\\Visual Studio Code.lnk\n"
	if got := FormatApps(apps); got != want {
		t.Fatalf("FormatApps:\n got %q\nwant %q", got, want)
	}
}

func TestFormatAppsParsesBack(t *testing.T) {
	// App lines never start with a tab; Window lines do. Splitting on tab
	// with limit 3 keeps titles intact.
	out := FormatApps([]App{{
		Name: "App", ID: "C:\\a.exe", Kind: IDExe, Running: true,
		Windows: []win.WindowInfo{winInfo(42, "App", "title with\ttab", "C:\\a.exe", 1)},
	}})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%q", len(lines), out)
	}
	if strings.HasPrefix(lines[0], "\t") {
		t.Fatalf("App line must not start with a tab: %q", lines[0])
	}
	appFields := strings.SplitN(lines[0], "\t", 5)
	if len(appFields) != 5 || appFields[0] != "App" || appFields[1] != "running" {
		t.Fatalf("App line fields: %q", appFields)
	}
	if !strings.HasPrefix(lines[1], "\t") {
		t.Fatalf("Window line must start with a tab: %q", lines[1])
	}
	winFields := strings.SplitN(strings.TrimPrefix(lines[1], "\t"), "\t", 2)
	if len(winFields) != 2 || winFields[0] != "42" || winFields[1] != "title with\ttab" {
		t.Fatalf("Window line fields: %q", winFields)
	}
}

func TestFormatAppsEmpty(t *testing.T) {
	if got := FormatApps(nil); got != "" {
		t.Fatalf("want empty output, got %q", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

// ---------------------------------------------------------------- Resolve

func TestResolvePathAndIdentifierForms(t *testing.T) {
	cases := []struct {
		name  string
		app   string
		spec  Spec
		match bool // compare the fields that matter
	}{
		{
			name: "exe path",
			app:  `C:\Apps\Foo\foo.exe`,
			spec: Spec{App: `C:\Apps\Foo\foo.exe`, ID: `C:\Apps\Foo\foo.exe`, Kind: IDExe, TargetExe: `C:\Apps\Foo\foo.exe`},
		},
		{
			name: "relative path with separator",
			app:  `Apps\Foo\foo.exe`,
			spec: Spec{App: `Apps\Foo\foo.exe`, ID: `Apps\Foo\foo.exe`, Kind: IDExe, TargetExe: `Apps\Foo\foo.exe`},
		},
		{
			name: "shortcut path",
			app:  `C:\Users\u\Desktop\Thing.lnk`,
			spec: Spec{App: `C:\Users\u\Desktop\Thing.lnk`, ID: `C:\Users\u\Desktop\Thing.lnk`, Kind: IDShortcut},
		},
		{
			name: "App User Model ID",
			app:  "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App",
			spec: Spec{App: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", ID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := Resolve(tc.app, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if spec != tc.spec {
				t.Fatalf("spec:\n got %+v\nwant %+v", spec, tc.spec)
			}
		})
	}
}

func TestResolveNotFound(t *testing.T) {
	spec, err := Resolve("nothing matches this", []Candidate{{Name: "Notepad", ID: "C:\\np.exe", Kind: IDExe}})
	if err == nil || spec.ID != "" {
		t.Fatalf("want a not-found error, got %+v, %v", spec, err)
	}
	var re *ResolveError
	if !errors.As(err, &re) || re.Kind != ErrAppNotFound || re.App != "nothing matches this" {
		t.Fatalf("want ErrAppNotFound, got %v", err)
	}
	if re.Error() != "app not found: nothing matches this" {
		t.Fatalf("error text: %q", re.Error())
	}
}

func TestResolveAmbiguous(t *testing.T) {
	// Two distinct Apps genuinely named "Notepad": a classic exe and an
	// unrelated packaged App. Nothing folds them, so naming one is an
	// error listing both — never a guess.
	cands := []Candidate{
		{Name: "Notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe},
		{Name: "Notepad", ID: "SomeOther.Notepad_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
	}
	_, err := Resolve("Notepad", cands)
	if err == nil {
		t.Fatal("want ambiguity")
	}
	var re *ResolveError
	if !errors.As(err, &re) || re.Kind != ErrAmbiguousApp {
		t.Fatalf("want ErrAmbiguousApp, got %v", err)
	}
	want := `app identifier "Notepad" is ambiguous; use the identifier shown by winuse list-apps; matches: Notepad, Notepad`
	if re.Error() != want {
		t.Fatalf("error text:\n got %q\nwant %q", re.Error(), want)
	}
}

func TestResolveNameMatching(t *testing.T) {
	cands := []Candidate{
		{Name: "Microsoft Edge", ID: `C:\Program Files\Edge\msedge.exe`, Kind: IDExe},
		{Name: "Windows Terminal", ID: "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
	}
	cases := []struct {
		name  string
		app   string
		want  Spec
		alias string
	}{
		{
			name: "display name, case-insensitive",
			app:  "microsoft edge",
			want: Spec{App: "microsoft edge", ID: `C:\Program Files\Edge\msedge.exe`, Kind: IDExe, TargetExe: `C:\Program Files\Edge\msedge.exe`},
		},
		{
			name: "exe base name without extension",
			app:  "msedge",
			want: Spec{App: "msedge", ID: `C:\Program Files\Edge\msedge.exe`, Kind: IDExe, TargetExe: `C:\Program Files\Edge\msedge.exe`},
		},
		{
			name: "exe base name with extension",
			app:  "msedge.exe",
			want: Spec{App: "msedge.exe", ID: `C:\Program Files\Edge\msedge.exe`, Kind: IDExe, TargetExe: `C:\Program Files\Edge\msedge.exe`},
		},
		{
			name: "store display name",
			app:  "windows terminal",
			want: Spec{App: "windows terminal", ID: "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
		},
		{
			name: "shortcut file name",
			app:  "edge",
			want: Spec{App: "edge", ID: `C:\Users\u\Start Menu\Programs\Edge.lnk`, Kind: IDShortcut, TargetExe: `C:\Program Files\Edge\msedge.exe`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			all := append([]Candidate{}, cands...)
			if tc.name == "shortcut file name" {
				// A shortcut whose file name is "Edge" with an Edge target:
				// its file name is a matchable alias.
				all = append(all, Candidate{Name: "Microsoft Edge", ID: `C:\Users\u\Start Menu\Programs\Edge.lnk`, Kind: IDShortcut, TargetExe: `C:\Program Files\Edge\msedge.exe`})
			}
			spec, err := Resolve(tc.app, all)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.app, err)
			}
			if spec.ID != tc.want.ID || spec.Kind != tc.want.Kind || spec.TargetExe != tc.want.TargetExe {
				t.Fatalf("Resolve(%q):\n got %+v\nwant %+v", tc.app, spec, tc.want)
			}
		})
	}
}

func TestResolveFoldsOneAppSeenBySeveralSources(t *testing.T) {
	// "Notepad": the running App, its Start-menu shortcut and its UserAssist
	// entry — all one App, so the name resolves, not ambiguity.
	cands := []Candidate{
		{Name: "Notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe},
		{Name: "Notepad", ID: `C:\Users\u\Start Menu\Programs\Notepad.lnk`, Kind: IDShortcut, TargetExe: `C:\Windows\System32\notepad.exe`},
		{Name: "notepad", ID: `C:\Windows\System32\notepad.exe`, Kind: IDExe},
	}
	spec, err := Resolve("Notepad", cands)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The shortcut is the preferred representative.
	if spec.Kind != IDShortcut || spec.ID != `C:\Users\u\Start Menu\Programs\Notepad.lnk` {
		t.Fatalf("spec: %+v", spec)
	}
	if spec.TargetExe != `C:\Windows\System32\notepad.exe` {
		t.Fatalf("shortcut spec must carry the target exe: %+v", spec)
	}
}

func TestResolveFoldsStoreAndRunningByName(t *testing.T) {
	// A packaged App running (its process exe) plus the Get-StartApps entry:
	// same display name, one App. The AUMID wins and adopts the running exe.
	cands := []Candidate{
		{Name: "Calculator", ID: `C:\Program Files\WindowsApps\Microsoft.WindowsCalculator_8wekyb3d8bbwe\CalculatorApp.exe`, Kind: IDExe},
		{Name: "Calculator", ID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", Kind: IDAppUserModelID},
	}
	spec, err := Resolve("Calculator", cands)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if spec.Kind != IDAppUserModelID || spec.ID != "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App" {
		t.Fatalf("spec must launch the AUMID: %+v", spec)
	}
	if spec.TargetExe != strings.ToLower(`C:\Program Files\WindowsApps\Microsoft.WindowsCalculator_8wekyb3d8bbwe\CalculatorApp.exe`) {
		t.Fatalf("AUMID spec must recognize the running exe's Windows: %+v", spec)
	}
}

func TestResolveDoesNotFoldDifferentExesWithSameName(t *testing.T) {
	cands := []Candidate{
		{Name: "Calc", ID: `C:\a\calc.exe`, Kind: IDExe},
		{Name: "Calc", ID: `C:\b\calc.exe`, Kind: IDExe},
	}
	_, err := Resolve("Calc", cands)
	var re *ResolveError
	if !errors.As(err, &re) || re.Kind != ErrAmbiguousApp {
		t.Fatalf("two different exes with one name must stay ambiguous, got %v", err)
	}
}

func TestLooksLikePathAndIdentifier(t *testing.T) {
	paths := []string{`C:\x.exe`, `relative\path.exe`, `foo.lnk`, `foo.LNK`, `D:`, `\rooted.exe`}
	for _, p := range paths {
		if !LooksLikePath(p) {
			t.Errorf("LooksLikePath(%q) = false, want true", p)
		}
	}
	names := []string{"Notepad", "notepad.exe", "Microsoft Edge", "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App"}
	for _, n := range names {
		if LooksLikePath(n) {
			t.Errorf("LooksLikePath(%q) = true, want false", n)
		}
	}
	if !LooksLikeIdentifier("Microsoft.WindowsCalculator_8wekyb3d8bbwe!App") {
		t.Error("an AUMID is an explicit identifier")
	}
	if LooksLikeIdentifier("Notepad") {
		t.Error("a plain name is not an explicit identifier")
	}
}

// ------------------------------------------------------- window matching

func TestMatchNewWindow(t *testing.T) {
	before := []win.WindowInfo{winInfo(1, "Old", "old", `C:\old\old.exe`, 1)}
	newWindow := winInfo(2, "App", "new window", `C:\apps\app.exe`, 9)
	spec := Spec{App: "App", ID: `C:\apps\app.exe`, Kind: IDExe, TargetExe: `C:\apps\app.exe`}

	t.Run("new window of the target exe", func(t *testing.T) {
		w, ok := MatchNewWindow(before, append(before, newWindow), 0, 0, spec, 0)
		if !ok || w.ID != 2 {
			t.Fatalf("got %+v, %v", w, ok)
		}
	})

	t.Run("case-insensitive exe match", func(t *testing.T) {
		w := winInfo(2, "App", "new", `C:\APPS\APP.EXE`, 9)
		if got, ok := MatchNewWindow(before, append(before, w), 0, 0, spec, 0); !ok || got.ID != 2 {
			t.Fatalf("got %+v, %v", got, ok)
		}
	})

	t.Run("pid match when the exe path is unknown", func(t *testing.T) {
		w := winInfo(2, "App", "new", "", 9) // process path unresolvable
		if got, ok := MatchNewWindow(before, append(before, w), 0, 0, spec, 9); !ok || got.ID != 2 {
			t.Fatalf("got %+v, %v", got, ok)
		}
		if _, ok := MatchNewWindow(before, append(before, w), 0, 0, spec, 8); ok {
			t.Fatal("a different pid must not match")
		}
	})

	t.Run("unrelated new window does not match", func(t *testing.T) {
		w := winInfo(2, "Other", "other", `C:\other\other.exe`, 9)
		if _, ok := MatchNewWindow(before, append(before, w), 0, 0, spec, 0); ok {
			t.Fatal("an unrelated new window must not match a known target")
		}
	})

	t.Run("single-instance App reusing a Window in the foreground", func(t *testing.T) {
		existing := winInfo(5, "App", "existing", `C:\apps\app.exe`, 3)
		withApp := append(append([]win.WindowInfo{}, before...), existing)
		after := []win.WindowInfo{existing, before[0]}
		w, ok := MatchNewWindow(withApp, after, 5, 1, spec, 0)
		if !ok || w.ID != 5 {
			t.Fatalf("got %+v, %v", w, ok)
		}
		// Same foreground as before the launch: nothing to report.
		if _, ok := MatchNewWindow(withApp, after, 5, 5, spec, 0); ok {
			t.Fatal("an unchanged foreground must not match")
		}
	})

	t.Run("unknown target: any new window", func(t *testing.T) {
		aumidSpec := Spec{App: "x", ID: "a!b", Kind: IDAppUserModelID}
		w, ok := MatchNewWindow(before, append(before, newWindow), 0, 0, aumidSpec, 0)
		if !ok || w.ID != 2 {
			t.Fatalf("got %+v, %v", w, ok)
		}
	})

	t.Run("nothing new", func(t *testing.T) {
		if _, ok := MatchNewWindow(before, before, 0, 0, spec, 0); ok {
			t.Fatal("nothing changed: no match")
		}
	})
}

func TestWaitForWindowFindsWindowAfterPolls(t *testing.T) {
	spec := Spec{App: "app", ID: `C:\apps\app.exe`, Kind: IDExe, TargetExe: `C:\apps\app.exe`}
	before := []win.WindowInfo{winInfo(1, "Old", "old", `C:\old\old.exe`, 1)}
	target := winInfo(2, "App", "new", `C:\apps\app.exe`, 9)

	calls := 0
	enum := func() ([]win.WindowInfo, error) {
		calls++
		if calls < 3 {
			return before, nil // not open yet
		}
		return append(before, target), nil
	}
	w, err := WaitForWindow(context.Background(), spec, 0, before, 0, 5*time.Second, enum, func() uint64 { return 0 }, func(time.Duration) {}, nil)
	if err != nil {
		t.Fatalf("WaitForWindow: %v", err)
	}
	if w.ID != 2 {
		t.Fatalf("window: %+v", w)
	}
	if calls != 3 {
		t.Fatalf("enum called %d times, want 3", calls)
	}
}

func TestWaitForWindowTimesOut(t *testing.T) {
	spec := Spec{App: "windowless app", ID: `C:\x.exe`, Kind: IDExe, TargetExe: `C:\x.exe`}
	before := []win.WindowInfo{winInfo(1, "Old", "old", `C:\old\old.exe`, 1)}
	_, err := WaitForWindow(context.Background(), spec, 0, before, 0, 30*time.Millisecond,
		func() ([]win.WindowInfo, error) { return before, nil }, func() uint64 { return 0 }, func(time.Duration) {}, nil)
	var noWindow *NoWindowError
	if !errors.As(err, &noWindow) {
		t.Fatalf("want NoWindowError, got %v", err)
	}
	if noWindow.Error() != "launched app did not expose a targetable window: windowless app" {
		t.Fatalf("error text: %q", noWindow.Error())
	}
}

func TestWaitForWindowContextCancelled(t *testing.T) {
	spec := Spec{App: "x", ID: `C:\x.exe`, Kind: IDExe, TargetExe: `C:\x.exe`}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WaitForWindow(ctx, spec, 0, nil, 0, time.Minute,
		func() ([]win.WindowInfo, error) { return nil, nil }, func() uint64 { return 0 }, func(time.Duration) {}, nil)
	var noWindow *NoWindowError
	if !errors.As(err, &noWindow) {
		t.Fatalf("want NoWindowError on cancellation, got %v", err)
	}
}

func TestWaitForWindowStoppedByEsc(t *testing.T) {
	// Ticket 12: the user pressing Esc while winuse waits for a launched
	// App's Window aborts the wait with ErrStopped (the caller reports the
	// exit-130 stop), even when the Window would have appeared on the next
	// poll — and before the timeout's NoWindow error.
	spec := Spec{App: "slow app", ID: `C:\slow.exe`, Kind: IDExe, TargetExe: `C:\slow.exe`}
	before := []win.WindowInfo{winInfo(1, "Old", "old", `C:\old\old.exe`, 1)}
	target := winInfo(2, "Slow", "slow", `C:\slow.exe`, 2)
	calls := 0
	enum := func() ([]win.WindowInfo, error) {
		calls++
		if calls >= 3 {
			return append(before, target), nil // would match on the 3rd poll
		}
		return before, nil
	}
	_, err := WaitForWindow(context.Background(), spec, 0, before, 0, 5*time.Second,
		enum, func() uint64 { return 0 }, func(time.Duration) {}, func() bool { return true })
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("want ErrStopped, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("a stopped wait must not poll; enum called %d times", calls)
	}
}

// A stop that latches mid-wait aborts on the next poll boundary, before
// the App's matching Window is acted on.
func TestWaitForWindowStopsMidWait(t *testing.T) {
	spec := Spec{App: "slow app", ID: `C:\slow.exe`, Kind: IDExe, TargetExe: `C:\slow.exe`}
	before := []win.WindowInfo{winInfo(1, "Old", "old", `C:\old\old.exe`, 1)}
	target := winInfo(2, "Slow", "slow", `C:\slow.exe`, 2)
	calls, stopped := 0, false
	enum := func() ([]win.WindowInfo, error) {
		calls++
		if calls <= 2 {
			if calls == 2 {
				stopped = true // the user presses Esc after the first poll
			}
			return before, nil // the App has not opened its Window yet
		}
		return append(before, target), nil // would match from the 3rd poll on
	}
	_, err := WaitForWindow(context.Background(), spec, 0, before, 0, 5*time.Second,
		enum, func() uint64 { return 0 }, func(time.Duration) {}, func() bool { return stopped })
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("want ErrStopped, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("enum called %d times, want 2 (stop noticed at the next poll)", calls)
	}
}

// ------------------------------------------------------------ RecentApps

func TestRecentAppsClassifiesRecords(t *testing.T) {
	last := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	recs := []RecentRecord{
		{Path: `C:\Portable\tool.exe`, UseCount: 3, LastUsed: last},
		{Path: `C:\Users\u\Start Menu\Programs\App.lnk`, UseCount: 5, LastUsed: last},
		{AUMID: "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", UseCount: 1, LastUsed: last},
	}
	got := RecentApps(recs,
		func(exe string) string { return "Portable Tool" },
		func(lnk string) string { return `C:\Apps\App\app.exe` })
	if len(got) != 3 {
		t.Fatalf("got %d apps: %+v", len(got), got)
	}
	if got[0].Name != "Portable Tool" || got[0].Kind != IDExe || got[0].ID != `C:\Portable\tool.exe` || got[0].UseCount != 3 {
		t.Fatalf("exe record: %+v", got[0])
	}
	if got[1].Name != "App" || got[1].Kind != IDShortcut || got[1].TargetExe != `C:\Apps\App\app.exe` {
		t.Fatalf("shortcut record: %+v", got[1])
	}
	if got[2].Kind != IDAppUserModelID || got[2].Name != "" {
		t.Fatalf("AUMID record: %+v", got[2])
	}
}
