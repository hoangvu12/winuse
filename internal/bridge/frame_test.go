package bridge

// Offline tests for native messaging framing, chunking, the refusal rule
// and the embedded extension's identity.

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"
)

func TestFrameRoundTripIsLittleEndian(t *testing.T) {
	var buf bytes.Buffer
	msg := []byte(`{"id":1,"method":"Target.getTargets"}`)
	if err := WriteFrame(&buf, msg); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if got := binary.LittleEndian.Uint32(raw[:4]); int(got) != len(msg) {
		t.Fatalf("header = % x, want little-endian %d", raw[:4], len(msg))
	}
	if raw[0] != byte(len(msg)) || raw[3] != 0 {
		t.Errorf("header = % x is not little-endian", raw[:4])
	}
	got, err := ReadFrame(&buf, MaxFrame)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("ReadFrame = %q, %v", got, err)
	}
	if _, err := ReadFrame(&buf, MaxFrame); err != io.EOF {
		t.Errorf("an empty stream = %v, want io.EOF", err)
	}
}

func TestFrameSurvivesPartialReads(t *testing.T) {
	var buf bytes.Buffer
	msgs := []string{`{"a":1}`, `{"b":"` + strings.Repeat("x", 70000) + `"}`, `{"c":true}`}
	for _, m := range msgs {
		WriteFrame(&buf, []byte(m))
	}
	r := iotest.OneByteReader(&buf)
	for _, want := range msgs {
		got, err := ReadFrame(r, MaxFrame)
		if err != nil || string(got) != want {
			t.Fatalf("one byte at a time: %d bytes, %v", len(got), err)
		}
	}
}

