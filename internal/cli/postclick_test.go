package cli

// --post on click and scroll (headless-max ticket 12): parsed anywhere,
// carried in the request and in Batch steps, refused with --headed and on
// scroll's Element forms.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestClickPostFlag(t *testing.T) {
	_, target, o, err := parseClickArgs([]string{"101", "--post", "@40,50", "--button", "right"})
	if err != nil || !o.post || o.button != "right" || target.point == nil {
		t.Fatalf("parseClickArgs = %+v, %+v, %v", target, o, err)
	}
	if _, _, o, err := parseClickArgs([]string{"101", "3"}); err != nil || o.post {
		t.Errorf("no --post: post %v, %v", o.post, err)
	}
	if _, _, _, err := parseClickArgs([]string{"101", "3", "--post=yes"}); err == nil || !strings.Contains(err.Error(), "--post takes no value") {
		t.Errorf("--post=yes = %v", err)
	}
}

func TestScrollPostFlag(t *testing.T) {
	a, err := scrollRequest([]string{"101", "@5,6", "-120", "240", "--post"}, false)
	if err != nil || !a.Post || a.Headed || a.ScrollX != -120 || a.ScrollY != 240 || a.X != 5 {
		t.Fatalf("scrollRequest --post = %+v, %v", a, err)
	}
	if _, err := scrollRequest([]string{"101", "@5,6", "0", "120", "--post"}, true); err != errPostHeadedClick {
		t.Errorf("--post with --headed = %v, want the usage error", err)
	}
	if _, err := scrollRequest([]string{"101", "3", "down", "--post"}, false); err == nil || !strings.Contains(err.Error(), "--post scrolls at an @x,y point") {
		t.Errorf("an Element scroll with --post = %v", err)
	}
	if a, err := scrollRequest([]string{"101", "@5,6", "0", "120"}, true); err != nil || a.Post || !a.Headed {
		t.Errorf("--headed without --post = %+v, %v", a, err)
	}
}

func TestBatchStepsCarryPost(t *testing.T) {
	st, err := parseBatchStep([]string{"click", "@40,50", "--count", "2", "--post"})
	if err != nil {
		t.Fatal(err)
	}
	var c protocol.ClickArgs
	if json.Unmarshal(st.Args, &c) != nil || !c.Post || c.ClickCount != 2 {
		t.Errorf("click step = %s", st.Args)
	}
	for _, tokens := range [][]string{{"scroll", "--post", "@5,6", "0", "120"}, {"scroll", "@5,6", "0", "120", "--post"}} {
		st, err := parseBatchStep(tokens)
		if err != nil {
			t.Fatalf("%q: %v", tokens, err)
		}
		var s protocol.ScrollArgs
		if json.Unmarshal(st.Args, &s) != nil || !s.Post || s.ScrollY != 120 {
			t.Errorf("%q: step = %s", tokens, st.Args)
		}
	}
	if _, err := parseBatchStep([]string{"scroll", "3", "down", "--post"}); err == nil {
		t.Error("an Element scroll step with --post must be refused")
	}
}
