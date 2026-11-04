// Package bridge is the winuse extension bridge (browser merge ticket 02):
// winuse drives the pages of the user's real, running browser through its
// own MV3 extension, with no debug port, no profile copy and no "Allow
// remote debugging?" prompt.
//
//	winuse helper --(loopback TCP, token)--> native host --(stdio)--> extension
//
// The native host is the winuse binary itself. The browser starts it when
// the extension calls connectNative, passing the extension origin as the
// first argument (IsHostLaunch); it lives exactly as long as that port. It
// listens on a loopback port, records {port, token, the browser that
// started it} in a bridge file (Info), and relays CDP JSON between helper
// connections and the extension, tagging each message with the connection
// it belongs to. The extension answers Target.* itself and sends page
// commands to chrome.debugger, so the helper side is the ordinary
// cdp.Client over a Conn.
//
// The helper link is raw length-prefixed frames, not WebSocket, on
// purpose: a web page can open a WebSocket to 127.0.0.1 but cannot speak
// raw TCP, so no site can even reach the token check.
//
// Ported from use-browser v0.9.0 (ext.go, extension/; see
// third_party/use-browser/NOTICE), with winuse's own extension identity,
// the foreground rule (Refusal), child sessions passed through, and
// commands over the 1 MB native messaging limit sent in chunks.
package bridge

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed extension
var extensionFiles embed.FS

const (
	// HostName is the native messaging host's name, in the host manifest
	// and in the extension's connectNative call.
	HostName = "com.hoangvu12.winuse"
	// ExtensionID is fixed by the "key" in extension/manifest.json, so an
	// unpacked install gets the same id on every machine and the host
	// manifest can name it. It differs from use-browser's
	// (blcbodpjbiimmoenmdneeacdogpglgne), so both can be loaded while a
	// user moves over.
	ExtensionID = "mjolifepdnkcigohjhadbidbojmfhcog"
	// Origin is the caller origin the browser passes the native host.
	Origin = "chrome-extension://" + ExtensionID + "/"
)

// Error codes on the bridge's CDP error responses. The extension sends
// CodeFailed, CodePaused and the Refusal codes; the helper side (Conn)
// answers the Refusal codes and CodeTooLarge itself, and so does the host
// for CodeTooLarge, without bothering the extension.
const (
	CodeFailed     = -32000 // chrome.debugger or chrome.tabs failed
	CodeRefused    = -32010 // a browser-level method the extension cannot run (Refusal)
	CodePaused     = -32011 // the user turned winuse off in this browser
	CodeTooLarge   = -32012 // a command over MaxCommand
	CodeForeground = -32013 // a method that would raise the browser window (Refusal)
)

// The bridge's own methods, browser-level (no sessionId):
//
//	Winuse.status       {} -> {browser, version, build, instance, paused}
//	Winuse.reload       {} -> {} then the extension reloads from its folder
//	Winuse.activateTab  {targetId} -> {}: select the tab in its window,
//	                    never focusing or raising the window (ticket 07)
//	Winuse.windows      {} -> {windows: [{windowId, left, top, width,
//	                    height, state}]}: every browser window's bounds in
//	                    DIPs, read with chrome.windows.getAll (ticket 04
//	                    maps a Window to its Tab with them, since
//	                    Browser.getWindowForTarget is refused here)
//	Winuse.navigateTab  {targetId, url} -> {}: open url in the tab with
//	                    chrome.tabs.update, never focusing or raising the
//	                    window or changing the selected tab. navigate uses
//	                    it on a tab that shows a browser page
//	                    (chrome://newtab/), which chrome.debugger cannot
//	                    attach to.
//	Winuse.holdTab      {targetId} -> {}: an agent works in the tab while
//	                    the user may look at another one (ADR 0017):
//	                    autoDiscardable:false, and a tab it opens never stays
//	                    the selected one (the user's tab is selected again,
//	                    the new tab joins its opener's group, a popup window
//	                    is minimized)
//	Winuse.releaseTab   {targetId} -> {}: the tab's own autoDiscardable
//	                    back; the debugger detaching releases it too
const (
	MethodStatus      = "Winuse.status"
	MethodReload      = "Winuse.reload"
	MethodActivateTab = "Winuse.activateTab"
	MethodWindows     = "Winuse.windows"
	MethodNavigateTab = "Winuse.navigateTab"
	MethodHoldTab     = "Winuse.holdTab"
	MethodReleaseTab  = "Winuse.releaseTab"
)

// emulated are the browser-level Target methods the extension emulates on
// chrome.tabs.
var emulated = map[string]bool{
	"Target.getTargets":       true,
	"Target.attachToTarget":   true,
	"Target.detachFromTarget": true,
	"Target.createTarget":     true,
	"Target.closeTarget":      true,
}