func TestFrameRejectsOversizeAndGarbage(t *testing.T) {
	frame := func(n uint32, body string) io.Reader {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, n)
		return bytes.NewReader(append(b, body...))
	}
	cases := map[string]io.Reader{
		"over the limit": frame(2<<20, "{}"),
		"zero length":    frame(0, ""),
		// A BOM and a brace where the length belongs read as about 2 GB.
		"BOM header":    bytes.NewReader([]byte("\xEF\xBB\xBF{\"hello\":{}}")),
		"text header":   bytes.NewReader([]byte(`{"hello":{"browser":"chrome"}}`)),
		"BOM body":      frame(5, "\xEF\xBB\xBF{}"),
		"not JSON":      frame(5, "hello"),
		"JSON array":    frame(2, "[]"),
		"broken JSON":   frame(6, `{"a":}`),
		"JSON string":   frame(4, `"ab"`),
		"trailing junk": frame(4, `{}{}`),
	}
	for name, r := range cases {
		if _, err := ReadFrame(r, 1<<20); !errors.Is(err, ErrBadFrame) {
			t.Errorf("%s: err = %v, want ErrBadFrame", name, err)
		}
	}
	if _, err := ReadFrame(frame(10, `{"a":1`), 1<<20); err != io.ErrUnexpectedEOF {
		t.Errorf("a cut body = %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{3, 0}), 1<<20); err != io.ErrUnexpectedEOF {
		t.Errorf("a cut header = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestSplitChunksKeepsRunesWhole(t *testing.T) {
	text := []byte(strings.Repeat("aé€😀", 1000)) // 1, 2, 3 and 4 byte runes
	for _, size := range []int{4, 5, 7, 64, 1000} {
		parts := splitChunks(text, size)
		var joined []byte
		for _, p := range parts {
			if len(p) > size || !utf8.Valid(p) {
				t.Fatalf("size %d: a part of %d bytes, valid=%v", size, len(p), utf8.Valid(p))
			}
			joined = append(joined, p...)
		}
		if !bytes.Equal(joined, text) {
			t.Fatalf("size %d: parts do not join back", size)
		}
	}
}

func TestChunkEnvelopeStaysUnderTheBrowserLimit(t *testing.T) {
	// The worst case for escaping: every byte a quote or a backslash.
	part := bytes.Repeat([]byte(`"\`), ChunkSize/2)
	env, err := chunkEnvelope(3, part, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) > MaxToExtension {
		t.Fatalf("envelope = %d bytes, over %d", len(env), MaxToExtension)
	}
	var back struct {
		C     int    `json:"c"`
		Chunk string `json:"chunk"`
		Last  bool   `json:"last"`
	}
	if err := json.Unmarshal(env, &back); err != nil || back.C != 3 || !back.Last || back.Chunk != string(part) {
		t.Fatalf("envelope does not decode back: %v", err)
	}
	if strings.Contains(string(env), `\u003c`) {
		t.Error("HTML escaping inflates chunks")
	}
}

func TestRefusal(t *testing.T) {
	browserLevel := "needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone."
	front := "would bring the browser window to the front, and winuse never takes the foreground."
	cases := []struct {
		method, session, want string
	}{
		{"Target.getTargets", "", ""},
		{"Target.attachToTarget", "", ""},
		{"Target.detachFromTarget", "", ""},
		{"Target.createTarget", "", ""},
		{"Target.closeTarget", "", ""},
		{"Winuse.status", "", ""},
		{"Winuse.activateTab", "", ""},
		{"Runtime.evaluate", "T1", ""},
		{"Target.setAutoAttach", "T1", ""},
		{"Input.dispatchMouseEvent", "CHILD-SESSION", ""},
		{"Browser.close", "", "Browser.close " + browserLevel},
		{"Browser.getVersion", "", "Browser.getVersion " + browserLevel},
		{"Browser.setDownloadBehavior", "T1", "Browser.setDownloadBehavior " + browserLevel},
		{"SystemInfo.getInfo", "", "SystemInfo.getInfo " + browserLevel},
		{"Tethering.bind", "", "Tethering.bind " + browserLevel},
		{"Target.setDiscoverTargets", "", "Target.setDiscoverTargets " + browserLevel},
		{"Runtime.evaluate", "", "Runtime.evaluate " + browserLevel},
		{"Winuse.status", "T1", "Winuse.status " + browserLevel},
		{"Page.bringToFront", "T1", "Page.bringToFront " + front},
		{"Target.activateTarget", "", "Target.activateTarget " + front},
	}
	for _, tc := range cases {
		code, got := Refusal(tc.method, tc.session)
		if got != tc.want {
			t.Errorf("Refusal(%s, %q) = %q\n want %q", tc.method, tc.session, got, tc.want)
		}
		wantCode := 0
		switch {
		case strings.HasSuffix(tc.want, browserLevel):
			wantCode = CodeRefused
		case strings.HasSuffix(tc.want, front):
			wantCode = CodeForeground
		}
		if code != wantCode {
			t.Errorf("Refusal(%s, %q) code = %d, want %d", tc.method, tc.session, code, wantCode)
		}
		if strings.ContainsAny(got, "\u2013\u2014\u2018\u2019\u201c\u201d") {
			t.Errorf("%s: a dash or curly quote", tc.method)
		}
	}
}

func TestIsHostLaunch(t *testing.T) {
	if !IsHostLaunch([]string{Origin, "--parent-window=0"}) {
		t.Error("the browser's host launch was not recognized")
	}
	for _, args := range [][]string{nil, {"status"}, {"__helper"}, {"browser", "chrome-extension://x/"}} {
		if IsHostLaunch(args) {
			t.Errorf("%q taken for a host launch", args)
		}
	}
}

// TestExtensionIdentity checks that the manifest's key gives ExtensionID
// (Chromium: the first 16 bytes of SHA-256 of the key, as a-p digits),
// that it is not use-browser's, and that background.js uses winuse's host
// name, tab group and error codes.
func TestExtensionIdentity(t *testing.T) {
	mb, err := extensionFiles.ReadFile("extension/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name    string   `json:"name"`
		Version string   `json:"version"`
		Key     string   `json:"key"`
		Perms   []string `json:"permissions"`
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	var id []byte
	for _, c := range hex.EncodeToString(sum[:16]) {
		if c >= 'a' {
			id = append(id, byte('a'+10+(c-'a')))
		} else {
			id = append(id, byte('a'+(c-'0')))
		}
	}
	if string(id) != ExtensionID {
		t.Errorf("the manifest key gives %s, ExtensionID is %s", id, ExtensionID)
	}
	if ExtensionID == "blcbodpjbiimmoenmdneeacdogpglgne" {
		t.Error("winuse must not reuse use-browser's extension id")
	}
	if m.Name != "winuse" || m.Version != Version() || Version() == "" {
		t.Errorf("manifest name %q version %q", m.Name, m.Version)
	}
	js, err := extensionFiles.ReadFile("extension/background.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, want := range []string{
		`const HOST = "` + HostName + `";`,
		`const GROUP_TITLE = "winuse";`,
		`const IDLE_DETACH_MS = 5 * 60 * 1000;`,
		"const CODE_FAILED = -32000;",
		"const CODE_REFUSED = -32010;",
		"const CODE_PAUSED = -32011;",
		"const CODE_FOREGROUND = -32013;",
		"active: false",
		`"canceled_by_user"`,
		"chrome.runtime.reload()",
		"{ targetId: parent, sessionId: m.sessionId }",
		// The build comes from the loaded manifest, split as StampedBuild does.
		"chrome.runtime.getManifest().version_name",
		`name.indexOf("` + buildMark + `")`,
		"build: loadedBuild() }",
		"build: loadedBuild(), instance, paused }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("background.js lacks %q", want)
		}
	}
	if strings.Contains(src, "build.json") {
		t.Error("background.js reads build.json, which a rewritten folder changes under the loaded extension")
	}
	// The foreground rule: nothing focuses or raises a window.
	for _, never := range []string{"chrome.windows.update", "focused: true", "focused:true"} {
		if strings.Contains(src, never) {
			t.Errorf("background.js has %q, which raises the browser window", never)
		}
	}
	for _, s := range []string{src, string(mb)} {
		if strings.ContainsAny(s, "\u2013\u2014\r") {
			t.Error("an extension file has an en or em dash, or a CR")
		}
	}
}

func TestWriteExtensionAndHostManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extension")
	// An older winuse left a build.json, which extension 0.1.2 and older
	// read as their own build.
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "build.json"), []byte(`{"build":"`+Build()+`"}`), 0o644)
	if err := WriteExtension(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "background.js"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "build.json")); !os.IsNotExist(err) {
		t.Errorf("the old build.json was kept: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if got := StampedBuild(b); got != Build() || len(Build()) != 12 {
		t.Errorf("stamped build %q, want %q in\n%s", got, Build(), b)
	}
	// The written manifest is the embedded one plus the stamp: the key, and
	// so the extension id, is unchanged.
	var written, embedded map[string]any
	if err := json.Unmarshal(b, &written); err != nil {
		t.Fatal(err)
	}
	eb, _ := extensionFiles.ReadFile("extension/manifest.json")
	json.Unmarshal(eb, &embedded)
	if written["version_name"] != Version()+" build "+Build() {
		t.Errorf("version_name = %v", written["version_name"])
	}
	delete(written, "version_name")
	wb, _ := json.Marshal(written)
	if eb, _ := json.Marshal(embedded); string(wb) != string(eb) || embedded["key"] == nil {
		t.Errorf("the written manifest differs from the embedded one beyond the stamp:\n%s", b)
	}
	if bytes.Contains(b, []byte("\r")) || !bytes.HasSuffix(b, []byte("}\n")) {
		t.Errorf("manifest line endings: %q", b)
	}
	// Today's 0.1.x installs have no stamp, and any other manifest neither.
	if StampedBuild(eb) != "" || StampedBuild([]byte(`{"version_name":"0.1.0"}`)) != "" || StampedBuild([]byte("not json")) != "" {
		t.Error("an unstamped manifest gave a build")
	}
	var hm struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Type    string   `json:"type"`
		Origins []string `json:"allowed_origins"`
	}
	if err := json.Unmarshal(HostManifest(`C:\w\winuse.exe`), &hm); err != nil {
		t.Fatal(err)
	}
	if hm.Name != HostName || hm.Path != `C:\w\winuse.exe` || hm.Type != "stdio" || len(hm.Origins) != 1 || hm.Origins[0] != "chrome-extension://"+ExtensionID+"/" {
		t.Errorf("host manifest = %+v", hm)
	}
}

func TestListDropsStaleBridgeFiles(t *testing.T) {
	dir := t.TempDir()
	live := Info{PID: 1, Port: 5001, Token: "t1", Browser: "chrome", Instance: "aaaa1111"}
	stale := Info{PID: 2, Port: 5002, Token: "t2", Browser: "edge", Instance: "bbbb2222"}
	for _, i := range []Info{live, stale} {
		if _, err := writeInfo(dir, i); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "junk.json"), []byte("not json"), 0o644)
	os.WriteFile(filepath.Join(dir, "host.log"), []byte("a log"), 0o644)
	got := List(dir, func(i Info) bool { return i.PID == 1 })
	if len(got) != 1 || got[0].Instance != "aaaa1111" || got[0].Path == "" {
		t.Fatalf("List = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, stale.FileName())); !os.IsNotExist(err) {
		t.Error("the stale bridge file was kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.json")); err != nil {
		t.Error("a file that is not a bridge file was removed")
	}
}

func TestValidHello(t *testing.T) {
	for _, s := range []string{"ab12cd34", "0f9e-77"} {
		if !validInstance(s) {
			t.Errorf("instance %q refused", s)
		}
	}
	for _, s := range []string{"", "../x", `a\b`, "AB12", strings.Repeat("a", 40)} {
		if validInstance(s) {
			t.Errorf("instance %q accepted", s)
		}
	}
	if !validBrowser("chrome") || validBrowser("chrome.exe") || validBrowser("") {
		t.Error("validBrowser")
	}
}
