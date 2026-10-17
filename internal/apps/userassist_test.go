package apps

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func TestRot13(t *testing.T) {
	// Hand-verified pairs pin the encoding; letters rotate (case
	// preserved), everything else is unchanged.
	for _, tc := range []struct{ in, want string }{
		{"Puebzr", "Chrome"},
		{"pnfg", "cast"},
		{"PNFG", "CAST"},
		{"Jvaqbjf", "Windows"},
		{"Flfgrz32", "System32"},
		{`P:\Jvaqbjf\Flfgrz32\Abgrcnq.rkr`, `C:\Windows\System32\Notepad.exe`},
		{"123-_.!()\\", "123-_.!()\\"}, // no letters: unchanged
		{"", ""},
	} {
		if got := Rot13(tc.in); got != tc.want {
			t.Errorf("Rot13(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := Rot13(Rot13(tc.in)); got != tc.in {
			t.Errorf("Rot13 twice must restore %q, got %q", tc.in, got)
		}
	}
}

// countData builds a 72-byte UserAssist Count value (run count at 4, last
// run FILETIME at 60).
func countData(runCount uint32, lastRun uint64) []byte {
	data := make([]byte, 72)
	binary.LittleEndian.PutUint32(data[4:8], runCount)
	binary.LittleEndian.PutUint64(data[60:68], lastRun)
	return data
}

// filetime builds a Win32 FILETIME for a point in time.
func filetime(t time.Time) uint64 {
	const windowsToUnixEpoch = 116444736000000000
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixNano()/100 + windowsToUnixEpoch)
}

func TestParseUserAssistValue(t *testing.T) {
	last := time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)
	data := countData(7, filetime(last))

	t.Run("exe path", func(t *testing.T) {
		rec, ok := parseUserAssistValue(Rot13(`C:\Windows\System32\Notepad.exe`), data)
		if !ok {
			t.Fatal("want a record")
		}
		if rec.Path != `C:\Windows\System32\Notepad.exe` || rec.AUMID != "" {
			t.Fatalf("record: %+v", rec)
		}
		if rec.UseCount != 7 {
			t.Fatalf("use count: %d", rec.UseCount)
		}
		if !rec.LastUsed.Equal(last) {
			t.Fatalf("last used: %v want %v", rec.LastUsed, last)
		}
	})

	t.Run("shortcut path", func(t *testing.T) {
		rec, ok := parseUserAssistValue(Rot13(`C:\Users\user\Start Menu\Programs\App.lnk`), data)
		if !ok {
			t.Fatal("want a record")
		}
		if rec.Path != `C:\Users\user\Start Menu\Programs\App.lnk` {
			t.Fatalf("record: %+v", rec)
		}
	})

	t.Run("AUMID", func(t *testing.T) {
		rec, ok := parseUserAssistValue(Rot13("Microsoft.WindowsCalculator_8wekyb3d8bbwe!App"), data)
		if !ok {
			t.Fatal("want a record")
		}
		if rec.AUMID != "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App" || rec.Path != "" {
			t.Fatalf("record: %+v", rec)
		}
	})

	t.Run("internal bookkeeping names are skipped", func(t *testing.T) {
		for _, name := range []string{"HRZR_PUHYNQ", "UEME_RPATH", "some value"} {
			if _, ok := parseUserAssistValue(name, data); ok {
				t.Errorf("%q must be skipped", name)
			}
		}
	})

	t.Run("uninstallers are skipped", func(t *testing.T) {
		if _, ok := parseUserAssistValue(Rot13(`C:\Program Files\App\Uninstall.exe`), data); ok {
			t.Fatal("an uninstaller entry must be skipped")
		}
	})

	t.Run("too-short data is skipped", func(t *testing.T) {
		if _, ok := parseUserAssistValue(Rot13(`C:\Notepad.exe`), []byte{0, 1, 2}); ok {
			t.Fatal("short data must be skipped")
		}
	})
}

