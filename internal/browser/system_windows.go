package browser

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/win"
)

// Live is the real System. Every call that starts or closes a process,
// probes a debugging port or dials one refuses inside a go test binary
// (win.LiveDesktopGuard): offline tests must fake the System, and a dial
// that slipped through could raise the user's Chrome approval prompt.
func Live() *System {
	local := os.Getenv("LOCALAPPDATA")
	s := &System{
		Env: Env{
			LocalAppData:    local,
			AppData:         os.Getenv("APPDATA"),
			ProgramFiles:    os.Getenv("ProgramFiles"),
			ProgramFilesX86: os.Getenv("ProgramFiles(x86)"),
		},
		StateDir:     filepath.Join(local, "winuse", "browser"),
		Exists:       fileExists,
		VersionWS:    liveVersionWS,
		PortAlive:    livePortAlive,
		ListenerPID:  listenerPID,
		ProcessImage: processImage,
		Running:      processRunning,
		Start:        startDetached,
		Foreground:   foregroundOwner,
		CloseApp:     closeApp,
		FreePort:     freeDebugPort,
		AppPort:      ephemeralPort,
		ParentPID:    parentPID,

		HasNodeDebug:   win.NodeDebugHandler,
		NodeDebug:      win.SignalNodeDebug,
		ListeningPorts: listeningPorts,
		InspectorWS:    liveInspectorWS,
		Dial:           liveDial,
		OpenBridge:     liveOpenBridge,
		Registry:       liveRegistry{},
		Executable:     liveExecutable,
		Now:            time.Now,
		Sleep:          time.Sleep,
	}
	// The winuse extension (extension_windows.go): discovery tries it first.
	s.Bridges = func() []bridge.Info {
		if win.LiveDesktopGuard() != nil {
			return nil // a test must not read, or prune, the real bridge files
		}
		return bridge.List(s.BridgesDir(), bridge.HostAlive)
	}
	s.Extension = s.extensionEndpoint
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func liveVersionWS(ctx context.Context, port int) (string, error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return "", err
	}
	return versionWS(ctx, "http://127.0.0.1:"+strconv.Itoa(port))
}

// versionWS asks a DevTools HTTP endpoint for the browser WebSocket URL.
// DevTools never closes the connection after answering, so the body is
// read to its Content-Length (net/http does that) and capped, never read
// to EOF; the request also asks for the connection to close.
func versionWS(ctx context.Context, base string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/version", nil)
	if err != nil {
		return "", err
	}
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/json/version answered %s", resp.Status)
	}
	if resp.ContentLength < 0 {
		return "", errors.New("/json/version has no Content-Length")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	if v.WS == "" {
		return "", errors.New("/json/version has no webSocketDebuggerUrl")
	}
	return v.WS, nil
}

func livePortAlive(port int) bool {
	if win.LiveDesktopGuard() != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func liveDial(ctx context.Context, wsURL string, handshake time.Duration) (cdp.Transport, error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return nil, err
	}
	return cdp.DialWS(ctx, wsURL, handshake)
}

// ---- the TCP listener table (GetExtendedTcpTable) ----

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER
	afInet                   = 2
	afInet6                  = 23
	tcp4RowSize              = 24 // MIB_TCPROW_OWNER_PID
	tcp6RowSize              = 56 // MIB_TCP6ROW_OWNER_PID
)

// tcpListener is one row of the listener table.
type tcpListener struct {
	Addr net.IP
	Port int
	PID  uint32
}

// listenerPID is the process listening on a TCP port on loopback or all
// interfaces, from the system's listener table, instead of use-browser's
// netstat and tasklist parsing (ub/browsers.go:686-743).
func listenerPID(port int) (uint32, bool) {
	for _, af := range []uint32{afInet, afInet6} {
		buf, err := tcpTable(af)
		if err != nil {
			continue
		}
		rows := parseTCP4Listeners(buf)
		if af == afInet6 {
			rows = parseTCP6Listeners(buf)
		}
		if pid, ok := pickListener(rows, port); ok {
			return pid, true
		}
	}
	return 0, false
}

// pickListener finds the loopback (or any-address) listener on a port.
func pickListener(rows []tcpListener, port int) (uint32, bool) {
	for _, r := range rows {
		if r.Port == port && (r.Addr.IsLoopback() || r.Addr.IsUnspecified()) {
			return r.PID, true
		}
	}
	return 0, false
}

func tcpTable(af uint32) ([]byte, error) {
	size := uint32(16 * 1024)
	for range 4 {
		buf := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPIDListener, 0)
		switch syscall.Errno(r) {
		case 0:
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue // size now holds what is needed
		default:
			return nil, syscall.Errno(r)
		}
	}
	return nil, errors.New("the TCP table kept growing")
}

