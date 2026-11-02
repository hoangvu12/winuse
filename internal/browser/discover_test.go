package browser

// Discovery and endpoint verification over the fake machine: the
// DevToolsActivePort reader, the loopback-only WebSocket rule, the port
// owner check, the pin, and the refusal to guess between two browsers.

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivePort(t *testing.T) {
	dir := t.TempDir()
	if p, path := activePort(dir); p != 0 || path != "" {
		t.Errorf("missing file = %d %q, want nothing", p, path)
	}
	writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "9222\r\n/devtools/browser/abc\r\n")
	if p, path := activePort(dir); p != 9222 || path != "/devtools/browser/abc" {
		t.Errorf("CRLF file = %d %q", p, path)
	}
	writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "9333")
	if p, path := activePort(dir); p != 9333 || path != "" {
		t.Errorf("port-only file = %d %q", p, path)
	}
	writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "not a port\n/devtools/browser/x")
	if p, _ := activePort(dir); p != 0 {
		t.Errorf("garbage file = %d, want 0", p)
	}
}

func TestCheckLoopbackWS(t *testing.T) {
	cases := []struct {
		url  string
		port int
		ok   bool
	}{
		{"ws://127.0.0.1:9222/devtools/browser/x", 9222, true},
		{"ws://localhost:9222/devtools/browser/x", 9222, true},
		{"ws://[::1]:9222/devtools/browser/x", 9222, true},
		{"ws://127.0.0.2:9222/devtools/browser/x", 9222, true}, // all of 127/8 is loopback
		{"ws://192.168.1.5:9222/devtools/browser/x", 9222, false},
		{"ws://0.0.0.0:9222/devtools/browser/x", 9222, false},
		{"ws://example.com:9222/devtools/browser/x", 9222, false},
		{"wss://127.0.0.1:9222/devtools/browser/x", 9222, false},
		{"ws://127.0.0.1:9333/devtools/browser/x", 9222, false}, // not the port whose owner was checked
		{"ws://127.0.0.1:9222/other", 9222, false},
		{"::not a url", 9222, false},
	}
	for _, tc := range cases {
		err := checkLoopbackWS(tc.url, tc.port)
		if (err == nil) != tc.ok {
			t.Errorf("checkLoopbackWS(%q, %d) = %v, want ok=%v", tc.url, tc.port, err, tc.ok)
		}
	}
}

