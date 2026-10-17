package apps

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows Explorer's UserAssist key tracks app launches: winuse reads it
// for the "recently used" part of list-apps (last used, use count). It is
// undocumented and best-effort — with app-launch tracking turned off the
// lists are simply empty. Value names are ROT13-encoded; paths may start
// with a KNOWNFOLDERID GUID instead of a drive letter.

const (
	// userAssistRoot is the UserAssist key under HKCU.
	userAssistRoot = `Software\Microsoft\Windows\CurrentVersion\Explorer\UserAssist`
	// The Count subkeys that matter: executables launched through
	// Explorer, and shortcuts.
	userAssistExeKey      = "{CEBFF5CD-ACE2-4F4F-9178-9926F41749EA}"
	userAssistShortcutKey = "{F4E57C4B-2036-45F0-A9AB-443BCFE33D9F}"
)

// RecentRecord is one recently used App from UserAssist: an executable or
// shortcut path, or an App User Model ID (exactly one of Path and AUMID is
// set), with usage statistics.
type RecentRecord struct {
	Path     string
	AUMID    string
	UseCount int
	LastUsed time.Time
}

// ReadUserAssist reads recent-app records from both UserAssist Count keys
// (real registry access). A missing key (fresh profile, tracking off)
// yields no records and no error; unexpected errors are returned.
func ReadUserAssist() ([]RecentRecord, error) {
	var out []RecentRecord
	for _, guid := range []string{userAssistExeKey, userAssistShortcutKey} {
		values, err := readCountValues(guid)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading UserAssist %s: %w", guid, err)
		}
		for _, name := range sortedNames(values) {
			if rec, ok := parseUserAssistValue(name, values[name]); ok {
				rec.Path = MapKnownFolderPrefix(rec.Path, KnownFolderPath)
				out = append(out, rec)
			}
		}
	}
	return out, nil
}

