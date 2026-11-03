package version

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Build identifies the running winuse executable (desktop-parity ticket
// 25): the CLI sends it with every request beside Version, and a helper
// whose own Build differs answers stale exactly as for another Version,
// because two builds of one version (a rebuild during development, an
// e2e binary) are otherwise identical. It is the Go linker's build ID
// stamped into every Go binary, its last part, which is the hash of the
// linked content; a binary without the stamp gives its size and
// modification time. "" when the executable cannot be read, which
// matches any build. Read once, at first use.
var Build = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return ExeBuild(exe)
})

// The Go build ID's frame in a non-ELF binary, within its first readSize
// bytes (as cmd/internal/buildid reads it: `go tool buildid`).
var (
	goBuildPrefix = []byte("\xff Go build ID: \"")
	goBuildEnd    = []byte("\"\n \xff")
)

const readSize = 32 * 1024

// ExeBuild is the Build of the executable at path: the last part of its
// Go build ID, else "<size>-<modification time>", else "".
func ExeBuild(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, readSize)
	n, _ := io.ReadFull(f, head)
	if id := goBuildID(head[:n]); id != "" {
		return id[strings.LastIndexByte(id, '/')+1:]
	}
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%x", fi.Size(), fi.ModTime().UnixNano())
}

// goBuildID finds the quoted Go build ID in a binary's first bytes; ""
// when it is not there.
func goBuildID(data []byte) string {
	i := bytes.Index(data, goBuildPrefix)
	if i < 0 {
		return ""
	}
	j := bytes.Index(data[i+len(goBuildPrefix):], goBuildEnd)
	if j < 0 {
		return ""
	}
	quoted := data[i+len(goBuildPrefix)-1 : i+len(goBuildPrefix)+j+1]
	id, err := strconv.Unquote(string(quoted))
	if err != nil {
		return ""
	}
	return id
}
