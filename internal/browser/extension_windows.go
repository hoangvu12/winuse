package browser

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/win"
)

// NativeHost runs winuse as the extension's native messaging host:
// cmd/winuse calls it when the browser starts winuse with the extension's
// origin (bridge.IsHostLaunch). It returns the process exit code.
func NativeHost(origin string) int {
	return bridge.HostMain(origin, Live().BridgesDir())
}

// liveOpenBridge connects to a native host. It refuses inside a go test
// binary, like every other dial.
func liveOpenBridge(ctx context.Context, b bridge.Info) (cdp.Transport, error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return nil, err
	}
	return bridge.Dial(ctx, b.Addr(), b.Token)
}

// liveExecutable is the running winuse binary, symlinks resolved.
func liveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// liveRegistry is HKCU. Writes refuse inside a go test binary, so no test
// registers a real native host.
type liveRegistry struct{}

func (liveRegistry) SetDefault(key, value string) error {
	if err := win.LiveDesktopGuard(); err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("", value)
}

func (liveRegistry) Default(key string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return v, err
}
