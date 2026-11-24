package page

// Offline tests of asset blocking (bladebro-ideas ticket 28) over a
// cdptest fake: one Network.setBlockedURLs with urlPatterns on the Tab's
// session, no domain turned on, nothing paused. No browser runs.

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
	// Nothing pauses and no domain is turned on: no Fetch, no enable.
	for _, m := range f.srv.Received() {
		if strings.HasPrefix(m.Method, "Fetch.") || strings.HasSuffix(m.Method, ".enable") {
			t.Errorf("%s was sent", m.Method)
		}
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
