package page

// Downloads (browser merge ticket 06): on a browser winuse launched or
// cloned, files the Page downloads go into one folder winuse names, and
// winuse follows each one to its end.
//
// Browser.setDownloadBehavior {behavior: allowAndName, downloadPath,
// eventsEnabled} saves every download under its GUID in that folder and
// reports Browser.downloadWillBegin (the URL and the suggested file name)
// and Browser.downloadProgress (received bytes, then completed or
// canceled). When one completes, winuse renames the GUID file to the
// suggested name (" (2)" and so on when the name is taken), so the path it
// reports is the file a person would expect. allowAndName rather than
// allow: the events carry no final path for allow, and the browser's own
// renaming rules would be guessed.
//
// Browser.* is browser-level, which the winuse extension cannot send
// (ticket 02), and a browser reached through its inspect toggle or a port
// is the user's own, whose download settings winuse leaves alone.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// Download is one file the Page downloads.
type Download struct {
	GUID     string
	URL      string
	Name     string // the suggested file name
	State    string // inProgress, completed or canceled
	Received float64
	Total    float64
	Path     string // the file, once completed and renamed
	Err      string // why the rename failed (the file keeps its GUID name)
	seq      int
	reported bool
}

// Downloads follows one connection's downloads into Dir.
type Downloads struct {
	Dir string

	c    *cdp.Client
	subs []*cdp.Subscription

	mu      sync.Mutex
	items   []*Download
	byGUID  map[string]*Download
	seq     int
	changed chan struct{}
}

var downloads = struct {
	mu sync.Mutex
	m  map[*cdp.Client]*Downloads
}{m: map[*cdp.Client]*Downloads{}}

// EnableDownloads sends Browser.setDownloadBehavior into dir (created when
// missing) once per connection and starts following its download events.
func EnableDownloads(ctx context.Context, c *cdp.Client, dir string) (*Downloads, error) {
	downloads.mu.Lock()
	for k, d := range downloads.m {
		if !k.Alive() {
			delete(downloads.m, k)
			d.close()
		}
	}
	if d := downloads.m[c]; d != nil && d.Dir == dir {
		downloads.mu.Unlock()
		return d, nil
	}
	downloads.mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	d := &Downloads{Dir: dir, c: c, byGUID: map[string]*Download{}, changed: make(chan struct{})}
	// One subscription for every browser-level event, so a download's
	// progress is never applied before its beginning; apply ignores the
	// rest.
	all := c.Subscribe("", "")
	d.subs = []*cdp.Subscription{all}
	if _, err := c.Call(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "allowAndName", "downloadPath": dir, "eventsEnabled": true}); err != nil {
		d.close()
		return nil, err
	}
	go d.follow(all)
	downloads.mu.Lock()
	if old := downloads.m[c]; old != nil {
		old.close()
	}
	downloads.m[c] = d
	downloads.mu.Unlock()
	return d, nil
}

// DownloadsOf is the connection's tracker, nil when downloads were never
// enabled on it.
func DownloadsOf(c *cdp.Client) *Downloads {
	downloads.mu.Lock()
	defer downloads.mu.Unlock()
	return downloads.m[c]
}

func (d *Downloads) close() {
	for _, s := range d.subs {
		s.Close()
	}
}

func (d *Downloads) follow(s *cdp.Subscription) {
	for ev := range s.Events() {
		d.apply(ev)
	}
}

