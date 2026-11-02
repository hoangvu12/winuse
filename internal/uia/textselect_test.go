package uia

import (
	"errors"
	"strings"
	"testing"
)

// TestCheckTextSpan pins select-text's span limits: 0 or more, and an end
// within MaxTextOffset; all is always a span.
func TestCheckTextSpan(t *testing.T) {
	for _, s := range []TextSpan{{}, {Start: 3, Length: 4}, {Start: 5}, {All: true}, {Start: MaxTextOffset}} {
		if err := CheckTextSpan(s); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	for _, s := range []TextSpan{{Start: -1}, {Length: -2}, {Start: MaxTextOffset, Length: 1}, {Start: 1, Length: MaxTextOffset}} {
		if err := CheckTextSpan(s); !errors.Is(err, ErrTextSpan) {
			t.Errorf("%+v: err = %v, want ErrTextSpan", s, err)
		}
	}
}

// TestTextSpanBounds: all is 0 to the length, any other span must end at
// or before it.
func TestTextSpanBounds(t *testing.T) {
	cases := []struct {
		span       TextSpan
		length     int
		start, end int
		err        bool
	}{
		{TextSpan{Start: 2, Length: 3}, 10, 2, 5, false},
		{TextSpan{Start: 7, Length: 3}, 10, 7, 10, false},
		{TextSpan{Start: 10}, 10, 10, 10, false}, // the caret at the end
		{TextSpan{All: true}, 10, 0, 10, false},
		{TextSpan{All: true}, 0, 0, 0, false},
		{TextSpan{Start: 8, Length: 3}, 10, 0, 0, true},
		{TextSpan{Start: 11}, 10, 0, 0, true},
	}
	for _, c := range cases {
		start, end, err := c.span.Bounds(c.length)
		if c.err {
			if !errors.Is(err, ErrTextRange) {
				t.Errorf("%+v in %d: err = %v, want ErrTextRange", c.span, c.length, err)
			}
			continue
		}
		if err != nil || start != c.start || end != c.end {
			t.Errorf("%+v in %d: %d..%d, %v; want %d..%d", c.span, c.length, start, end, err, c.start, c.end)
		}
	}
	_, _, err := TextSpan{Start: 8, Length: 3}.Bounds(10)
	if err == nil || !strings.Contains(err.Error(), "it would end at 11, and the text holds 10 characters") {
		t.Errorf("message: %v", err)
	}
}

// TestSpanMoved: the Text pattern route's check on how far the range's
// endpoints moved from the document's start.
func TestSpanMoved(t *testing.T) {
	span := TextSpan{Start: 4, Length: 3}
	if err := spanMoved(span, 7, 4); err != nil {
		t.Errorf("a full move: %v", err)
	}
	if err := spanMoved(span, 5, 4); !errors.Is(err, ErrTextRange) || !strings.Contains(err.Error(), "holds 5 characters") {
		t.Errorf("a short end: %v", err)
	}
	if err := spanMoved(span, 7, 3); !errors.Is(err, ErrTextUnreadable) {
		t.Errorf("a short start: %v", err)
	}
	if err := spanMoved(TextSpan{Start: 0, Length: 0}, 0, 0); err != nil {
		t.Errorf("the caret at 0: %v", err)
	}
}

func TestTextSpanString(t *testing.T) {
	if got := (TextSpan{Start: 2, Length: 5}).String(); got != "characters 2 to 7" {
		t.Errorf("got %q", got)
	}
	if got := (TextSpan{All: true}).String(); got != "all of the text" {
		t.Errorf("got %q", got)
	}
}
