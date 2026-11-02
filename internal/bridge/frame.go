package bridge

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Size limits.
const (
	// MaxToExtension is what the browser accepts in one native message
	// from the host: it kills the host over anything larger. Commands over
	// it are sent in chunks of ChunkSize (see Host).
	MaxToExtension = 1 << 20
	// ChunkSize is how much command text one chunk carries. JSON-escaping
	// a piece of JSON text at most doubles it, so a chunk's envelope stays
	// well under MaxToExtension.
	ChunkSize = 256 << 10
	// MaxCommand bounds one command from the helper (a long
	// Input.insertText or Runtime.evaluate fits many times over).
	MaxCommand = 64 << 20
	// MaxFrame bounds one frame in either direction; messages from the
	// extension (a screenshot, a large accessibility tree) can be big.
	MaxFrame = 256 << 20
)

// ErrBadFrame is a frame that is not one JSON message: an empty body, a
// length over the limit (a stray UTF-8 BOM or text where a length belongs
// reads as hundreds of megabytes), or a body that is not JSON.
var ErrBadFrame = errors.New("not a native messaging frame")

// WriteFrame writes one message the native messaging way: a 4-byte length
// in native byte order (little-endian on every platform Chromium ships on
// Windows), then the bytes. The helper link uses the same framing.
func WriteFrame(w io.Writer, b []byte) error {
	if len(b) > MaxFrame {
		return fmt.Errorf("%w: %d bytes is over %d", ErrBadFrame, len(b), MaxFrame)
	}
	buf := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads one message, at most max bytes long, whose body must be
// a JSON object. A short read waits for the rest (io.ReadFull); an EOF
// before the first byte is io.EOF, in the middle io.ErrUnexpectedEOF.
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || uint64(n) > uint64(max) {
		return nil, fmt.Errorf("%w: a length of %d bytes (the limit is %d)", ErrBadFrame, n, max)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	if !isJSONObject(b) {
		return nil, fmt.Errorf("%w: the body is not a JSON object", ErrBadFrame)
	}
	return b, nil
}

// isJSONObject reports whether b is one JSON object, surrounding
// whitespace allowed. A BOM in front is not JSON.
func isJSONObject(b []byte) bool {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i < len(b) && b[i] == '{' && json.Valid(b)
}
