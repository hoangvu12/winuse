package page

// The Tabs of a browser Window (browser merge ticket 07): listing the Tabs
// in the same browser window as the Window's active Tab, their short ids,
// and the CDP calls the tab commands make. Nothing here calls
// Page.bringToFront or Target.activateTarget: a Tab is made active through
// the extension's Winuse.activateTab, or with real keys by the helper.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
)

// TabIDLength is the length of a Tab's short id: the first 8 characters
// of its target id, lowercased, as use-browser printed them. Target ids
// are 32 hex characters, so 8 stay unique within one browser in practice.
const TabIDLength = 8

// MinTabRef is the shortest tab id prefix the tab commands accept.
const MinTabRef = 4

// TabID is a Tab's short id.
func TabID(targetID string) string {
	id := strings.ToLower(targetID)
	if len(id) > TabIDLength {
		id = id[:TabIDLength]
	}
	return id
}

// WindowTabs are the Tabs of one browser window.
type WindowTabs struct {
	WindowID int
	Active   Tab   // the Tab the Window shows (MatchTab)
	Tabs     []Tab // every Tab of that browser window, the active one included, in the browser's order
}

// IsActive reports whether t is the Window's active Tab.
func (w WindowTabs) IsActive(t Tab) bool {
	if t.Active != nil {
		return *t.Active
	}
	return t.TargetID == w.Active.TargetID
}

// ListWindowTabs finds the Tab the browser Window g shows (FindTab), then
// lists every Tab in the same browser window. Through the extension the
// window comes from each tab's windowId (chrome.tabs), in tab strip
// order, with chrome.tabs' active flag. Otherwise it comes from
// Browser.getWindowForTarget per Tab, in the order Target.getTargets
// lists them, and the active Tab is the one FindTab matched.
func ListWindowTabs(ctx context.Context, conn *browser.Connection, g Geometry) (WindowTabs, error) {
	active, err := FindTab(ctx, conn, g)
	if err != nil {
		return WindowTabs{}, err
	}
	all, err := pageTabs(ctx, conn.Client)
	if err != nil {
		return WindowTabs{}, err
	}
	out := WindowTabs{WindowID: active.WindowID, Active: active}
	for _, t := range all {
		if conn.Mode != browser.ModeExtension {
			if t.TargetID != active.TargetID {
				w, err := windowForTarget(ctx, conn.Client, t.TargetID)
				if err != nil {
					if ctx.Err() != nil {
						return WindowTabs{}, err
					}
					continue // the Tab went away meanwhile
				}
				t.WindowID = w.id
			} else {
				t.WindowID = active.WindowID
			}
		}
		if t.WindowID == active.WindowID {
			out.Tabs = append(out.Tabs, t)
		}
	}
	return out, nil
}

