# -*- coding: utf-8 -*-
# Finding 10: the request ctx deadline must stop work.

# 1. win/input.go: MouseDrag takes a stopped func; dragSteps checkpoints.
p = "internal/win/input.go"
s = open(p, encoding="utf-8").read()
old = '''// MouseDrag drags over the Window target from screen point (fx, fy) to
// (tx, ty) \u2014 physical pixels \u2014 with the left button (Codex: non-left
// drag is not supported): the pointer is placed pixel-exactly at the
// start, the button goes down, interpolated injected absolute moves
// carry the pointer to the end in steps (so apps register a drag \u2014
// menus track, canvases draw, lists reorder \u2014 instead of a teleport),
// the exact end point is pinned, and the button comes up. The button
// is released even when a move fails, so a failed drag never leaves it
// held.
func MouseDrag(target uint64, fx, fy, tx, ty int32) error {
	// Both endpoints are verified before anything moves: a drag whose
	// start or end is over another window would drag that app's content.
	if err := checkOverTarget(target, fx, fy); err != nil {
		return err
	}
	if err := checkOverTarget(target, tx, ty); err != nil {
		return err
	}
	if err := moveCursor(fx, fy); err != nil {
		return err
	}
	if err := sendInput(mouseEvent(mouseEventfLeftDown)); err != nil {
		return fmt.Errorf("injecting the drag press failed: %v", err)
	}
	err := dragSteps(fx, fy, tx, ty)'''
new = '''// MouseDrag drags over the Window target from screen point (fx, fy) to
// (tx, ty) \u2014 physical pixels \u2014 with the left button (Codex: non-left
// drag is not supported): the pointer is placed pixel-exactly at the
// start, the button goes down, interpolated injected absolute moves
// carry the pointer to the end in steps (so apps register a drag \u2014
// menus track, canvases draw, lists reorder \u2014 instead of a teleport),
// the exact end point is pinned, and the button comes up. The button
// is released even when a move fails, so a failed drag never leaves it
// held.
//
// stopped is the abort checkpoint (finding 10, the Esc stop's drag
// interpolation follow-up from ADR 0007): polled before anything moves
// and between the interpolation steps, an abort returns ErrEscStopped
// with the events already injected landed (the button is still released).
func MouseDrag(target uint64, fx, fy, tx, ty int32, stopped func() bool) error {
	// Both endpoints are verified before anything moves: a drag whose
	// start or end is over another window would drag that app's content.
	if err := checkOverTarget(target, fx, fy); err != nil {
		return err
	}
	if err := checkOverTarget(target, tx, ty); err != nil {
		return err
	}
	if stopped != nil && stopped() {
		// Nothing has moved yet: stop before the button goes down.
		return ErrEscStopped
	}
	if err := moveCursor(fx, fy); err != nil {
		return err
	}
	if err := sendInput(mouseEvent(mouseEventfLeftDown)); err != nil {
		return fmt.Errorf("injecting the drag press failed: %v", err)
	}
	err := dragSteps(fx, fy, tx, ty, stopped)'''
assert old in s, "mousedrag"
s = s.replace(old, new)

old = '''// dragSteps interpolates the injected absolute moves from (fx, fy) to
// (tx, ty) \u2014 one SendInput per step, ~10 ms apart so apps pump the
// moves as separate positions \u2014 and pins the cursor on the exact end
// point (the absolute normalization can round a step off by a pixel).
func dragSteps(fx, fy, tx, ty int32) error {
	for i := 1; i <= dragStepCount; i++ {
		x, y := dragStepPoint(fx, fy, tx, ty, i, dragStepCount)
		if err := sendInput(absoluteMoveInput(x, y)); err != nil {
			return fmt.Errorf("injecting a drag move failed: %v", err)
		}
		if i < dragStepCount {
			time.Sleep(dragStepDelay)
		}
	}
	return moveCursor(tx, ty)
}'''
new = '''// dragSteps interpolates the injected absolute moves from (fx, fy) to
// (tx, ty) \u2014 one SendInput per step, ~10 ms apart so apps pump the
// moves as separate positions \u2014 and pins the cursor on the exact end
// point (the absolute normalization can round a step off by a pixel).
// The stopped checkpoint runs before every step, so an Esc press or an
// expired request budget stops the interpolation (finding 10).
func dragSteps(fx, fy, tx, ty int32, stopped func() bool) error {
	for i := 1; i <= dragStepCount; i++ {
		if stopped != nil && stopped() {
			return ErrEscStopped
		}
		x, y := dragStepPoint(fx, fy, tx, ty, i, dragStepCount)
		if err := sendInput(absoluteMoveInput(x, y)); err != nil {
			return fmt.Errorf("injecting a drag move failed: %v", err)
		}
		if i < dragStepCount {
			time.Sleep(dragStepDelay)
		}
	}
	return moveCursor(tx, ty)
}'''
assert old in s, "dragsteps"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("input ok")

