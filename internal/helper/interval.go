package helper

// type-text --interval-ms (desktop-parity ticket 23, agent-desktop #348):
// a pause between characters for Apps that drop fast input. It paces the
// routes that type character by character: Headed typing (real key
// events) and Posted Keys (WM_CHAR). The Headless routes put the text in
// with one call (EM_REPLACESEL, the caret splice, the Page's insert), so
// the pause has nothing to space out; their output says it did not apply.

import (
	"fmt"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// maxIntervalMS bounds --interval-ms: slower than a second a character is
// no typing pace, and the request budget would run out on any real text.
const maxIntervalMS = 1000

// intervalOf is type-text's pause between characters, checked.
func intervalOf(a protocol.TypeTextArgs) (time.Duration, *Error) {
	if a.IntervalMS < 0 || a.IntervalMS > maxIntervalMS {
		return 0, Fail(fmt.Sprintf("type-text: --interval-ms must be 0 to %d, got %d", maxIntervalMS, a.IntervalMS), typeTextUsageHint)
	}
	return time.Duration(a.IntervalMS) * time.Millisecond, nil
}

// pacedUnits splits text into the pieces typed one at a time: a rune
// each, with a "\r\n" pair kept as one piece (Posted Keys post it as one
// carriage return).
func pacedUnits(text string) []string {
	var units []string
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
			units = append(units, "\r\n")
			i++
			continue
		}
		units = append(units, string(rs[i]))
	}
	return units
}

// typePaced types text piece by piece through typeOne, pausing interval
// between pieces with sleep. A stop between pieces (the Esc stop or the
// request's deadline, stopped) ends it with win.ErrEscStopped, the way
// win.TypeText stops between its batches.
func typePaced(text string, interval time.Duration, stopped func() bool, sleep func(time.Duration), typeOne func(piece string) error) error {
	for i, piece := range pacedUnits(text) {
		if i > 0 {
			sleep(interval)
			if stopped != nil && stopped() {
				return win.ErrEscStopped
			}
		}
		if err := typeOne(piece); err != nil {
			return err
		}
	}
	return nil
}

// typeKeys is Headed typing through Services.TypeText, paced when
// interval is set.
func (s Services) typeKeys(text string, interval time.Duration, stopped func() bool) error {
	if interval <= 0 {
		return s.TypeText(text, stopped)
	}
	return typePaced(text, interval, stopped, s.sleep, func(piece string) error { return s.TypeText(piece, stopped) })
}

// postText is Posted Keys' text through Services.PostText, paced when
// interval is set; it counts every message posted, so a failure still
// tells "nothing was sent" from a partial post.
func (s Services) postText(target uint64, text string, interval time.Duration, stopped func() bool) (int, error) {
	if interval <= 0 {
		return s.PostText(target, text)
	}
	total := 0
	err := typePaced(text, interval, stopped, s.sleep, func(piece string) error {
		n, err := s.PostText(target, piece)
		total += n
		return err
	})
	return total, err
}

// sleep is Services.Sleep, or time.Sleep for Services built without
// defaultServices.
func (s Services) sleep(d time.Duration) {
	if s.Sleep != nil {
		s.Sleep(d)
		return
	}
	time.Sleep(d)
}

// intervalNote marks a Headless route's success when --interval-ms was
// given: the text went in at once, unpaced. The note goes last.
func intervalNote(out string) string {
	if !isOK(out) {
		return out
	}
	return out + notePrefix + "--interval-ms does not apply: this route puts the text in at once\n"
}
