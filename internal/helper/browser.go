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

// browserCommands are the registry entries.
func browserCommands(s Services) map[string]Command {
	b := s.Browser
	return map[string]Command{
		"browser-status":  browserNamed("browser-status", b.Status),
		"browser-use":     browserNamed("browser-use", b.Use),
		"browser-launch":  browserNamed("browser-launch", b.Launch),
		"browser-clone":   browserClone(s),
		"browser-connect": browserNamed("browser-connect", b.Connect),
		"browser-stop":    browserNamed("browser-stop", b.Stop),
		// The winuse extension (browser merge ticket 02).
		"browser-ext-install": browserNamed("browser-ext-install", b.ExtInstall),
		"browser-ext-status":  browserNamed("browser-ext-status", b.ExtStatus),
	}
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
		return Fail(err.Error(), "")
	}
	return Fail(err.Error(), "run winuse browser status")
}

func browserUsageHint(cmd string) string {
	sub := strings.Replace(cmd[len("browser-"):], "ext-", "ext ", 1)
	if sub == "ext install" {
		return "usage: winuse browser ext install [browser|all]"
	}
	return "usage: winuse browser " + sub + " [browser]"
}