func TestIdentifyImage(t *testing.T) {
	env := Env{LocalAppData: `C:\Users\u\AppData\Local`, AppData: `C:\Users\u\AppData\Roaming`, ProgramFiles: `C:\Program Files`, ProgramFilesX86: `C:\Program Files (x86)`}
	cases := map[string]string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`:                      "chrome",
		`c:\program files\google\chrome\application\chrome.exe`:                      "chrome",
		`C:\Users\u\AppData\Local\Chromium\Application\chrome.exe`:                   "chromium",
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`:               "edge",
		`C:\Users\u\AppData\Local\Programs\Opera\124.0.5705.42\opera.exe`:            "opera",
		`D:\PortableApps\Brave\brave.exe`:                                            "brave",
		`D:\Somewhere\chrome.exe`:                                                    "chrome",
		`C:\Program Files\nodejs\node.exe`:                                           "",
		`C:\Users\u\AppData\Local\BraveSoftware\Brave-Browser\Application\brave.exe`: "brave",
		``: "",
	}
	for path, want := range cases {
		if got := env.IdentifyImage(path); got != want {
			t.Errorf("IdentifyImage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDiscoverFindsWinusesOwnLaunch(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	ep, err := f.sys.discover(context.Background(), "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if ep.Browser != "chrome" || ep.Mode != ModeLaunch || ep.Port != 9301 || ep.PID != 100 {
		t.Errorf("endpoint = %+v", ep)
	}
}

func TestDiscoverTwoDebuggableBrowsersIsAnErrorNamingBoth(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.install("edge")
	f.process("chrome", 100)
	f.process("edge", 200)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	f.toggle("edge", 9222, "/devtools/browser/e")
	f.listen(9222, "", 200) // the inspect toggle: /json/version answers 404
	_, err := f.sys.discover(context.Background(), "")
	p := problemOf(t, err, Ambiguous)
	if strings.Join(p.Others, ",") != "chrome,edge" {
		t.Errorf("Others = %v, want chrome and edge", p.Others)
	}
	if want := "Chrome and Edge both accept a debugging connection and no browser is pinned, so winuse will not guess"; p.Sentence() != want {
		t.Errorf("sentence = %q", p.Sentence())
	}
	if p.Next() != "run winuse browser use chrome to pin one (the pin persists)" {
		t.Errorf("next = %q", p.Next())
	}
}

func TestDiscoverPinnedBrowserWins(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.install("edge")
	f.process("chrome", 100)
	f.process("edge", 200)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	f.toggle("edge", 9222, "/devtools/browser/e")
	f.listen(9222, "", 200)
	ep, err := f.sys.discover(context.Background(), "edge")
	if err != nil {
		t.Fatalf("discover edge: %v", err)
	}
	if ep.Browser != "edge" || ep.Mode != ModeToggle || ep.WSURL != "ws://127.0.0.1:9222/devtools/browser/e" {
		t.Errorf("endpoint = %+v", ep)
	}
	// Through the manager, the stored pin does the same.
	m := NewManager(f.sys)
	if err := f.sys.SetPin("edge"); err != nil {
		t.Fatal(err)
	}
	want, err := m.target("")
	if err != nil || want != "edge" {
		t.Errorf("target with the pin = %q, %v", want, err)
	}
}

func TestDiscoverRejectsAWrongPortOwner(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.install("brave")
	f.process("brave", 300)
	// winuse's Chrome profile names port 9301, but Brave holds it now.
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 300)
	_, err := f.sys.discover(context.Background(), "chrome")
	p := problemOf(t, err, WrongOwner)
	if p.Owner != "brave.exe" || p.Port != 9301 || p.Browser != "chrome" {
		t.Errorf("problem = %+v", p)
	}
}

func TestDiscoverRejectsAnUnknownOrUnreadableOwner(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.listen(9222, "ws://127.0.0.1:9222/devtools/browser/n", 400)
	f.images[400] = `C:\Program Files\nodejs\node.exe`
	_, err := f.sys.discover(context.Background(), "")
	p := problemOf(t, err, WrongOwner)
	if p.Owner != "node.exe" || p.Browser != "" {
		t.Errorf("problem = %+v", p)
	}

	f2 := newFake(t)
	f2.install("chrome")
	f2.listen(9222, "ws://127.0.0.1:9222/devtools/browser/n", 0) // owner unreadable
	_, err = f2.sys.discover(context.Background(), "")
	problemOf(t, err, UnknownOwner)
}

func TestDiscoverRejectsANonLoopbackURL(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://10.0.0.7:9301/devtools/browser/a", 100)
	_, err := f.sys.discover(context.Background(), "chrome")
	p := problemOf(t, err, NotLoopback)
	if p.URL != "ws://10.0.0.7:9301/devtools/browser/a" {
		t.Errorf("problem = %+v", p)
	}
}

func TestDiscoverIgnoresAStaleDevToolsActivePort(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.toggle("chrome", 9222, "/devtools/browser/old") // left behind by a closed Chrome
	_, err := f.sys.discover(context.Background(), "chrome")
	problemOf(t, err, NotRunning)

	f.process("chrome", 100) // running again, toggle off
	_, err = f.sys.discover(context.Background(), "chrome")
	problemOf(t, err, NoDebugging)
}

func TestDiscoverProbesTheCommonPortsAndAttributesTheOwner(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.install("edge")
	f.process("edge", 200)
	f.listen(9223, "ws://127.0.0.1:9223/devtools/browser/e", 200)
	ep, err := f.sys.discover(context.Background(), "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if ep.Browser != "edge" || ep.Mode != ModePort || ep.Port != 9223 {
		t.Errorf("endpoint = %+v", ep)
	}
	// Pinned to Chrome, Edge's port is not Chrome's.
	_, err = f.sys.discover(context.Background(), "chrome")
	p := problemOf(t, err, WrongOwner)
	if p.Owner != "msedge.exe" {
		t.Errorf("problem = %+v", p)
	}
}

func TestDiscoverExtensionComesFirst(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	var asked string
	f.sys.Extension = func(ctx context.Context, pin string) (*Endpoint, error) {
		asked = pin
		return &Endpoint{Browser: "chrome", Mode: ModeExtension, WSURL: "extension bridge"}, nil
	}
	ep, err := f.sys.discover(context.Background(), "chrome")
	if err != nil || ep.Mode != ModeExtension || asked != "chrome" {
		t.Errorf("discover = %+v, %v (asked %q), want the extension first", ep, err, asked)
	}
}

func TestNothingFound(t *testing.T) {
	f := newFake(t)
	_, err := f.sys.discover(context.Background(), "")
	problemOf(t, err, NoBrowser)

	f.install("edge")
	f.install("chrome")
	_, err = f.sys.discover(context.Background(), "")
	if p := problemOf(t, err, NotRunning); p.Browser != "chrome" {
		t.Errorf("with nothing running the first installed is named, got %q", p.Browser)
	}
	f.process("edge", 200)
	_, err = f.sys.discover(context.Background(), "")
	if p := problemOf(t, err, NoDebugging); p.Browser != "edge" {
		t.Errorf("the running browser is named, got %q", p.Browser)
	}
}

func TestParseTCPListeners(t *testing.T) {
	// Two IPv4 rows: 127.0.0.1:9222 owned by 1234, 0.0.0.0:445 by 4.
	v4 := make([]byte, 4+2*tcp4RowSize)
	binary.LittleEndian.PutUint32(v4, 2)
	row := v4[4:]
	binary.LittleEndian.PutUint32(row[0:], 2) // LISTEN
	copy(row[4:8], []byte{127, 0, 0, 1})
	row[8], row[9] = 0x24, 0x06 // 9222 in network order
	binary.LittleEndian.PutUint32(row[20:], 1234)
	row = v4[4+tcp4RowSize:]
	row[8], row[9] = 0x01, 0xBD // 445
	binary.LittleEndian.PutUint32(row[20:], 4)
	rows := parseTCP4Listeners(v4)
	if len(rows) != 2 || rows[0].Port != 9222 || rows[0].PID != 1234 || !rows[0].Addr.IsLoopback() || rows[1].Port != 445 || !rows[1].Addr.IsUnspecified() {
		t.Fatalf("v4 rows = %+v", rows)
	}
	if pid, ok := pickListener(rows, 9222); !ok || pid != 1234 {
		t.Errorf("pickListener 9222 = %d, %v", pid, ok)
	}
	if _, ok := pickListener(rows, 9223); ok {
		t.Error("pickListener found a port nobody listens on")
	}

	// One IPv6 row: [::1]:9229 owned by 77.
	v6 := make([]byte, 4+tcp6RowSize)
	binary.LittleEndian.PutUint32(v6, 1)
	row = v6[4:]
	row[15] = 1 // ::1
	row[20], row[21] = 0x24, 0x0D
	binary.LittleEndian.PutUint32(row[52:], 77)
	rows = parseTCP6Listeners(v6)
	if len(rows) != 1 || rows[0].Port != 9229 || rows[0].PID != 77 || !rows[0].Addr.IsLoopback() {
		t.Fatalf("v6 rows = %+v", rows)
	}
	if rows := parseTCP4Listeners(v4[:10]); len(rows) != 0 {
		t.Errorf("a truncated table gave rows %+v", rows)
	}
}
