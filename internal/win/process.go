package win

import (
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dialogClass is the pre-defined dialog window class ("Save As"-style owned
// dialogs, message boxes).
const dialogClass = "#32770"

// defaultLangIDs are the StringFileInfo language codes tried when a version
// resource does not expose a translation table (0409 = en-US, b0 = Unicode).
var defaultLangIDs = []string{"040904b0", "040904e4", "000004b0"}

// exePath returns the full path of the process with the given pid. It uses
// PROCESS_QUERY_LIMITED_INFORMATION, which works for processes of other
// users and most elevated processes.
func exePath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// fileDescription reads the FileDescription string from a file's version
// resource, preferring the resource's own translation. Returns "" when the
// resource is missing or has no FileDescription.
func fileDescription(path string) string {
	sz, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || sz == 0 {
		return ""
	}
	data := make([]byte, sz)
	if err := windows.GetFileVersionInfo(path, 0, sz, unsafe.Pointer(&data[0])); err != nil {
		return ""
	}
	block := unsafe.Pointer(&data[0])

	langs := defaultLangIDs
	var p unsafe.Pointer
	var n uint32
	if windows.VerQueryValue(block, `\VarFileInfo\Translation`, unsafe.Pointer(&p), &n) == nil && n >= 4 {
		pair := (*[2]uint16)(p)
		preferred := []string{stringLangID(pair[0], pair[1])}
		langs = append(preferred, defaultLangIDs...)
	}
	for _, lang := range langs {
		var s unsafe.Pointer
		if windows.VerQueryValue(block, `\StringFileInfo\`+lang+`\FileDescription`, unsafe.Pointer(&s), &n) != nil || n == 0 {
			continue
		}
		if str := windows.UTF16PtrToString((*uint16)(s)); str != "" {
			return str
		}
	}
	return ""
}

func stringLangID(langID, charsetID uint16) string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 8)
	for i, v := range []uint16{langID, charsetID} {
		b[i*4+0] = hexDigits[(v>>12)&0xf]
		b[i*4+1] = hexDigits[(v>>8)&0xf]
		b[i*4+2] = hexDigits[(v>>4)&0xf]
		b[i*4+3] = hexDigits[v&0xf]
	}
	return string(b)
}

// appNames resolves and caches App display names by executable path: the
// FileDescription from the version resource, falling back to the executable
// base name without extension.
type appNames struct {
	mu   sync.Mutex
	apps map[string]string
}

func newAppNames() *appNames {
	return &appNames{apps: map[string]string{}}
}

// Name returns the App name for an executable path ("" for an empty path).
func (a *appNames) Name(exePath string) string {
	if exePath == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if name, ok := a.apps[exePath]; ok {
		return name
	}
	name := fileDescription(exePath)
	if name == "" {
		name = trimExt(filepath.Base(exePath))
	}
	a.apps[exePath] = name
	return name
}

func trimExt(base string) string {
	ext := filepath.Ext(base)
	if len(ext) < len(base) {
		return base[:len(base)-len(ext)]
	}
	return base
}
