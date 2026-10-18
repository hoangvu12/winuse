package apps

import (
	"encoding/binary"
	"os"
	"unicode/utf16"
)

// LinkTarget extracts the local target path from .lnk shortcut bytes — a
// minimal MS-SHLLINK reader: the 76-byte header, an optional target
// IDList, then the LinkInfo block's local base path (Unicode when present,
// else ANSI plus the common path suffix). ok is false when the shortcut
// has no readable local target (network targets, IDList-only shortcuts,
// malformed data); the caller then falls back to launching the .lnk
// through the shell, which needs no target resolution.
func LinkTarget(data []byte) (string, bool) {
	const headerSize = 76
	const (
		flagHasLinkTargetIDList = 0x00000001
		flagHasLinkInfo         = 0x00000002
		flagLinkInfoUnicode     = 0x00000001
		linkInfoFixedSize       = 24
		linkInfoUnicodeOffset   = 24
	)
	if len(data) < headerSize {
		return "", false
	}
	if binary.LittleEndian.Uint32(data[0:4]) != headerSize {
		return "", false
	}
	if !isLinkCLSID(data[4:20]) {
		return "", false
	}
	flags := binary.LittleEndian.Uint32(data[20:24])

	off := uint32(headerSize)
	if flags&flagHasLinkTargetIDList != 0 {
		if int(off)+2 > len(data) {
			return "", false
		}
		idListSize := binary.LittleEndian.Uint16(data[off : off+2])
		off += 2 + uint32(idListSize)
		if int(off) > len(data) {
			return "", false
		}
	}
	if flags&flagHasLinkInfo == 0 {
		return "", false
	}
	if int(off)+linkInfoFixedSize > len(data) {
		return "", false
	}
	info := data[off:]
	infoSize := binary.LittleEndian.Uint32(info[0:4])
	if infoSize < linkInfoFixedSize || int(infoSize) > len(info) {
		return "", false
	}
	infoFlags := binary.LittleEndian.Uint32(info[4:8])
	localBase := binary.LittleEndian.Uint32(info[12:16])
	commonSuffix := binary.LittleEndian.Uint32(info[20:24])

	// Prefer the Unicode local base path (modern shortcuts carry the full
	// path there).
	if infoFlags&flagLinkInfoUnicode != 0 && infoSize >= linkInfoUnicodeOffset+4 {
		unicodeOffset := binary.LittleEndian.Uint32(info[linkInfoUnicodeOffset : linkInfoUnicodeOffset+4])
		if unicodeOffset != 0 && unicodeOffset < infoSize {
			if s, ok := utf16Z(info[unicodeOffset:]); ok {
				return s, true
			}
		}
	}
	if localBase != 0 && localBase < infoSize {
		if s, ok := ansiZ(info[localBase:]); ok {
			if commonSuffix != 0 && commonSuffix < infoSize {
				if suffix, ok := ansiZ(info[commonSuffix:]); ok {
					s += suffix
				}
			}
			return s, true
		}
	}
	return "", false
}

// LnkTargetFile reads a .lnk file and returns its local target path (""
// when unreadable). Real file access, read-only.
func LnkTargetFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	target, ok := LinkTarget(data)
	if !ok {
		return ""
	}
	return target
}

// isLinkCLSID checks the ShellLink CLSID
// {00021401-0000-0000-C000-000000000046}.
func isLinkCLSID(b []byte) bool {
	want := []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}
	for i, v := range want {
		if b[i] != v {
			return false
		}
	}
	return true
}

// utf16Z reads a NUL-terminated UTF-16LE string.
func utf16Z(b []byte) (string, bool) {
	var units []uint16
	for i := 0; i+1 < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i : i+2])
		if u == 0 {
			return string(utf16.Decode(units)), true
		}
		units = append(units, u)
	}
	return "", false // no terminator: not a valid string
}

// ansiZ reads a NUL-terminated byte string (ASCII/ANSI; shortcut paths
// from the Unicode field are preferred anyway).
func ansiZ(b []byte) (string, bool) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), true
		}
	}
	return "", false // no terminator: not a valid string
}
