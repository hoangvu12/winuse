// Package apps builds winuse's App catalog and launches Apps.
//
// An App is an installed or running program — never the target of an
// Action; Actions target Windows. The catalog answers `winuse list-apps`:
// running Apps with their Windows plus recently used Apps marked not
// running. `launch-app` starts an App by name or by identifier
// (executable path, shortcut path, App User Model ID) and reports the
// Window it opened.
//
// The catalog is assembled from best-effort sources (window enumeration,
// the UserAssist registry, Start-menu shortcuts, the Start-apps listing);
// assembly, name matching and output formatting are pure functions so they
// are tested offline. Win32 launching lives in launch.go.
package apps

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// IDKind says how an App is launched.
type IDKind int

const (
	// IDExe: an executable path, started directly.
	IDExe IDKind = iota
	// IDShortcut: a shortcut (.lnk) path, started through the shell.
	IDShortcut
	// IDAppUserModelID: an App User Model ID (packaged/Store apps),
	// started through the shell's Apps folder.
	IDAppUserModelID
)

// MaxRecentApps bounds how many not-running recently used Apps list-apps
// prints (running Apps are all listed). The UserAssist key tracks every
// executable ever launched through Explorer; without a bound the listing
// would bury the Apps an agent cares about. Codex caps list_apps at 40
// entries (a bug); this cap is explicit and only drops the tail of the
// recency-ordered recent list.
const MaxRecentApps = 30

// App is one App of the catalog. ID is the canonical launch identifier —
// exactly what launch-app accepts: an executable path (running and
// exe-sourced Apps), a shortcut path, or an App User Model ID.
type App struct {
	Name     string
	ID       string
	Kind     IDKind
	Running  bool
	Windows  []win.WindowInfo
	LastUsed time.Time // zero when unknown
	UseCount int

	// TargetExe is the executable behind the App when known (a shortcut's
	// target, a running packaged App's exe). It merges catalog entries and
	// recognizes the Window a launch opens; it is not printed.
	TargetExe string
}

// Exe returns the executable path behind the App ("" when unknown).
func (a App) Exe() string {
	if a.Kind == IDExe {
		return a.ID
	}
	return a.TargetExe
}

// Sources are the catalog inputs, gathered by the caller (all real-world
// access lives there so everything in this package stays pure).
type Sources struct {
	Running   []App            // Apps with visible Windows
	Recent    []App            // recently used Apps (UserAssist)
	StartMenu []StartMenuEntry // installed shortcuts (launch resolution only)
	Store     []StoreApp       // packaged apps (Get-StartApps)
}

// RunningFromWindows groups visible Windows into running Apps: one App per
// executable path, Windows in z-order, the App name and identifier taken
// from the first (topmost) Window. Windows whose process path cannot be
// resolved are dropped — they have no launchable identity.
func RunningFromWindows(ws []win.WindowInfo) []App {
	var out []App
	byExe := map[string]int{} // lower-cased exe path -> index into out
	for _, w := range ws {
		if w.ExePath == "" {
			continue
		}
		key := strings.ToLower(w.ExePath)
		if i, ok := byExe[key]; ok {
			out[i].Windows = append(out[i].Windows, w)
			continue
		}
		byExe[key] = len(out)
		out = append(out, App{
			Name:    w.App,
			ID:      w.ExePath,
			Kind:    IDExe,
			Running: true,
			Windows: []win.WindowInfo{w},
		})
	}
	return out
}

// Catalog returns the Apps list-apps prints: every running App with its
// Windows (in z-order), then recently used Apps marked not running, most
// recent first, capped at MaxRecentApps. Entries describing the same App
// are merged: usage statistics attach to running Apps, and a running
// packaged App carries its App User Model ID (launching a packaged App's
// exe directly does not work).
func Catalog(s Sources) []App {
	out, recents := merge(s)
	if len(recents) > MaxRecentApps {
		recents = recents[:MaxRecentApps]
	}
	return append(out, recents...)
}

// Candidates returns every launchable identity from the sources, for
// launch-app name resolution: the merged catalog, all Start-menu
// shortcuts and all packaged Apps. Unlike Catalog this is uncapped and
// unfiltered — any installed App resolves by name even when it is not
// printed by list-apps.
func (s Sources) Candidates() []Candidate {
	out := []Candidate{}
	for _, a := range mergeApps(s) {
		out = append(out, Candidate{Name: a.Name, ID: a.ID, Kind: a.Kind, TargetExe: a.TargetExe})
	}
	for _, m := range s.StartMenu {
		out = append(out, Candidate{Name: m.Name, ID: m.Path, Kind: IDShortcut, TargetExe: m.TargetExe})
	}
	for _, st := range s.Store {
		out = append(out, Candidate{Name: st.Name, ID: st.AppID, Kind: IDAppUserModelID})
	}
	return out
}

