package helper

// Offline tests for asset blocking (bladebro-ideas ticket 28) through the
// fake winuse extension (bgRig): tab new --block opens the Tab on
// about:blank, blocks, then goes to the URL; navigate --block changes what
// a Tab winuse opened blocks; the user's Tabs are refused with nothing
// sent; and reads, --observe and screenshots of a blocking Tab end with
// the note. The block is Network.enable then Network.setBlockedURLs with
// urlPatterns, on the blocking Tab's session only (never the user's Tabs),
// and --block none ends with Network.disable; no Fetch, no Runtime.enable.
// No browser runs.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// The patterns of each class, as page.BlockPatterns gives them.
var (
	imagePatterns = []string{"*://*:*/*.png", "*://*:*/*.jpg", "*://*:*/*.jpeg", "*://*:*/*.gif", "*://*:*/*.webp", "*://*:*/*.avif", "*://*:*/*.svg", "*://*:*/*.ico"}
	fontPatterns  = []string{"*://*:*/*.woff", "*://*:*/*.woff2", "*://*:*/*.ttf", "*://*:*/*.otf"}
)

// newTabID is the target id the fake extension gives the first Tab tab new
// opens (three Tabs are open), and its short form.
const (
	newTabTarget = "NEWTAB4"
	newTabShort  = "newtab4"
)

// blockCall is one Network.setBlockedURLs the fake extension received.
type blockCall struct {
	session  string
	patterns []string
}

// blockCalls lists the Network.setBlockedURLs calls, in order.
func (r *bgRig) blockCalls(t *testing.T) []blockCall {
	t.Helper()
	var out []blockCall
	for _, c := range r.ext.Commands() {
		if c.Method != "Network.setBlockedURLs" {
			continue
		}
		var p struct {
			URLs        []string `json:"urls"`
			URLPatterns []struct {
				URLPattern string `json:"urlPattern"`
				Block      bool   `json:"block"`
			} `json:"urlPatterns"`
		}
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.URLs) != 0 {
			t.Errorf("the urls form was sent: %v", p.URLs)
		}
		bc := blockCall{session: c.SessionID, patterns: []string{}}
		for _, u := range p.URLPatterns {
			if !u.Block {
				t.Errorf("%s does not block", u.URLPattern)
			}
			bc.patterns = append(bc.patterns, u.URLPattern)
		}
		out = append(out, bc)
	}
	return out
}

// methodOrder is the methods the fake extension received, in order.
func (r *bgRig) methodOrder() []string {
	var out []string
	for _, c := range r.ext.Commands() {
		out = append(out, c.Method)
	}
	return out
}

// networkOnlyOn fails when Network was turned on in any session but the
// blocking Tabs' (the fake's session id is the target id), or turned on
// there only after the block; when Runtime was turned on; or when a request
// paused.
func (r *bgRig) networkOnlyOn(t *testing.T, blocking ...string) {
	t.Helper()
	enabled := map[string]bool{}
	for _, c := range r.ext.Commands() {
		switch {
		case c.Method == "Network.enable":
			if indexOf(blocking, c.SessionID) < 0 {
				t.Errorf("Network.enable on session %q, which blocks nothing", c.SessionID)
			}
			enabled[c.SessionID] = true
		case c.Method == "Network.setBlockedURLs" && !enabled[c.SessionID] && !strings.Contains(string(c.Params), `"urlPatterns":[]`):
			t.Errorf("session %q blocked before Network.enable", c.SessionID)
		case c.Method == "Runtime.enable" || strings.HasPrefix(c.Method, "Fetch."):
			t.Errorf("%s was sent", c.Method)
		}
	}
	for _, s := range blocking {
		if !enabled[s] {
			t.Errorf("Network.enable was not sent on %q", s)
		}
	}
}

// networkCalls counts the Network methods sent, setBlockedURLs included.
func (r *bgRig) networkCalls() int {
	n := 0
	for _, m := range r.methodOrder() {
		if strings.HasPrefix(m, "Network.") {
			n++
		}
	}
	return n
}

