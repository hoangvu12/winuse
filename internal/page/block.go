package page

// Asset blocking (bladebro-ideas ticket 28, tickets 10 and 18): a Tab winuse
// opened can stop loading images, fonts and media through
// Network.setBlockedURLs with urlPatterns (URLPattern syntax, the form
// Chrome 155 takes). It pauses nothing, so nothing can hang; it works
// through the extension too; and it ends when the session detaches, so a
// helper that died leaves nothing behind. It matches by file extension, so
// an image whose URL has none still loads.

import (
	"context"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// assetExtensions are each class's file extensions.
var assetExtensions = map[string][]string{
	"images": {"png", "jpg", "jpeg", "gif", "webp", "avif", "svg", "ico"},
	"fonts":  {"woff", "woff2", "ttf", "otf"},
	"media":  {"mp4", "webm", "mp3", "ogg", "m4a"},
}

// BlockPatterns are the URL patterns that block the classes' files.
func BlockPatterns(classes []string) []string {
	out := []string{}
	for _, c := range classes {
		for _, ext := range assetExtensions[c] {
			out = append(out, "*://*:*/*."+ext)
		}
	}
	return out
}

// BlockAssets makes a Tab's session block the classes' files from now on,
// replacing what it blocked before; no classes block nothing.
func BlockAssets(ctx context.Context, c *cdp.Client, session string, classes []string) error {
	patterns := []map[string]any{}
	for _, p := range BlockPatterns(classes) {
		patterns = append(patterns, map[string]any{"urlPattern": p, "block": true})
	}
	_, err := c.CallSession(ctx, session, "Network.setBlockedURLs", map[string]any{"urlPatterns": patterns})
	return err
}