// merge combines running, recent and store Apps into one list: running
// Apps (with store identifiers adopted and usage merged) followed by
// unmatched recents sorted most-recent-first. Nameless entries are
// dropped: they cannot be matched or recognized.
func merge(s Sources) (running []App, recents []App) {
	running = make([]App, len(s.Running))
	copy(running, s.Running)
	recents = make([]App, len(s.Recent))
	copy(recents, s.Recent)

	// Packaged apps: a running App whose exe lives in WindowsApps adopts
	// the App User Model ID as its launch identifier (a packaged App's exe
	// is not directly launchable); a recent AUMID entry gets the store's
	// name.
	for _, st := range s.Store {
		for i := range running {
			if running[i].Kind == IDExe && isPackagedExe(running[i].ID) && ciEqual(running[i].Name, st.Name) {
				running[i].TargetExe = running[i].ID
				running[i].ID = st.AppID
				running[i].Kind = IDAppUserModelID
				break
			}
		}
		for i := range recents {
			if recents[i].Kind == IDAppUserModelID && ciEqual(recents[i].ID, st.AppID) {
				recents[i].Name = st.Name
				break
			}
		}
	}

	kept := make([]App, 0, len(recents))
	for _, rec := range recents {
		if mergeUsage(running, rec) {
			continue
		}
		if rec.Name == "" {
			continue // nameless AUMID entry the store could not name
		}
		kept = append(kept, rec)
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].LastUsed.After(kept[j].LastUsed) })
	return running, kept
}

// mergeApps is merge as a single slice (Candidates' input).
func mergeApps(s Sources) []App {
	running, recents := merge(s)
	return append(running, recents...)
}

// mergeUsage attaches a recent entry's usage statistics to the running App
// it describes, returning whether it matched. Shortcut entries match by
// their resolved target or by their own path; exe entries by their path;
// AUMID entries by their identifier.
func mergeUsage(running []App, rec App) bool {
	for i := range running {
		switch rec.Kind {
		case IDExe:
			if ciEqual(running[i].Exe(), rec.ID) {
				running[i].LastUsed, running[i].UseCount = rec.LastUsed, rec.UseCount
				return true
			}
		case IDShortcut:
			if (rec.TargetExe != "" && ciEqual(running[i].Exe(), rec.TargetExe)) || ciEqual(running[i].ID, rec.ID) {
				running[i].LastUsed, running[i].UseCount = rec.LastUsed, rec.UseCount
				return true
			}
		case IDAppUserModelID:
			if ciEqual(running[i].ID, rec.ID) {
				running[i].LastUsed, running[i].UseCount = rec.LastUsed, rec.UseCount
				return true
			}
		}
	}
	return false
}

