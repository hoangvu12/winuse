package page

// Offline tests of asset blocking (bladebro-ideas ticket 28) over a
// cdptest fake: Network.enable and one Network.setBlockedURLs with
// urlPatterns on the Tab's session (Network.disable after --block none),
// nothing paused. No browser runs.

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// blockedPatterns are the urlPatterns of every Network.setBlockedURLs the
// fake received, by session, in order.
func blockedPatterns(t *testing.T, f *fakeTab) (sessions []string, sets [][]string) {
	t.Helper()
	for _, m := range f.srv.Received() {
		if m.Method != "Network.setBlockedURLs" {
			continue
		}
		var p struct {
			URLs        []string `json:"urls"`
			URLPatterns []struct {
				URLPattern string `json:"urlPattern"`
				Block      bool   `json:"block"`
			} `json:"urlPatterns"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.URLs) != 0 {
			t.Errorf("the old urls form was sent: %v", p.URLs)
		}
		if !strings.Contains(string(m.Params), `"urlPatterns":[`) {
			t.Errorf("no urlPatterns array: %s", m.Params)
		}
		set := []string{}
		for _, u := range p.URLPatterns {
			if !u.Block {
				t.Errorf("%s does not block", u.URLPattern)
			}
			set = append(set, u.URLPattern)
		}
		sessions = append(sessions, m.SessionID)
		sets = append(sets, set)
	}
	return sessions, sets
}

func TestBlockAssetsSendsURLPatterns(t *testing.T) {
	f := newFakeTab(t)
	if err := BlockAssets(context.Background(), f.c, "S1", []string{"images", "fonts", "media"}); err != nil {
		t.Fatal(err)
	}
	sessions, sets := blockedPatterns(t, f)
	want := []string{
		"*://*:*/*.png", "*://*:*/*.jpg", "*://*:*/*.jpeg", "*://*:*/*.gif", "*://*:*/*.webp", "*://*:*/*.avif", "*://*:*/*.svg", "*://*:*/*.ico",
		"*://*:*/*.woff", "*://*:*/*.woff2", "*://*:*/*.ttf", "*://*:*/*.otf",
		"*://*:*/*.mp4", "*://*:*/*.webm", "*://*:*/*.mp3", "*://*:*/*.ogg", "*://*:*/*.m4a",
	}
	if len(sets) != 1 || sessions[0] != "S1" || !reflect.DeepEqual(sets[0], want) {
		t.Errorf("sent %v on %v, want %v on S1", sets, sessions, want)
	}
	// Network on in that session first (the block needs it), then the
	// patterns; nothing pauses (no Fetch) and no other domain goes on.
	if got := sessionMethods(f); strings.Join(got, " ") != "S1 Network.enable S1 Network.setBlockedURLs" {
		t.Errorf("sent %v", got)
	}
}

// sessionMethods are the fake's requests as "<session> <method>", in order.
func sessionMethods(f *fakeTab) []string {
	var out []string
	for _, m := range f.srv.Received() {
		out = append(out, m.SessionID+" "+m.Method)
	}
	return out
}

// --block none empties the list, then turns Network off again.
func TestBlockAssetsNoneTurnsNetworkOff(t *testing.T) {
	f := newFakeTab(t)
	if err := BlockAssets(context.Background(), f.c, "S3", nil); err != nil {
		t.Fatal(err)
	}
	if got := sessionMethods(f); strings.Join(got, " ") != "S3 Network.setBlockedURLs S3 Network.disable" {
		t.Errorf("sent %v", got)
	}
}

func TestBlockAssetsPerClass(t *testing.T) {
	for _, c := range []struct {
		classes []string
		want    []string
	}{
		{[]string{"fonts"}, []string{"*://*:*/*.woff", "*://*:*/*.woff2", "*://*:*/*.ttf", "*://*:*/*.otf"}},
		{[]string{"media"}, []string{"*://*:*/*.mp4", "*://*:*/*.webm", "*://*:*/*.mp3", "*://*:*/*.ogg", "*://*:*/*.m4a"}},
		{nil, []string{}}, // --block none: the list is emptied
	} {
		f := newFakeTab(t)
		if err := BlockAssets(context.Background(), f.c, "S2", c.classes); err != nil {
			t.Fatal(err)
		}
		if _, sets := blockedPatterns(t, f); len(sets) != 1 || !reflect.DeepEqual(sets[0], c.want) {
			t.Errorf("%v: sent %v, want %v", c.classes, sets, c.want)
		}
	}
}
