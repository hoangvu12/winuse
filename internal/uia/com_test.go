package uia

import "testing"

// TestHRESULTConstants pins the signed constants to their documented hex
// values: a one-digit slip silently misclassifies COM errors (the walk
// would treat UIA_E_TIMEOUT as an unknown failure instead of the
// time-limit cut).
func TestHRESULTConstants(t *testing.T) {
	var hr = uiaErrTimeout
	if uint32(hr) != 0x80131505 {
		t.Errorf("uiaErrTimeout = %s, want 0x80131505 (UIA_E_TIMEOUT)", hr)
	}
}

func TestHRESULTFailed(t *testing.T) {
	if !HRESULT(-1).Failed() || HRESULT(0).Failed() || HRESULT(1).Failed() {
		t.Error("only negative HRESULTs are failures")
	}
	if HRESULT(0).String() != "0x00000000" || uiaErrTimeout.String() != "0x80131505" {
		t.Errorf("HRESULT strings: %s, %s", HRESULT(0).String(), uiaErrTimeout.String())
	}
}
