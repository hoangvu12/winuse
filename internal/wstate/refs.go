package wstate

// name:/id: Element Reference resolution (ticket 11): the Element
// Reference forms that name an Element by its Name or its AutomationId
// instead of its Element Index (spec, Command surface). This file is the
// pure matching logic — resolution from one Window State's Elements — so
// the seam-2 offline tests cover it against recorded and hand-authored
// trees with no desktop (spec, Testing Decisions).
//
// A reference resolves against exactly one Window State (the latest one
// the helper stored for the Window), matching the Name or AutomationId as
// that Window State printed it: whitespace-collapsed, otherwise exact
// (case-sensitive). A reference matching more than one Element is an
// error carrying the candidate lines, never a guess (user story 40).

import (
	"fmt"
	"strings"
)

// Element Reference prefixes (spec, Command surface): name:<text> and
// id:<AutomationId>. The Element Index form is a bare integer and the
// @x,y form is click's own (screenshot coordinates), so neither passes
// through here.
const (
	NameRefPrefix = "name:"
	IDRefPrefix   = "id:"
)

// RefKind is which Element property a name:/id: reference matches.
type RefKind int

const (
	// RefName matches the Element's Name (the tree's name column).
	RefName RefKind = iota
	// RefID matches the Element's AutomationId (the tree's "ID:" column).
	RefID
)

// kindWord names the kind in error messages.
func (k RefKind) word() string {
	if k == RefID {
		return "ID"
	}
	return "name"
}

// ParseElementRef splits a name:/id: Element Reference into its kind and
// text. A reference without a known prefix, or one whose text is empty
// (after collapsing), is a BadRefError — callers report it as a usage
// failure. The text is returned verbatim (collapsed at match time).
func ParseElementRef(ref string) (kind RefKind, text string, err error) {
	for _, k := range []struct {
		kind   RefKind
		prefix string
	}{
		{RefName, NameRefPrefix},
		{RefID, IDRefPrefix},
	} {
		if strings.HasPrefix(ref, k.prefix) {
			text := ref[len(k.prefix):]
			if Collapse(text) == "" {
				return 0, "", &BadRefError{Ref: ref}
			}
			return k.kind, text, nil
		}
	}
	return 0, "", &BadRefError{Ref: ref}
}

// BadRefError reports a reference that is neither name:<text> nor
// id:<AutomationId> (including the empty-text forms).
type BadRefError struct{ Ref string }

func (e *BadRefError) Error() string {
	return fmt.Sprintf("invalid element reference %q; expected name:<text> or id:<AutomationId>", e.Ref)
}

// AmbiguousRefError reports a reference matching more than one Element of
// the Window State it was resolved against. Candidates holds the matching
// Elements' tree lines (index, role, name, ID: …) so the caller can list
// them and the agent can disambiguate.
type AmbiguousRefError struct {
	Kind       RefKind
	Text       string   // the collapsed reference text
	Matches    []int    // the matching Element Indexes
	Candidates []string // the matching Elements' tree lines
}

func (e *AmbiguousRefError) Error() string {
	return fmt.Sprintf("%s %q matches %d elements", e.Kind.word(), e.Text, len(e.Matches))
}

// NoMatchRefError reports a reference matching no Element of the Window
// State it was resolved against.
type NoMatchRefError struct {
	Kind RefKind
	Text string // the collapsed reference text
}

func (e *NoMatchRefError) Error() string {
	if e.Kind == RefID {
		return fmt.Sprintf("no element with ID %q", e.Text)
	}
	return fmt.Sprintf("no element named %q", e.Text)
}

// ResolveElementRef resolves a name:/id: Element Reference against one
// Window State's Elements: exactly one match resolves to its Element
// Index. Matching compares the Element's Name (RefName) or AutomationId
// (RefID) with the reference text, both whitespace-collapsed — the form
// the tree prints — and otherwise exact, so a reference the agent copied
// from the Window State resolves to the Element behind that line. Several
// matches are an AmbiguousRefError with the candidate lines; none is a
// NoMatchRefError; a malformed reference is a BadRefError.
func ResolveElementRef(elements []Element, ref string) (int, error) {
	kind, text, err := ParseElementRef(ref)
	if err != nil {
		return 0, err
	}
	want := Collapse(text)
	var matches []int
	for i := range elements {
		var got string
		if kind == RefName {
			got = Collapse(elements[i].Name)
		} else {
			got = Collapse(elements[i].AutomationId)
		}
		if got == want {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return 0, &NoMatchRefError{Kind: kind, Text: want}
	default:
		return 0, &AmbiguousRefError{
			Kind:       kind,
			Text:       want,
			Matches:    matches,
			Candidates: candidateLines(elements, matches),
		}
	}
}

// candidateLines renders the matching Elements' tree lines, without their
// indentation, in index order — the candidate list an ambiguous
// reference reports.
func candidateLines(elements []Element, matches []int) []string {
	lines := make([]string, 0, len(matches))
	for _, i := range matches {
		lines = append(lines, Line(i, &elements[i]))
	}
	return lines
}

// Line renders one Element's tree line without its indentation (the same
// grammar and content the tree prints for it, minus the leading TABs):
// the form the focused-Element sentence and the ambiguous-reference
// candidate lists use.
func Line(index int, e *Element) string {
	var b strings.Builder
	writeElement(&b, index, e, false)
	return b.String()
}
