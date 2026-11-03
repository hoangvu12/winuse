// Package shot models screenshots: the captured frames, the model-friendly
// scaled image, and — the part every later ticket reuses — the coordinate
// mapping between a screenshot's pixels and the Window it shows.
//
// The mapping is deliberately pure data plus pure functions with no Win32
// dependency, so it is offline-testable (spec, Testing Decisions) and
// reusable: the Windows.Graphics.Capture path (ticket 09) and ticket 10's
// pointer actions (scroll, drag, hover) map through the same functions.
//
// # Coordinate spaces
//
// All coordinates are physical pixels — the helper process is
// per-monitor-v2 DPI aware, so no logical pixels exist anywhere in the
// pipeline (Codex's 175%-scaling offset bug never gets a chance):
//
//   - screenshot space: pixels of the saved, scaled image, (0,0) at the
//     top-left. `@x,y` Element References and zoom regions live here.
//   - window space: pixels relative to the Window's extended frame bounds
//     top-left (DWMWA_EXTENDED_FRAME_BOUNDS — the visible extent, without
//     the shadow or the invisible resize borders). Mappings and frames
//     anchor here.
//   - screen space: absolute pixels of the virtual desktop:
//     window space + the Window's extended frame bounds top-left, read
//     fresh at Action time.
//
// Window space is the pivot. A screenshot records where its pixels sat
// relative to the Window, not to the screen, so a `@x,y` click survives
// the Window moving between the screenshot and the click (the anchor is
// re-read then); and at any display scaling, on any monitor, the mapping
// is exact because every space is physical.
package shot

import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
)

// MaxLongEdge is the model-friendly long-edge cap for screenshots
// (orchestrator decision): the saved image's long edge is at most 1568 px.
const MaxLongEdge = 1568

// Mapping is the coordinate mapping of one screenshot: how its pixels
// correspond to the Window's physical pixels. The helper records the
// latest screenshot's Mapping per Window (Store); `@x,y` clicks and zoom
// regions resolve through it.
type Mapping struct {
	Scale  float64 // screenshot px per window px; (0, 1]
	Width  int     // screenshot width in px
	Height int     // screenshot height in px

	// Coverage is the screenshot's window-space rectangle. Coverage.Min is
	// the window-space position of screenshot pixel (0,0) (zero for a
	// plain window capture; negative when the capture was widened with
	// popups that extend past the Window's edge).
	Coverage image.Rectangle
}

// FitScale returns the scale that fits a w×h physical capture into
// MaxLongEdge, never upscaling: a Window already within the cap stays 1:1.
func FitScale(w, h int) float64 {
	long := w
	if h > long {
		long = h
	}
	if long <= MaxLongEdge {
		return 1
	}
	return float64(MaxLongEdge) / float64(long)
}

// MappingFor builds the Mapping of a frame captured at scale. Width and
// Height are the rounded scaled extents — exactly what ScaleImage produces
// for the same scale, so the Mapping describes the saved image.
func MappingFor(f Frame, scale float64) Mapping {
	if scale <= 0 || scale > 1 {
		scale = 1
	}
	return Mapping{
		Scale:    scale,
		Width:    scaledSize(f.Rect.Dx(), scale),
		Height:   scaledSize(f.Rect.Dy(), scale),
		Coverage: f.Rect.Sub(f.Anchor),
	}
}

// scaledSize rounds a physical extent to its scaled extent, always at
// least one pixel so a non-empty capture never maps to an empty image.
func scaledSize(v int, scale float64) int {
	if v <= 0 {
		return 0
	}
	n := int(math.Round(float64(v) * scale))
	if n < 1 {
		n = 1
	}
	return n
}

// WindowPoint maps a screenshot pixel to window space: the physical pixel
// aimed at is the centre of the screenshot pixel's footprint
// (pixel-centre rounding), so the mapping is exact at scale 1 and lands
// within half a footprint of the aim at any scale.
func (m Mapping) WindowPoint(x, y int) image.Point {
	return image.Point{
		X: m.Coverage.Min.X + footprintCentre(x, m.Scale),
		Y: m.Coverage.Min.Y + footprintCentre(y, m.Scale),
	}
}

