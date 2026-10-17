package tasksuite

import (
	"strings"
	"testing"
)

func TestCheckUserStartedRefuses(t *testing.T) {
	err := CheckUserStarted(false)
	if err == nil {
		t.Fatal("a run without the explicit user start must be refused")
	}
	for _, want := range []string{"refusing to start", UserStartFlag, "only by the user"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

func TestCheckUserStartedAcceptsTheExplicitStart(t *testing.T) {
	if err := CheckUserStarted(true); err != nil {
		t.Errorf("the explicit user start must pass: %v", err)
	}
}