// usersTabUntouched is noUserImpact for a test that ran tab new, whose
// Agent Cursor glides to the Window: the user's Tab still shown, nothing
// activated, no real input, and nothing sent to the user's Tab.
func (r *bgRig) usersTabUntouched(t *testing.T) {
	t.Helper()
	if got := r.ext.ActiveTab(1); got != bgInbox {
		t.Errorf("window 1 shows %s, not the user's Inbox", got)
	}
	if got := r.ext.Activated(); len(got) != 0 {
		t.Errorf("tabs activated: %v", got)
	}
	for _, s := range r.log() {
		if s == "activate" || strings.HasPrefix(s, "press ") {
			t.Errorf("the user saw %q: %v", s, r.log())
		}
	}
	noForegroundCalls(t, extMethods(r.ext))
	for _, c := range r.sent(bgInbox, "") {
		t.Errorf("the user's Tab got %q", c)
	}
}

func samePatterns(a, b []string) bool {
	return strings.Join(a, " ") == strings.Join(b, " ")
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

func lastIndexOf(list []string, s string) int {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i] == s {
			return i
		}
	}
	return -1
}

// TestTabNewBlockBlocksBeforeTheLoad: tab new --block opens the Tab on
// about:blank, blocks on its session, and only then sends it to the URL,
// so the first load is already blocked.
func TestTabNewBlockBlocksBeforeTheLoad(t *testing.T) {
	r := newBgRig(t)
	out, e := r.as(t, "claude", "tab-new", protocol.TabArgs{Window: "301", URL: "https://news.test/", Block: "images,fonts"})
	if e != nil || out != "ok "+newTabShort+"\n" {
		t.Fatalf("tab new: %q %+v", out, e)
	}
	var created struct {
		URL string `json:"url"`
	}
	for _, c := range r.ext.Commands() {
		if c.Method == "Target.createTarget" {
			json.Unmarshal(c.Params, &created)
		}
	}
	if created.URL != "about:blank" {
		t.Errorf("the Tab was created on %q, which loads before the block", created.URL)
	}
	calls := r.blockCalls(t)
	if len(calls) != 1 || calls[0].session != newTabTarget || !samePatterns(calls[0].patterns, append(append([]string{}, imagePatterns...), fontPatterns...)) {
		t.Errorf("blocked: %+v", calls)
	}
	order := r.methodOrder()
	if b, n := indexOf(order, "Network.setBlockedURLs"), indexOf(order, "Page.navigate"); b < 0 || n < 0 || b > n {
		t.Errorf("setBlockedURLs at %d, Page.navigate at %d: %v", b, n, order)
	}
	if en, b := indexOf(order, "Network.enable"), indexOf(order, "Network.setBlockedURLs"); en < 0 || en > b {
		t.Errorf("Network.enable at %d, setBlockedURLs at %d: %v", en, b, order)
	}
	r.mu.Lock()
	went := r.navigated[newTabTarget]
	r.mu.Unlock()
	if went != "https://news.test/" {
		t.Errorf("the new Tab went to %q", went)
	}
	r.networkOnlyOn(t, newTabTarget)
	r.usersTabUntouched(t)

	// Without a URL there is nothing to navigate: the Tab blocks on
	// about:blank.
	out, e = r.as(t, "claude", "tab-new", protocol.TabArgs{Window: "301", Block: "media"})
	if e != nil || !strings.HasPrefix(out, "ok ") || len(r.blockCalls(t)) != 2 {
		t.Errorf("tab new without a URL: %q %+v %+v", out, e, r.blockCalls(t))
	}

	// tab new without --block opens the URL directly and blocks nothing.
	out, e = r.as(t, "claude", "tab-new", protocol.TabArgs{Window: "301", URL: "https://plain.test/"})
	if e != nil || !strings.HasPrefix(out, "ok ") || len(r.blockCalls(t)) != 2 {
		t.Errorf("tab new without --block: %q %+v %+v", out, e, r.blockCalls(t))
	}
}