// footprintCentre returns the window-space centre of the footprint of
// screenshot pixel v: floor((v+0.5)/scale). At scale 1 that is v itself.
func footprintCentre(v int, scale float64) int {
	return int(math.Floor((float64(v) + 0.5) / scale))
}

// WindowRect maps a screenshot-space rectangle to window space, rounded
// outward so the window-space rectangle covers at least the whole
// screenshot region: a zoom crop never loses an edge pixel.
func (m Mapping) WindowRect(x, y, w, h int) image.Rectangle {
	return image.Rectangle{
		Min: image.Point{
			X: m.Coverage.Min.X + outwardLow(x, m.Scale),
			Y: m.Coverage.Min.Y + outwardLow(y, m.Scale),
		},
		Max: image.Point{
			X: m.Coverage.Min.X + outwardHigh(x+w, m.Scale),
			Y: m.Coverage.Min.Y + outwardHigh(y+h, m.Scale),
		},
	}
}

func outwardLow(v int, scale float64) int  { return int(math.Floor(float64(v) / scale)) }
func outwardHigh(v int, scale float64) int { return int(math.Ceil(float64(v) / scale)) }

// Contains reports whether (x, y) is a pixel of the screenshot: `@x,y`
// references must aim inside the image the agent actually saw.
func (m Mapping) Contains(x, y int) bool {
	return x >= 0 && y >= 0 && x < m.Width && y < m.Height
}

// Store keeps each Agent's latest screenshot Mapping of each Window.
// `@x,y` references and zoom regions are defined in the latest
// screenshot's coordinate space (spec), so the helper records one Mapping
// per Agent and Window, and every new screenshot that Agent takes of the
// Window replaces it (multi-agent ticket 03: another Agent's screenshot
// never moves this Agent's points). Requests that name no Agent share the
// default Agent's. Like the live-Element store, but pure data: no
// handles, no COM, safe to construct and inspect in offline tests.
//
// Memory (ADR 0014, Consequences): an Agent other than the default one
// idle past Idle loses its Mappings, and at most maxMappings are kept,
// the least recently recorded dropped first.
type Store struct {
	mu   sync.Mutex
	maps map[storeKey]record
	used map[string]time.Time // each Agent's last use

	Now  func() time.Time // the clock; nil is time.Now
	Idle time.Duration    // an Agent idle this long loses its Mappings; 0 never
}

// storeKey is one Agent's Window.
type storeKey struct {
	agent string
	hwnd  uint64
}

// record is one stored Mapping and when it was recorded.
type record struct {
	m  Mapping
	at time.Time
}

// maxMappings caps the Mappings kept across Agents.
const maxMappings = 256

// defaultIdle is how long an Agent may stay idle before its Mappings go:
// the helper's default idle period.
const defaultIdle = 5 * time.Minute

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{maps: map[storeKey]record{}, used: map[string]time.Time{}, Idle: defaultIdle}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Record remembers m as the Mapping of the Agent's latest screenshot of
// hwnd.
func (s *Store) Record(agentID string, hwnd uint64, m Mapping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	key := storeKey{agentID, hwnd}
	s.maps[key] = record{m: m, at: now}
	s.used[agentID] = now
	if s.Idle > 0 {
		cutoff := now.Add(-s.Idle)
		for a, at := range s.used {
			if a == agent.DefaultID || a == agentID || !at.Before(cutoff) {
				continue
			}
			for k := range s.maps {
				if k.agent == a {
					delete(s.maps, k)
				}
			}
			delete(s.used, a)
		}
	}
	for len(s.maps) > maxMappings {
		var oldest storeKey
		var oldestAt time.Time
		found := false
		for k, r := range s.maps {
			if k != key && (!found || r.at.Before(oldestAt)) {
				oldest, oldestAt, found = k, r.at, true
			}
		}
		if !found {
			break
		}
		delete(s.maps, oldest)
	}
}

// Lookup returns the Mapping of the Agent's latest screenshot of hwnd,
// and whether that Agent recorded one in this helper.
func (s *Store) Lookup(agentID string, hwnd uint64) (Mapping, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.maps[storeKey{agentID, hwnd}]
	s.used[agentID] = s.now()
	return r.m, ok
}