# 2. helper/state.go: Services.Drag signature
p = "internal/helper/state.go"
s = open(p, encoding="utf-8").read()
old = '''	// The pointer input (ticket 10). Nil values fall back to the real
	// SendInput implementations; tests fake them. dx/dy are Codex's
	// scroll amounts (positive right/down, wheel units \u2014 the win layer
	// flips the vertical to wheel semantics).
	Scroll func(target uint64, x, y, dx, dy int32) error   // wheel scroll at a point
	Drag   func(target uint64, fx, fy, tx, ty int32) error // left-button drag, from point to point
	Hover  func(target uint64, x, y int32) error           // move the pointer and leave it there'''
new = '''	// The pointer input (ticket 10). Nil values fall back to the real
	// SendInput implementations; tests fake them. dx/dy are Codex's
	// scroll amounts (positive right/down, wheel units \u2014 the win layer
	// flips the vertical to wheel semantics). Drag's stopped is the abort
	// checkpoint between its interpolation steps (the Esc stop or the
	// request's deadline, finding 10).
	Scroll func(target uint64, x, y, dx, dy int32) error                      // wheel scroll at a point
	Drag   func(target uint64, fx, fy, tx, ty int32, stopped func() bool) error // left-button drag, from point to point
	Hover  func(target uint64, x, y int32) error                               // move the pointer and leave it there'''
assert old in s, "services drag"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("state ok")

# 3. helper/pointer.go: drag passes the checkpoint; typing gets it too below
p = "internal/helper/pointer.go"
s = open(p, encoding="utf-8").read()
old = '''			if err := s.Drag(w.ID, int32(from.X), int32(from.Y), int32(to.X), int32(to.Y)); err != nil {
				return "", inputError(err)
			}'''
new = '''			if err := s.Drag(w.ID, int32(from.X), int32(from.Y), int32(to.X), int32(to.Y),
				func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
				return "", inputAbortError(s, ctx, err, "during the drag")
			}'''
assert old in s, "pointer drag"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("pointer ok")

# 4. helper/keyboard.go: typing's stopped also returns on the deadline
p = "internal/helper/keyboard.go"
s = open(p, encoding="utf-8").read()
old = '''		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if err := s.TypeText(a.Text, s.Esc.Stopped); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})'''
new = '''		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			// The abort checkpoint polls the Esc stop and the request's
			// deadline (finding 10): typing stops between its batches when
			// either fires.
			if err := s.TypeText(a.Text, func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
				return "", inputAbortError(s, ctx, err, "while typing (the text injected so far landed)")
			}
			return okOutput, nil
		})'''
assert old in s, "typing"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("keyboard ok")

# 5. helper/keyboard.go doc: the command comment mentions the checkpoint
p = "internal/helper/keyboard.go"
s = open(p, encoding="utf-8").read()
old = '''// The Esc-stop
// checkpoint rides along: typing aborts between its batches when the
// user pressed Esc, and the abort reports the exit-130 stop.'''
new = '''// The abort
// checkpoint rides along: typing aborts between its batches when the
// user pressed Esc \u2014 the abort reports the exit-130 stop \u2014 or when
// the request's deadline expires (finding 10).'''
assert old in s, "typing doc"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("keyboard doc ok")

# 6. helper/keyboard.go doc of win.TypeText's stopped param
p = "internal/win/keyboard.go"
s = open(p, encoding="utf-8").read()
old = '''// stopped is the Esc-stop checkpoint (ticket 12): it is polled between
// batches \u2014 the pause is the abort point ticket 06 designed in \u2014 and a
// stop returns ErrEscStopped with the batches already injected landed
// and nothing further sent. A nil stopped never aborts.'''
new = '''// stopped is the abort checkpoint (the Esc stop, ticket 12; the
// request's deadline, finding 10): it is polled between batches \u2014 the
// pause is the abort point ticket 06 designed in \u2014 and an abort returns
// ErrEscStopped with the batches already injected landed and nothing
// further sent. A nil stopped never aborts.'''
assert old in s, "win typing doc"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("win keyboard doc ok")

# 7. helper/pointer.go drag doc
p = "internal/helper/pointer.go"
s = open(p, encoding="utf-8").read()
old = '''// Signs and units follow Codex's scroll: positive scrollY scrolls down,
// positive scrollX scrolls right, amounts are in wheel units (one notch
// is 120); drag is left-button only (Codex supports no other). Hover is
// a winuse extension: move the pointer and leave it there.'''
new = '''// Signs and units follow Codex's scroll: positive scrollY scrolls down,
// positive scrollX scrolls right, amounts are in wheel units (one notch
// is 120); drag is left-button only (Codex supports no other). Hover is
// a winuse extension: move the pointer and leave it there.
//
// The scroll's and hover's input is a single SendInput batch, so they
// cannot checkpoint mid-way (like a click or a chord, ADR 0007); the
// drag's interpolation does (finding 10): it polls the Esc stop and the
// request's deadline between its steps.'''
assert old in s, "pointer doc"
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("pointer doc ok")
