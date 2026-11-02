package win

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WindowsBuild names the running Windows for winuse status, e.g.
// "Windows 11 25H2, build 26200.6899". The version numbers come from
// RtlGetVersion (which no compatibility shim lies to); the update build
// revision and the marketing version from the CurrentVersion registry key,
// best effort. It only reads.
func WindowsBuild() string {
	v := windows.RtlGetVersion()
	var ubr uint64
	var display string
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		ubr, _, _ = k.GetIntegerValue("UBR")
		display, _, _ = k.GetStringValue("DisplayVersion")
		k.Close()
	}
	return FormatWindowsBuild(v.MajorVersion, v.MinorVersion, v.BuildNumber, uint32(ubr), display)
}

// FormatWindowsBuild renders the Windows version: Windows 11 is 10.0 from
// build 22000 on, so the name follows the build; ubr 0 and an empty
// display version are left out.
func FormatWindowsBuild(major, minor, build, ubr uint32, display string) string {
	name := fmt.Sprintf("Windows %d.%d", major, minor)
	if major == 10 && minor == 0 {
		name = "Windows 10"
		if build >= 22000 {
			name = "Windows 11"
		}
	}
	if display != "" {
		name += " " + display
	}
	number := fmt.Sprint(build)
	if ubr != 0 {
		number += fmt.Sprintf(".%d", ubr)
	}
	return name + ", build " + number
}