// sortedNames iterates Count values deterministically.
func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	// insertion-sort is fine for registry-sized sets and avoids pulling in
	// sort for one small list
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// readCountValues reads one Count key's values (real registry access).
func readCountValues(guid string) (map[string][]byte, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, userAssistRoot+`\`+guid+`\Count`, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		data, _, err := key.GetBinaryValue(name)
		if err != nil {
			continue // unreadable value: skip, best-effort
		}
		out[name] = data
	}
	return out, nil
}

// parseUserAssistValue decodes one Count value, purely: the ROT13 name
// into a path or AUMID, the data into usage statistics. Uninstallers,
// Explorer's internal bookkeeping names and values with unreadable data
// are skipped.
func parseUserAssistValue(name string, data []byte) (RecentRecord, bool) {
	path, aumid := classifyDecoded(Rot13(name))
	useCount, lastUsed, ok := parseCountData(data)
	if !ok {
		return RecentRecord{}, false
	}
	if aumid != "" {
		return RecentRecord{AUMID: aumid, UseCount: useCount, LastUsed: lastUsed}, true
	}
	if path == "" || isUninstaller(path) {
		return RecentRecord{}, false
	}
	return RecentRecord{Path: path, UseCount: useCount, LastUsed: lastUsed}, true
}

// classifyDecoded splits a decoded UserAssist name into an executable or
// shortcut path, or an App User Model ID. Anything else (Explorer's
// internal bookkeeping values, GUID-less strings) matches nothing.
func classifyDecoded(name string) (path, aumid string) {
	if strings.Contains(name, "!") && !strings.ContainsAny(name, `\/`) && len(name) > 1 {
		return "", name
	}
	if !looksLikePath(name) {
		return "", ""
	}
	if hasSuffixFold(name, ".lnk") {
		return name, ""
	}
	return name, ""
}

// looksLikePath reports whether a decoded name starts like a filesystem
// path: a drive prefix, a UNC root or a known-folder GUID.
func looksLikePath(name string) bool {
	if len(name) >= 2 && name[1] == ':' {
		return true
	}
	if strings.HasPrefix(name, `\\`) {
		return true
	}
	if strings.HasPrefix(name, "{") && strings.Contains(name, "}") {
		return true
	}
	return false
}

// isUninstaller skips uninstall entries (hygiene: they are never Apps an
// agent should start).
func isUninstaller(path string) bool {
	return strings.Contains(strings.ToLower(path), "uninstall")
}

// parseCountData reads the usage statistics out of a UserAssist Count
// value's 72-byte data (Win7+ layout): run count at offset 4, last run
// FILETIME at offset 60. ok is false for values too short or too old to
// carry both.
func parseCountData(data []byte) (useCount int, lastUsed time.Time, ok bool) {
	if len(data) < 68 {
		return 0, time.Time{}, false
	}
	useCount = int(binary.LittleEndian.Uint32(data[4:8]))
	lastUsed = filetimeToTime(binary.LittleEndian.Uint64(data[60:68]))
	return useCount, lastUsed, true
}

// filetimeToTime converts a Win32 FILETIME (100 ns intervals since 1601)
// to local time; zero and pre-1970 values give the zero time.
func filetimeToTime(ft uint64) time.Time {
	const windowsToUnixEpoch = 116444736000000000 // 100 ns units between 1601-01-01 and 1970-01-01
	if ft == 0 {
		return time.Time{}
	}
	n := int64(ft) - windowsToUnixEpoch
	if n < 0 {
		return time.Time{}
	}
	return time.Unix(0, n*100)
}

// Rot13 rotates letters (case preserved) — UserAssist value names are
// ROT13-encoded. Applying it twice restores the input.
func Rot13(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = 'a' + (c-'a'+13)%26
		case c >= 'A' && c <= 'Z':
			b[i] = 'A' + (c-'A'+13)%26
		}
	}
	return string(b)
}

// MapKnownFolderPrefix replaces a leading "{GUID}" known-folder marker in
// a path with the folder's real path (UserAssist paths use them, e.g.
// "{6D809377-...}\App\App.exe" for Program Files). Unresolvable prefixes
// keep the path unchanged; resolve receives the GUID without braces and
// returns "" when it cannot resolve it.
func MapKnownFolderPrefix(path string, resolve func(guid string) string) string {
	if !strings.HasPrefix(path, "{") {
		return path
	}
	end := strings.Index(path, "}")
	if end < 0 {
		return path
	}
	guid := path[1:end]
	if _, ok := ParseGUID(guid); !ok {
		return path
	}
	dir := resolve(guid)
	if dir == "" {
		return path
	}
	rest := path[end+1:]
	switch {
	case rest == "":
		return dir
	case strings.HasPrefix(rest, `\`):
		return dir + rest
	default:
		return dir + `\` + rest
	}
}

// KnownFolderPath resolves a KNOWNFOLDERID GUID (braces stripped) to its
// current path; "" when unresolvable. A package variable so tests can stub
// it; the real one is SHGetKnownFolderPath.
var KnownFolderPath = func(guid string) string {
	g, ok := ParseGUID(guid)
	if !ok {
		return ""
	}
	folder := windows.KNOWNFOLDERID(g)
	path, err := windows.KnownFolderPath(&folder, 0)
	if err != nil {
		return ""
	}
	return path
}

// ParseGUID parses "{...}" or bare "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
// into a windows.GUID.
func ParseGUID(s string) (windows.GUID, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	parts := strings.Split(s, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return windows.GUID{}, false
	}
	d1, err1 := strconv.ParseUint(parts[0], 16, 32)
	d2, err2 := strconv.ParseUint(parts[1], 16, 16)
	d3, err3 := strconv.ParseUint(parts[2], 16, 16)
	data4, err4 := hex.DecodeString(parts[3] + parts[4])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || len(data4) != 8 {
		return windows.GUID{}, false
	}
	g := windows.GUID{Data1: uint32(d1), Data2: uint16(d2), Data3: uint16(d3)}
	copy(g.Data4[:], data4)
	return g, true
}
