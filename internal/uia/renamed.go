package uia

// The renamed-item read-back (headless-max ticket 24). File Explorer's
// files are list items whose Value is their name, and setting it renames
// the file. The write rebuilds the item, so the Element winuse wrote to
// reads "" afterwards, although the rename took (measured in the
// 2026-11-03 Task Suite run). Before reporting that as a mismatch, the
// set-value read-back looks among the Element's siblings for one now
// named as asked; finding it is the read-back.

import (
	"errors"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/linebreak"
)

// readEmpty reports whether err is a set-value read-back that found the
// Element reading "" (a password field's withheld read is not one).
func readEmpty(err error) bool {
	var v *VerifyError
	return errors.As(err, &v) && v.Action == "set-value" && v.Observed == `""`
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
