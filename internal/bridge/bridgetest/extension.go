// Package bridgetest is a fake winuse extension for offline tests. It runs
// a real bridge.ServeHost over in-memory pipes and plays the extension's
// side of native messaging, with background.js's routing rules in Go:
// emulated Target methods, tab sessions named by target id, child sessions
// routed through their tab, events forwarded with their sessionId, the off
// switch, the build of the manifest it was loaded with (a folder rewritten
// under it is not seen until a reload), and the 1 MB limit on every native
// message from the host. The host listens on an ephemeral loopback port
// only; no browser is started.
package bridgetest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
)

// Tab is one tab the fake lists in Target.getTargets.
type Tab struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	TabID    int    `json:"tabId"`
	WindowID int    `json:"windowId"`
	Active   bool   `json:"active"`
	// Group is the tab group an agent tab joined (createTab): its title
	// and colour; empty for a tab the agent did not open.
	Group TabGroup `json:"-"`
}

// TabGroup is a chrome.tabGroups group: a title and one of its colours.
type TabGroup struct {
	Title string `json:"title"`
	Color string `json:"color"`
}

// Group is the tab group the agent tab with this target id joined (zero
// when there is no such tab).
func (e *Extension) Group(targetID string) TabGroup {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.Tabs {
		if t.TargetID == targetID {
			return t.Group
		}
	}
	return TabGroup{}
}

// tabGroupColors are the colours chrome.tabGroups accepts.
var tabGroupColors = map[string]bool{"grey": true, "blue": true, "red": true, "yellow": true, "green": true, "pink": true, "purple": true, "cyan": true, "orange": true}

// tabGroupFor is background.js's tabGroupFor: winuse's group, else the
// default blue "winuse".
func tabGroupFor(g *TabGroup) TabGroup {
	out := TabGroup{Title: "winuse", Color: "blue"}
	if g == nil {
		return out
	}
	if t := strings.TrimSpace(g.Title); t != "" {
		if r := []rune(t); len(r) > 40 {
			t = string(r[:40])
		}
		out.Title = t
	}
	if tabGroupColors[g.Color] {
		out.Color = g.Color
	}
	return out
}