// pageTabs lists the connection's Tabs (IsTabTarget), browser pages marked
// Internal, with the extension's windowId and active fields when it sends
// them.
func pageTabs(ctx context.Context, c *cdp.Client) ([]Tab, error) {
	res, err := c.Call(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			TabID    *int   `json:"tabId"`
			WindowID int    `json:"windowId"`
			Active   *bool  `json:"active"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var tabs []Tab
	for _, t := range r.TargetInfos {
		tab, internal := IsTabTarget(t.Type, t.URL, t.TabID != nil)
		if !tab {
			continue
		}
		tabs = append(tabs, Tab{TargetID: t.TargetID, Title: t.Title, URL: t.URL, WindowID: t.WindowID, Active: t.Active, Internal: internal})
	}
	return tabs, nil
}

// browserPageSchemes are the schemes of the browser's own pages that open
// in a tab of the tab strip: the New Tab page, settings, history,
// downloads, and the like.
var browserPageSchemes = []string{"chrome://", "edge://", "brave://", "vivaldi://", "opera://"}

// IsTabTarget reports whether a target is a Tab of the tab strip, and
// whether that Tab shows a browser page (internal) rather than a site.
//
//   - A page target that is no browser page (browser.IsInternal) is a Tab.
//   - Through the extension (hasTabID: chrome.tabs gave it a tabId) every
//     page target is a Tab: background.js lists only chrome.tabs' tabs, so
//     an extension's page opened in a tab counts, and DevTools windows and
//     popups never appear.
//   - Otherwise a browser page is a Tab only under the browser's own
//     scheme (chrome://, edge://, ...), and not the WebUI that Chrome draws
//     in its frame (hosts ending in .top-chrome: tab search, side panels,
//     the omnibox popup). devtools://, chrome-extension:// and
//     chrome-untrusted:// targets are DevTools windows, extension pages
//     and parts of other pages (Vivaldi's own UI is a chrome-extension://
//     page), which CDP cannot tell from tabs, so they stay out.
func IsTabTarget(typ, url string, hasTabID bool) (tab, internal bool) {
	if typ != "page" {
		return false, false
	}
	if !browser.IsInternal(url) {
		return true, false
	}
	if hasTabID {
		return true, true
	}
	for _, s := range browserPageSchemes {
		if rest, ok := strings.CutPrefix(url, s); ok {
			host, _, _ := strings.Cut(rest, "/")
			return !strings.HasSuffix(strings.ToLower(host), ".top-chrome"), true
		}
	}
	return false, true
}

// TabRefError says a tab id names no Tab of the window, or several.
type TabRefError struct {
	Ref     string
	Matches []Tab // several: the Tabs the prefix matched
	Short   bool  // the prefix is shorter than MinTabRef
}

func (e *TabRefError) Error() string {
	switch {
	case e.Short:
		return fmt.Sprintf("tab id %q is too short: use at least %d characters of an id from tab list", e.Ref, MinTabRef)
	case len(e.Matches) > 1:
		var ids []string
		for _, t := range e.Matches {
			ids = append(ids, fmt.Sprintf("%s %q", TabID(t.TargetID), Collapse(t.Title)))
		}
		return fmt.Sprintf("tab id %q matches %d tabs (%s); use more characters", e.Ref, len(e.Matches), strings.Join(ids, ", "))
	}
	return fmt.Sprintf("no tab has the id %q", e.Ref)
}

// ResolveTab finds the one Tab whose target id starts with ref, ignoring
// case. A prefix shorter than MinTabRef, one that matches nothing and one
// that matches several Tabs are a *TabRefError; nothing is guessed.
func ResolveTab(tabs []Tab, ref string) (Tab, error) {
	ref = strings.TrimSpace(ref)
	if len(ref) < MinTabRef {
		return Tab{}, &TabRefError{Ref: ref, Short: true}
	}
	var hits []Tab
	for _, t := range tabs {
		if strings.HasPrefix(strings.ToLower(t.TargetID), strings.ToLower(ref)) {
			hits = append(hits, t)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return Tab{}, &TabRefError{Ref: ref, Matches: hits}
}

// OtherWindowError says a new Tab opened in another browser window than
// the one asked for.
type OtherWindowError struct {
	TargetID string
	WindowID int
}

func (e *OtherWindowError) Error() string {
	return fmt.Sprintf("the new tab %s opened in browser window %d", TabID(e.TargetID), e.WindowID)
}

// NewTab opens a Tab in the background of browser window windowID and
// returns its target id.
//
// Through the extension, chrome.tabs.create takes the windowId, with
// active:false, and groups the tab under "winuse". CDP's
// Target.createTarget has no window parameter: with newWindow:false
// Chrome puts the tab in the profile's last active browser window. So on
// a launched, cloned or toggled browser the new Tab's window is read
// back with Browser.getWindowForTarget, and a Tab that landed in another
// window is an *OtherWindowError (the Tab is still open; the caller
// decides what to do with it).
func NewTab(ctx context.Context, conn *browser.Connection, windowID int, url string) (string, error) {
	params := map[string]any{"url": tabURL(url), "background": true}
	if conn.Mode == browser.ModeExtension {
		params["windowId"] = windowID
	} else {
		params["newWindow"] = false
	}
	res, err := conn.Client.Call(ctx, "Target.createTarget", params)
	if err != nil {
		return "", err
	}
	var r struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", err
	}
	if r.TargetID == "" {
		return "", errors.New("the browser returned no target id for the new tab")
	}
	got, err := TabWindow(ctx, conn, r.TargetID)
	if err != nil {
		return r.TargetID, err
	}
	if got != windowID {
		return r.TargetID, &OtherWindowError{TargetID: r.TargetID, WindowID: got}
	}
	return r.TargetID, nil
}

// ErrTabGone says a target is no longer open.
var ErrTabGone = errors.New("the tab is no longer open")

// TabWindow reads the browser window a Tab is in: the extension's windowId,
// or Browser.getWindowForTarget.
func TabWindow(ctx context.Context, conn *browser.Connection, targetID string) (int, error) {
	if conn.Mode != browser.ModeExtension {
		w, err := windowForTarget(ctx, conn.Client, targetID)
		if err != nil {
			return 0, err
		}
		return w.id, nil
	}
	t, err := findTab(ctx, conn.Client, targetID)
	if err != nil {
		return 0, err
	}
	return t.WindowID, nil
}

// findTab is one page Tab by target id, or ErrTabGone.
func findTab(ctx context.Context, c *cdp.Client, targetID string) (Tab, error) {
	tabs, err := pageTabs(ctx, c)
	if err != nil {
		return Tab{}, err
	}
	for _, t := range tabs {
		if t.TargetID == targetID {
			return t, nil
		}
	}
	return Tab{}, ErrTabGone
}

// TabOpen reports whether a target is still open.
func TabOpen(ctx context.Context, c *cdp.Client, targetID string) (bool, error) {
	_, err := findTab(ctx, c, targetID)
	if errors.Is(err, ErrTabGone) {
		return false, nil
	}
	return err == nil, err
}

// CloseTab asks the browser to close a Tab (Target.closeTarget, which the
// extension turns into chrome.tabs.remove). A page may keep itself open
// to ask whether to leave; TabOpen tells.
func CloseTab(ctx context.Context, c *cdp.Client, targetID string) error {
	_, err := c.Call(ctx, "Target.closeTarget", map[string]any{"targetId": targetID})
	return err
}

// ActivateTabThroughExtension makes a Tab the selected one in its window
// through the extension's Winuse.activateTab (chrome.tabs.update active),
// which never focuses or raises the window.
func ActivateTabThroughExtension(ctx context.Context, c *cdp.Client, targetID string) error {
	_, err := c.Call(ctx, bridge.MethodActivateTab, map[string]any{"targetId": targetID})
	return err
}

// NavigateTabThroughExtension opens url in a Tab through the extension's
// Winuse.navigateTab (chrome.tabs.update url), which never focuses or
// raises the window and leaves the selected Tab alone. It is how a Tab
// that shows a browser page is navigated there: chrome.debugger cannot
// attach to it, so Page.navigate cannot reach it.
func NavigateTabThroughExtension(ctx context.Context, c *cdp.Client, targetID, url string) error {
	_, err := c.Call(ctx, bridge.MethodNavigateTab, map[string]any{"targetId": targetID, "url": url})
	return err
}

// TabNow is a Tab as the browser lists it now (its URL and title), or
// ErrTabGone.
func TabNow(ctx context.Context, c *cdp.Client, targetID string) (Tab, error) {
	return findTab(ctx, c, targetID)
}

// ExtensionTabActive reads chrome.tabs' active flag of a Tab through the
// extension's Target.getTargets.
func ExtensionTabActive(ctx context.Context, c *cdp.Client, targetID string) (bool, error) {
	t, err := findTab(ctx, c, targetID)
	if err != nil {
		return false, err
	}
	return t.Active != nil && *t.Active, nil
}

// Visibility reads document.visibilityState of Tabs over sessions it
// attaches on first use: "visible" for the selected Tab of a browser
// window on screen, "hidden" for a background Tab. It is how a switch on
// a launched or cloned browser is verified, after the Window was brought
// to the foreground. Close detaches its sessions.
type Visibility struct {
	conn     *browser.Connection
	sessions map[string]string
}

// NewVisibility builds a Visibility over a connection.
func NewVisibility(conn *browser.Connection) *Visibility {
	return &Visibility{conn: conn, sessions: map[string]string{}}
}

// Visible reports whether the Tab's document is visible.
func (v *Visibility) Visible(ctx context.Context, targetID string) (bool, error) {
	session, ok := v.sessions[targetID]
	if !ok {
		s, err := v.conn.AttachPage(ctx, targetID)
		if err != nil {
			return false, err
		}
		session = s
		v.sessions[targetID] = s
	}
	res, err := v.conn.Client.CallSession(ctx, session, "Runtime.evaluate", map[string]any{
		"expression":    "document.visibilityState",
		"returnByValue": true,
	})
	if err != nil {
		return false, err
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return false, err
	}
	return r.Result.Value == "visible", nil
}

// Close detaches every session Visible attached.
func (v *Visibility) Close(ctx context.Context) {
	for _, s := range v.sessions {
		v.conn.Client.Call(ctx, "Target.detachFromTarget", map[string]any{"sessionId": s})
	}
	v.sessions = map[string]string{}
}

// tabURL is the URL a new Tab opens: about:blank when none is given, and
// https:// in front of an address without a scheme (use-browser's rule).
func tabURL(u string) string {
	u = strings.TrimSpace(u)
	switch {
	case u == "":
		return "about:blank"
	case strings.Contains(u, "://"), strings.HasPrefix(u, "about:"):
		return u
	}
	return "https://" + u
}
