package bridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Info is what a running native host publishes about itself in its bridge
// file. There is one file per browser profile that runs the extension,
// since each profile runs its own extension instance and so its own host.
type Info struct {
	PID   int    `json:"pid"` // the native host (a winuse.exe the browser started)
	Exe   string `json:"exe"` // the host's image path, so a reused pid is not taken for it
	Port  int    `json:"port"`
	Token string `json:"token"`

	// The browser that started the host: its process (the host's parent,
	// past the cmd.exe Chromium starts hosts through), so winuse can tell
	// which browser this extension is connected to.
	BrowserPID int    `json:"browser_pid"`
	BrowserExe string `json:"browser_exe"`

	// From the extension's hello.
	Browser  string `json:"browser"` // the extension's own guess, from the user agent brands
	Instance string `json:"instance"`
	Version  string `json:"version"`
	Build    string `json:"build"` // the build stamped in the manifest the browser loaded; "" before the stamp

	Path string `json:"-"` // the bridge file
}

// Addr is the host's loopback address.
func (i Info) Addr() string { return "127.0.0.1:" + strconv.Itoa(i.Port) }

// FileName is the bridge file's name inside the bridges directory.
func (i Info) FileName() string { return i.Browser + "-" + i.Instance + ".json" }

// writeInfo publishes a host's bridge file. The file holds the token; it
// lives in the user's %LOCALAPPDATA%, which only that user can read.
func writeInfo(dir string, info Info) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, info.FileName())
	b, _ := json.Marshal(info)
	return path, os.WriteFile(path, b, 0o600)
}

// List reads the bridge files in dir, sorted by file name. alive decides
// whether a host still runs; a file whose host is gone (the browser
// closed, or killed the host before it cleaned up) is deleted.
func List(dir string, alive func(Info) bool) []Info {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(paths)
	var out []Info
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var info Info
		if json.Unmarshal(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), &info) != nil || info.Port == 0 || info.Token == "" {
			continue
		}
		if alive != nil && !alive(info) {
			os.Remove(p)
			continue
		}
		info.Path = p
		out = append(out, info)
	}
	return out
}

// validInstance accepts the extension's instance id: short, lower-case
// letters, digits and dashes, so it is safe in a file name.
func validInstance(s string) bool {
	if s == "" || len(s) > 36 {
		return false
	}
	return strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}

// validBrowser accepts the extension's browser name the same way.
func validBrowser(s string) bool {
	return s != "" && len(s) <= 16 && strings.Trim(s, "abcdefghijklmnopqrstuvwxyz") == ""
}
