package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StoreApp is a packaged (Store) App from the Start-apps listing.
type StoreApp struct {
	Name  string // display name
	AppID string // App User Model ID (contains "!")
}

// storeTimeout bounds one Get-StartApps run so a stuck PowerShell cannot
// eat the whole list-apps budget.
const storeTimeout = 6 * time.Second

// GetStartApps lists packaged Start apps by running PowerShell's
// Get-StartApps once — the interim shortcut for enumerating the Apps
// folder (a COM enumeration can replace it later). It costs about a
// second, so callers cache the result (see StoreSource). Real subprocess;
// read-only, no window, no input.
func GetStartApps(ctx context.Context) ([]StoreApp, error) {
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		"[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Get-StartApps | ConvertTo-Json -Compress")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return ParseStartApps(out)
}

// ParseStartApps parses `Get-StartApps | ConvertTo-Json` output: one JSON
// object or an array of {Name, AppID}. Only packaged Apps are kept (their
// AppID is an App User Model ID and contains "!"); classic Apps' entries
// add nothing over the Start-menu shortcut walk.
func ParseStartApps(data []byte) ([]StoreApp, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var rows []startAppJSON
	if data[0] == '[' {
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
	} else {
		var one startAppJSON
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, err
		}
		rows = append(rows, one)
	}
	seen := map[string]bool{}
	var out []StoreApp
	for _, r := range rows {
		if r.Name == "" || !strings.Contains(r.AppID, "!") {
			continue
		}
		key := strings.ToLower(r.AppID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, StoreApp{Name: r.Name, AppID: r.AppID})
	}
	return out, nil
}

type startAppJSON struct {
	Name  string `json:"Name"`
	AppID string `json:"AppID"`
}

// StoreSource caches the Start-apps listing: Get-StartApps is a ~1 s
// subprocess, so it is computed lazily on first use and remembered for the
// helper's lifetime. A failed run is retried on the next call.
type StoreSource struct {
	// Run lists the apps (GetStartApps by default; tests stub it).
	Run func(ctx context.Context) ([]StoreApp, error)

	mu   sync.Mutex
	apps []StoreApp
	done bool
}

// Apps returns the listing, running it once if needed. Errors mean the
// listing is unavailable (PowerShell missing or blocked); callers treat
// that as best-effort and carry on without packaged Apps.
func (s *StoreSource) Apps(ctx context.Context) ([]StoreApp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return s.apps, nil
	}
	if s.Run == nil {
		return nil, nil
	}
	apps, err := s.Run(ctx)
	if err != nil {
		return nil, err // not cached: the next call retries
	}
	s.apps, s.done = apps, true
	return apps, nil
}