// foreground are the methods that would raise the user's browser window.
var foreground = map[string]bool{
	"Page.bringToFront":     true,
	"Target.activateTarget": true,
}

// browserDomains exist only on the browser target.
var browserDomains = map[string]bool{"Browser": true, "SystemInfo": true, "Tethering": true}

// Refusal says why a method never goes through the extension, with its
// error code, or 0 and "" when it may. background.js's refusal() is the
// same rule; both sides apply it, so a refused method never reaches
// chrome.debugger.
//
//   - Page.bringToFront and Target.activateTarget would raise the window
//     (CodeForeground).
//   - Browser.*, SystemInfo.* and Tethering.* exist only on the browser
//     target, which an extension cannot reach (CodeRefused).
//   - Without a session, only the emulated Target methods and the
//     bridge's own Winuse methods exist (CodeRefused).
func Refusal(method, sessionID string) (int, string) {
	if foreground[method] {
		return CodeForeground, method + " would bring the browser window to the front, and winuse never takes the foreground."
	}
	domain, _, _ := strings.Cut(method, ".")
	if domain == "Winuse" && sessionID == "" {
		return 0, ""
	}
	if browserDomains[domain] || domain == "Winuse" || (sessionID == "" && !emulated[method]) {
		return CodeRefused, method + " needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone."
	}
	return 0, ""
}

// IsHostLaunch recognizes the browser starting winuse as a native
// messaging host: the first argument is the calling extension's origin
// (on Windows a --parent-window=<hwnd> argument follows).
func IsHostLaunch(args []string) bool {
	return len(args) > 0 && strings.HasPrefix(args[0], "chrome-extension://")
}

// Version is the version of the extension embedded in this binary.
func Version() string {
	b, _ := extensionFiles.ReadFile("extension/manifest.json")
	var m struct {
		Version string `json:"version"`
	}
	json.Unmarshal(b, &m)
	return m.Version
}

// Build hashes the extension embedded in this binary. WriteExtension
// stamps it into the manifest it writes and the loaded extension reports
// it back, so any change to the extension is noticed without anyone
// bumping a version.
func Build() string {
	h := sha256.New()
	fs.WalkDir(extensionFiles, "extension", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := extensionFiles.ReadFile(p)
			fmt.Fprintf(h, "%s %d\n", p, len(data))
			h.Write(data)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// buildMark joins the version and the build hash in the version_name
// WriteExtension stamps; background.js's loadedBuild() splits on it too.
const buildMark = " build "

// WriteExtension writes the embedded extension into dir, where the user
// loads it unpacked from, with its build hash stamped into the manifest
// ("version_name": "<version> build <hash>"). Chrome reads the manifest
// only when it loads or reloads the extension, so the stamp the extension
// reports names the code that is running, even after the folder was
// rewritten under it. version_name is shown on the extensions page and
// does not change the extension id, which comes from "key".
func WriteExtension(dir string) error {
	err := fs.WalkDir(extensionFiles, "extension", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "extension")))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := extensionFiles.ReadFile(p)
		if err != nil {
			return err
		}
		if p == "extension/manifest.json" {
			if data, err = stampManifest(data); err != nil {
				return err
			}
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("writing the winuse extension: %w", err)
	}
	// Extension 0.1.2 and older read their build from a build.json here at
	// every connect, so a folder rewritten without a reload passed for
	// current. Without the file they report no build, which is outdated, and
	// self-update reloads them into the stamped manifest.
	if err := os.Remove(filepath.Join(dir, "build.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("writing the winuse extension: %w", err)
	}
	return nil
}

// stampManifest adds the version_name stamp to the embedded manifest.
func stampManifest(data []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	var version string
	json.Unmarshal(m["version"], &version)
	m["version_name"], _ = json.Marshal(version + buildMark + Build())
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(m)
	return out.Bytes(), err
}

// StampedBuild reads the build stamped into an extension manifest, as
// background.js reads it from chrome.runtime.getManifest(). A manifest
// without the stamp (one written before it existed) gives "", which never
// matches Build, so that extension counts as outdated.
func StampedBuild(manifest []byte) string {
	var m struct {
		VersionName string `json:"version_name"`
	}
	json.Unmarshal(bytes.TrimPrefix(manifest, []byte{0xEF, 0xBB, 0xBF}), &m)
	_, build, _ := strings.Cut(m.VersionName, buildMark)
	return build
}

// HostManifest is the native messaging host manifest, pointing the
// browser at the winuse binary and allowing only winuse's extension.
func HostManifest(exe string) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"name":            HostName,
		"description":     "winuse extension bridge",
		"path":            exe,
		"type":            "stdio",
		"allowed_origins": []string{Origin},
	}, "", "  ")
	return append(b, '\n')
}