// apply records one download event; a completed download is renamed to
// its suggested name.
func (d *Downloads) apply(ev cdp.Event) {
	var p struct {
		GUID              string  `json:"guid"`
		URL               string  `json:"url"`
		SuggestedFilename string  `json:"suggestedFilename"`
		ReceivedBytes     float64 `json:"receivedBytes"`
		TotalBytes        float64 `json:"totalBytes"`
		State             string  `json:"state"`
	}
	if json.Unmarshal(ev.Params, &p) != nil || p.GUID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	it := d.byGUID[p.GUID]
	switch ev.Method {
	case "Browser.downloadWillBegin":
		if it != nil {
			return
		}
		d.seq++
		it = &Download{GUID: p.GUID, URL: p.URL, Name: p.SuggestedFilename, State: "inProgress", seq: d.seq}
		d.items = append(d.items, it)
		d.byGUID[p.GUID] = it
	case "Browser.downloadProgress":
		if it == nil || it.State != "inProgress" {
			return
		}
		it.Received, it.Total = p.ReceivedBytes, p.TotalBytes
		switch p.State {
		case "completed":
			it.State = "completed"
			it.Path, it.Err = d.rename(it)
		case "canceled":
			it.State = "canceled"
		}
	default:
		return
	}
	close(d.changed)
	d.changed = make(chan struct{})
}

// rename moves a completed GUID file to its suggested name in Dir.
func (d *Downloads) rename(it *Download) (string, string) {
	src := filepath.Join(d.Dir, it.GUID)
	name := SafeFileName(it.Name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n < 1000; n++ {
		dst := filepath.Join(d.Dir, name)
		if n > 1 {
			dst = filepath.Join(d.Dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return src, err.Error()
		}
		return dst, ""
	}
	return src, "every name like " + name + " is taken"
}

// SafeFileName is a suggested file name made safe for Windows: no folder
// part, no reserved characters, never empty.
func SafeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "/", `\`))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" || name == "." || name == `\` {
		return "download"
	}
	return name
}

// Seq is the number of downloads begun so far.
func (d *Downloads) Seq() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seq
}

// Changed is closed at the next download event (nil for no tracker).
func (d *Downloads) Changed() <-chan struct{} {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.changed
}

// Since lists the downloads begun after seq.
func (d *Downloads) Since(seq int) []Download {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []Download
	for _, it := range d.items {
		if it.seq > seq {
			out = append(out, *it)
		}
	}
	return out
}

// ErrDownloadCanceled is a download the browser or the page canceled.
var ErrDownloadCanceled = errors.New("the download was canceled")

// DownloadTimeoutError is a wait for a download that ran out of time.
type DownloadTimeoutError struct {
	Started  bool // a download began (and is still running)
	Name     string
	Received float64
	After    time.Duration
}

func (e *DownloadTimeoutError) Error() string {
	after := e.After.Round(100 * time.Millisecond)
	if !e.Started {
		return fmt.Sprintf("no download began within %s", after)
	}
	return fmt.Sprintf("the download of %q was still running after %s (%s received)", e.Name, after, byteCount(e.Received))
}

func byteCount(n float64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", n/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", n/(1<<10))
	}
	return fmt.Sprintf("%.0f bytes", n)
}

// Wait waits for the oldest download not reported yet to finish, or for
// one to begin and finish, and marks it reported. It stops at the Esc stop
// (ErrStopped) and at ctx's end (*DownloadTimeoutError).
func (d *Downloads) Wait(ctx context.Context, stopped func() bool) (Download, error) {
	start := time.Now()
	for {
		d.mu.Lock()
		var it *Download
		for _, x := range d.items {
			if !x.reported {
				it = x
				break
			}
		}
		changed := d.changed
		var done *Download
		if it != nil && it.State != "inProgress" {
			it.reported = true
			c := *it
			done = &c
		}
		var pending Download
		if it != nil {
			pending = *it
		}
		d.mu.Unlock()
		if done != nil {
			if done.State == "canceled" {
				return *done, ErrDownloadCanceled
			}
			return *done, nil
		}
		if stopped != nil && stopped() {
			return Download{}, ErrStopped
		}
		select {
		case <-changed:
		case <-time.After(pollEvery):
		case <-ctx.Done():
			return pending, &DownloadTimeoutError{Started: it != nil, Name: pending.Name, Received: pending.Received, After: time.Since(start)}
		}
	}
}
