#include "textflag.h"

// fromPointThunk is the vtable entry for
// IRawElementProviderFragmentRoot::ElementProviderFromPoint(this, double x,
// double y, **ret). The Windows x64 convention passes x and y in X1 and X2;
// Go's callback trampoline reads only the integer argument registers, so
// copy the doubles' bits into DX and R8 (their integer slots) and tail-jump
// to the Go callback. Stack and return address are untouched.
TEXT ·fromPointThunk(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X1, DX
	MOVQ X2, R8
	MOVQ ·fromPointTarget(SB), AX
	JMP  AX

// fromPointThunkPC returns fromPointThunk's ABI0 entry address.
TEXT ·fromPointThunkPC(SB), NOSPLIT, $0-8
	LEAQ ·fromPointThunk(SB), AX
	MOVQ AX, ret+0(FP)
	RET
