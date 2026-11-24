package cli

// Offline tests for --block's CLI side (bladebro-ideas ticket 28): tab new
// and navigate parse it into the canonical value, a Batch's steps too, and
// a bad value or a command without it is a usage error before the helper
// is dialed.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestTabNewBlock(t *testing.T) {
	for _, c := range []struct {
		args []string
		want protocol.TabArgs
	}{
		{[]string{"new", "301", "https://a.test/", "--block", "images,fonts,media"}, protocol.TabArgs{Window: "301", URL: "https://a.test/", Block: "images,fonts,media"}},
		{[]string{"new", "301", "--block=media,images", "https://a.test/"}, protocol.TabArgs{Window: "301", URL: "https://a.test/", Block: "images,media"}},
		{[]string{"new", "301", "--block", "none"}, protocol.TabArgs{Window: "301", Block: "none"}},
	} {
		cmd, got, err := parseTabArgs(c.args)
		if err != nil || cmd != "tab-new" || got != c.want {
			t.Errorf("%q: %s %+v, %v", c.args, cmd, got, err)
		}
	}
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"new", "301", "--block", "trackers"}, `--block has no class "trackers"`},
		{[]string{"new", "301", "--block"}, "--block takes images, fonts and media"},
		{[]string{"new", "301", "--block", "none,images"}, "--block none turns blocking off"},
		{[]string{"list", "301", "--block", "images"}, "tab list has no option --block"},
		{[]string{"new", "301", "--wait", "load"}, "tab new has no option --wait"},
	} {
		if _, _, err := parseTabArgs(c.args); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%q: %v, want %q", c.args, err, c.err)
		}
	}
}

func TestNavigateBlock(t *testing.T) {
	got, err := parsePageArgs("navigate", []string{"https://a.test/", "--block", "fonts,images", "--wait", "none"})
	if want := (protocol.NavigateArgs{URL: "https://a.test/", Wait: "none", Block: "images,fonts"}); err != nil || got != want {
		t.Errorf("navigate --block: %+v %v", got, err)
	}
	got, err = parsePageArgs("navigate", []string{"--block=none", "https://a.test/"})
	if want := (protocol.NavigateArgs{URL: "https://a.test/", Block: "none"}); err != nil || got != want {
		t.Errorf("navigate --block=none: %+v %v", got, err)
	}
	for _, c := range []struct {
		cmd  string
		args []string
		err  string
	}{
		{"navigate", []string{"https://a.test/", "--block", "ads"}, `--block has no class "ads"`},
		{"navigate", []string{"https://a.test/", "--block"}, "--block takes images, fonts and media"},
		{"reload", []string{"--block", "images"}, "reload takes no --block"},
		{"back", []string{"--block=images"}, "back takes no --block"},
	} {
		if _, err := parsePageArgs(c.cmd, c.args); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s %q: %v, want %q", c.cmd, c.args, err, c.err)
		}
	}
}

func TestBatchStepsTakeBlock(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"301", "tab", "new", "a.test", "--block", "images", "then", "navigate", "b.test", "--block", "none"})
	if err != nil {
		t.Fatal(err)
	}
	var tab protocol.TabArgs
	var nav protocol.NavigateArgs
	json.Unmarshal(steps[0].Args, &tab)
	json.Unmarshal(steps[1].Args, &nav)
	if steps[0].Kind != "tab-new" || tab.URL != "a.test" || tab.Block != "images" {
		t.Errorf("tab new step: %s %s", steps[0].Kind, steps[0].Args)
	}
	if steps[1].Kind != "navigate" || nav.URL != "b.test" || nav.Block != "none" {
		t.Errorf("navigate step: %s %s", steps[1].Kind, steps[1].Args)
	}
	if _, _, err := parseBatchArgs([]string{"301", "tab", "new", "--block", "ads"}); err == nil || !strings.Contains(err.Error(), `--block has no class "ads"`) {
		t.Errorf("a bad batch tab new --block: %v", err)
	}
}

func TestUsageDocumentsBlock(t *testing.T) {
	for _, want := range []string{
		"tab new <window> [url] [--block",
		"navigate <window> <url> [--wait load|domcontentloaded|none]\n                               [--block images,fonts,media|none]",
		"images whose URLs have no file extension",
	} {
		if !strings.Contains(Usage, want) {
			t.Errorf("the usage lacks %q", want)
		}
	}
}
