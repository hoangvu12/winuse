package cli

// Literal text from stdin or a file (learn-from 06, agent-desktop's
// --stdin): type-text's text, set-value's value and clipboard-set's text
// can come from standard input or a file instead of the command line, so
// a secret stays out of the process command line and PowerShell 5.1 does
// not strip the double quotes in it.
//
//	winuse type-text <window> [--post | --into <element>] [--interval-ms <n>] --stdin | --file <path>
//	winuse set-value <window> <element> --stdin | --file <path>
//	winuse clipboard-set --stdin | --text-file <path>
//
// The flag stands where the text would start and replaces it: text after
// it is refused, and the global flags after it are flags again (headed.go).
// clipboard-set's file flag is --text-file, because its --file copies
// files. --file - reads stdin, as eval's --file does. The CLI reads the
// text before it dials the helper, and the request carries it as if it
// had been typed on the command line; nothing new crosses the pipe.
//
// The text is UTF-8; a UTF-8 byte order mark is dropped, and one for
// UTF-16 (what Windows PowerShell 5.1's Out-File and > write) decodes the
// rest as UTF-16. Anything else that is not valid UTF-8 is refused. One
// trailing newline (\n or \r\n) is dropped, the one echo, Set-Content and
// most editors add, so it never presses Return after the text; end with
// two to keep one. --stdin refuses a terminal, where it would wait for the
// user. A Batch step takes neither flag: `batch -` already reads the whole
// Batch from stdin.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The text source flags' spellings.
const (
	stdinFlag    = "--stdin"
	fileFlag     = "--file"      // type-text and set-value (eval's spelling)
	textFileFlag = "--text-file" // clipboard-set, whose --file copies files
)

// maxSourcedText bounds the text read from stdin or a file: a request
// line holds at most protocol.MaxLine, and no field needs more.
const maxSourcedText = 1 << 20

// textSource is where a command's literal text comes from instead of its
// arguments: stdin, or a file (path "-" is stdin too). The zero value is
// the command line.
type textSource struct {
	stdin bool
	path  string
	flag  string // the spelling given, for messages
}

func (s textSource) given() bool { return s.stdin || s.path != "" }

// takeTextSource reads a text source flag at region[0], spelled --stdin or
// file (with its path next, or after =), and returns it and how many
// tokens it spans; 0 when region[0] is no source flag.
func takeTextSource(region []string, file string) (textSource, int, error) {
	if len(region) == 0 {
		return textSource{}, 0, nil
	}
	tok := region[0]
	if tok == stdinFlag {
		return textSource{stdin: true, flag: stdinFlag}, 1, nil
	}
	if path, ok := strings.CutPrefix(tok, file+"="); ok {
		if strings.TrimSpace(path) == "" {
			return textSource{}, 0, fmt.Errorf("%s takes a file path", file)
		}
		return textSource{path: path, flag: file}, 1, nil
	}
	if tok != file {
		return textSource{}, 0, nil
	}
	if len(region) < 2 || strings.TrimSpace(region[1]) == "" {
		return textSource{}, 0, fmt.Errorf("%s takes a file path", file)
	}
	return textSource{path: region[1], flag: file}, 2, nil
}

// textSourceLen is a sourceEnd for set-value's and clipboard-set's text
// position: how many tokens the source flag at its start spans, or -1
// when the text is literal.
func textSourceLen(file string) func([]string) int {
	return func(region []string) int {
		_, n, err := takeTextSource(region, file)
		if n == 0 || err != nil {
			return -1
		}
		return n
	}
}

// stdinIsTerminal reports whether stdin is a console (or NUL), where
// --stdin would wait for the user or read nothing. Tests replace it.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// read reads the source's text: UTF-8 (UTF-16 with its byte order mark),
// one trailing newline dropped.
func (s textSource) read() (string, error) {
	var r io.Reader
	where := s.path
	if s.stdin || s.path == "-" {
		if stdinIsTerminal() {
			return "", fmt.Errorf("%s reads piped or redirected input, and stdin is a terminal: pipe the text in, or use a file", s.flag)
		}
		r, where = os.Stdin, "stdin"
	} else {
		f, err := os.Open(s.path)
		if err != nil {
			return "", fmt.Errorf("reading the text from %s: %v", s.path, err)
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxSourcedText+1))
	if err != nil {
		return "", fmt.Errorf("reading the text from %s: %v", where, err)
	}
	if len(b) > maxSourcedText {
		return "", fmt.Errorf("the text from %s is longer than %d bytes", where, maxSourcedText)
	}
	text, err := decodeText(b)
	if err != nil {
		return "", fmt.Errorf("the text from %s %v", where, err)
	}
	return text, nil
}

// decodeText turns the bytes read into the text: a UTF-8 byte order mark
// dropped, UTF-16 decoded by its byte order mark, otherwise valid UTF-8
// only; then one trailing newline dropped.
func decodeText(b []byte) (string, error) {
	var s string
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		little := b[0] == 0xFF
		b = b[2:]
		if len(b)%2 != 0 {
			return "", errors.New("is not whole UTF-16: save it as UTF-8")
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			if little {
				units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
			} else {
				units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
			}
		}
		s = string(utf16.Decode(units))
	} else {
		b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
		if !utf8.Valid(b) {
			return "", errors.New("is not UTF-8: save it as UTF-8")
		}
		s = string(b)
	}
	if t, ok := strings.CutSuffix(s, "\r\n"); ok {
		return t, nil
	}
	return strings.TrimSuffix(s, "\n"), nil
}

// errSourceInBatch refuses a text source flag in a Batch step.
func errSourceInBatch(kind string) error {
	return fmt.Errorf("a batch %s Action takes its text in the step: --stdin and --file go on the standalone command, and `winuse batch -` reads the whole Batch from stdin", kind)
}