func TestFiletimeToTime(t *testing.T) {
	if !filetimeToTime(0).IsZero() {
		t.Fatal("zero FILETIME must be the zero time")
	}
	want := time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)
	got := filetimeToTime(filetime(want))
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !filetimeToTime(1).IsZero() {
		t.Fatal("pre-1970 FILETIME must be the zero time")
	}
}

func TestParseCountData(t *testing.T) {
	useCount, lastUsed, ok := parseCountData(countData(3, filetime(time.Now())))
	if !ok || useCount != 3 || lastUsed.IsZero() {
		t.Fatalf("got %d, %v, %v", useCount, lastUsed, ok)
	}
	if _, _, ok := parseCountData(make([]byte, 67)); ok {
		t.Fatal("short data must not parse")
	}
}

func TestMapKnownFolderPrefix(t *testing.T) {
	resolve := func(guid string) string {
		if strings.EqualFold(guid, "6d809377-6af0-444b-8957-a3773f02200e") {
			return `C:\Program Files`
		}
		return "" // unresolvable
	}
	cases := []struct{ in, want string }{
		{"{6d809377-6af0-444b-8957-a3773f02200e}\\App\\app.exe", `C:\Program Files\App\app.exe`},
		{"{6D809377-6AF0-444B-8957-A3773F02200E}\\App\\app.exe", `C:\Program Files\App\app.exe`},          // case-insensitive GUID
		{"{00000000-0000-0000-0000-000000000000}\\x.exe", `{00000000-0000-0000-0000-000000000000}\x.exe`}, // unresolvable: unchanged
		{`C:\Plain\path.exe`, `C:\Plain\path.exe`},                                                        // no prefix
		{"{not a guid}\\x.exe", "{not a guid}\\x.exe"},                                                    // not a GUID: unchanged
		{"{6d809377-6af0-444b-8957-a3773f02200e}", `C:\Program Files`},                                    // GUID only: the folder itself
		{"{6d809377-6af0-444b-8957-a3773f02200e}app.exe", `C:\Program Files\app.exe`},                     // missing separator: repaired
	}
	for _, tc := range cases {
		if got := MapKnownFolderPrefix(tc.in, resolve); got != tc.want {
			t.Errorf("MapKnownFolderPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseGUID(t *testing.T) {
	g, ok := ParseGUID("{6d809377-6af0-444b-8957-a3773f02200e}")
	if !ok {
		t.Fatal("want a GUID")
	}
	if g.Data1 != 0x6d809377 || g.Data2 != 0x6af0 || g.Data3 != 0x444b {
		t.Fatalf("fields: %+v", g)
	}
	want4 := []byte{0x89, 0x57, 0xa3, 0x77, 0x3f, 0x02, 0x20, 0x0e}
	for i, v := range want4 {
		if g.Data4[i] != v {
			t.Fatalf("Data4: %x", g.Data4)
		}
	}
	if _, ok := ParseGUID("not-a-guid"); ok {
		t.Error("junk must not parse")
	}
	if _, ok := ParseGUID("{6d809377-6af0-444b-8957-a3773f02200}"); ok {
		t.Error("wrong length must not parse")
	}
	if _, ok := ParseGUID("6d809377-6af0-444b-8957-a3773f02200e"); !ok {
		t.Error("bare GUID (no braces) must parse")
	}
}

func TestClassifyDecoded(t *testing.T) {
	cases := []struct {
		in          string
		path, aumid string
	}{
		{`C:\x.exe`, `C:\x.exe`, ""},
		{`\\server\share\app.exe`, `\\server\share\app.exe`, ""},
		{`{6d809377-6af0-444b-8957-a3773f02200e}\x.exe`, `{6d809377-6af0-444b-8957-a3773f02200e}\x.exe`, ""},
		{`C:\Start Menu\App.lnk`, `C:\Start Menu\App.lnk`, ""},
		{"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", "", "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App"},
		{"UEME_RPATH", "", ""},
		{"random text", "", ""},
	}
	for _, tc := range cases {
		path, aumid := classifyDecoded(tc.in)
		if path != tc.path || aumid != tc.aumid {
			t.Errorf("classifyDecoded(%q) = %q, %q; want %q, %q", tc.in, path, aumid, tc.path, tc.aumid)
		}
	}
}
