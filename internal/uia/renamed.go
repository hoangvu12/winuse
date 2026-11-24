package uia

// The renamed-item read-back (headless-max ticket 24). File Explorer's
// files are list items whose Value is their name, and setting it renames
// the file. The write rebuilds the item, so the Element winuse wrote to
// reads "" afterwards, although the rename took (measured in the
// 2026-11-03 Task Suite run); a stale item may instead read its old name
// (bladebro-ideas 21). Before reporting either as a mismatch, the
// set-value read-back looks among the Element's siblings for one now
// named as asked; finding it is the read-back. Only a name that is new
// counts: a sibling named so before the write (a label "Name" beside the
// edit set to "Name", another file already showing that name) proves
// nothing, so then the mismatch stands.

import (
	"errors"
	"strconv"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/linebreak"
)

// readEmpty reports whether err is a set-value read-back that found the
// Element reading "" (a password field's withheld read is not one).
func readEmpty(err error) bool {
	var v *VerifyError
	return errors.As(err, &v) && v.Action == "set-value" && v.Observed == `""`
}

// readStale reports whether err is a set-value read-back that found the
// Element reading "" or old, its value before the write, line breaks
// aside (bladebro-ideas 21): an item the write rebuilt may read either.
func readStale(err error, old string) bool {
	if readEmpty(err) {
		return true
	}
	var v *VerifyError
	if !errors.As(err, &v) || v.Action != "set-value" {
		return false
	}
	got, uerr := strconv.Unquote(v.Observed)
	return uerr == nil && linebreak.Same(got, old)
}

// renamedInPlace decides the read-back of a write that read "" or the
// Element's old value: the write took when no sibling was named value
// before it and one is now (namedAfter, asked only then).
func renamedInPlace(err error, old string, namedBefore bool, namedAfter func() bool) bool {
	return readStale(err, old) && !namedBefore && namedAfter()
}

// parentOf is el's parent in the raw view, or nil; the caller releases it.
func (th *thread) parentOf(el comPtr) comPtr {
	var walker comPtr
	if hr := call(th.auto, uiaGetRawViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
		return nil
	}
	defer release(walker)
	var parent comPtr
	if hr := call(walker, walkerGetParent, uintptr(el), uintptr(unsafe.Pointer(&parent))); hr.Failed() {
		return nil
	}
	return parent
}

// childNamed reports whether one of parent's control-view children reads
// value as its Name or its Value, line breaks aside.
func (th *thread) childNamed(parent comPtr, value string) bool {
	var cond comPtr
	if hr := call(th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return false
	}
	defer release(cond)
	kids, _ := findChildren(parent, cond)
	defer releaseAll(kids)
	for _, k := range kids {
		if namedAs(currentString(k, propName), currentString(k, propValueValue), value) {
			return true
		}
	}
	return false
}

// namedAs reports whether an Element reading name and val is named value.
func namedAs(name, val, value string) bool {
	return value != "" && (linebreak.Same(name, value) || linebreak.Same(val, value))
}