// Window is one browser window Winuse.windows reports, bounds in DIPs.
type Window struct {
	WindowID int    `json:"windowId"`
	Left     int    `json:"left"`
	Top      int    `json:"top"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	State    string `json:"state"`
}

// Command is one command the fake received from the host.
type Command struct {
	Conn      int
	ID        int64
	Method    string
	SessionID string
	Params    json.RawMessage
	Size      int // the command's size in bytes, after joining chunks
}

// Extension is the fake. Set its fields, then call Start.
type Extension struct {
	Browser  string // the extension's hello; default chrome
	Instance string // default ab12cd34
	// Version and Build are what the manifest the browser loaded says
	// (chrome.runtime.getManifest()), which the hello and Winuse.status
	// report. Default bridge.Version() and bridge.Build(), or Dir's.
	Version string
	Build   string
	// Dir, when set, is the unpacked folder the extension is loaded from:
	// Start reads Version and Build from its manifest.json once, as the
	// browser does on a load, so rewriting the folder later changes nothing
	// until a reload. A manifest without a stamp gives Build "".
	Dir        string
	BrowserPID int // the process the host records as its browser
	BrowserExe string
	Tabs       []Tab
	// Windows is what Winuse.windows reports (chrome.windows.getAll).
	Windows []Window

	// Page answers a command on a tab or child session; nil answers {}.
	Page func(sessionID, method string, params json.RawMessage) (any, error)
	// OnReload runs after Winuse.reload is answered: the real extension
	// reloads from its folder, which ends this host and starts a new one.
	// Without it, an extension with a Dir does that itself (Reload), and
	// one without a Dir answers and stays as it is.
	OnReload func()

	t        testing.TB
	dir      string // the bridges directory
	loaded   bool   // Version and Build are settled (Start, Reconnect)
	toHost   *io.PipeWriter
	fromHost *io.PipeReader
	wmu      sync.Mutex
	served   chan error

	// Info is the bridge file the host published.
	Info bridge.Info

	mu        sync.Mutex
	paused    bool
	attached  map[string]bool
	children  map[string]string // child session id -> its tab's target id
	conns     map[int]map[string]bool
	partial   map[int][]byte
	commands  []Command
	envelopes []int // the size of every native message from the host
	chunked   int   // commands that arrived in chunks
	activated []string
	navigated []string
	reloads   int
	closed    bool
	held      map[string]bool // Winuse.holdTab's tabs, by target id
	holds     []string        // every hold and release, as "hold <id>" or "release <id>"
	minimized map[int]bool    // popup windows the guard minimized
}

// Held reports whether a tab is held (Winuse.holdTab, ADR 0017).
func (e *Extension) Held(targetID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.held[targetID]
}

// Holds lists every Winuse.holdTab and Winuse.releaseTab, in order.
func (e *Extension) Holds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.holds...)
}

// ActiveTab is the target id of a window's selected tab ("" for none).
func (e *Extension) ActiveTab(windowID int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.Tabs {
		if t.WindowID == windowID && t.Active {
			return t.TargetID
		}
	}
	return ""
}

// Minimized reports whether the guard minimized a popup window.
func (e *Extension) Minimized(windowID int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.minimized[windowID]
}

// OpenFromTab is the page in the tab opener opening a new tab, as a
// target=_blank link or window.open does: the browser selects the new tab
// in the opener's window, or opens it in a new popup window. Then
// background.js's guard runs when the opener is held: the tab the user had
// selected is selected again, the new tab joins its opener's group, and a
// popup window is minimized. It returns the new tab's target id.
func (e *Extension) OpenFromTab(opener, url string, popup bool) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var from *Tab
	for i := range e.Tabs {
		if e.Tabs[i].TargetID == opener {
			from = &e.Tabs[i]
		}
	}
	if from == nil {
		return ""
	}
	window, group := from.WindowID, from.Group
	if popup {
		window = 900 + len(e.Windows)
		e.Windows = append(e.Windows, Window{WindowID: window, Width: 500, Height: 600, State: "normal"})
	}
	id := fmt.Sprintf("OPENED%d", len(e.Tabs)+1)
	users := ""
	for i := range e.Tabs {
		if e.Tabs[i].WindowID == window && e.Tabs[i].Active {
			users = e.Tabs[i].TargetID
			e.Tabs[i].Active = false
		}
	}
	e.Tabs = append(e.Tabs, Tab{TargetID: id, URL: url, Title: url, TabID: 2000 + len(e.Tabs), WindowID: window, Active: true})
	if !e.held[opener] {
		return id
	}
	n := &e.Tabs[len(e.Tabs)-1]
	if popup {
		if e.minimized == nil {
			e.minimized = map[int]bool{}
		}
		e.minimized[window] = true
		return id
	}
	n.Group = group
	if users != "" {
		n.Active = false
		for i := range e.Tabs {
			if e.Tabs[i].TargetID == users {
				e.Tabs[i].Active = true
			}
		}
	}
	return id
}

// Start runs a host for the fake in dir (the bridges directory) and waits
// for its bridge file.
func Start(t testing.TB, dir string, e *Extension) *Extension {
	t.Helper()
	if e.Browser == "" {
		e.Browser = "chrome"
	}
	if e.Instance == "" {
		e.Instance = "ab12cd34"
	}
	if !e.loaded {
		e.load(t)
	}
	e.t = t
	e.dir = dir
	e.attached = map[string]bool{}
	e.children = map[string]string{}
	e.conns = map[int]map[string]bool{}
	e.partial = map[int][]byte{}
	e.served = make(chan error, 1)

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	e.toHost, e.fromHost = stdinW, stdoutR
	cfg := bridge.HostConfig{
		Dir:     dir,
		Origin:  bridge.Origin,
		PID:     4100,
		Browser: func() (int, string) { return e.BrowserPID, e.BrowserExe },
		Listen:  func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") },
	}
	go func() {
		err := bridge.ServeHost(cfg, stdinR, stdoutW)
		stdoutW.Close()
		e.served <- err
	}()
	go e.readLoop()
	e.send(map[string]any{"hello": map[string]string{"browser": e.Browser, "instance": e.Instance, "version": e.Version, "build": e.Build}})
	t.Cleanup(func() { e.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, info := range bridge.List(dir, nil) {
			if info.Instance == e.Instance && info.Browser == e.Browser {
				e.Info = info
				return e
			}
		}
		// A reload hook's Start can lose the race to the code under test,
		// which saw the bridge file and ended the test, whose cleanup
		// closed this host before the file was read here.
		e.mu.Lock()
		closed := e.closed
		e.mu.Unlock()
		if closed {
			return e
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("bridgetest: the host never published its bridge file in %s", dir)
	return e
}

// load is the browser loading the extension: Version and Build come from
// the manifest in Dir, else they default to the embedded extension's.
func (e *Extension) load(t testing.TB) {
	t.Helper()
	e.loaded = true
	if e.Dir != "" {
		b, err := os.ReadFile(filepath.Join(e.Dir, "manifest.json"))
		if err != nil {
			t.Fatalf("bridgetest: loading the extension: %v", err)
		}
		var m struct {
			Version string `json:"version"`
		}
		json.Unmarshal(b, &m)
		e.Version, e.Build = m.Version, bridge.StampedBuild(b)
		return
	}
	if e.Version == "" {
		e.Version = bridge.Version()
	}
	if e.Build == "" {
		e.Build = bridge.Build()
	}
}

// Reconnect is the extension connecting again without a reload: its host
// exited and the reconnect alarm started a new one. The manifest the
// browser loaded, and so the build reported, stays the same whatever the
// folder holds now. It returns the extension behind the new host.
func (e *Extension) Reconnect() *Extension { return e.restart(true) }

// Reload is chrome.runtime.reload(): the host exits and the browser loads
// the extension from Dir again (the embedded one without a Dir), so the
// new host gets the version and build stamped there now.
func (e *Extension) Reload() *Extension { return e.restart(false) }

func (e *Extension) restart(sameLoad bool) *Extension {
	e.Close()
	e.mu.Lock()
	n := &Extension{
		Browser: e.Browser, Instance: e.Instance, Dir: e.Dir,
		BrowserPID: e.BrowserPID, BrowserExe: e.BrowserExe,
		Tabs: append([]Tab(nil), e.Tabs...), Windows: append([]Window(nil), e.Windows...),
		Page: e.Page, OnReload: e.OnReload,
		paused: e.paused, // the off switch is in chrome.storage, which outlives both
	}
	e.mu.Unlock()
	if sameLoad {
		n.Version, n.Build, n.loaded = e.Version, e.Build, true
	}
	return Start(e.t, e.dir, n)
}

// Close ends the extension's side of stdio, as the browser does when the
// extension's port goes away, and returns the host's result.
func (e *Extension) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	e.toHost.Close()
	select {
	case err := <-e.served:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("bridgetest: the host did not exit after stdin closed")
	}
}

// Raw writes one native message to the host as is (garbage tests).
func (e *Extension) Raw(b []byte) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	_, err := e.toHost.Write(b)
	return err
}

// Wait waits up to d for the host to return, and reports whether it did
// and what it returned.
func (e *Extension) Wait(d time.Duration) (bool, error) {
	select {
	case err := <-e.served:
		e.served <- err
		return true, err
	case <-time.After(d):
		return false, nil
	}
}

func (e *Extension) send(v any) {
	b, _ := json.Marshal(v)
	e.wmu.Lock()
	defer e.wmu.Unlock()
	bridge.WriteFrame(e.toHost, b)
}

// Set replaces the tabs and windows the extension lists, as the user
// opening, closing and navigating them would.
func (e *Extension) Set(tabs []Tab, windows []Window) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Tabs = append([]Tab(nil), tabs...)
	e.Windows = append([]Window(nil), windows...)
}

// SetPaused flips the off switch (Cancel on the debugging bar, or the
// toolbar icon). Turning it on detaches every tab.
func (e *Extension) SetPaused(on bool) {
	e.mu.Lock()
	e.paused = on
	var tabs []string
	if on {
		for id := range e.attached {
			tabs = append(tabs, id)
		}
	}
	e.mu.Unlock()
	for _, id := range tabs {
		e.Detach(id, "canceled_by_user")
	}
}

// Emit is chrome.debugger.onEvent: an event from a tab, or from one of its
// child sessions when sessionID is set.
func (e *Extension) Emit(tabID, sessionID, method string, params any) {
	raw, _ := json.Marshal(params)
	var p struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(raw, &p)
	e.mu.Lock()
	switch {
	case method == "Target.attachedToTarget" && p.SessionID != "":
		e.children[p.SessionID] = tabID
	case method == "Target.detachedFromTarget" && p.SessionID != "":
		delete(e.children, p.SessionID)
	}
	e.mu.Unlock()
	if sessionID == "" {
		sessionID = tabID
	}
	e.forward(tabID, map[string]any{"method": method, "params": json.RawMessage(raw), "sessionId": sessionID})
}

// Detach is chrome.debugger.onDetach for a tab: its children go too.
func (e *Extension) Detach(tabID, reason string) {
	e.mu.Lock()
	delete(e.attached, tabID)
	if e.held[tabID] {
		e.held[tabID] = false // background.js's unholdTarget
		e.holds = append(e.holds, "release "+tabID)
	}
	var kids []string
	for sid, parent := range e.children {
		if parent == tabID {
			kids = append(kids, sid)
			delete(e.children, sid)
		}
	}
	if reason == "canceled_by_user" {
		e.paused = true
	}
	e.mu.Unlock()
	for _, sid := range kids {
		e.forward(tabID, map[string]any{"method": "Target.detachedFromTarget", "params": map[string]string{"sessionId": sid}, "sessionId": tabID})
	}
	e.forward(tabID, map[string]any{"method": "Target.detachedFromTarget", "params": map[string]string{"sessionId": tabID, "targetId": tabID}})
}

func (e *Extension) forward(tabID string, msg any) {
	e.mu.Lock()
	var cs []int
	for c, tabs := range e.conns {
		if tabs[tabID] {
			cs = append(cs, c)
		}
	}
	e.mu.Unlock()
	for _, c := range cs {
		e.send(map[string]any{"c": c, "m": msg})
	}
}

// Commands lists what the fake received, in order.
func (e *Extension) Commands() []Command {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Command(nil), e.commands...)
}

// Envelopes lists the size of every native message the host sent.
func (e *Extension) Envelopes() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.envelopes...)
}

// Chunked counts the commands that arrived in chunks.
func (e *Extension) Chunked() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.chunked
}

// Activated lists the tabs Winuse.activateTab selected.
func (e *Extension) Activated() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.activated...)
}

// Reloads counts Winuse.reload calls.
func (e *Extension) Reloads() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reloads
}

// Conns counts the helper connections the fake knows (a close envelope
// removes one).
func (e *Extension) Conns() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.conns)
}

// readLoop reads the host's native messages. Anything over the browser's
// 1 MB limit fails the test, as the browser would kill the host over it.
func (e *Extension) readLoop() {
	for {
		b, err := bridge.ReadFrame(e.fromHost, bridge.MaxFrame)
		if err != nil {
			return
		}
		e.mu.Lock()
		e.envelopes = append(e.envelopes, len(b))
		e.mu.Unlock()
		if len(b) > bridge.MaxToExtension {
			e.t.Errorf("bridgetest: the host sent a %d byte native message; the browser would kill it", len(b))
		}
		var env struct {
			C     int             `json:"c"`
			M     json.RawMessage `json:"m"`
			Chunk *string         `json:"chunk"`
			Last  bool            `json:"last"`
			Close bool            `json:"close"`
		}
		if json.Unmarshal(b, &env) != nil {
			continue
		}
		if env.Close {
			e.mu.Lock()
			delete(e.conns, env.C)
			delete(e.partial, env.C)
			e.mu.Unlock()
			continue
		}
		m := []byte(env.M)
		if env.Chunk != nil {
			e.mu.Lock()
			text := append(e.partial[env.C], *env.Chunk...)
			if !env.Last {
				e.partial[env.C] = text
				e.mu.Unlock()
				continue
			}
			delete(e.partial, env.C)
			e.chunked++
			e.mu.Unlock()
			m = text
		}
		go e.onCommand(env.C, m)
	}
}

type bridgeError struct {
	code int
	msg  string
}

func (b *bridgeError) Error() string { return b.msg }

func (e *Extension) onCommand(c int, raw []byte) {
	var m struct {
		ID        int64           `json:"id"`
		Method    string          `json:"method"`
		SessionID string          `json:"sessionId"`
		Params    json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	e.mu.Lock()
	e.commands = append(e.commands, Command{Conn: c, ID: m.ID, Method: m.Method, SessionID: m.SessionID, Params: m.Params, Size: len(raw)})
	e.mu.Unlock()
	result, err := e.handle(c, m.Method, m.SessionID, m.Params)
	reply := map[string]any{"id": m.ID}
	if err != nil {
		code := bridge.CodeFailed
		var be *bridgeError
		if errors.As(err, &be) {
			code = be.code
		}
		reply["error"] = map[string]any{"code": code, "message": err.Error()}
	} else {
		if result == nil {
			result = map[string]any{}
		}
		reply["result"] = result
	}
	if m.SessionID != "" {
		reply["sessionId"] = m.SessionID
	}
	e.send(map[string]any{"c": c, "m": reply})
}

// handle is background.js's handle().
func (e *Extension) handle(c int, method, sessionID string, params json.RawMessage) (any, error) {
	e.mu.Lock()
	paused := e.paused
	e.mu.Unlock()
	switch method {
	case bridge.MethodStatus:
		return map[string]any{"browser": e.Browser, "version": e.Version, "build": e.Build, "instance": e.Instance, "paused": paused}, nil
	case bridge.MethodReload:
		e.mu.Lock()
		e.reloads++
		on := e.OnReload
		e.mu.Unlock()
		if on == nil && e.Dir != "" {
			on = func() { e.Reload() }
		}
		if on != nil {
			go func() {
				time.Sleep(20 * time.Millisecond) // after the reply, as the extension does
				on()
			}()
		}
		return nil, nil
	}
	if paused {
		return nil, &bridgeError{bridge.CodePaused, "winuse is turned off in this browser: the user pressed Cancel on the debugging bar or clicked the winuse icon."}
	}
	if code, r := bridge.Refusal(method, sessionID); code != 0 {
		return nil, &bridgeError{code, r}
	}
	var p struct {
		TargetID  string    `json:"targetId"`
		SessionID string    `json:"sessionId"`
		URL       string    `json:"url"`
		WindowID  *int      `json:"windowId"`
		Group     *TabGroup `json:"winuseGroup"`
	}
	json.Unmarshal(params, &p)
	if method == bridge.MethodWindows {
		e.mu.Lock()
		wins := append([]Window{}, e.Windows...)
		e.mu.Unlock()
		return map[string]any{"windows": wins}, nil
	}
	if method == bridge.MethodNavigateTab {
		return nil, e.navigateTab(p.TargetID, p.URL)
	}
	if method == bridge.MethodHoldTab || method == bridge.MethodReleaseTab {
		// background.js's holdTab and releaseTab: autoDiscardable off while
		// held, the tab's own value back on release.
		e.mu.Lock()
		defer e.mu.Unlock()
		known := false
		for _, tab := range e.Tabs {
			known = known || tab.TargetID == p.TargetID
		}
		if !known {
			return nil, fmt.Errorf("no tab with target id %s", p.TargetID)
		}
		if e.held == nil {
			e.held = map[string]bool{}
		}
		hold := method == bridge.MethodHoldTab
		e.held[p.TargetID] = hold
		word := "release"
		if hold {
			word = "hold"
		}
		e.holds = append(e.holds, word+" "+p.TargetID)
		return nil, nil
	}
	if method == bridge.MethodActivateTab {
		e.mu.Lock()
		e.activated = append(e.activated, p.TargetID)
		// chrome.tabs.update(active): the tab becomes its window's selected
		// one, and the window's other tabs are not.
		window := -1
		for _, tab := range e.Tabs {
			if tab.TargetID == p.TargetID {
				window = tab.WindowID
			}
		}
		for i := range e.Tabs {
			if e.Tabs[i].WindowID == window {
				e.Tabs[i].Active = e.Tabs[i].TargetID == p.TargetID
			}
		}
		e.mu.Unlock()
		return nil, nil
	}
	if sessionID != "" {
		return e.sessionCall(sessionID, method, params)
	}
	switch method {
	case "Target.getTargets":
		e.mu.Lock()
		tabs := append([]Tab(nil), e.Tabs...)
		e.mu.Unlock()
		for i := range tabs {
			if tabs[i].Type == "" {
				tabs[i].Type = "page"
			}
		}
		return map[string]any{"targetInfos": tabs}, nil
	case "Target.attachToTarget":
		if p.TargetID == "" {
			return nil, errors.New("targetId is required")
		}
		e.mu.Lock()
		if err := e.refuseBrowserPage(p.TargetID); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		e.attached[p.TargetID] = true
		if e.conns[c] == nil {
			e.conns[c] = map[string]bool{}
		}
		e.conns[c][p.TargetID] = true
		e.mu.Unlock()
		return map[string]any{"sessionId": p.TargetID}, nil
	case "Target.detachFromTarget":
		id := p.SessionID
		if id == "" {
			id = p.TargetID
		}
		e.mu.Lock()
		delete(e.conns[c], id)
		e.mu.Unlock()
		return nil, nil
	case "Target.createTarget":
		// chrome.tabs.create({active:false, windowId}): a background tab in
		// that window, or in window 1 (the last focused) without one, in
		// its Agent's tab group (browser-ux 10).
		e.mu.Lock()
		id := fmt.Sprintf("NEWTAB%d", len(e.Tabs)+1)
		window := 1
		if p.WindowID != nil {
			window = *p.WindowID
		}
		e.Tabs = append(e.Tabs, Tab{TargetID: id, URL: p.URL, TabID: 1000 + len(e.Tabs), WindowID: window, Group: tabGroupFor(p.Group)})
		e.mu.Unlock()
		return map[string]any{"targetId": id}, nil
	case "Target.closeTarget":
		// chrome.tabs.remove: the tab is gone from later listings.
		e.mu.Lock()
		for i, tab := range e.Tabs {
			if tab.TargetID == p.TargetID {
				e.Tabs = append(e.Tabs[:i:i], e.Tabs[i+1:]...)
				break
			}
		}
		e.mu.Unlock()
		return map[string]any{"success": true}, nil
	}
	return nil, &bridgeError{bridge.CodeRefused, method + " is not available through the winuse extension."}
}

// sessionCall is background.js's sessionCall(): a child session goes to
// its tab, a tab session attaches again after an idle detach.
func (e *Extension) sessionCall(sessionID, method string, params json.RawMessage) (any, error) {
	e.mu.Lock()
	parent, child := e.children[sessionID]
	if child && !e.attached[parent] {
		e.mu.Unlock()
		return nil, fmt.Errorf("the frame session %s was detached", sessionID)
	}
	if !child {
		// chrome.debugger.attach fails for an id that is no tab's target
		// (a child session id that is gone, for one).
		known := e.attached[sessionID]
		for _, tab := range e.Tabs {
			known = known || tab.TargetID == sessionID
		}
		if !known {
			e.mu.Unlock()
			return nil, fmt.Errorf("no target with given id %s found", sessionID)
		}
		if err := e.refuseBrowserPage(sessionID); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		e.attached[sessionID] = true
	}
	page := e.Page
	e.mu.Unlock()
	if page == nil {
		return nil, nil
	}
	return page(sessionID, method, params)
}

// browserPagePrefixes are the URLs chrome.debugger.attach refuses: the
// browser's own pages and other extensions' pages.
var browserPagePrefixes = []string{"chrome://", "edge://", "brave://", "vivaldi://", "opera://", "chrome-untrusted://", "devtools://", "chrome-extension://"}

// refuseBrowserPage is chrome.debugger's refusal to attach to a tab that
// shows a browser page, as Chrome answers it; nil for any other tab. Call
// with mu held.
func (e *Extension) refuseBrowserPage(targetID string) error {
	for _, tab := range e.Tabs {
		if tab.TargetID != targetID {
			continue
		}
		for _, p := range browserPagePrefixes {
			if strings.HasPrefix(tab.URL, p) {
				delete(e.attached, targetID) // Chrome detaches a tab that went there
				scheme, _, _ := strings.Cut(p, ":")
				return fmt.Errorf("Cannot access a %s:// URL", scheme)
			}
		}
	}
	return nil
}

// navigateTab is background.js's navigateTab(): chrome.tabs.update(tabId,
// {url}). The tab shows the URL at once, titled with it until the page
// names itself; nothing else changes (no window focus, no selection).
func (e *Extension) navigateTab(targetID, url string) error {
	if url == "" {
		return errors.New("url is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.Tabs {
		if e.Tabs[i].TargetID == targetID {
			e.Tabs[i].URL, e.Tabs[i].Title = url, url
			e.navigated = append(e.navigated, targetID+" "+url)
			return nil
		}
	}
	return fmt.Errorf("no tab with target id %s", targetID)
}

// Navigated lists Winuse.navigateTab's navigations, as "<targetId> <url>".
func (e *Extension) Navigated() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.navigated...)
}
