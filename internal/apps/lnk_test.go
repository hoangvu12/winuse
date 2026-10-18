package apps

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// linkCLSID is the ShellLink CLSID {00021401-0000-0000-C000-000000000046}.
var linkCLSID = []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}

// buildLink assembles a minimal MS-SHLLINK file: header, optional target
// IDList, then LinkInfo with the given local base paths (ANSI and/or
// Unicode) and common path suffix.
func buildLink(t *testing.T, withIDList bool, ansiPath, unicodePath, ansiSuffix string) []byte {
	t.Helper()
	const (
		flagHasLinkTargetIDList = 0x00000001
		flagHasLinkInfo         = 0x00000002
		flagLinkInfoUnicode     = 0x00000001
	)
	flags := uint32(flagHasLinkInfo)
	if withIDList {
		flags |= flagHasLinkTargetIDList
	}

	header := make([]byte, 76)
	binary.LittleEndian.PutUint32(header[0:4], 76)
	copy(header[4:20], linkCLSID)
	binary.LittleEndian.PutUint32(header[20:24], flags)

	var body []byte
	if withIDList {
		idList := []byte{0x12, 0x34, 0x56, 0x78} // opaque IDList bytes
		body = binary.LittleEndian.AppendUint16(body, uint16(len(idList)))
		body = append(body, idList...)
	}

	// LinkInfo: fixed 24-byte header (+8 when the Unicode flag is set),
	// then the path strings.
	unicode := unicodePath != ""
	infoFlags := uint32(0)
	if unicode {
		infoFlags |= flagLinkInfoUnicode
	}
	localBase := uint32(0)
	if ansiPath != "" {
		localBase = 24 + 8 // with the Unicode fields present
		if !unicode {
			localBase = 24
		}
	}
	suffix := uint32(0)
	if ansiSuffix != "" {
		suffixAt := localBase
		if ansiPath != "" {
			suffixAt += uint32(len(ansiPath) + 1)
		}
		suffix = suffixAt
	}
	infoHeader := make([]byte, 24)
	binary.LittleEndian.PutUint32(infoHeader[4:8], infoFlags)
	binary.LittleEndian.PutUint32(infoHeader[12:16], localBase)
	binary.LittleEndian.PutUint32(infoHeader[20:24], suffix)

	if unicode {
		infoHeader = binary.LittleEndian.AppendUint32(infoHeader, 0) // placeholder for the Unicode offset
	}
	info := append([]byte{}, infoHeader...)
	if ansiPath != "" {
		info = append(info, ansiPath...)
		info = append(info, 0)
	}
	if ansiSuffix != "" {
		info = append(info, ansiSuffix...)
		info = append(info, 0)
	}
	if unicode {
		unicodeOffset := uint32(len(info))
		// write the real Unicode offset back into the header
		binary.LittleEndian.PutUint32(info[24:28], unicodeOffset)
		for _, r := range []rune(unicodePath) {
			info = binary.LittleEndian.AppendUint16(info, uint16(r))
		}
		info = binary.LittleEndian.AppendUint16(info, 0)
	}
	binary.LittleEndian.PutUint32(info[0:4], uint32(len(info)))

	return append(header, append(body, info...)...)
}

func TestLinkTargetUnicode(t *testing.T) {
	data := buildLink(t, true, `C:\Old\ANSI\app.exe`, `C:\Program Files\App\app.exe`, "")
	got, ok := LinkTarget(data)
	if !ok || got != `C:\Program Files\App\app.exe` {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestLinkTargetAnsiWithSuffix(t *testing.T) {
	data := buildLink(t, true, `C:\Base`, "", `sub\app.exe`)
	got, ok := LinkTarget(data)
	if !ok || got != `C:\Base`+`sub\app.exe` {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestLinkTargetNoIDList(t *testing.T) {
	data := buildLink(t, false, `C:\App\app.exe`, "", "")
	got, ok := LinkTarget(data)
	if !ok || got != `C:\App\app.exe` {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestLinkTargetJunk(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short header", make([]byte, 20)},
		{"no LinkInfo flag", buildLink(t, false, "", "", "")[:76]},
		{"no CLSID", func() []byte {
			data := buildLink(t, false, `C:\a.exe`, "", "")
			copy(data[4:20], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
			return data
		}()},
		{"truncated IDList", func() []byte {
			data := buildLink(t, true, `C:\a.exe`, "", "")
			return data[:80] // cut inside the IDList
		}()},
		{"unterminated string", func() []byte {
			data := buildLink(t, false, `C:\a.exe`, "", "")
			return data[:len(data)-1] // drop the trailing NUL
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := LinkTarget(tc.data); ok {
				t.Fatalf("want no target, got %q", got)
			}
		})
	}
}

func TestLnkTargetFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "App.lnk")
	if err := os.WriteFile(path, buildLink(t, true, "", `C:\Program Files\App\app.exe`, ""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LnkTargetFile(path); got != `C:\Program Files\App\app.exe` {
		t.Fatalf("got %q", got)
	}
	if got := LnkTargetFile(filepath.Join(dir, "missing.lnk")); got != "" {
		t.Fatalf("missing file must give no target, got %q", got)
	}
}
