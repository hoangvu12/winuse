package helper

// The Browser Connection commands (browser merge tickets 01 and 02):
// winuse browser status|use|launch|clone|connect|stop|ext install|ext
// status. The work lives in internal/browser; these handlers parse the
// arguments, call the service and map a *browser.Problem to its one
// sentence and next command.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// BrowserService is the helper's Browser Connections: one persistent
// connection per browser process for the helper's lifetime. The real one
// is *browser.Manager (helper.Main builds it over browser.Live and closes
// it on exit); tests fake it so no browser is probed, started or dialed.
// Ticket 04's Window State reaches a Window's connection by asserting
// *browser.Manager (its WindowConnection method, pageBrowsers).
type BrowserService interface {
	Status(ctx context.Context, name string) (string, error)
	Use(ctx context.Context, name string) (string, error)
	Launch(ctx context.Context, name string) (string, error)
	Clone(ctx context.Context, opts browser.CloneOptions) (string, error)
	Connect(ctx context.Context, name string) (string, error)
	Stop(ctx context.Context, name string) (string, error)
	ExtInstall(ctx context.Context, name string) (string, error)
	ExtStatus(ctx context.Context, name string) (string, error)
}

// browserNamed builds the handler of a command that takes only an
// optional browser name.
func browserNamed(cmd string, call func(ctx context.Context, name string) (string, error)) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.BrowserArgs
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &a); err != nil {
				return "", Fail(cmd+": invalid arguments: "+err.Error(), browserUsageHint(cmd))
			}
		}
		out, err := call(ctx, a.Browser)
		if err != nil {
			return "", browserError(err)
		}
		return out, nil
	}
}

// browserClone is browser clone's handler.
func browserClone(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.BrowserCloneArgs
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &a); err != nil {
				return "", Fail("browser-clone: invalid arguments: "+err.Error(), "usage: winuse browser clone [browser] [--profile <folder>] [--fresh] [--close-source]")
			}
		}
		out, err := s.Browser.Clone(ctx, browser.CloneOptions{Browser: a.Browser, Profile: a.Profile, Fresh: a.Fresh, CloseSource: a.CloseSource})
		if err != nil {
			return "", browserError(err)
		}
		return out, nil
	}
}

// extOpener is the BrowserService's optional --open side of ext install
// (browser-ux 02): *browser.Manager.
type extOpener interface {
	ExtOpen(ctx context.Context, name string) (string, error)
}

// browserExtInstall is browser ext install's handler: the install, then,
// with --open, the extensions page and the folder opened for the user.
func browserExtInstall(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.BrowserExtArgs
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &a); err != nil {
				return "", Fail("browser-ext-install: invalid arguments: "+err.Error(), browserUsageHint("browser-ext-install"))
			}
		}
		out, err := s.Browser.ExtInstall(ctx, a.Browser)
		if err != nil {
			return "", browserError(err)
		}
		if a.Open {
			o, ok := s.Browser.(extOpener)
			if !ok {
				return out + notePrefix + "this helper cannot open the extensions page; the user opens it\n", nil
			}
			more, err := o.ExtOpen(ctx, a.Browser)
			if err != nil {
				return "", browserError(err)
			}
			out += more
		}
		return out, nil
	}
}

// browserCommands are the registry entries.
func browserCommands(s Services) map[string]Command {
	b := s.Browser
	return map[string]Command{
		"browser-status":  browserNamed("browser-status", s.browserStatus),
		"browser-use":     browserNamed("browser-use", b.Use),
		"browser-launch":  browserNamed("browser-launch", b.Launch),
		"browser-clone":   browserClone(s),
		"browser-connect": browserNamed("browser-connect", b.Connect),
		"browser-stop":    browserNamed("browser-stop", b.Stop),
		// The winuse extension (browser merge ticket 02).
		"browser-ext-install": browserExtInstall(s),
		"browser-ext-status":  browserNamed("browser-ext-status", b.ExtStatus),
		// The attach to a running Electron App (attach.go, headless-max 15).
		"browser-attach": browserAttach(s),
		"browser-detach": browserDetach(s),
	}
}

// namedConnections is the BrowserService's lookup of the connection a
// browser command resolves: *browser.Manager.
type namedConnections interface {
	Connection(ctx context.Context, name string) (*browser.Connection, error)
}

// browserStatus is browser status: the service's report, plus a note with
// the folder winuse saves downloads in when the connection is to a browser
// winuse launched or cloned. Download tracking is turned on here, if no
// page Action did yet, so the note is true from now on (pageDownloads).
func (s Services) browserStatus(ctx context.Context, name string) (string, error) {
	out, err := s.Browser.Status(ctx, name)
	if err != nil {
		return "", err
	}
	nc, ok := s.Browser.(namedConnections)
	if !ok {
		return out, nil
	}
	c, err := nc.Connection(ctx, name)
	if err != nil {
		return out, nil
	}
	if d := s.pageDownloads(ctx, c); d != nil {
		out += "note: downloads go to " + d.Dir + " (winuse wait <window> download prints each file's path)\n"
	}
	return out, nil
}

// browserError maps a Browser Connection failure to the user-facing line:
// a Problem's one sentence with its next command as the hint (the
// extension's refusals and its off switch become Problems too), anything
// else as is. A context deadline stays plain, so the server's timeout
// policy reports it.
func browserError(err error) *Error {
	if p, ok := browser.AsProblem(err, ""); ok {
		return Fail(p.Sentence(), p.Next())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Fail(err.Error(), "retry; run winuse browser status if it keeps timing out")
	}
	return Fail(err.Error(), "run winuse browser status")
}

func browserUsageHint(cmd string) string {
	sub := strings.Replace(cmd[len("browser-"):], "ext-", "ext ", 1)
	if sub == "ext install" {
		return "usage: winuse browser ext install [browser|all] [--open]"
	}
	return "usage: winuse browser " + sub + " [browser]"
}