// FormatApps renders the list-apps output: one line per App,
//
//	<App name>\t<state>\t<last used>\t<use count>\t<launch identifier>
//
// followed by one line per visible Window of a running App,
//
//	\t<Window ID>\t<title>
//
// Window lines start with a tab so an agent can tell them from App lines.
// The state is `running` or `not running`; last used is ISO 8601 or `-`
// when unknown. Tab-separated (split on "\t") so lines parse back
// unambiguously. Empty input prints nothing.
func FormatApps(apps []App) string {
	var b strings.Builder
	for _, a := range apps {
		state := "not running"
		if a.Running {
			state = "running"
		}
		lastUsed := "-"
		if !a.LastUsed.IsZero() {
			lastUsed = a.LastUsed.Format(time.RFC3339)
		}
		b.WriteString(a.Name)
		b.WriteByte('\t')
		b.WriteString(state)
		b.WriteByte('\t')
		b.WriteString(lastUsed)
		b.WriteByte('\t')
		b.WriteString(strconv.Itoa(a.UseCount))
		b.WriteByte('\t')
		b.WriteString(a.ID)
		b.WriteByte('\n')
		for _, w := range a.Windows {
			b.WriteByte('\t')
			b.WriteString(strconv.FormatUint(w.ID, 10))
			b.WriteByte('\t')
			b.WriteString(w.Title)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ciEqual compares two strings case-insensitively.
func ciEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

// hasSuffixFold reports a case-insensitive path suffix match.
func hasSuffixFold(s, suffix string) bool {
	return strings.HasSuffix(strings.ToLower(s), strings.ToLower(suffix))
}

// trimExt strips a file name's extension ("app.EXE" -> "app"); a name that
// is all extension keeps itself.
func trimExt(base string) string {
	ext := filepath.Ext(base)
	if len(ext) < len(base) {
		return base[:len(base)-len(ext)]
	}
	return base
}

// Candidate is one launchable identity name resolution matches against.
type Candidate struct {
	Name      string
	ID        string
	Kind      IDKind
	TargetExe string // the exe behind the identity, "" when unknown
}

// ResolveError kinds.
const (
	// ErrAppNotFound: no App matches the requested name.
	ErrAppNotFound = "not-found"
	// ErrAmbiguousApp: several distinct Apps match the requested name.
	ErrAmbiguousApp = "ambiguous"
)

// ResolveError is a launch-app resolution failure in Codex's error style.
type ResolveError struct {
	Kind    string   // ErrAppNotFound or ErrAmbiguousApp
	App     string   // what was requested
	Matches []string // the matching Apps' names or identifiers
}

func (e *ResolveError) Error() string {
	if e.Kind == ErrAmbiguousApp {
		return fmt.Sprintf("app identifier %q is ambiguous; use the identifier shown by winuse list-apps; matches: %s",
			e.App, strings.Join(e.Matches, ", "))
	}
	return "app not found: " + e.App
}

// Spec says how to start one App.
type Spec struct {
	App       string // what the user requested (used in messages)
	ID        string // the launch identifier
	Kind      IDKind
	TargetExe string // the executable whose Windows belong to the App, "" when unknown
}

// LooksLikePath reports whether an argument is an explicit path rather
// than an App name: path separators, a drive prefix or a .lnk suffix.
func LooksLikePath(arg string) bool {
	if strings.ContainsAny(arg, `\/`) {
		return true
	}
	if len(arg) >= 2 && arg[1] == ':' {
		return true
	}
	return hasSuffixFold(arg, ".lnk")
}

// LooksLikeIdentifier reports whether an argument is an explicit launch
// identifier (path or App User Model ID) and needs no catalog lookup.
func LooksLikeIdentifier(arg string) bool {
	return LooksLikePath(arg) || isAUMID(arg)
}

func isAUMID(arg string) bool {
	return strings.Contains(arg, "!") && !strings.ContainsAny(arg, `\/`)
}

// Resolve decides how to launch the requested App: an explicit identifier
// (path or AUMID) is taken as-is; a name is matched case-insensitively
// against the candidates' display names and aliases (executable base
// names, shortcut names), with distinct Apps sharing an executable merged.
// Zero matches and several distinct matches are errors, never guesses.
func Resolve(app string, cands []Candidate) (Spec, error) {
	app = strings.TrimSpace(app)
	if app == "" {
		return Spec{}, &ResolveError{Kind: ErrAppNotFound, App: app}
	}
	if isAUMID(app) {
		return Spec{App: app, ID: app, Kind: IDAppUserModelID}, nil
	}
	if LooksLikePath(app) {
		kind := IDExe
		if hasSuffixFold(app, ".lnk") {
			kind = IDShortcut
		}
		spec := Spec{App: app, ID: app, Kind: kind}
		if kind == IDExe {
			spec.TargetExe = app // the path's Windows are the App's
		}
		return spec, nil
	}
	matches := matchCandidates(cands, app)
	switch len(matches) {
	case 0:
		return Spec{}, &ResolveError{Kind: ErrAppNotFound, App: app}
	case 1:
		rep := matches[0]
		spec := Spec{App: app, ID: rep.ID, Kind: rep.Kind, TargetExe: rep.TargetExe}
		if rep.Kind == IDExe {
			spec.TargetExe = rep.ID
		}
		return spec, nil
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, displayOf(m))
		}
		return Spec{}, &ResolveError{Kind: ErrAmbiguousApp, App: app, Matches: names}
	}
}

// displayOf names a candidate for ambiguity messages.
func displayOf(c Candidate) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

// matchCandidates finds candidates matching a name and folds them into
// distinct Apps. Several catalog entries routinely describe one App (the
// running App, its Start-menu shortcut, its UserAssist entry); they are
// deduplicated by launch identity (same executable, or same identifier)
// so naming such an App is not an ambiguity.

// group is one distinct App during name-resolution folding: a chosen
// representative plus every identity (identifiers and executables) seen
// for it.
type group struct {
	rep  Candidate // the representative: shortcut > exe > AUMID
	ids  map[string]bool
	exes map[string]bool
}

// matchCandidates finds candidates matching a name and folds them into
// distinct Apps.
func matchCandidates(cands []Candidate, name string) []Candidate {
	var matched []Candidate
	for _, c := range cands {
		if candidateMatches(c, name) {
			matched = append(matched, c)
		}
	}

	// Several entries routinely describe one App (the running App, its
	// Start-menu shortcut, its UserAssist entry). Folding is a fixed point:
	// merging two groups can make a third share an identity with the result.
	groups := make([]*group, 0, len(matched))
	for _, c := range matched {
		groups = append(groups, newGroup(c))
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(groups) && !changed; i++ {
			for j := i + 1; j < len(groups); j++ {
				if groups[j] == nil {
					continue
				}
				if groups[i] == nil {
					groups[i], groups[j], changed = groups[j], nil, true
					break
				}
				if shareIdentity(groups[i], groups[j]) {
					groups[i] = foldGroups(groups[i], groups[j])
					groups[j] = nil
					changed = true
					break
				}
			}
		}
	}
	live := make([]*group, 0, len(groups))
	for _, g := range groups {
		if g != nil {
			live = append(live, g)
		}
	}

	// Fold a store (AUMID) group with the one other group whose executable
	// lives in WindowsApps: they are the same packaged App seen by two
	// sources (its running process and the Get-StartApps entry) — even
	// when the process's display name differs from the store's.
	if len(live) == 2 {
		a, b := live[0], live[1]
		if (a.rep.Kind == IDAppUserModelID) != (b.rep.Kind == IDAppUserModelID) {
			if b.rep.Kind == IDAppUserModelID {
				a, b = b, a
			}
			if isPackagedExe(candidateExe(b.rep)) {
				a.rep.TargetExe = candidateExe(b.rep) // running exe known: recognize its Windows
				live = []*group{a}
			}
		}
	}

	out := make([]Candidate, 0, len(live))
	for _, g := range live {
		out = append(out, g.rep)
	}
	return out
}

// newGroup starts a folding group for one candidate.
func newGroup(c Candidate) *group {
	g := &group{rep: c, ids: map[string]bool{strings.ToLower(c.ID): true}, exes: map[string]bool{}}
	if exe := candidateExe(c); exe != "" {
		g.exes[exe] = true
	}
	return g
}

// shareIdentity reports whether two groups launch the same App: shared
// executable or shared identifier.
func shareIdentity(a, b *group) bool {
	for exe := range a.exes {
		if b.exes[exe] {
			return true
		}
	}
	for id := range a.ids {
		if b.ids[id] {
			return true
		}
	}
	return false
}

// foldGroups merges b into a, keeping the better representative
// (shortcut first, then executable, then AUMID) and the merged identities.
func foldGroups(a, b *group) *group {
	rep := a.rep
	if kindRank(b.rep.Kind) < kindRank(a.rep.Kind) {
		rep = b.rep
	}
	target := rep.TargetExe
	if target == "" {
		for exe := range b.exes {
			target = exe
			break
		}
	}
	if target == "" {
		for exe := range a.exes {
			target = exe
			break
		}
	}
	merged := &group{rep: Candidate{Name: rep.Name, ID: rep.ID, Kind: rep.Kind, TargetExe: target},
		ids: map[string]bool{}, exes: map[string]bool{}}
	for _, g := range []*group{a, b} {
		for id := range g.ids {
			merged.ids[id] = true
		}
		for exe := range g.exes {
			merged.exes[exe] = true
		}
	}
	return merged
}

func kindRank(k IDKind) int {
	switch k {
	case IDShortcut:
		return 0
	case IDExe:
		return 1
	default:
		return 2
	}
}

// isPackagedExe reports whether an executable path belongs to a packaged
// (Store) App: those live under the WindowsApps folder, whose path embeds
// the package identity.
func isPackagedExe(exe string) bool {
	return strings.Contains(strings.ToLower(exe), `\windowsapps\`)
}

// candidateExe returns the executable a candidate's Windows belong to.
func candidateExe(c Candidate) string {
	if c.Kind == IDExe {
		return strings.ToLower(c.ID)
	}
	return strings.ToLower(c.TargetExe)
}

// candidateMatches reports whether a name (already trimmed) equals one of
// the candidate's names or aliases, case-insensitively.
func candidateMatches(c Candidate, name string) bool {
	want := strings.ToLower(name)
	for _, alias := range aliasesOf(c) {
		if alias == want {
			return true
		}
	}
	return false
}

// aliasesOf lists a candidate's matchable names, lower-cased: the display
// name, the executable's base name with and without extension (running
// Apps, resolved targets) and the shortcut's file name.
func aliasesOf(c Candidate) []string {
	var out []string
	add := func(v string) {
		if v != "" {
			out = append(out, strings.ToLower(v))
		}
	}
	add(c.Name)
	exes := []string{}
	if c.Kind == IDExe {
		exes = append(exes, c.ID)
	} else if c.TargetExe != "" {
		exes = append(exes, c.TargetExe)
	}
	for _, exe := range exes {
		base := filepath.Base(exe)
		add(base)
		add(trimExt(base))
	}
	if c.Kind == IDShortcut {
		add(trimExt(filepath.Base(c.ID)))
	}
	return out
}
