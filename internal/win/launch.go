package win

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LaunchOptions are how winuse starts an App's executable (headless-max
// ticket 01): its arguments, working directory and environment overrides,
// and whether its first Window shows minimized.
type LaunchOptions struct {
	Args      []string // each one quoted onto the command line
	Dir       string   // the working directory; "" is the executable's folder
	Env       []string // KEY=VALUE entries over the helper's environment
	Minimized bool     // SW_SHOWMINNOACTIVE instead of SW_SHOWNOACTIVATE
}

// LaunchShowState is the show state a launch hands the App (STARTUPINFO's
// wShowWindow, ShellExecuteEx's nShow): never one that activates. The App
// applies it to its first Window; an App can still activate itself, and
// the caller compares the foreground afterwards.
func LaunchShowState(minimized bool) uint16 {
	if minimized {
		return swShowMinNoActive
	}
	return swShowNoActivate
}

// createUnicodeEnvironment marks a UTF-16 environment block.
const createUnicodeEnvironment = 0x00000400

// launchCall is one CreateProcess call, built without touching the system
// so its parts can be tested offline.
type launchCall struct {
	app     string   // lpApplicationName: the absolute executable path
	cmdLine string   // the quoted executable, then the quoted arguments
	dir     string   // lpCurrentDirectory
	env     []string // the whole environment; nil inherits the helper's
	show    uint16   // STARTUPINFO.wShowWindow (with STARTF_USESHOWWINDOW)
}

// buildLaunch builds the CreateProcess call for path with opts over the
// base environment.
func buildLaunch(path string, opts LaunchOptions, base []string) (launchCall, error) {
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	parts := make([]string, 0, len(opts.Args)+1)
	parts = append(parts, windows.EscapeArg(path))
	for _, a := range opts.Args {
		if strings.ContainsRune(a, 0) {
			return launchCall{}, errors.New("an argument contains a NUL character")
		}
		parts = append(parts, windows.EscapeArg(a))
	}
	c := launchCall{
		app:     path,
		cmdLine: strings.Join(parts, " "),
		dir:     opts.Dir,
		show:    LaunchShowState(opts.Minimized),
	}
	if c.dir == "" {
		c.dir = filepath.Dir(path)
	}
	if len(opts.Env) > 0 {
		env, err := MergeEnv(base, opts.Env)
		if err != nil {
			return launchCall{}, err
		}
		c.env = env
	}
	return c, nil
}

// envKey is the name of a KEY=VALUE entry. Windows keeps per-drive
// directories as "=C:=C:\dir", so a leading '=' belongs to the name.
func envKey(entry string) string {
	if entry == "" {
		return ""
	}
	if i := strings.IndexByte(entry[1:], '='); i >= 0 {
		return entry[:i+1]
	}
	return entry
}

// ParseEnvEntry checks one --env entry: a non-empty name without '=', then
// '=' and the value (which may be empty or hold '=').
func ParseEnvEntry(entry string) (key, value string, err error) {
	key, value, ok := strings.Cut(entry, "=")
	if !ok || key == "" {
		return "", "", fmt.Errorf("--env %q: want KEY=VALUE", entry)
	}
	if strings.ContainsRune(entry, 0) {
		return "", "", fmt.Errorf("--env %q: contains a NUL character", entry)
	}
	return key, value, nil
}

// MergeEnv sets the KEY=VALUE overrides over base: an override replaces the
// entry of the same name in place (names compare without case, as Windows
// does), and a new name is appended.
func MergeEnv(base, overrides []string) ([]string, error) {
	out := append([]string(nil), base...)
	for _, o := range overrides {
		key, _, err := ParseEnvEntry(o)
		if err != nil {
			return nil, err
		}
		replaced := false
		for i, e := range out {
			if strings.EqualFold(envKey(e), key) {
				out[i] = o
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, o)
		}
	}
	return out, nil
}

// envBlock is a CREATE_UNICODE_ENVIRONMENT block: each entry, NUL-ended,
// then one more NUL.
func envBlock(env []string) []uint16 {
	var b []uint16
	for _, e := range env {
		b = append(b, utf16.Encode([]rune(e))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0) // an empty block still ends in two NULs
	}
	return append(b, 0)
}

// StartProcess starts an executable as a launched App: a direct
// CreateProcess with STARTF_USESHOWWINDOW and a non-activating show state
// (Go's SysProcAttr can only hide the window). Its creation flags (spawn.go)
// put the App in a new process group, so it does not share the helper's
// Ctrl-C group, and break it away from the helper's job, so it survives a
// harness job being closed (retried without breakaway when the job forbids
// it). No DETACHED_PROCESS and no hidden window: a launched App is supposed
// to appear. It returns the new process id and never waits for the App. It
// refuses inside a go test binary (LiveDesktopGuard).
func StartProcess(path string, opts LaunchOptions) (uint32, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	c, err := buildLaunch(path, opts, os.Environ())
	if err != nil {
		return 0, err
	}
	app, err := windows.UTF16PtrFromString(c.app)
	if err != nil {
		return 0, err
	}
	dir, err := windows.UTF16PtrFromString(c.dir)
	if err != nil {
		return 0, err
	}
	var envPtr *uint16
	var extra uint32
	if c.env != nil {
		block := envBlock(c.env)
		envPtr = &block[0]
		extra = createUnicodeEnvironment
	}
	var lastErr error
	for _, flags := range []uint32{createNewProcessGroup | createBreakawayFromJob, createNewProcessGroup} {
		// CreateProcessW may write into the command line, so each try gets
		// a fresh copy.
		cmdLine, err := windows.UTF16FromString(c.cmdLine)
		if err != nil {
			return 0, err
		}
		si := windows.StartupInfo{Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: c.show}
		si.Cb = uint32(unsafe.Sizeof(si))
		var pi windows.ProcessInformation
		if err := windows.CreateProcess(app, &cmdLine[0], nil, nil, false, flags|extra, envPtr, dir, &si, &pi); err != nil {
			lastErr = err
			continue
		}
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(pi.Process)
		return pi.ProcessId, nil
	}
	return 0, lastErr
}
