package page

// Background Tabs (ADR 0017): a Tab an agent names as <window>:<tab> is read
// and acted on over its Browser Connection alone, whether or not its window
// shows it. Here: reading one Tab by target id (no Window mapping), keeping
// a held Tab alive while it is hidden, and capturing it.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
)

// ReadTab reads one Tab's Page by its target id, as Read does for the Tab
// a Window shows: the accessibility tree, the cursor-pointer pass, the
// frames' loader ids and the iframes one level deep. A browser page is
// never read (*InternalPageError).
func (r *Reader) ReadTab(ctx context.Context, conn *browser.Connection, tab Tab) (*Raw, error) {
	if tab.Internal {
		return nil, &InternalPageError{URL: tab.URL}
	}
	raw, err := r.readTab(ctx, conn, tab)
	if err != nil && isGone(err) && ctx.Err() == nil {
		r.drop(tabKey{conn.Client, tab.TargetID})
		raw, err = r.readTab(ctx, conn, tab)
	}
	return raw, err
}

// HoldTab keeps a Tab an agent works in alive while the user looks at
// another one (ADR 0017): focus emulation, so its renderer paints and takes
// input as a visible Tab does (AttachPage turned it on; a release turned it
// off); Page.setWebLifecycleState active, which unfreezes a Tab Energy Saver
// froze; and through the extension Winuse.holdTab, which keeps the browser
// from discarding it and guards the user's selected Tab from the Tabs it
// opens. Each step is best effort; the first failure is returned for the
// log, and the Action goes on.
func HoldTab(ctx context.Context, conn *browser.Connection, session, targetID string) error {
	c := conn.Client
	var errs []error
	if _, err := c.CallSession(ctx, session, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}); err != nil {
		errs = append(errs, err)
	}
	if _, err := c.CallSession(ctx, session, "Page.setWebLifecycleState", map[string]any{"state": "active"}); err != nil {
		errs = append(errs, err)
	}
	if conn.Mode == browser.ModeExtension {
		if _, err := c.Call(ctx, bridge.MethodHoldTab, map[string]any{"targetId": targetID}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ReleaseTab ends a hold: focus emulation off, and through the extension
// the Tab's own autoDiscardable back. A Tab that closed meanwhile fails
// quietly.
func ReleaseTab(ctx context.Context, conn *browser.Connection, session, targetID string) error {
	c := conn.Client
	var errs []error
	if _, err := c.CallSession(ctx, session, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": false}); err != nil {
		errs = append(errs, err)
	}
	if conn.Mode == browser.ModeExtension {
		if _, err := c.Call(ctx, bridge.MethodReleaseTab, map[string]any{"targetId": targetID}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CaptureTab captures the visible part of a Tab's Page as a JPEG through
// Page.captureScreenshot, from its compositor surface, so a background Tab
// with focus emulation on has a frame to give. A hidden Tab's capture can
// hang (anthropics/claude-code#78533): the caller bounds ctx and falls back.
func CaptureTab(ctx context.Context, conn *browser.Connection, session string, quality int) ([]byte, error) {
	res, err := conn.Client.CallSession(ctx, session, "Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": quality, "fromSurface": true,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.Data == "" {
		return nil, errors.New("the browser returned an empty screenshot")
	}
	return base64.StdEncoding.DecodeString(r.Data)
}