// TestBlockingTabReadsEndWithTheNote: get-window-state, its screenshot and
// --observe of a blocking Tab end with the note naming the classes; every
// command on it sends the block again, so a session the helper attached
// anew (the extension's idle detach) blocks too.
func TestBlockingTabReadsEndWithTheNote(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "tab-new", protocol.TabArgs{Window: "301", URL: "https://news.test/", Block: "images,fonts,media"}); e != nil {
		t.Fatal(e.Msg)
	}
	const note = "Note: images, fonts and media are blocked in this Tab\n"
	ref := "301:" + newTabShort
	out, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: ref})
	if e != nil || !strings.HasSuffix(out, note) {
		t.Errorf("get-window-state: %q %+v", out, e)
	}
	out, e = r.as(t, "claude", "get-window-state", map[string]any{"window": ref, "screenshot": true})
	if e != nil || !strings.HasSuffix(out, note) || strings.Count(out, note) != 1 {
		t.Errorf("get-window-state --screenshot: %q %+v", out, e)
	}
	raw, _ := json.Marshal(protocol.ClickArgs{Window: ref, ElementIndex: idx(elSignIn)})
	ctx := withObserve(withAgent(context.Background(), agent.Identity{ID: "claude", Label: "claude"}), &protocol.Observe{Mode: protocol.ObserveFull})
	out, e = r.cmds["click"](ctx, raw)
	if e != nil || !strings.HasPrefix(out, "ok") || !strings.HasSuffix(out, note) || strings.Count(out, note) != 1 {
		t.Errorf("click --observe: %q %+v", out, e)
	}
	// An Action without --observe prints no note.
	out, e = r.as(t, "claude", "click", protocol.ClickArgs{Window: ref, ElementIndex: idx(elSignIn)})
	if e != nil || strings.Contains(out, "blocked") {
		t.Errorf("click: %q %+v", out, e)
	}
	before := len(r.blockCalls(t))
	if before < 5 {
		t.Errorf("the block was not sent again with each command: %+v", r.blockCalls(t))
	}

	// The extension detached the Tab (its idle detach): the next command
	// attaches again and blocks there again.
	r.ext.Detach(newTabTarget, "target_closed")
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: ref}); e != nil {
		t.Fatal(e.Msg)
	}
	calls := r.blockCalls(t)
	if len(calls) <= before || calls[len(calls)-1].session != newTabTarget || len(calls[len(calls)-1].patterns) != 17 {
		t.Errorf("after the detach: %+v", calls)
	}

	// Another Tab's reads carry no note.
	if out, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil || strings.Contains(out, "blocked") {
		t.Errorf("a Tab that blocks nothing: %q %+v", out, e)
	}
	r.networkOnlyOn(t, newTabTarget)
	r.usersTabUntouched(t)
}

// TestNavigateBlockOnATabWinuseOpened: navigate --block on a Tab tab new
// opened changes what it blocks before the navigation; --block none turns
// it off, and its reads end without the note.
func TestNavigateBlockOnATabWinuseOpened(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "tab-new", protocol.TabArgs{Window: "301", URL: "https://news.test/"}); e != nil {
		t.Fatal(e.Msg)
	}
	ref := "301:" + newTabShort
	out, e := r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: ref, URL: "https://news.test/2", Block: "images"})
	if e != nil || out != "ok https://news.test/2\n" {
		t.Fatalf("navigate --block images: %q %+v", out, e)
	}
	calls := r.blockCalls(t)
	if len(calls) != 1 || calls[0].session != newTabTarget || !samePatterns(calls[0].patterns, imagePatterns) {
		t.Errorf("blocked: %+v", calls)
	}
	order := r.methodOrder()
	if b, n := indexOf(order, "Network.setBlockedURLs"), indexOf(order, "Page.navigate"); b < 0 || b > n {
		t.Errorf("the block came after the navigation: %v", order)
	}
	if out, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: ref}); e != nil || !strings.HasSuffix(out, "Note: images are blocked in this Tab\n") {
		t.Errorf("read: %q %+v", out, e)
	}

	// Another Agent's commands on the Tab see the same block.
	if out, e := r.as(t, "codex", "get-window-state", protocol.GetWindowArgs{Window: ref}); e != nil || !strings.HasSuffix(out, "Note: images are blocked in this Tab\n") {
		t.Errorf("another Agent's read: %q %+v", out, e)
	}

	out, e = r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: ref, URL: "https://news.test/3", Block: "none"})
	if e != nil || out != "ok https://news.test/3\n" {
		t.Fatalf("navigate --block none: %q %+v", out, e)
	}
	calls = r.blockCalls(t)
	if last := calls[len(calls)-1]; last.session != newTabTarget || len(last.patterns) != 0 {
		t.Errorf("--block none sent %+v", last)
	}
	// Network goes off again after the emptied list.
	order = r.methodOrder()
	if d, b := lastIndexOf(order, "Network.disable"), lastIndexOf(order, "Network.setBlockedURLs"); d < 0 || d < b {
		t.Errorf("--block none did not end with Network.disable: %v", order)
	}
	n := r.networkCalls()
	if out, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: ref}); e != nil || strings.Contains(out, "blocked") {
		t.Errorf("read after --block none: %q %+v", out, e)
	}
	// --block none on a Tab that blocks nothing sends nothing.
	if out, e := r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: ref, URL: "https://news.test/4", Block: "none"}); e != nil || out != "ok https://news.test/4\n" {
		t.Errorf("navigate --block none again: %q %+v", out, e)
	}
	if got := r.networkCalls(); got != n {
		t.Errorf("a Tab that blocks nothing got %d more Network calls", got-n)
	}
	r.networkOnlyOn(t, newTabTarget)
	r.usersTabUntouched(t)
}

