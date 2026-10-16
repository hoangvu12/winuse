// Package pipe owns the named-pipe transport between the winuse CLI and the
// background helper: pipe naming per user/session, the user-restricted
// security descriptor, the single-instance guard, dialing with not-found
// detection, and verifying that a dialed pipe is actually served by a
// winuse helper process of the current user (anti-squatting).
package pipe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/win"
)

// Default pipe layout: \\.\\pipe\winuse-<sid>-<session>[-admin]. Pipe names
// live in one global (machine) namespace, so the user SID and the session id
// keep helpers of different users and sessions apart, and the "-admin" suffix
// keeps an elevated helper separate from a normal one.
const namePrefix = `\\.\pipe\winuse-`

// The single-instance mutex lives in the per-session ("Local\") kernel
// namespace: one helper per user per session per integrity level.
const mutexPrefix = `Local\winuse-helper-`

// buffer sizes per direction; a Window State tree fits comfortably.
const ioBufferSize = 64 << 10

// ErrNoHelper reports that no helper is listening on the pipe.
var ErrNoHelper = errors.New("winuse helper is not running")

// Name returns the helper pipe name for the given identity.
func Name(id win.Identity) string {
	return namePrefix + id.String()
}

// MutexName returns the helper single-instance mutex name for the identity.
func MutexName(id win.Identity) string {
	return mutexPrefix + fmt.Sprintf("%s-%d", id.SID, id.Session) + adminSuffix(id)
}

func adminSuffix(id win.Identity) string {
	if id.Elevated {
		return "-admin"
	}
	return ""
}

// SDDLFor returns the security descriptor that restricts a pipe to the given
// user: a protected DACL with a single ACE granting GENERIC_ALL to the user's
// SID only. No other user can open it; without a mandatory-label ACE the
// object defaults to a medium integrity level with no-write-up, so
// low-integrity (sandboxed) processes of the same user cannot drive the
// helper either.
func SDDLFor(sid string) string {
	return "D:P(A;;GA;;;" + sid + ")"
}

// Listen creates the helper pipe for the given identity with the
// user-restricted DACL. The first instance is created with FILE_CREATE, so a
// second helper listening on the same name fails — a backstop behind the
// single-instance mutex.
func Listen(id win.Identity) (net.Listener, error) {
	return winio.ListenPipe(Name(id), &winio.PipeConfig{
		SecurityDescriptor: SDDLFor(id.SID),
		InputBufferSize:    ioBufferSize,
		OutputBufferSize:   ioBufferSize,
	})
}

// Dial connects to the helper pipe. It returns ErrNoHelper wrapped in the
// error when no helper exists (the caller spawns one then), and otherwise
// retries busy pipes until ctx ends.
func Dial(ctx context.Context, id win.Identity) (net.Conn, error) {
	conn, err := winio.DialPipeContext(ctx, Name(id))
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoHelper, Name(id))
		}
		return nil, err
	}
	return conn, nil
}

// isNotFound reports whether a dial error means "no such pipe" (rather than
// busy or access denied).
func isNotFound(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, os.ErrNotExist) ||
		strings.Contains(err.Error(), "not found")
}

// ServerPID returns the process id of the helper serving a dialed pipe
// connection.
func ServerPID(conn net.Conn) (uint32, error) {
	fder, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("pipe connection does not expose its handle")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(fder.Fd()), &pid); err != nil {
		return 0, fmt.Errorf("identifying helper process: %w", err)
	}
	return pid, nil
}

// VerifyServer checks that the helper serving conn runs as the given user:
// another user could pre-create the pipe name (squatting) and answer with
// fake responses. winio rejects remote clients, and the pipe DACL restricts
// opens to the user, but this check closes the squatting hole for real.
func VerifyServer(conn net.Conn, wantSID string) error {
	pid, err := ServerPID(conn)
	if err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Errorf("helper process %d cannot be opened: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return fmt.Errorf("helper process %d token: %w", pid, err)
	}
	defer tok.Close()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return fmt.Errorf("helper process %d user: %w", pid, err)
	}
	if got := tu.User.Sid.String(); got != wantSID {
		return fmt.Errorf("helper pipe is served by a different user (process %d, SID %s); refusing", pid, got)
	}
	return nil
}

// ErrAlreadyRunning reports that a helper singleton already exists.
var ErrAlreadyRunning = errors.New("winuse helper is already running")

// AcquireSingleton takes the helper single-instance mutex. The loser of a
// start race (two CLIs spawning at once) gets ErrAlreadyRunning and exits.
// The returned release function must be called before process exit; the
// kernel releases the mutex on crash, so there is no stale lock to clear.
func AcquireSingleton(id win.Identity) (release func(), err error) {
	name, err := windows.UTF16PtrFromString(MutexName(id))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, fmt.Errorf("creating helper mutex: %w", err)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h)
		return nil, ErrAlreadyRunning
	}
	return func() { windows.CloseHandle(h) }, nil
}