// parseTCP4Listeners reads a MIB_TCPTABLE_OWNER_PID: a row count, then
// rows of state, local address, local port, remote address, remote port
// and owning pid. Addresses and ports are in network byte order.
func parseTCP4Listeners(buf []byte) []tcpListener {
	if len(buf) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf))
	var out []tcpListener
	for i := 0; i < n; i++ {
		off := 4 + i*tcp4RowSize
		if off+tcp4RowSize > len(buf) {
			break
		}
		row := buf[off : off+tcp4RowSize]
		out = append(out, tcpListener{
			Addr: net.IPv4(row[4], row[5], row[6], row[7]),
			Port: int(row[8])<<8 | int(row[9]),
			PID:  binary.LittleEndian.Uint32(row[20:]),
		})
	}
	return out
}

// parseTCP6Listeners reads a MIB_TCP6TABLE_OWNER_PID: rows of local
// address (16), scope, local port, remote address (16), scope, remote
// port, state and owning pid.
func parseTCP6Listeners(buf []byte) []tcpListener {
	if len(buf) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf))
	var out []tcpListener
	for i := 0; i < n; i++ {
		off := 4 + i*tcp6RowSize
		if off+tcp6RowSize > len(buf) {
			break
		}
		row := buf[off : off+tcp6RowSize]
		out = append(out, tcpListener{
			Addr: net.IP(append([]byte(nil), row[0:16]...)),
			Port: int(row[20])<<8 | int(row[21]),
			PID:  binary.LittleEndian.Uint32(row[52:]),
		})
	}
	return out
}

// processImage is a process's full image path.
func processImage(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// processRunning reports whether a process with this image name runs.
func processRunning(exe string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			return true
		}
	}
	return false
}

// createNoWindow keeps taskkill from flashing a console.
const createNoWindow = 0x08000000

// startDetached starts a browser the way launch-app starts an App
// (headless-max ticket 01): win.StartProcess, outside the helper's job when
// allowed (the browser must outlive the helper, which exits when idle), with
// a non-activating show state for its first Window.
func startDetached(path string, args []string) (int, error) {
	pid, err := win.StartProcess(path, win.LaunchOptions{Args: args})
	return int(pid), err
}

// foregroundOwner is the foreground Window and its process, for browser
// launch's foreground check.
func foregroundOwner() (uint64, uint32) {
	fg := win.ForegroundWindow()
	return fg, win.WindowPID(fg)
}

// taskkillArgs closes every process of an image the clean way: without
// /F, taskkill sends the close request Chromium shuts down on, flushing
// its databases and session (ub/clone.go:416-425).
func taskkillArgs(exe string) []string { return []string{"/IM", exe} }

func closeApp(exe string) error {
	if err := win.LiveDesktopGuard(); err != nil {
		return err
	}
	cmd := exec.Command("taskkill", taskkillArgs(exe)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil // a non-zero exit only means nothing matched
	}
	return err
}

// freeDebugPort is the first port from 9222 up that nothing listens on, so
// another browser holding 9222 never stops a launch (ub/browsers.go:179-192).
func freeDebugPort() int {
	for port := 9222; port < 9322; port++ {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		l.Close()
		return port
	}
	return 9222
}

// ephemeralPort is a free loopback port the system hands out: listen on
// port 0, read the port, close.
func ephemeralPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// parentPID is a process's parent from a process snapshot.
func parentPID(pid uint32) (uint32, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == pid {
			return e.ParentProcessID, true
		}
	}
	return 0, false
}

// listeningPorts lists the loopback (or any-address) TCP ports a process
// listens on, from the system's listener table.
func listeningPorts(pid uint32) []int {
	var out []int
	for _, af := range []uint32{afInet, afInet6} {
		buf, err := tcpTable(af)
		if err != nil {
			continue
		}
		rows := parseTCP4Listeners(buf)
		if af == afInet6 {
			rows = parseTCP6Listeners(buf)
		}
		for _, r := range rows {
			if r.PID == pid && (r.Addr.IsLoopback() || r.Addr.IsUnspecified()) && !slices.Contains(out, r.Port) {
				out = append(out, r.Port)
			}
		}
	}
	return out
}

func liveInspectorWS(ctx context.Context, port int) (string, error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return "", err
	}
	return inspectorWS(ctx, "http://127.0.0.1:"+strconv.Itoa(port))
}

// inspectorWS asks a Node inspector's /json/list for the WebSocket URL of
// its one target (type "node"), read as versionWS reads /json/version.
func inspectorWS(ctx context.Context, base string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/list", nil)
	if err != nil {
		return "", err
	}
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/json/list answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var targets []struct {
		Type string `json:"type"`
		WS   string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &targets); err != nil {
		return "", err
	}
	for _, t := range targets {
		if t.Type == "node" && t.WS != "" {
			return t.WS, nil
		}
	}
	return "", errors.New("/json/list names no Node.js target")
}