// TestBlockRefusedOnTheUsersTabs: --block on a Tab winuse did not open in
// the user's browser, or on a Window, is refused before anything reaches
// the Tab, and the hint says to open a Tab with tab new.
func TestBlockRefusedOnTheUsersTabs(t *testing.T) {
	r := newBgRig(t)
	for _, ref := range []string{"301:e100", "301:e300"} {
		_, e := r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: ref, URL: "https://x.test/", Block: "images"})
		if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) || !strings.Contains(e.Msg, "--block") ||
			!strings.Contains(e.Hint, "winuse tab new 301 ") || e.Delivery != protocol.DeliveryNotDelivered {
			t.Errorf("navigate %s --block: %+v", ref, e)
		}
	}
	// A Batch's navigate step is checked the same way.
	_, e := r.as(t, "claude", "batch", protocol.BatchArgs{Window: "301:e300", Steps: []protocol.Step{
		{Kind: "navigate", Args: json.RawMessage(`{"url":"https://x.test/","block":"fonts"}`)},
	}})
	if e == nil || !strings.Contains(e.Msg, "--block") || !strings.Contains(e.Hint, "winuse tab new 301 ") {
		t.Errorf("batch navigate --block: %+v", e)
	}
	// navigate on a Window names the Tab it shows, the user's.
	_, e = r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: "301", URL: "https://x.test/", Block: "images"})
	if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) || !strings.Contains(e.Hint, "winuse tab new 301 ") {
		t.Errorf("navigate 301 --block: %+v", e)
	}
	if calls := r.blockCalls(t); len(calls) != 0 {
		t.Errorf("blocked: %+v", calls)
	}
	for _, target := range []string{bgInbox, bgShop} {
		if got := r.sent(target, ""); len(got) != 0 {
			t.Errorf("tab %s got %v", target[:8], got)
		}
	}
	r.mu.Lock()
	went := len(r.navigated)
	r.mu.Unlock()
	if went != 0 {
		t.Errorf("navigated: %v", r.navigated)
	}
	if n := r.networkCalls(); n != 0 {
		t.Errorf("%d Network calls: %v", n, r.methodOrder())
	}
	r.noUserImpact(t)

	// back, forward and reload take no --block.
	if _, e := r.as(t, "claude", "reload", protocol.NavigateArgs{Window: "301:e200", Block: "images"}); e == nil || !strings.Contains(e.Msg, "--block") {
		t.Errorf("reload --block: %+v", e)
	}
}

// TestBlockOnALaunchedBrowsersTab: on a browser winuse launched or cloned
// every Tab is winuse's, so navigate --block takes any of them.
func TestBlockOnALaunchedBrowsersTab(t *testing.T) {
	dir := t.TempDir()
	old := pageDownloadDir
	pageDownloadDir = func() (string, error) { return dir, nil } // a launched browser's downloads, kept out of the user's folders
	t.Cleanup(func() { pageDownloadDir = old })
	for _, mode := range []browser.Mode{browser.ModeLaunch, browser.ModeClone} {
		r := newBgRig(t)
		r.svc.Pages.(*livePages).browsers.(pageConns).conn.Mode = mode
		out, e := r.as(t, "claude", "navigate", protocol.NavigateArgs{Window: "301:e300", URL: "https://shop.test/2", Block: "fonts"})
		if e != nil || out != "ok https://shop.test/2\n" {
			t.Errorf("%s: navigate --block: %q %+v", mode, out, e)
		}
		if calls := r.blockCalls(t); len(calls) != 1 || calls[0].session != bgShop || !samePatterns(calls[0].patterns, fontPatterns) {
			t.Errorf("%s: blocked %+v", mode, calls)
		}
		r.networkOnlyOn(t, bgShop)
		r.svc.Pages.(*livePages).browsers.(pageConns).conn.Mode = browser.ModeExtension // the rig's cleanup
	}
}
