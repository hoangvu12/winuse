package agent

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// snapshot lists every process with its parent and executable name
// (CreateToolhelp32Snapshot). It only reads.
func snapshot() (map[uint32]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	procs := map[uint32]Proc{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs[e.ProcessID] = Proc{PID: e.ProcessID, Parent: e.ParentProcessID, Exe: windows.UTF16ToString(e.ExeFile[:])}
	}
	return procs, nil
}

// processStart reads a process's creation time (GetProcessTimes), in
// 100-nanosecond units since 1601; 0 when the process cannot be opened.
func processStart(pid uint32) int64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return 0
	}
	return int64(created.HighDateTime)<<32 | int64(created.LowDateTime)
}
